package server

import (
	"auto-code/internal/logging"
	"auto-code/internal/service"

	"go.uber.org/zap"
)

// cliRuntimeManagerAdapter bridges app-layer CLISessionManager to service.RuntimeManager.
type cliRuntimeManagerAdapter struct {
	manager *CLISessionManager
}

// appendRuntimeEnvInjectionFields appends token injection diagnostics to runtime logs.
func appendRuntimeEnvInjectionFields(fields []zap.Field, env map[string]string) []zap.Field {
	summary := logging.SummarizeEnvInjection(env)
	return append(
		fields,
		zap.Bool("token_env_configured", summary.TokenEnvConfigured),
		zap.Bool("token_injected", summary.TokenInjected),
		zap.Strings("token_env_injected_keys", summary.InjectedTokenKeys),
		zap.Strings("token_env_empty_keys", summary.EmptyTokenKeys),
	)
}

// newCLIRuntimeManagerAdapter builds a runtime manager adapter.
func newCLIRuntimeManagerAdapter(manager *CLISessionManager) service.RuntimeManager {
	if manager == nil {
		return nil
	}
	return &cliRuntimeManagerAdapter{manager: manager}
}

// Create starts a session with command override.
func (a *cliRuntimeManagerAdapter) Create(command string) (service.RuntimeSession, error) {
	return a.CreateWithSize(command, 0, 0)
}

// CreateWithSize starts a session with command override and explicit initial PTY size.
func (a *cliRuntimeManagerAdapter) CreateWithSize(command string, cols, rows int) (service.RuntimeSession, error) {
	logger := logging.Named("runtime.adapter")
	logger.Info(
		"create runtime session",
		zap.String("command", command),
		zap.Int("cols", cols),
		zap.Int("rows", rows),
	)
	session, err := a.manager.CreateWithSize(command, cols, rows)
	if err != nil {
		logger.Error(
			"create runtime session failed",
			zap.String("command", command),
			zap.Int("cols", cols),
			zap.Int("rows", rows),
			zap.Error(err),
		)
		return nil, err
	}
	logger.Info(
		"runtime session created",
		zap.String("session_id", session.ID),
		zap.String("agent_id", session.AgentID),
	)
	return &cliRuntimeSessionAdapter{session: session}, nil
}

// CreateWithWorkDir starts a session with command override and work directory.
func (a *cliRuntimeManagerAdapter) CreateWithWorkDir(command, workDir string) (service.RuntimeSession, error) {
	return a.CreateWithWorkDirAndEnvAndSize(command, workDir, nil, 0, 0)
}

// CreateWithWorkDirAndEnv starts a session with command/work directory and environment overrides.
func (a *cliRuntimeManagerAdapter) CreateWithWorkDirAndEnv(command, workDir string, env map[string]string) (service.RuntimeSession, error) {
	return a.CreateWithWorkDirAndEnvAndSize(command, workDir, env, 0, 0)
}

// CreateWithWorkDirAndEnvAndSize starts a session with command/work directory, env overrides, and initial PTY size.
func (a *cliRuntimeManagerAdapter) CreateWithWorkDirAndEnvAndSize(command, workDir string, env map[string]string, cols, rows int) (service.RuntimeSession, error) {
	logger := logging.Named("runtime.adapter")
	logger.Info(
		"create runtime session with environment",
		appendRuntimeEnvInjectionFields([]zap.Field{
			zap.String("command", command),
			zap.String("work_dir", workDir),
			zap.Int("env_count", len(env)),
			zap.Int("cols", cols),
			zap.Int("rows", rows),
		}, env)...,
	)
	session, err := a.manager.CreateWithWorkDirAndEnvAndSize(command, workDir, env, cols, rows)
	if err != nil {
		logger.Error(
			"create runtime session with environment failed",
			appendRuntimeEnvInjectionFields([]zap.Field{
				zap.String("command", command),
				zap.String("work_dir", workDir),
				zap.Int("env_count", len(env)),
				zap.Int("cols", cols),
				zap.Int("rows", rows),
				zap.Error(err),
			}, env)...,
		)
		return nil, err
	}
	logger.Info(
		"runtime session with environment created",
		zap.String("session_id", session.ID),
		zap.String("agent_id", session.AgentID),
	)
	return &cliRuntimeSessionAdapter{session: session}, nil
}

// CreateWithIdentity starts or restores a runtime session using fixed session/agent identifiers.
func (a *cliRuntimeManagerAdapter) CreateWithIdentity(sessionID, agentID, command, workDir string) (service.RuntimeSession, error) {
	return a.CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir, nil, 0, 0)
}

