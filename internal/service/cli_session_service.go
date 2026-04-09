package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	appconfig "auto-code/internal/config"
	"auto-code/internal/domain"
	"auto-code/internal/logging"
	"auto-code/internal/persistence"

	"go.uber.org/zap"
)

const (
	standaloneCLIType    = "standalone"
	standaloneCLIProfile = "manual"
)

var externalEnvRefPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// RuntimeSessionSummary is a runtime-only snapshot returned by CLI manager adapters.
type RuntimeSessionSummary struct {
	ID          string
	AgentID     string
	Command     string
	State       string
	LaunchMode  string
	WorkDir     string
	ProcessPID  int
	ProcessPGID int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExitCode    *int
	LastError   string
}

// RuntimeSession represents one running CLI runtime session.
type RuntimeSession interface {
	ID() string
	AgentID() string
	Summary() RuntimeSessionSummary
	WriteInput(text string) error
}

// RuntimeManager defines the runtime operations CLISessionService depends on.
type RuntimeManager interface {
	Create(command string) (RuntimeSession, error)
	CreateWithSize(command string, cols, rows int) (RuntimeSession, error)
	CreateWithWorkDir(command, workDir string) (RuntimeSession, error)
	CreateWithWorkDirAndEnv(command, workDir string, env map[string]string) (RuntimeSession, error)
	CreateWithWorkDirAndEnvAndSize(command, workDir string, env map[string]string, cols, rows int) (RuntimeSession, error)
	CreateWithIdentity(sessionID, agentID, command, workDir string) (RuntimeSession, error)
	CreateWithIdentityAndEnv(sessionID, agentID, command, workDir string, env map[string]string) (RuntimeSession, error)
	CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir string, env map[string]string, cols, rows int) (RuntimeSession, error)
	Destroy(sessionID string) error
	Get(sessionID string) (RuntimeSession, bool)
	List() []RuntimeSessionSummary
}

// CLISessionService orchestrates profile-driven session creation and metadata persistence.
type CLISessionService struct {
	store              *persistence.SQLiteStore
	projectService     *ProjectService
	requirementService *RequirementService
	profileRegistry    *appconfig.CLIProfileRegistry
	runtime            RuntimeManager
	appRoot            string
	ensureMu           sync.Mutex
}

// NewCLISessionService constructs a CLI session application service.
func NewCLISessionService(
	store *persistence.SQLiteStore,
	projectService *ProjectService,
	requirementService *RequirementService,
	profileRegistry *appconfig.CLIProfileRegistry,
	runtime RuntimeManager,
	appRoot string,
) *CLISessionService {
	return &CLISessionService{
		store:              store,
		projectService:     projectService,
		requirementService: requirementService,
		profileRegistry:    profileRegistry,
		runtime:            runtime,
		appRoot:            appRoot,
	}
}

// cliSessionLogger returns component logger for CLI session service.
func cliSessionLogger() *zap.Logger {
	return logging.Named("service.cli-session")
}

// appendTokenInjectionFields appends token injection diagnostics to logger fields.
func appendTokenInjectionFields(fields []zap.Field, env map[string]string) []zap.Field {
	summary := logging.SummarizeEnvInjection(env)
	return append(
		fields,
		zap.Bool("token_env_configured", summary.TokenEnvConfigured),
		zap.Bool("token_injected", summary.TokenInjected),
		zap.Strings("token_env_injected_keys", summary.InjectedTokenKeys),
		zap.Strings("token_env_empty_keys", summary.EmptyTokenKeys),
	)
}

// Profiles returns CLI profile definitions grouped by type.
func (s *CLISessionService) Profiles() map[string][]appconfig.CLIProfile {
	if s == nil || s.profileRegistry == nil {
		return nil
	}
	return s.profileRegistry.All()
}

// Types returns configured CLI types sorted alphabetically.
func (s *CLISessionService) Types() []string {
	if s == nil || s.profileRegistry == nil {
		return nil
	}
	return s.profileRegistry.Types()
}

// DefaultProfileID returns the default profile id for one cli type.
func (s *CLISessionService) DefaultProfileID(cliType string) string {
	if s == nil || s.profileRegistry == nil {
		return ""
	}
	return strings.TrimSpace(s.profileRegistry.GetDefaultProfileID(cliType))
}

// SupportsMultipleAccounts reports whether one cli type supports account-level profile selection.
func (s *CLISessionService) SupportsMultipleAccounts(cliType string) bool {
	if s == nil || s.profileRegistry == nil {
		return false
	}
	return s.profileRegistry.SupportsMultipleAccounts(cliType)
}

func (s *CLISessionService) normalizeRequestedProfileID(cliType, profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID != "" {
		return profileID
	}
	return strings.TrimSpace(s.DefaultProfileID(cliType))
}

func (s *CLISessionService) resolveProfile(cliType, profileID string) (appconfig.CLIProfile, error) {
	if s == nil || s.profileRegistry == nil {
		return appconfig.CLIProfile{}, errors.New("profile registry is not initialized")
	}
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	profile, ok := s.profileRegistry.Find(cliType, profileID)
	if !ok {
		return appconfig.CLIProfile{}, fmt.Errorf("profile not found: %s/%s", cliType, profileID)
	}
	return profile, nil
}

// validateSessionCreation 验证 CLI 会话创建的前置条件
// 包括：Profile 存在性、工作目录可访问性和权限
func (s *CLISessionService) validateSessionCreation(cliType, profileID, workDir string) error {
	logger := cliSessionLogger()
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	logger.Debug(
		"validate cli session creation",
		zap.String("cli_type", cliType),
		zap.String("profile", profileID),
		zap.String("work_dir", workDir),
	)

	// 1. 验证 Profile 存在（跳过 standalone 类型）
	if cliType != standaloneCLIType {
		profile, err := s.resolveProfile(cliType, profileID)
		if err != nil {
			logger.Warn(
				"validate cli session creation failed: profile not found",
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.Error(err),
			)
			return err
		}

		// 2. 验证可拼装出可执行命令和运行环境
		spec, err := buildProfileLaunchSpec(profile, cliType, s.appRoot)
		if err != nil {
			logger.Error(
				"validate cli session creation failed: build launch spec error",
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.Error(err),
			)
			return err
		}
		if strings.TrimSpace(spec.Command) == "" {
			logger.Warn(
				"validate cli session creation failed: empty command",
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
			)
			return fmt.Errorf("profile command is empty after resolve: %s/%s", cliType, profileID)
		}
	}

	// 3. 验证工作目录非空
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		logger.Warn("validate cli session creation failed: empty work directory")
		return errors.New("work directory cannot be empty")
	}

	// 4. 检查工作目录状态
	info, err := os.Stat(workDir)
	if err != nil {
		if os.IsNotExist(err) {
			// 目录不存在，尝试创建
			if err := os.MkdirAll(workDir, 0o755); err != nil {
				logger.Error(
					"validate cli session creation failed: create work directory error",
					zap.String("work_dir", workDir),
					zap.Error(err),
				)
				return fmt.Errorf("cannot create work directory %s: %w", workDir, err)
			}
			logger.Info("work directory created for cli session", zap.String("work_dir", workDir))
			// 创建成功，继续验证写权限
		} else {
			// 其他错误（权限不足、路径问题等）
			logger.Error(
				"validate cli session creation failed: work directory inaccessible",
				zap.String("work_dir", workDir),
				zap.Error(err),
			)
			return fmt.Errorf("work directory is not accessible: %w", err)
		}
	} else {
		// 5. 目录存在，验证是目录而不是文件
		if !info.IsDir() {
			logger.Warn(
				"validate cli session creation failed: work directory is not directory",
				zap.String("work_dir", workDir),
			)
			return fmt.Errorf("work directory path is not a directory: %s", workDir)
		}
	}

	// 6. 验证写权限（尝试创建临时文件）
	testFile := filepath.Join(workDir, ".cli_session_write_test")
	f, err := os.Create(testFile)
	if err != nil {
		logger.Error(
			"validate cli session creation failed: work directory not writable",
			zap.String("work_dir", workDir),
			zap.Error(err),
		)
		return fmt.Errorf("work directory is not writable: %w", err)
	}
	f.Close()
	os.Remove(testFile)

	logger.Debug(
		"validate cli session creation completed",
		zap.String("cli_type", cliType),
		zap.String("profile", profileID),
		zap.String("work_dir", workDir),
	)
	return nil
}

