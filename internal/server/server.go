package server

import (
	"auto-code/internal/embedfs"
	"auto-code/internal/gitops"
	"auto-code/internal/logging"
	runtimex "auto-code/internal/runtime"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// App wires all infrastructure and service dependencies used by HTTP handlers.
type App struct {
	cliMgr             *CLISessionManager
	cliArchive         *CLIOutputArchive
	gitOpsClient       gitops.Client
	store              *SQLiteStore
	projectSvc         *ProjectService
	requirementSvc     *RequirementService
	workflowSvc        *WorkflowService
	projectFileSvc     *ProjectFileService
	cliSessionSvc      *CLISessionService
	requirementAuto    *RequirementAutomationCoordinator
	frontendEmbeddedFS fs.FS
	frontendDistDir    string
	defaultCLICommand  string
	authSvc            *AuthService
	backgroundStopCh   chan struct{}
	backgroundWG       sync.WaitGroup
	closeOnce          sync.Once
}

// runServer initializes dependencies and starts HTTP server.
func runServer() error {
	cfg, err := LoadAppConfig()
	if err != nil {
		return err
	}

	app, activeProcessorMode, err := bootstrapApp(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if app != nil {
			app.Close()
		}
	}()

	server := newHTTPServer(cfg.Port, buildHTTPMux(app))
	logAppStartup(cfg, app, activeProcessorMode)
	logger := logging.Named("server.lifecycle")
	cleanupOnce := &sync.Once{}
	cleanup := func(reason string) {
		cleanupOnce.Do(func() {
			if app == nil {
				return
			}
			app.shutdownRuntimeSessions(reason)
		})
	}
	defer cleanup("run-server-exit")

	signalCtx, signalStop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer signalStop()

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.ListenAndServe()
	}()

	select {
	case err = <-serverErrCh:
		cleanup("listen-return")
	case <-signalCtx.Done():
		logger.Info("shutdown signal received, stopping server")
		cleanup("signal")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
			logger.Warn("http server shutdown returned error", zap.Error(shutdownErr))
		}
		cancel()
		err = <-serverErrCh
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// bootstrapApp prepares runtime storage and service dependencies.
func bootstrapApp(cfg AppConfig) (*App, string, error) {
	if err := prepareAppStorage(cfg.DataDir); err != nil {
		return nil, "", err
	}

	appRoot, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	store, err := NewSQLiteStore(filepath.Join(cfg.DataDir, "auto-code.db"), appRoot)
	if err != nil {
		return nil, "", fmt.Errorf("init sqlite store: %w", err)
	}

	cliArchive, err := NewCLIOutputArchive(cfg.CLIOutputArchiveDir, cfg.CLIOutputArchiveLimit)
	if err != nil {
		return nil, "", fmt.Errorf("init cli output archive: %w", err)
	}
	frontendEmbeddedFS, err := embedfs.FrontendDist()
	if err != nil {
		return nil, "", fmt.Errorf("get embedded frontend dist: %w", err)
	}

	cliMgr := NewCLISessionManager(cfg.DefaultCLICommand)
	maxSessionSeq, err := store.MaxCLISessionSequence()
	if err != nil {
		return nil, "", fmt.Errorf("query max cli session sequence: %w", err)
	}
	cliMgr.EnsureNextIDAtLeast(maxSessionSeq)
	projectSvc := NewProjectService(store, appRoot)
	requirementSvc := NewRequirementService(store, projectSvc)
	workflowSvc := NewWorkflowService(store, projectSvc, filepath.Join(cfg.DataDir, "workflow-artifacts"))
	requirementSvc.SetWorkflowService(workflowSvc)
	projectFileSvc := NewProjectFileService(projectSvc, requirementSvc)
	cliSessionSvc := NewCLISessionService(store, projectSvc, requirementSvc, cfg.ProfileRegistry, cliMgr, appRoot)

	app := &App{
		cliMgr:             cliMgr,
		cliArchive:         cliArchive,
		gitOpsClient:       gitops.NewCLIClient(5 * time.Second),
		store:              store,
		projectSvc:         projectSvc,
		requirementSvc:     requirementSvc,
		workflowSvc:        workflowSvc,
		projectFileSvc:     projectFileSvc,
		cliSessionSvc:      cliSessionSvc,
		frontendEmbeddedFS: frontendEmbeddedFS,
		frontendDistDir:    filepath.Join(appRoot, "frontend", "dist"),
		defaultCLICommand:  cfg.DefaultCLICommand,
		authSvc:            NewAuthService(cfg.Auth),
		backgroundStopCh:   make(chan struct{}),
	}
	app.requirementAuto = NewRequirementAutomationCoordinator(app, cfg.Automation)
	activeProcessorMode := configureCLIOutputProcessing(app.cliMgr, cfg.CLIOutputProcessorsMode)
	if err := app.reconcilePersistedCLISessionsOnStartup(); err != nil {
		return nil, "", err
	}
	app.startCLIArchivePump()
	if app.requirementAuto != nil {
		app.requirementAuto.Start()
	}
	return app, activeProcessorMode, nil
}

// buildHTTPMux registers all application routes.
func buildHTTPMux(app *App) http.Handler {
	mux := http.NewServeMux()

	// Public routes (no auth required)
	mux.HandleFunc("/login", app.handleLoginPage)
	mux.HandleFunc("/api/auth/login", app.handleAPILogin)
	mux.HandleFunc("/api/auth/logout", app.handleAPILogout)
	mux.HandleFunc("/api/auth/status", app.handleAPIAuthStatus)
	mux.HandleFunc("/app", app.handleFrontendApp)
	mux.HandleFunc("/app/", app.handleFrontendApp)

	// Protected routes (auth required)
	mux.HandleFunc("/", app.handleIndex)
	mux.HandleFunc("/api/projects/list", app.handleAPIProjectList)
	mux.HandleFunc("/api/projects/create", app.handleAPIProjectCreate)
	mux.HandleFunc("/api/projects/delete/", app.handleAPIProjectDelete)
	mux.HandleFunc("/api/projects/", app.handleAPIProjectScopedRoutes)
	mux.HandleFunc("/api/requirements/list", app.handleAPIRequirementList)
	mux.HandleFunc("/api/requirements/create", app.handleAPIRequirementCreate)
	mux.HandleFunc("/api/requirements/delete/", app.handleAPIRequirementDelete)
	mux.HandleFunc("/api/git/query", app.handleAPIGitQuery)
	mux.HandleFunc("/api/cli/profiles", app.handleAPICLIProfiles)
	mux.HandleFunc("/api/v1/dashboard/stats", app.handleAPIV1DashboardStats)
	mux.HandleFunc("/api/v1/dashboard/activities", app.handleAPIV1DashboardActivities)
	mux.HandleFunc("/api/v1/projects", app.handleAPIV1Projects)
	mux.HandleFunc("/api/v1/projects/", app.handleAPIV1ProjectByID)
	mux.HandleFunc("/api/v1/requirements", app.handleAPIV1Requirements)
	mux.HandleFunc("/api/v1/requirements/", app.handleAPIV1RequirementByID)
	mux.HandleFunc("/api/v1/solution/templates", app.handleAPIV1SolutionTemplates)
	mux.HandleFunc("/api/v1/solution/bootstrap", app.handleAPIV1SolutionBootstrap)
	mux.HandleFunc("/api/v1/sessions", app.handleAPIV1Sessions)
	mux.HandleFunc("/api/v1/sessions/", app.handleAPIV1SessionByID)
	mux.HandleFunc("/api/v1/workflows", app.handleAPIV1Workflows)
	mux.HandleFunc("/api/v1/workflows/", app.handleAPIV1WorkflowByID)
	mux.HandleFunc("/api/v1/stages/", app.handleAPIV1StageByID)
	mux.HandleFunc("/api/v1/reviews", app.handleAPIV1Reviews)
	mux.HandleFunc("/api/v1/reviews/", app.handleAPIV1ReviewByID)
	mux.HandleFunc("/api/v1/changesets", app.handleAPIV1ChangeSets)
	mux.HandleFunc("/api/v1/changesets/", app.handleAPIV1ChangeSetByID)
	mux.HandleFunc("/api/v1/artifacts", app.handleAPIV1Artifacts)
	mux.HandleFunc("/api/v1/artifacts/", app.handleAPIV1ArtifactByID)
	mux.HandleFunc("/api/v1/decisions", app.handleAPIV1Decisions)
	mux.HandleFunc("/api/v1/decisions/", app.handleAPIV1DecisionByID)
	mux.HandleFunc("/api/v1/snapshots", app.handleAPIV1Snapshots)
	mux.HandleFunc("/api/v1/automation/status", app.handleAPIAutomationStatus)
	mux.HandleFunc("/cli/sessions", app.handleCLISessions)
	mux.HandleFunc("/cli/sessions/", app.handleCLISessionSubRoutes)
	mux.HandleFunc("/cli/events", app.handleCLIEvents)

	// Wrap with auth middleware and logging middleware
	handler := AuthMiddleware(app.authSvc, mux)
	return loggingMiddleware(handler)
}

// newHTTPServer builds a safe default net/http server.
func newHTTPServer(port string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// logAppStartup writes startup diagnostics through structured logger.
func logAppStartup(cfg AppConfig, app *App, activeProcessorMode string) {
	logger := logging.Named("server")
	logger.Info("server started", zap.String("url", "http://127.0.0.1:"+cfg.Port))
	logger.Info("cli default command", zap.String("command", cfg.DefaultCLICommand))
	logger.Info("cli output processors configured", zap.String("mode", activeProcessorMode))
	if cfg.ConfigPath != "" {
		logger.Info("app config loaded", zap.String("path", cfg.ConfigPath))
	}
	if app != nil && app.cliArchive != nil {
		logger.Info(
			"cli output archive ready",
			zap.String("root", app.cliArchive.Root()),
			zap.Int("limit", app.cliArchive.MaxEntries()),
		)
	}
	if app != nil && app.authSvc != nil && app.authSvc.Enabled() {
		logger.Info("authentication enabled", zap.String("username", cfg.Auth.Username))
	}
}

// prepareAppStorage creates runtime directories.
func prepareAppStorage(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	return nil
}

// derefInt safely dereferences optional integer pointer values.
func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// configureCLIOutputProcessing applies output processor pipeline mode.
func configureCLIOutputProcessing(manager *CLISessionManager, mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "default"
	}
	if manager == nil {
		if mode == "none" || mode == "raw" {
			return "raw"
		}
		return mode
	}

	switch mode {
	case "none", "raw":
		return "raw"
	case "default":
		events := manager.Events()
		if events == nil {
			return "default"
		}
		for _, processor := range DefaultCLIOutputProcessors() {
			events.AddProcessor(processor)
		}
		return "default"
	default:
		logging.Named("server").Warn(
			"unknown CLI_OUTPUT_PROCESSORS mode, fallback to raw",
			zap.String("mode", mode),
		)
		return "raw"
	}
}

// loggingMiddleware logs HTTP request latency.
func loggingMiddleware(next http.Handler) http.Handler {
	logger := logging.Named("server.http")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info(
			"http request completed",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Duration("latency", time.Since(start)),
		)
	})
}