// CreateWithIdentityAndEnv starts or restores one runtime using fixed session identity and env overrides.
func (a *cliRuntimeManagerAdapter) CreateWithIdentityAndEnv(sessionID, agentID, command, workDir string, env map[string]string) (service.RuntimeSession, error) {
	return a.CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir, env, 0, 0)
}

// CreateWithIdentityAndEnvAndSize starts or restores one runtime using fixed session identity and explicit initial PTY size.
func (a *cliRuntimeManagerAdapter) CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir string, env map[string]string, cols, rows int) (service.RuntimeSession, error) {
	logger := logging.Named("runtime.adapter")
	logger.Info(
		"create runtime session with identity and environment",
		appendRuntimeEnvInjectionFields([]zap.Field{
			zap.String("session_id", sessionID),
			zap.String("agent_id", agentID),
			zap.String("command", command),
			zap.String("work_dir", workDir),
			zap.Int("env_count", len(env)),
			zap.Int("cols", cols),
			zap.Int("rows", rows),
		}, env)...,
	)
	session, err := a.manager.CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir, env, cols, rows)
	if err != nil {
		logger.Error(
			"create runtime session with identity and environment failed",
			appendRuntimeEnvInjectionFields([]zap.Field{
				zap.String("session_id", sessionID),
				zap.String("agent_id", agentID),
				zap.String("command", command),
				zap.String("work_dir", workDir),
				zap.Int("env_count", len(env)),
				zap.Int("cols", cols),
				zap.Int("rows", rows),
				zap.Error(err),
			}, env)...,
		)
		return nil, err
	}
	logger.Info(
		"runtime session with identity and environment created",
		zap.String("session_id", session.ID),
		zap.String("agent_id", session.AgentID),
	)
	return &cliRuntimeSessionAdapter{session: session}, nil
}

// Destroy removes one runtime session from manager and terminates process if still running.
func (a *cliRuntimeManagerAdapter) Destroy(sessionID string) error {
	return a.manager.Destroy(sessionID)
}

// Get returns one runtime session by ID.
func (a *cliRuntimeManagerAdapter) Get(sessionID string) (service.RuntimeSession, bool) {
	session, ok := a.manager.Get(sessionID)
	if !ok {
		return nil, false
	}
	return &cliRuntimeSessionAdapter{session: session}, true
}

// List returns runtime summaries for all sessions.
func (a *cliRuntimeManagerAdapter) List() []service.RuntimeSessionSummary {
	raw := a.manager.List()
	list := make([]service.RuntimeSessionSummary, 0, len(raw))
	for _, summary := range raw {
		list = append(list, service.RuntimeSessionSummary{
			ID:          summary.ID,
			AgentID:     summary.AgentID,
			Command:     summary.Command,
			State:       summary.State,
			LaunchMode:  summary.LaunchMode,
			WorkDir:     summary.WorkDir,
			ProcessPID:  summary.ProcessPID,
			ProcessPGID: summary.ProcessPGID,
			CreatedAt:   summary.CreatedAt,
			UpdatedAt:   summary.UpdatedAt,
			ExitCode:    summary.ExitCode,
			LastError:   summary.LastError,
		})
	}
	return list
}

// cliRuntimeSessionAdapter bridges app-layer CLISession to service.RuntimeSession.
type cliRuntimeSessionAdapter struct {
	session *CLISession
}

// ID returns runtime session ID.
func (a *cliRuntimeSessionAdapter) ID() string {
	if a == nil || a.session == nil {
		return ""
	}
	return a.session.ID
}

// AgentID returns runtime session agent id.
func (a *cliRuntimeSessionAdapter) AgentID() string {
	if a == nil || a.session == nil {
		return ""
	}
	return a.session.AgentID
}

// Summary returns runtime session summary.
func (a *cliRuntimeSessionAdapter) Summary() service.RuntimeSessionSummary {
	if a == nil || a.session == nil {
		return service.RuntimeSessionSummary{}
	}
	summary := a.session.Summary()
	return service.RuntimeSessionSummary{
		ID:          summary.ID,
		AgentID:     summary.AgentID,
		Command:     summary.Command,
		State:       summary.State,
		LaunchMode:  summary.LaunchMode,
		WorkDir:     summary.WorkDir,
		ProcessPID:  summary.ProcessPID,
		ProcessPGID: summary.ProcessPGID,
		CreatedAt:   summary.CreatedAt,
		UpdatedAt:   summary.UpdatedAt,
		ExitCode:    summary.ExitCode,
		LastError:   summary.LastError,
	}
}

// WriteInput forwards user input to runtime session.
func (a *cliRuntimeSessionAdapter) WriteInput(text string) error {
	if a == nil || a.session == nil {
		return errSessionNotRunning
	}
	return a.session.WriteInput(text)
}