// EnsureProjectSession ensures one project-bound CLI session exists for given cli type/profile.
// If a matching session already exists, the existing session is reused (and reconnected if needed).
func (s *CLISessionService) EnsureProjectSession(projectID, cliType, profileID, commandOverride string) (*domain.CLISessionView, bool, error) {
	return s.EnsureProjectSessionWithSize(projectID, cliType, profileID, commandOverride, 0, 0)
}

// EnsureProjectSessionWithSize ensures one project session exists and applies explicit initial PTY size for new runtimes.
func (s *CLISessionService) EnsureProjectSessionWithSize(projectID, cliType, profileID, commandOverride string, cols, rows int) (*domain.CLISessionView, bool, error) {
	logger := cliSessionLogger()
	if s == nil || s.store == nil || s.projectService == nil || s.profileRegistry == nil || s.runtime == nil {
		return nil, false, errors.New("cli session service is not initialized")
	}

	projectID = strings.TrimSpace(projectID)
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	commandOverride = strings.TrimSpace(commandOverride)
	if projectID == "" {
		return nil, false, errors.New("project_id is required")
	}
	if cliType == "" {
		return nil, false, errors.New("cli_type is required")
	}
	if profileID == "" {
		return nil, false, errors.New("profile is required")
	}

	project, err := s.projectService.Get(projectID)
	if err != nil {
		return nil, false, err
	}
	workDir := strings.TrimSpace(s.projectService.EffectiveWorkDir(*project))
	if workDir == "" {
		return nil, false, errors.New("project work directory is empty")
	}
	if err := s.validateSessionCreation(cliType, profileID, workDir); err != nil {
		return nil, false, err
	}

	s.ensureMu.Lock()
	defer s.ensureMu.Unlock()

	records, err := s.store.ListCLISessionRecordsByProject(projectID)
	if err != nil {
		return nil, false, err
	}
	for _, record := range records {
		if strings.TrimSpace(strings.ToLower(record.CLIType)) != cliType {
			continue
		}
		if s.normalizeRequestedProfileID(cliType, record.Profile) != profileID {
			continue
		}
		view, err := s.ensureRecordConnected(record)
		if err != nil {
			logger.Warn(
				"reuse project session failed, fallback to create new runtime session",
				zap.String("project_id", projectID),
				zap.String("session_id", record.ID),
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.Error(err),
			)
			break
		}
		return view, true, nil
	}

	view, err := s.createProjectBoundSession(projectID, strings.TrimSpace(project.Name), cliType, profileID, commandOverride, workDir, cols, rows)
	if err != nil {
		return nil, false, err
	}
	return view, false, nil
}

// ensureRecordConnected returns one reusable view for persisted records and reconnects runtime when needed.
func (s *CLISessionService) ensureRecordConnected(record domain.CLISessionRecord) (*domain.CLISessionView, error) {
	sessionID := strings.TrimSpace(record.ID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if runtimeSession, ok := s.runtime.Get(sessionID); ok {
		summary := runtimeSession.Summary()
		if normalizeSessionState(summary.State) == domain.CLISessionStateRunning {
			_ = s.syncPersistedRuntime(sessionID, summary, record.LaunchMode)
			view := s.mergeRecordWithRuntime(record, summary)
			return &view, nil
		}
		if err := s.runtime.Destroy(sessionID); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "not found") {
				return nil, err
			}
		}
	}
	view, _, err := s.Reconnect(sessionID)
	if err != nil {
		return nil, err
	}
	return view, nil
}

// createProjectBoundSession launches a fresh session bound to one project.
func (s *CLISessionService) createProjectBoundSession(
	projectID,
	projectName,
	cliType,
	profileID,
	commandOverride,
	workDir string,
	cols,
	rows int,
) (*domain.CLISessionView, error) {
	logger := cliSessionLogger()
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	profile, err := s.resolveProfile(cliType, profileID)
	if err != nil {
		return nil, err
	}

	spec, err := buildProfileLaunchSpec(profile, cliType, s.appRoot)
	if err != nil {
		return nil, err
	}
	if commandOverride != "" {
		spec.Command = commandOverride
	}
	spec.Command = stabilizeProjectScopedCLICommand(cliType, spec.Command, workDir)
	logger.Info(
		"initialize project cli runtime environment",
		appendTokenInjectionFields([]zap.Field{
			zap.String("project_id", projectID),
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.String("work_dir", workDir),
			zap.String("command", spec.Command),
			zap.Int("env_count", len(spec.Env)),
			zap.Bool("command_override", commandOverride != ""),
		}, spec.Env)...,
	)

	session, err := s.runtime.CreateWithWorkDirAndEnvAndSize(spec.Command, workDir, spec.Env, cols, rows)
	if err != nil {
		logger.Error(
			"create project cli runtime failed",
			appendTokenInjectionFields([]zap.Field{
				zap.String("project_id", projectID),
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.String("work_dir", workDir),
				zap.String("command", spec.Command),
				zap.Int("env_count", len(spec.Env)),
				zap.Error(err),
			}, spec.Env)...,
		)
		return nil, err
	}

	summary := session.Summary()
	record := domain.CLISessionRecord{
		ID:           session.ID(),
		CLIType:      cliType,
		Profile:      profile.ID,
		AgentID:      session.AgentID(),
		ProjectID:    projectID,
		ProjectName:  projectName,
		WorkDir:      fallbackWorkDir(summary.WorkDir, workDir),
		SessionState: normalizeSessionState(summary.State),
		LaunchMode:   summary.LaunchMode,
		ProcessPID:   summary.ProcessPID,
		ProcessPGID:  summary.ProcessPGID,
		CreatedAt:    summary.CreatedAt,
		LastActiveAt: summary.UpdatedAt,
	}
	if err := s.store.CreateCLISessionRecord(record); err != nil {
		logger.Error(
			"persist project cli session record failed",
			zap.String("session_id", record.ID),
			zap.String("project_id", projectID),
			zap.Error(err),
		)
		_ = s.runtime.Destroy(session.ID())
		return nil, err
	}

	view := s.mergeRecordWithRuntime(record, summary)
	if strings.TrimSpace(view.ProfileName) == "" {
		view.ProfileName = profile.Name
	}
	return &view, nil
}