// startCLIArchivePump subscribes to output events and appends them to archive.
func (a *App) startCLIArchivePump() {
	if a == nil || a.cliMgr == nil || a.cliArchive == nil {
		return
	}
	events := a.cliMgr.Events()
	if events == nil {
		return
	}
	subscription := events.Subscribe(CLIEventFilter{}, 0)
	a.backgroundWG.Add(1)
	go func() {
		defer a.backgroundWG.Done()
		defer subscription.Close()
		for {
			select {
			case <-a.backgroundStopCh:
				return
			case event, ok := <-subscription.Events:
				if !ok {
					return
				}
				if event.Type != "output" {
					continue
				}
				if err := a.cliArchive.Append(event); err != nil {
					logging.Named("server.cli-archive").Error(
						"archive cli output event failed",
						zap.String("session_id", event.SessionID),
						zap.Int64("seq", event.Seq),
						zap.Error(err),
					)
				}
			}
		}
	}()
}

// Close stops background workers and closes durable resources.
func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		if a.requirementAuto != nil {
			a.requirementAuto.Stop()
		}
		if a.backgroundStopCh != nil {
			close(a.backgroundStopCh)
		}
		a.backgroundWG.Wait()
		if a.store != nil {
			_ = a.store.Close()
		}
	})
}