// CreateForRequirement creates exactly one effective requirement-bound session for a running requirement.
func (s *CLISessionService) CreateForRequirement(requirementID string, selections []domain.CLISessionSelection) ([]domain.CLISessionView, error) {
	if s == nil || s.requirementService == nil || s.runtime == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	requirement, err := s.requirementService.Get(requirementID)
	if err != nil {
		return nil, err
	}
	if requirement.Status != domain.RequirementStatusRunning {
		return nil, errors.New("only running requirements can create cli sessions")
	}
	if len(selections) == 0 {
		return nil, errors.New("at least one cli profile must be selected")
	}
	if len(selections) > 1 {
		return nil, errors.New("only one cli profile can be selected per project")
	}

	item := selections[0]
	cliType := strings.TrimSpace(strings.ToLower(item.CLIType))
	profileID := strings.TrimSpace(item.Profile)
	view, reused, err := s.EnsureRequirementSession(requirement.ID, cliType, profileID, "")
	if err != nil {
		return nil, err
	}
	if requirement.ExecutionMode != domain.RequirementExecutionModeAuto {
		shouldSendPrompt := !reused || requirement.PromptSentAt == nil || requirement.PromptSentAt.IsZero()
		if shouldSendPrompt {
			if prompt := strings.TrimSpace(requirement.Description); prompt != "" {
				if runtimeSession, ok := s.runtime.Get(view.ID); ok {
					_ = runtimeSession.WriteInput(prompt + "\n")
					s.Touch(view.ID)
				}
			}
		}
	}
	return []domain.CLISessionView{*view}, nil
}

// CreateStandalone creates a manually-commanded standalone session for /cli console page.
func (s *CLISessionService) CreateStandalone(command string) (*domain.CLISessionView, error) {
	return s.CreateStandaloneWithSize(command, 0, 0)
}

// CreateStandaloneWithSize creates one manual standalone session with explicit initial PTY size.
func (s *CLISessionService) CreateStandaloneWithSize(command string, cols, rows int) (*domain.CLISessionView, error) {
	logger := cliSessionLogger()
	if s == nil || s.store == nil || s.runtime == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	logger.Info("create standalone cli session", zap.String("command", command))
	session, err := s.runtime.CreateWithSize(command, cols, rows)
	if err != nil {
		logger.Error(
			"create standalone cli session failed",
			zap.String("command", command),
			zap.Error(err),
		)
		return nil, err
	}
	summary := session.Summary()
	record := domain.CLISessionRecord{
		ID:           session.ID(),
		CLIType:      standaloneCLIType,
		Profile:      standaloneCLIProfile,
		AgentID:      session.AgentID(),
		WorkDir:      fallbackWorkDir(summary.WorkDir, s.appRoot),
		SessionState: normalizeSessionState(summary.State),
		LaunchMode:   summary.LaunchMode,
		ProcessPID:   summary.ProcessPID,
		ProcessPGID:  summary.ProcessPGID,
		CreatedAt:    summary.CreatedAt,
		LastActiveAt: summary.UpdatedAt,
	}
	if err := s.store.CreateCLISessionRecord(record); err != nil {
		logger.Error(
			"persist standalone cli session failed",
			zap.String("session_id", session.ID()),
			zap.Error(err),
		)
		_ = s.runtime.Destroy(session.ID())
		return nil, err
	}
	view := s.mergeRecordWithRuntime(record, summary)
	logger.Info(
		"standalone cli session created",
		zap.String("session_id", session.ID()),
		zap.String("agent_id", session.AgentID()),
	)
	return &view, nil
}

// CreateStandaloneWithProfile creates one standalone session with selected cli type/profile.
// Unlike CreateForRequirement, this flow does not bind session to any requirement.
func (s *CLISessionService) CreateStandaloneWithProfile(cliType, profileID, commandOverride string) (*domain.CLISessionView, error) {
	return s.createStandaloneWithProfile(cliType, profileID, commandOverride, "", 0, 0)
}

// CreateStandaloneWithProfileAndWorkDir creates one standalone session with selected profile at explicit work dir.
// This flow is requirement-unbound but can be project-scoped by passing project root as workDir.
func (s *CLISessionService) CreateStandaloneWithProfileAndWorkDir(cliType, profileID, commandOverride, workDir string) (*domain.CLISessionView, error) {
	return s.createStandaloneWithProfile(cliType, profileID, commandOverride, workDir, 0, 0)
}

// CreateStandaloneWithProfileAndSize creates one standalone profile session with explicit initial PTY size.
func (s *CLISessionService) CreateStandaloneWithProfileAndSize(cliType, profileID, commandOverride string, cols, rows int) (*domain.CLISessionView, error) {
	return s.createStandaloneWithProfile(cliType, profileID, commandOverride, "", cols, rows)
}

func (s *CLISessionService) createStandaloneWithProfile(
	cliType,
	profileID,
	commandOverride,
	workDirOverride string,
	cols,
	rows int,
) (*domain.CLISessionView, error) {
	logger := cliSessionLogger()
	if s == nil || s.store == nil || s.runtime == nil || s.profileRegistry == nil {
		return nil, errors.New("cli session service is not initialized")
	}

	cliType = strings.TrimSpace(strings.ToLower(cliType))
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	if cliType == "" || profileID == "" {
		return nil, errors.New("cli_type and profile are required")
	}

	profile, err := s.resolveProfile(cliType, profileID)
	if err != nil {
		logger.Warn(
			"create standalone session with profile failed: profile not found",
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.Error(err),
		)
		return nil, err
	}

	spec, err := buildProfileLaunchSpec(profile, cliType, s.appRoot)
	if err != nil {
		logger.Error(
			"create standalone session with profile failed: build launch spec error",
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.Error(err),
		)
		return nil, err
	}

	commandOverride = strings.TrimSpace(commandOverride)
	if commandOverride != "" {
		spec.Command = commandOverride
	}
	workDir := strings.TrimSpace(workDirOverride)
	if workDir == "" {
		workDir = fallbackWorkDir("", s.appRoot)
	}
	spec.Command = stabilizeProjectScopedCLICommand(cliType, spec.Command, workDir)
	if err := s.validateSessionCreation(cliType, profileID, workDir); err != nil {
		logger.Error(
			"create standalone session with profile failed: work directory validation error",
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.String("work_dir", workDir),
			zap.Error(err),
		)
		return nil, err
	}
	logger.Info(
		"initialize standalone cli runtime environment",
		appendTokenInjectionFields([]zap.Field{
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.String("work_dir", workDir),
			zap.String("command", spec.Command),
			zap.Int("env_count", len(spec.Env)),
			zap.Bool("command_override", commandOverride != ""),
		}, spec.Env)...,
	)

	session, err := s.runtime.CreateWithWorkDirAndEnvAndSize(spec.Command, workDir, spec.Env, cols, rows)
	if err != nil {
		logger.Error(
			"create standalone session runtime with profile failed",
			appendTokenInjectionFields([]zap.Field{
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.String("work_dir", workDir),
				zap.String("command", spec.Command),
				zap.Int("env_count", len(spec.Env)),
				zap.Bool("command_override", commandOverride != ""),
				zap.Error(err),
			}, spec.Env)...,
		)
		return nil, err
	}

	summary := session.Summary()
	record := domain.CLISessionRecord{
		ID:           session.ID(),
		CLIType:      cliType,
		Profile:      profile.ID,
		AgentID:      session.AgentID(),
		WorkDir:      fallbackWorkDir(summary.WorkDir, workDir),
		SessionState: normalizeSessionState(summary.State),
		LaunchMode:   summary.LaunchMode,
		ProcessPID:   summary.ProcessPID,
		ProcessPGID:  summary.ProcessPGID,
		CreatedAt:    summary.CreatedAt,
		LastActiveAt: summary.UpdatedAt,
	}
	if err := s.store.CreateCLISessionRecord(record); err != nil {
		logger.Error(
			"persist standalone session with profile failed",
			zap.String("session_id", session.ID()),
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.Error(err),
		)
		_ = s.runtime.Destroy(session.ID())
		return nil, err
	}

	view := s.mergeRecordWithRuntime(record, summary)
	if strings.TrimSpace(view.ProfileName) == "" {
		view.ProfileName = profile.Name
	}
	logger.Info(
		"standalone cli session with profile created",
		zap.String("session_id", session.ID()),
		zap.String("agent_id", session.AgentID()),
		zap.String("cli_type", cliType),
		zap.String("profile", profileID),
	)
	return &view, nil
}

// ListAllViews lists all sessions merged with current runtime data.
func (s *CLISessionService) ListAllViews(cliType string) ([]domain.CLISessionView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	records, err := s.store.ListCLISessionRecords(cliType)
	if err != nil {
		return nil, err
	}
	return s.mergeRecords(records), nil
}

// ListProjectViews lists all sessions bound to one project.
func (s *CLISessionService) ListProjectViews(projectID string) ([]domain.CLISessionView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	records, err := s.store.ListCLISessionRecordsByProject(projectID)
	if err != nil {
		return nil, err
	}
	return s.mergeRecords(records), nil
}

// ListRequirementViews lists all project-bound sessions under one requirement's project.
func (s *CLISessionService) ListRequirementViews(requirementID string) ([]domain.CLISessionView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	records, err := s.store.ListCLISessionRecordsByRequirement(requirementID)
	if err != nil {
		return nil, err
	}
	return s.mergeRecords(records), nil
}

// GetView returns merged metadata/runtime view for one session id.
func (s *CLISessionService) GetView(sessionID string) (*domain.CLISessionView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	record, err := s.store.GetCLISessionRecord(sessionID)
	if err != nil {
		return nil, err
	}
	views := s.mergeRecords([]domain.CLISessionRecord{*record})
	if len(views) == 0 {
		return nil, persistence.ErrNotFound
	}
	return &views[0], nil
}

func (s *CLISessionService) CreateRequirementBoundSession(
	requirementID,
	cliType,
	profileID,
	commandOverride string,
) (*domain.CLISessionView, error) {
	if s == nil || s.requirementService == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	requirement, err := s.requirementService.Get(requirementID)
	if err != nil {
		return nil, err
	}
	return s.createRequirementBoundSession(*requirement, cliType, profileID, commandOverride, 0, 0)
}

// EnsureRequirementSession returns one reusable requirement session for a project/cli/profile combination.
func (s *CLISessionService) EnsureRequirementSession(requirementID, cliType, profileID, commandOverride string) (*domain.CLISessionView, bool, error) {
	if s == nil || s.requirementService == nil || s.runtime == nil {
		return nil, false, errors.New("cli session service is not initialized")
	}
	requirement, err := s.requirementService.Get(requirementID)
	if err != nil {
		return nil, false, err
	}
	return s.ensureRequirementSession(*requirement, cliType, profileID, commandOverride, 0, 0)
}

func (s *CLISessionService) ensureRequirementSession(
	requirement domain.Requirement,
	cliType,
	profileID,
	commandOverride string,
	cols,
	rows int,
) (*domain.CLISessionView, bool, error) {
	logger := cliSessionLogger()
	if s == nil || s.store == nil || s.runtime == nil || s.projectService == nil || s.profileRegistry == nil || s.requirementService == nil {
		return nil, false, errors.New("cli session service is not initialized")
	}
	if requirement.Status != domain.RequirementStatusRunning {
		return nil, false, errors.New("only running requirements can create cli sessions")
	}

	if strings.TrimSpace(cliType) == "" {
		cliType = requirement.CLIType
	}
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	if cliType == "" {
		return nil, false, errors.New("cli_type is required")
	}
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	if profileID == "" {
		return nil, false, errors.New("profile is required")
	}

	s.ensureMu.Lock()
	defer s.ensureMu.Unlock()

	records, err := s.store.ListCLISessionRecordsByProject(requirement.ProjectID)
	if err != nil {
		return nil, false, err
	}
	for _, record := range records {
		if strings.TrimSpace(strings.ToLower(record.CLIType)) != cliType {
			continue
		}
		if s.normalizeRequestedProfileID(cliType, record.Profile) != profileID {
			continue
		}
		if !s.requirementRecordReusable(record, requirement.ID) {
			continue
		}
		view, ensureErr := s.ensureRecordConnected(record)
		if ensureErr != nil {
			logger.Warn(
				"reuse requirement session failed, fallback to create new runtime session",
				zap.String("project_id", requirement.ProjectID),
				zap.String("requirement_id", requirement.ID),
				zap.String("session_id", record.ID),
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.Error(ensureErr),
			)
			continue
		}
		if strings.TrimSpace(view.RequirementID) != requirement.ID {
			view, ensureErr = s.RebindSessionToRequirement(view.ID, requirement.ID)
			if ensureErr != nil {
				logger.Warn(
					"rebind reused session to requirement failed, fallback to create new runtime session",
					zap.String("project_id", requirement.ProjectID),
					zap.String("requirement_id", requirement.ID),
					zap.String("session_id", record.ID),
					zap.Error(ensureErr),
				)
				continue
			}
		}
		return view, true, nil
	}

	view, err := s.createRequirementBoundSession(requirement, cliType, profileID, commandOverride, cols, rows)
	if err != nil {
		return nil, false, err
	}
	return view, false, nil
}

func (s *CLISessionService) requirementRecordReusable(record domain.CLISessionRecord, requirementID string) bool {
	existingRequirementID := strings.TrimSpace(record.RequirementID)
	if existingRequirementID == "" || existingRequirementID == requirementID {
		return true
	}
	if s == nil || s.requirementService == nil {
		return false
	}
	requirement, err := s.requirementService.Get(existingRequirementID)
	if err != nil || requirement == nil {
		return true
	}
	return requirement.Status != domain.RequirementStatusRunning && requirement.Status != domain.RequirementStatusPaused
}