// reconcilePersistedCLISessionsOnStartup closes stale processes left from previous service instance.
func (a *App) reconcilePersistedCLISessionsOnStartup() error {
	if a == nil || a.store == nil {
		return nil
	}
	logger := logging.Named("server.cli-reconcile")
	records, err := a.store.ListCLISessionRecords("")
	if err != nil {
		return fmt.Errorf("list cli sessions for startup reconcile: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	reconciled := 0
	killOK := 0
	killFailed := 0
	for _, record := range records {
		state := strings.TrimSpace(strings.ToLower(record.SessionState))
		hasProcessMeta := record.ProcessPID > 0 || record.ProcessPGID > 0
		shouldReconcile := state == CLISessionStateRunning || hasProcessMeta
		if !shouldReconcile {
			continue
		}
		reconciled++
		if hasProcessMeta {
			if killErr := runtimex.TerminateDetachedProcess(record.ProcessPID, record.ProcessPGID); killErr != nil {
				killFailed++
				logger.Warn(
					"terminate detached cli process failed",
					zap.String("session_id", record.ID),
					zap.Int("process_pid", record.ProcessPID),
					zap.Int("process_pgid", record.ProcessPGID),
					zap.Error(killErr),
				)
			} else {
				killOK++
			}
		}
		if err := a.store.UpdateCLISessionRuntime(
			record.ID,
			CLISessionStateTerminated,
			record.LaunchMode,
			0,
			0,
			time.Now(),
		); err != nil && !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("mark stale cli session terminated (%s): %w", record.ID, err)
		}
	}

	if reconciled > 0 {
		logger.Info(
			"startup cli session reconcile completed",
			zap.Int("records_total", len(records)),
			zap.Int("reconciled", reconciled),
			zap.Int("kill_ok", killOK),
			zap.Int("kill_failed", killFailed),
		)
	}
	return nil
}

// shutdownRuntimeSessions terminates all in-memory runtime sessions and persists terminated state.
func (a *App) shutdownRuntimeSessions(reason string) {
	if a == nil || a.cliMgr == nil || a.store == nil {
		return
	}
	logger := logging.Named("server.cli-shutdown")
	summaries := a.cliMgr.List()
	if len(summaries) == 0 {
		return
	}
	terminated := 0
	for _, summary := range summaries {
		sessionID := strings.TrimSpace(summary.ID)
		if sessionID == "" {
			continue
		}
		if err := a.cliMgr.Destroy(sessionID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			logger.Warn(
				"destroy runtime session during shutdown failed",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
		}
		if err := a.store.UpdateCLISessionRuntime(
			sessionID,
			CLISessionStateTerminated,
			summary.LaunchMode,
			0,
			0,
			time.Now(),
		); err != nil && !errors.Is(err, ErrNotFound) {
			logger.Warn(
				"persist terminated cli session during shutdown failed",
				zap.String("session_id", sessionID),
				zap.Error(err),
			)
			continue
		}
		terminated++
	}
	logger.Info(
		"runtime sessions shutdown completed",
		zap.String("reason", strings.TrimSpace(reason)),
		zap.Int("terminated", terminated),
	)
}