func (s *CLISessionService) createRequirementBoundSession(
	requirement domain.Requirement,
	cliType,
	profileID,
	commandOverride string,
	cols,
	rows int,
) (*domain.CLISessionView, error) {
	logger := cliSessionLogger()
	if s == nil || s.store == nil || s.runtime == nil || s.projectService == nil || s.profileRegistry == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	if requirement.Status != domain.RequirementStatusRunning {
		return nil, errors.New("only running requirements can create cli sessions")
	}

	if strings.TrimSpace(cliType) == "" {
		cliType = requirement.CLIType
	}
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	if cliType == "" {
		return nil, errors.New("cli_type is required")
	}
	profileID = s.normalizeRequestedProfileID(cliType, profileID)
	if profileID == "" {
		return nil, errors.New("profile is required")
	}
	commandOverride = strings.TrimSpace(commandOverride)

	project, err := s.projectService.Get(requirement.ProjectID)
	if err != nil {
		return nil, err
	}
	workDir := strings.TrimSpace(s.projectService.EffectiveWorkDir(*project))
	if workDir == "" {
		return nil, errors.New("project work directory is empty")
	}
	if err := s.validateSessionCreation(cliType, profileID, workDir); err != nil {
		return nil, err
	}

	profile, err := s.resolveProfile(cliType, profileID)
	if err != nil {
		return nil, err
	}
	spec, err := buildProfileLaunchSpec(profile, cliType, s.appRoot)
	if err != nil {
		return nil, err
	}
	if commandOverride != "" {
		spec.Command = commandOverride
	}
	spec.Command = stabilizeProjectScopedCLICommand(cliType, spec.Command, workDir)
	logger.Info(
		"initialize requirement cli runtime environment",
		appendTokenInjectionFields([]zap.Field{
			zap.String("project_id", requirement.ProjectID),
			zap.String("requirement_id", requirement.ID),
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.String("work_dir", workDir),
			zap.String("command", spec.Command),
			zap.Int("env_count", len(spec.Env)),
			zap.Bool("command_override", commandOverride != ""),
		}, spec.Env)...,
	)

	session, err := s.runtime.CreateWithWorkDirAndEnvAndSize(spec.Command, workDir, spec.Env, cols, rows)
	if err != nil {
		logger.Error(
			"create requirement cli runtime failed",
			appendTokenInjectionFields([]zap.Field{
				zap.String("project_id", requirement.ProjectID),
				zap.String("requirement_id", requirement.ID),
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.String("work_dir", workDir),
				zap.String("command", spec.Command),
				zap.Int("env_count", len(spec.Env)),
				zap.Error(err),
			}, spec.Env)...,
		)
		return nil, err
	}

	summary := session.Summary()
	record := domain.CLISessionRecord{
		ID:               session.ID(),
		CLIType:          cliType,
		Profile:          profile.ID,
		AgentID:          session.AgentID(),
		ProjectID:        requirement.ProjectID,
		ProjectName:      requirement.ProjectName,
		RequirementID:    requirement.ID,
		RequirementTitle: requirement.Title,
		WorkDir:          fallbackWorkDir(summary.WorkDir, workDir),
		SessionState:     normalizeSessionState(summary.State),
		LaunchMode:       summary.LaunchMode,
		ProcessPID:       summary.ProcessPID,
		ProcessPGID:      summary.ProcessPGID,
		CreatedAt:        summary.CreatedAt,
		LastActiveAt:     summary.UpdatedAt,
	}
	if err := s.store.CreateCLISessionRecord(record); err != nil {
		logger.Error(
			"persist requirement cli session failed",
			zap.String("session_id", session.ID()),
			zap.String("project_id", requirement.ProjectID),
			zap.String("requirement_id", requirement.ID),
			zap.Error(err),
		)
		_ = s.runtime.Destroy(session.ID())
		return nil, err
	}

	view := s.mergeRecordWithRuntime(record, summary)
	if strings.TrimSpace(view.ProfileName) == "" {
		view.ProfileName = profile.Name
	}
	return &view, nil
}

// RebindSessionToRequirement moves one reusable session record onto the current running requirement.
func (s *CLISessionService) RebindSessionToRequirement(sessionID, requirementID string) (*domain.CLISessionView, error) {
	if s == nil || s.store == nil || s.requirementService == nil {
		return nil, errors.New("cli session service is not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	requirementID = strings.TrimSpace(requirementID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if requirementID == "" {
		return nil, errors.New("requirement id is required")
	}

	requirement, err := s.requirementService.Get(requirementID)
	if err != nil {
		return nil, err
	}
	if requirement.Status != domain.RequirementStatusRunning && requirement.Status != domain.RequirementStatusPaused {
		return nil, errors.New("only running or paused requirements can bind cli sessions")
	}

	record, err := s.store.GetCLISessionRecord(sessionID)
	if err != nil {
		return nil, err
	}
	if record.ProjectID != "" && strings.TrimSpace(record.ProjectID) != strings.TrimSpace(requirement.ProjectID) {
		return nil, fmt.Errorf("session %s belongs to different project", sessionID)
	}
	requiredCLIType := strings.TrimSpace(strings.ToLower(requirement.CLIType))
	if requiredCLIType != "" && strings.TrimSpace(strings.ToLower(record.CLIType)) != requiredCLIType {
		return nil, fmt.Errorf("session %s cli type %s does not match requirement cli type %s", sessionID, record.CLIType, requiredCLIType)
	}
	if existingRequirementID := strings.TrimSpace(record.RequirementID); existingRequirementID != "" && existingRequirementID != requirementID {
		existingRequirement, existingErr := s.requirementService.Get(existingRequirementID)
		if existingErr == nil && existingRequirement != nil {
			if existingRequirement.Status == domain.RequirementStatusRunning || existingRequirement.Status == domain.RequirementStatusPaused {
				return nil, fmt.Errorf("session %s is still bound to active requirement %s", sessionID, existingRequirementID)
			}
		}
	}

	if err := s.store.UpdateCLISessionBinding(sessionID, requirement.ProjectID, requirement.ID); err != nil {
		return nil, err
	}
	return s.GetView(sessionID)
}

// MarkTerminated persists terminated state after explicit stop/destroy actions.
func (s *CLISessionService) MarkTerminated(sessionID string) error {
	if s == nil || s.store == nil {
		return errors.New("cli session service is not initialized")
	}
	summary := RuntimeSessionSummary{}
	if runtimeSession, ok := s.runtime.Get(sessionID); ok {
		summary = runtimeSession.Summary()
	}
	launchMode := strings.TrimSpace(summary.LaunchMode)
	if launchMode == "" {
		record, err := s.store.GetCLISessionRecord(sessionID)
		if err == nil {
			launchMode = record.LaunchMode
		}
	}
	return s.store.UpdateCLISessionRuntime(
		sessionID,
		domain.CLISessionStateTerminated,
		launchMode,
		0,
		0,
		time.Now(),
	)
}

// DeleteRecord removes persisted metadata after a session is destroyed.
func (s *CLISessionService) DeleteRecord(sessionID string) error {
	if s == nil || s.store == nil {
		return errors.New("cli session service is not initialized")
	}
	if err := s.store.DeleteCLISessionRecord(sessionID); err != nil && !errors.Is(err, persistence.ErrNotFound) {
		return err
	}
	return nil
}

// Reconnect ensures one persisted session has an active runtime instance.
// It returns the active session view and whether the original runtime was reused.
func (s *CLISessionService) Reconnect(sessionID string) (*domain.CLISessionView, bool, error) {
	logger := cliSessionLogger()
	if s == nil || s.store == nil || s.runtime == nil {
		return nil, false, errors.New("cli session service is not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, false, errors.New("session id is required")
	}
	logger.Info("reconnect cli session requested", zap.String("session_id", sessionID))

	reconnectCommand := ""
	if runtimeSession, ok := s.runtime.Get(sessionID); ok {
		summary := runtimeSession.Summary()
		reconnectCommand = strings.TrimSpace(summary.Command)
		if normalizeSessionState(summary.State) != domain.CLISessionStateRunning {
			logger.Info(
				"found stale runtime session before reconnect, destroying old runtime",
				zap.String("session_id", sessionID),
				zap.String("state", summary.State),
			)
			if err := s.runtime.Destroy(sessionID); err != nil {
				logger.Error(
					"destroy stale runtime before reconnect failed",
					zap.String("session_id", sessionID),
					zap.Error(err),
				)
				return nil, false, err
			}
		} else {
			view, err := s.reuseConnectedRuntime(sessionID, runtimeSession)
			if err != nil {
				logger.Error(
					"reuse connected runtime during reconnect failed",
					zap.String("session_id", sessionID),
					zap.Error(err),
				)
				return nil, false, err
			}
			logger.Info("reconnect reused running runtime", zap.String("session_id", sessionID))
			return view, true, nil
		}
	}

	record, err := s.store.GetCLISessionRecord(sessionID)
	if err != nil {
		logger.Error(
			"load persisted cli session record for reconnect failed",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return nil, false, err
	}
	view, err := s.rebuildDisconnectedRuntime(*record, reconnectCommand)
	if err != nil {
		logger.Error(
			"rebuild disconnected runtime failed",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return nil, false, err
	}
	logger.Info("reconnect cli session completed", zap.String("session_id", sessionID))
	return view, false, nil
}

// Touch updates session last active timestamp.
func (s *CLISessionService) Touch(sessionID string) {
	if s == nil || s.store == nil {
		return
	}
	_ = s.store.TouchCLISession(sessionID, time.Now())
}

// syncPersistedRuntime updates persisted runtime metadata for an active running session.
func (s *CLISessionService) syncPersistedRuntime(sessionID string, summary RuntimeSessionSummary, fallbackLaunchMode string) error {
	if s == nil || s.store == nil {
		return errors.New("cli session service is not initialized")
	}
	launchMode := strings.TrimSpace(summary.LaunchMode)
	if launchMode == "" {
		launchMode = strings.TrimSpace(fallbackLaunchMode)
	}
	updatedAt := summary.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	return s.store.UpdateCLISessionRuntime(
		sessionID,
		domain.CLISessionStateRunning,
		launchMode,
		summary.ProcessPID,
		summary.ProcessPGID,
		updatedAt,
	)
}

// reuseConnectedRuntime returns merged view for already-running runtime sessions.
func (s *CLISessionService) reuseConnectedRuntime(sessionID string, runtimeSession RuntimeSession) (*domain.CLISessionView, error) {
	summary := runtimeSession.Summary()
	record, err := s.store.GetCLISessionRecord(sessionID)
	if err != nil {
		if errors.Is(err, persistence.ErrNotFound) {
			view := domain.CLISessionView{
				ID:         summary.ID,
				AgentID:    summary.AgentID,
				WorkDir:    fallbackWorkDir(summary.WorkDir, s.appRoot),
				State:      normalizeSessionState(summary.State),
				LaunchMode: summary.LaunchMode,
				CreatedAt:  summary.CreatedAt,
				LastActiveAt: func() time.Time {
					if summary.UpdatedAt.IsZero() {
						return summary.CreatedAt
					}
					return summary.UpdatedAt
				}(),
				ExitCode:  summary.ExitCode,
				LastError: summary.LastError,
			}
			return &view, nil
		}
		return nil, err
	}
	_ = s.syncPersistedRuntime(sessionID, summary, record.LaunchMode)
	view := s.mergeRecordWithRuntime(*record, summary)
	return &view, nil
}

// rebuildDisconnectedRuntime recreates one disconnected runtime with existing session identity.
func (s *CLISessionService) rebuildDisconnectedRuntime(record domain.CLISessionRecord, commandOverride string) (*domain.CLISessionView, error) {
	logger := cliSessionLogger()
	cliType, profile := normalizeSessionProfile(record.CLIType, record.Profile)
	profile = s.normalizeRequestedProfileID(cliType, profile)
	workDir := fallbackWorkDir(record.WorkDir, s.appRoot)

	command := strings.TrimSpace(commandOverride)
	var env map[string]string
	if command == "" || cliType != standaloneCLIType {
		spec, err := s.resolveReconnectLaunchSpec(cliType, profile)
		if err != nil {
			return nil, err
		}
		if command == "" {
			command = spec.Command
		}
		env = spec.Env
	}
	command = stabilizeProjectScopedCLICommand(cliType, command, workDir)
	logger.Info(
		"initialize cli runtime environment for reconnect",
		appendTokenInjectionFields([]zap.Field{
			zap.String("session_id", record.ID),
			zap.String("agent_id", record.AgentID),
			zap.String("cli_type", cliType),
			zap.String("profile", profile),
			zap.String("work_dir", workDir),
			zap.String("command", command),
			zap.Int("env_count", len(env)),
		}, env)...,
	)

	session, err := s.runtime.CreateWithIdentityAndEnv(record.ID, record.AgentID, command, workDir, env)
	if err != nil {
		logger.Error(
			"create runtime during reconnect failed",
			appendTokenInjectionFields([]zap.Field{
				zap.String("session_id", record.ID),
				zap.String("command", command),
				zap.Int("env_count", len(env)),
				zap.Error(err),
			}, env)...,
		)
		return nil, err
	}
	summary := session.Summary()
	if normalizeSessionState(summary.State) != domain.CLISessionStateRunning {
		logger.Warn(
			"reconnect created runtime but process is not running",
			zap.String("session_id", record.ID),
			zap.String("state", summary.State),
		)
		return nil, fmt.Errorf("reconnect failed: session process is not running")
	}
	_ = s.syncPersistedRuntime(record.ID, summary, record.LaunchMode)

	viewRecord := record
	viewRecord.CLIType = cliType
	viewRecord.Profile = profile
	view := s.mergeRecordWithRuntime(viewRecord, summary)
	logger.Info(
		"reconnect runtime created successfully",
		zap.String("session_id", record.ID),
		zap.String("agent_id", session.AgentID()),
	)
	return &view, nil
}

// resolveReconnectLaunchSpec determines runtime launch spec for one session profile.
func (s *CLISessionService) resolveReconnectLaunchSpec(cliType, profile string) (launchSpec, error) {
	logger := cliSessionLogger()
	if cliType == standaloneCLIType {
		return launchSpec{}, nil
	}
	item, err := s.resolveProfile(cliType, profile)
	if err != nil {
		logger.Warn(
			"resolve reconnect launch spec failed: profile not found",
			zap.String("cli_type", cliType),
			zap.String("profile", profile),
			zap.Error(err),
		)
		return launchSpec{}, err
	}
	logger.Debug(
		"resolve reconnect launch spec",
		zap.String("cli_type", cliType),
		zap.String("profile", profile),
	)
	return buildProfileLaunchSpec(item, cliType, s.appRoot)
}

// normalizeSessionProfile applies default standalone profile values when record fields are empty.
func normalizeSessionProfile(cliType, profile string) (string, string) {
	normalizedType := strings.TrimSpace(strings.ToLower(cliType))
	if normalizedType == "" {
		normalizedType = standaloneCLIType
	}
	normalizedProfile := strings.TrimSpace(profile)
	if normalizedProfile == "" {
		normalizedProfile = standaloneCLIProfile
	}
	return normalizedType, normalizedProfile
}

// mergeRecords combines persistent records with in-memory runtime snapshots.
func (s *CLISessionService) mergeRecords(records []domain.CLISessionRecord) []domain.CLISessionView {
	summaries := map[string]RuntimeSessionSummary{}
	if s != nil && s.runtime != nil {
		for _, summary := range s.runtime.List() {
			summaries[summary.ID] = summary
		}
	}

	views := make([]domain.CLISessionView, 0, len(records))
	for _, record := range records {
		summary, ok := summaries[record.ID]
		if !ok {
			summary = RuntimeSessionSummary{}
		}
		views = append(views, s.mergeRecordWithRuntime(record, summary))
	}
	sort.Slice(views, func(i, j int) bool {
		return views[i].CreatedAt.After(views[j].CreatedAt)
	})
	return views
}

// mergeRecordWithRuntime overlays runtime fields on top of persisted session metadata.
func (s *CLISessionService) mergeRecordWithRuntime(record domain.CLISessionRecord, summary RuntimeSessionSummary) domain.CLISessionView {
	state := strings.TrimSpace(record.SessionState)
	launchMode := strings.TrimSpace(record.LaunchMode)
	lastActiveAt := record.LastActiveAt
	processPID := record.ProcessPID
	processPGID := record.ProcessPGID
	exitCode := (*int)(nil)
	lastError := ""

	if summary.ID != "" {
		state = normalizeSessionState(summary.State)
		if strings.TrimSpace(summary.LaunchMode) != "" {
			launchMode = summary.LaunchMode
		}
		if !summary.UpdatedAt.IsZero() {
			lastActiveAt = summary.UpdatedAt
		}
		if summary.ProcessPID > 0 {
			processPID = summary.ProcessPID
		}
		if summary.ProcessPGID > 0 {
			processPGID = summary.ProcessPGID
		}
		exitCode = summary.ExitCode
		lastError = summary.LastError
	}

	if state == "" {
		state = domain.CLISessionStateTerminated
	}

	if s != nil {
		record.Profile = s.normalizeRequestedProfileID(record.CLIType, record.Profile)
	}

	profileName := record.Profile
	if s != nil {
		if s.profileRegistry != nil {
			if profile, ok := s.profileRegistry.Find(record.CLIType, record.Profile); ok {
				profileName = profile.Name
			} else if record.CLIType == standaloneCLIType {
				profileName = "Manual Command"
			}
		} else if record.CLIType == standaloneCLIType {
			profileName = "Manual Command"
		}
	}

	workDir := strings.TrimSpace(record.WorkDir)
	if workDir == "" {
		workDir = fallbackWorkDir(summary.WorkDir, s.appRoot)
	}

	return domain.CLISessionView{
		ID:               record.ID,
		CLIType:          record.CLIType,
		Profile:          record.Profile,
		ProfileName:      profileName,
		AgentID:          fallbackText(record.AgentID, summary.AgentID),
		ProjectID:        record.ProjectID,
		ProjectName:      record.ProjectName,
		RequirementID:    record.RequirementID,
		RequirementTitle: record.RequirementTitle,
		WorkDir:          workDir,
		State:            state,
		LaunchMode:       launchMode,
		ProcessPID:       processPID,
		ProcessPGID:      processPGID,
		CreatedAt:        record.CreatedAt,
		LastActiveAt:     lastActiveAt,
		ExitCode:         exitCode,
		LastError:        lastError,
	}
}

type launchSpec struct {
	Command string
	Env     map[string]string
}

// logProfileLaunchSpecReady records finalized profile launch spec diagnostics.
func logProfileLaunchSpecReady(logger *zap.Logger, cliType, profileID string, spec launchSpec, hasPreScript bool) {
	logger.Info(
		"cli profile launch spec ready",
		appendTokenInjectionFields([]zap.Field{
			zap.String("cli_type", cliType),
			zap.String("profile", profileID),
			zap.String("command", spec.Command),
			zap.Int("env_count", len(spec.Env)),
			zap.Bool("has_pre_script", hasPreScript),
		}, spec.Env)...,
	)
}

// buildProfileLaunchSpec builds runtime command and environment for a profile.
// Final command shape: `<pre_script> && <script_command>`
func buildProfileLaunchSpec(profile appconfig.CLIProfile, cliType, appRoot string) (launchSpec, error) {
	logger := cliSessionLogger()
	logger.Debug(
		"build cli profile launch spec",
		zap.String("cli_type", cliType),
		zap.String("profile", profile.ID),
		zap.Bool("has_pre_script", strings.TrimSpace(profile.PreScript) != ""),
		zap.Bool("has_script_command", strings.TrimSpace(profile.ScriptCommand) != ""),
		zap.Int("raw_env_count", len(profile.Env)),
	)

	launch := normalizeShellCommand(profile.ScriptCommand, appRoot)
	if strings.TrimSpace(launch) == "" {
		launch = defaultCLICommandForType(cliType)
	}
	launch = stabilizeCLICommand(cliType, launch)
	if strings.TrimSpace(launch) == "" {
		logger.Warn(
			"build cli profile launch spec failed: empty launch command",
			zap.String("cli_type", cliType),
			zap.String("profile", profile.ID),
		)
		return launchSpec{}, fmt.Errorf("profile command is empty: %s/%s", cliType, profile.ID)
	}
	resolvedEnv, err := resolveProfileEnv(profile.Env, cliType, profile.ID)
	if err != nil {
		logger.Error(
			"build cli profile launch spec failed: resolve env error",
			zap.String("cli_type", cliType),
			zap.String("profile", profile.ID),
			zap.Error(err),
		)
		return launchSpec{}, fmt.Errorf("resolve profile env %s/%s: %w", cliType, profile.ID, err)
	}

	pre := normalizeShellCommand(profile.PreScript, appRoot)
	if strings.TrimSpace(pre) == "" {
		spec := launchSpec{
			Command: launch,
			Env:     resolvedEnv,
		}
		logProfileLaunchSpecReady(logger, cliType, profile.ID, spec, false)
		return spec, nil
	}

	if shouldExecLaunchCommand(launch) {
		spec := launchSpec{
			Command: pre + " && exec " + launch,
			Env:     resolvedEnv,
		}
		logProfileLaunchSpecReady(logger, cliType, profile.ID, spec, true)
		return spec, nil
	}
	spec := launchSpec{
		Command: pre + " && " + launch,
		Env:     resolvedEnv,
	}
	logProfileLaunchSpecReady(logger, cliType, profile.ID, spec, true)
	return spec, nil
}

// normalizeShellCommand trims shell command and resolves relative script path to absolute.
func normalizeShellCommand(raw, appRoot string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return raw
	}
	first := parts[0]
	if strings.HasPrefix(first, "./") || strings.HasPrefix(first, "../") {
		parts[0] = shellQuote(filepath.Clean(filepath.Join(appRoot, first)))
		if len(parts) == 1 {
			return parts[0]
		}
		return parts[0] + " " + strings.Join(parts[1:], " ")
	}
	return raw
}

func stabilizeCLICommand(cliType, launch string) string {
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	launch = strings.TrimSpace(launch)
	if launch == "" {
		return launch
	}

	fields := strings.Fields(launch)
	if len(fields) == 0 {
		return launch
	}
	binary := strings.Trim(fields[0], `"'`)
	switch filepath.Base(binary) {
	case "codex":
		return normalizeCodexLaunchCommand(fields)
	case "claude":
		if !strings.Contains(launch, "--dangerously-skip-permissions") {
			launch += " --dangerously-skip-permissions"
		}
	}
	return launch
}

func normalizeCodexLaunchCommand(fields []string) string {
	if len(fields) == 0 {
		return ""
	}

	mode := ""
	extras := make([]string, 0, len(fields)-1)
	for i := 1; i < len(fields); i++ {
		token := fields[i]
		normalized := strings.Trim(token, `"'`)
		switch normalized {
		case "--dangerously-bypass-approvals-and-sandbox":
			mode = "--dangerously-bypass-approvals-and-sandbox"
			continue
		case "--yolo":
			if mode == "" {
				mode = "--yolo"
			}
			continue
		case "-c", "--config":
			if i+1 < len(fields) && isLegacyCodexConfigValue(fields[i+1]) {
				i++
				continue
			}
		}
		extras = append(extras, token)
	}
	if mode == "" {
		mode = "--yolo"
	}
	result := []string{fields[0], mode}
	result = append(result, extras...)
	return strings.Join(result, " ")
}

func isLegacyCodexConfigValue(raw string) bool {
	value := strings.Trim(raw, `"'`)
	return value == "check_for_update_on_startup=false" ||
		value == "trust_level=trusted" ||
		strings.Contains(value, `.trust_level="trusted"`)
}

func stabilizeProjectScopedCLICommand(cliType, launch, workDir string) string {
	return strings.TrimSpace(launch)
}

// defaultCLICommandForType returns CLI binary fallback when script_command is omitted.
func defaultCLICommandForType(cliType string) string {
	switch strings.TrimSpace(strings.ToLower(cliType)) {
	case "claudecode", "claude":
		return "claude --dangerously-skip-permissions"
	case "codex":
		return "codex --yolo"
	case "cursor":
		return "cursor"
	default:
		return strings.TrimSpace(strings.ToLower(cliType))
	}
}

// shouldExecLaunchCommand returns true when launch command is a direct CLI command.
// Script path/complex shell expressions keep original form to avoid changing behavior.
func shouldExecLaunchCommand(launch string) bool {
	launch = strings.TrimSpace(launch)
	if launch == "" {
		return false
	}
	// Keep complex shell expressions intact (do not inject exec).
	if hasTopLevelShellOperator(launch) {
		return false
	}

	fields := strings.Fields(launch)
	if len(fields) == 0 {
		return false
	}
	first := unquoteShellToken(fields[0])
	lower := strings.ToLower(first)

	// Path-like/script-like launch commands are treated as scripts.
	if strings.HasPrefix(first, "/") || strings.HasPrefix(first, "./") || strings.HasPrefix(first, "../") {
		return false
	}
	if strings.Contains(first, "/") {
		return false
	}
	if strings.HasSuffix(lower, ".sh") || strings.HasSuffix(lower, ".bash") || strings.HasSuffix(lower, ".zsh") {
		return false
	}
	if (lower == "sh" || lower == "bash" || lower == "zsh") && len(fields) > 1 {
		next := strings.ToLower(unquoteShellToken(fields[1]))
		if strings.HasPrefix(next, "/") || strings.HasPrefix(next, "./") || strings.HasPrefix(next, "../") {
			return false
		}
		if strings.HasSuffix(next, ".sh") || strings.HasSuffix(next, ".bash") || strings.HasSuffix(next, ".zsh") {
			return false
		}
	}

	return true
}

func hasTopLevelShellOperator(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	for index := 0; index < len(command); index++ {
		ch := command[index]
		if escaped {
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			if !inSingleQuote {
				escaped = true
			}
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		}
		if inSingleQuote || inDoubleQuote {
			continue
		}
		if index+1 < len(command) {
			token := command[index : index+2]
			if token == "&&" || token == "||" {
				return true
			}
		}
		if strings.ContainsRune(";|<>", rune(ch)) {
			return true
		}
	}
	return false
}

// unquoteShellToken trims one optional single/double quoted shell token.
func unquoteShellToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) < 2 {
		return token
	}
	if (strings.HasPrefix(token, "'") && strings.HasSuffix(token, "'")) ||
		(strings.HasPrefix(token, `"`) && strings.HasSuffix(token, `"`)) {
		return token[1 : len(token)-1]
	}
	return token
}

// resolveProfileEnv resolves profile env map and supports ${ENV_NAME} external references.
func resolveProfileEnv(raw map[string]string, cliType, profileID string) (map[string]string, error) {
	logger := cliSessionLogger()
	cliType = strings.TrimSpace(strings.ToLower(cliType))
	profileID = strings.TrimSpace(profileID)
	if len(raw) == 0 {
		logger.Debug("profile env is empty")
		return nil, nil
	}
	logger.Debug("resolve profile env", zap.Int("raw_env_count", len(raw)))

	out := make(map[string]string, len(raw))
	for key, value := range raw {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		resolved := strings.TrimSpace(value)
		if externalName, ok := parseExternalEnvReference(resolved); ok {
			logger.Debug(
				"resolve external env reference",
				zap.String("cli_type", cliType),
				zap.String("profile", profileID),
				zap.String("target_env", name),
				zap.String("external_env", externalName),
			)
			externalValue := strings.TrimSpace(os.Getenv(externalName))
			if externalValue == "" {
				fixExample := fmt.Sprintf("export %s='<TOKEN>'", externalName)
				logger.Error(
					"resolve profile env blocked: missing external env",
					zap.String("cli_type", cliType),
					zap.String("profile", profileID),
					zap.String("target_env", name),
					zap.String("external_env", externalName),
					zap.String("action", "inject required environment variable before creating session"),
					zap.String("example", fixExample),
				)
				return nil, fmt.Errorf(
					"missing external env %s for %s (profile %s/%s); set %s before creating session, e.g. `%s`",
					externalName,
					name,
					cliType,
					profileID,
					externalName,
					fixExample,
				)
			}
			resolved = externalValue
		}
		out[name] = resolved
	}
	if len(out) == 0 {
		logger.Debug("profile env resolved to empty map")
		return nil, nil
	}
	logger.Info(
		"profile env resolved",
		appendTokenInjectionFields([]zap.Field{
			zap.Int("resolved_env_count", len(out)),
		}, out)...,
	)
	return out, nil
}

// parseExternalEnvReference parses `${ENV_NAME}` marker and returns referenced env key.
func parseExternalEnvReference(value string) (string, bool) {
	value = strings.TrimSpace(value)
	match := externalEnvRefPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

// normalizeSessionState maps runtime states to persistence state enum.
func normalizeSessionState(runtimeState string) string {
	runtimeState = strings.TrimSpace(strings.ToLower(runtimeState))
	switch runtimeState {
	case "running":
		return domain.CLISessionStateRunning
	case "stopped", "exited", domain.CLISessionStateTerminated:
		return domain.CLISessionStateTerminated
	default:
		if runtimeState == "" {
			return domain.CLISessionStateTerminated
		}
		return runtimeState
	}
}

// fallbackWorkDir returns fallback path when runtime summary has no workdir value.
func fallbackWorkDir(workDir, appRoot string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir != "" {
		return workDir
	}
	if strings.TrimSpace(appRoot) != "" {
		return appRoot
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

// fallbackText returns first non-empty string.
func fallbackText(primary, secondary string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return strings.TrimSpace(secondary)
}

// shellQuote safely quotes one string for shell usage.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
