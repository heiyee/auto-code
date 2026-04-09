package server

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"auto-code/internal/logging"

	"go.uber.org/zap"
)

const (
	requirementAutomationCompletionMarker = "所有任务全部完成"
	requirementAutomationCompletionPrompt = "当你确认全部任务已完成后，请在最后单独输出一行：所有任务全部完成"
	requirementAutomationScanInterval     = 3 * time.Second
	requirementAutomationPendingTTL       = 30 * time.Second
	requirementAutomationConsumedTTL      = requirementAutomationPendingTTL
	requirementAutomationMaxBufferBytes   = 256 * 1024
	requirementAutomationReplayCooldown   = 6 * time.Second
	requirementAutomationNoProgressDelay  = 8 * time.Second
	requirementAutomationMaxReplayCount   = 6
	requirementAutomationNoOutputTimeout  = 10 * time.Minute
)

const (
	requirementAutomationHealthRunning              = "running"
	requirementAutomationHealthAwaitingConfirmation = "awaiting_confirmation"
	requirementAutomationHealthStalledNoOutput      = "stalled_no_output"
	requirementAutomationHealthStalledNetwork       = "stalled_network"
	requirementAutomationHealthStalledQuota         = "stalled_quota"
	requirementAutomationHealthStalledInterrupted   = "stalled_interrupted"
	requirementAutomationHealthFailed               = "failed"
	requirementAutomationHealthCompleted            = "completed"
)

var requirementAutomationCompletionRegex = regexp.MustCompile(`(?m)^[\s"'“”‘’]*所有任务全部完成[\s"'“”‘’。！!]*$`)
var requirementAutomationCompletionPrefixRegex = regexp.MustCompile(`^\s*(?:(?:[-*#>•]+|\d+[\.\)、])\s*)+`)

var requirementAutomationCompletionVariants = map[string]struct{}{
	"所有任务全部完成":   {},
	"所有任务已完成":    {},
	"所有任务已全部完成":  {},
	"所有任务已经完成":   {},
	"所有任务已经全部完成": {},
	"全部任务已完成":    {},
	"全部任务已经完成":   {},
	"任务已全部完成":    {},
	"任务已经全部完成":   {},
	"已完成所有任务":    {},
}

const (
	automationDispatchBlockedNone                 = ""
	automationDispatchBlockedNoSessionBound       = "no_session_bound"
	automationDispatchBlockedSessionNotRunning    = "session_not_running"
	automationDispatchBlockedWaitingReconnect     = "waiting_reconnect"
	automationDispatchBlockedRetryBudgetExhausted = "dispatch_retry_budget_exhausted"
	automationDispatchBlockedPromptDispatchFailed = "prompt_dispatch_failed"
)

const (
	automationRetryReasonWatchdogResend      = "watchdog_resend_requirement"
	automationRetryReasonWatchdogRebuild     = "watchdog_close_and_resend_requirement"
	automationRetryReasonSessionRecovery     = "session_rebuild_recovery"
	automationRetryReasonSchedulerRedispatch = "scheduler_dispatch_retry"
	automationRetryReasonBudgetExhausted     = "retry_budget_exhausted"
)

type trackedOutboundInput struct {
	raw          string
	actualSent   string
	markerLines  []string
	contextLines []string
	recordedAt   time.Time
	consumedAt   time.Time
}

type requirementAutomationSessionState struct {
	cleanOutput           string
	pending               []trackedOutboundInput
	requirementID         string
	prompt                string
	outputBaselineOffset  int64
	lastPromptSentAt      time.Time
	lastOutputAt          time.Time
	lastMeaningfulAt      time.Time
	lastConfirmationAt    time.Time
	lastConfirmationKey   string
	lastReplayAt          time.Time
	replayCount           int
	replayPending         bool
	replayPendingReason   string
	dispatchBlockedReason string
	progressSeen          bool
	health                string
	healthReason          string
	healthUpdatedAt       time.Time
	lastReconnectAt       time.Time
	nextReconnectAfter    time.Time
	reconnectCount        int
}

type requirementAutomationRequirementState struct {
	dispatchAttempted     bool
	dispatchBlockedReason string
	updatedAt             time.Time
}

// RequirementAutomationCoordinator advances auto requirements for one project queue.
type RequirementAutomationCoordinator struct {
	app    *App
	config AutomationConfig

	mu                sync.Mutex
	syncMu            sync.Mutex
	sessionState      map[string]*requirementAutomationSessionState
	requirementState  map[string]*requirementAutomationRequirementState
	projectOwners     map[string]*requirementSessionOwner
	sessionOwners     map[string]string
	requirementOwners map[string]string
	projectSync       map[string]bool
	// completionPending tracks requirement IDs where the completion marker has been
	// seen in agent output but could not be acted upon due to active blockers.
	// TryCompleteRunningRequirement checks this before marking a requirement done.
	completionPending map[string]bool
	lastScanAt        time.Time
	nextScanAt        time.Time
	scanCount         int64

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewRequirementAutomationCoordinator builds the project requirement automation coordinator.
func NewRequirementAutomationCoordinator(app *App, cfg AutomationConfig) *RequirementAutomationCoordinator {
	if app == nil || app.requirementSvc == nil || app.cliSessionSvc == nil || app.cliMgr == nil {
		return nil
	}
	if cfg.MaxRequirementRetryAttempts <= 0 {
		cfg.MaxRequirementRetryAttempts = 5
	}
	if cfg.ReconnectBaseSeconds <= 0 {
		cfg.ReconnectBaseSeconds = 15
	}
	if cfg.ReconnectMaxSeconds <= 0 {
		cfg.ReconnectMaxSeconds = 600
	}
	return &RequirementAutomationCoordinator{
		app:               app,
		config:            cfg,
		sessionState:      make(map[string]*requirementAutomationSessionState),
		requirementState:  make(map[string]*requirementAutomationRequirementState),
		projectOwners:     make(map[string]*requirementSessionOwner),
		sessionOwners:     make(map[string]string),
		requirementOwners: make(map[string]string),
		projectSync:       make(map[string]bool),
		completionPending: make(map[string]bool),
		stopCh:            make(chan struct{}),
	}
}

func requirementAutomationLogger() *zap.Logger {
	return logging.Named("server.requirement-auto")
}

// Start launches background event processing and periodic queue syncing.
func (c *RequirementAutomationCoordinator) Start() {
	if c == nil || c.app == nil || c.app.cliMgr == nil {
		return
	}
	c.startOnce.Do(func() {
		c.wg.Add(2)
		go c.runOutputWatcher()
		go c.runScheduler()
	})
}

// Stop terminates background workers.
func (c *RequirementAutomationCoordinator) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.wg.Wait()
	})
}

// TrackOutboundInput records text written by us so echoed input is not mistaken for AI completion.
func (c *RequirementAutomationCoordinator) TrackOutboundInput(sessionID, text string) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	record := buildTrackedOutboundInput(text, c.normalizeTrackedOutboundActualSent(sessionID, text))
	if sessionID == "" || record == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureSessionStateLocked(sessionID)
	state.pending = append(state.pending, *record)
	if len(state.pending) > 16 {
		state.pending = append([]trackedOutboundInput(nil), state.pending[len(state.pending)-16:]...)
	}
}

func (c *RequirementAutomationCoordinator) normalizeTrackedOutboundActualSent(sessionID, text string) string {
	actualSent := strings.TrimSpace(normalizeRequirementAutomationText(text))
	if actualSent == "" {
		return ""
	}
	if c == nil || c.app == nil || c.app.cliMgr == nil {
		return actualSent
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return actualSent
	}
	if session, ok := c.app.cliMgr.Get(sessionID); ok {
		if isCodexAutomationCommand(session.Summary().Command) {
			return normalizeRequirementAutomationText(normalizeAutomationCodexTypedInput(text))
		}
	}
	if c.app.cliSessionSvc != nil {
		if view, err := c.app.cliSessionSvc.GetView(sessionID); err == nil && strings.EqualFold(strings.TrimSpace(view.CLIType), "codex") {
			return normalizeRequirementAutomationText(normalizeAutomationCodexTypedInput(text))
		}
	}
	return actualSent
}

// RegisterRequirementSession binds one running requirement to a specific session for health tracking.
func (c *RequirementAutomationCoordinator) RegisterRequirementSession(requirement Requirement, sessionID string, at time.Time) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	prompt := buildRequirementDispatchPrompt(requirement)
	if strings.TrimSpace(prompt) == "" {
		return
	}
	c.setRequirementOwner(requirement, sessionID, at)
	c.bindRequirementSession(sessionID, requirement.ID, prompt, at, false)
}

func (c *RequirementAutomationCoordinator) bindRequirementSession(sessionID, requirementID, prompt string, at time.Time, replay bool) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	requirementID = strings.TrimSpace(requirementID)
	prompt = strings.TrimSpace(prompt)
	if sessionID == "" || requirementID == "" || prompt == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}

	outputBaselineOffset := int64(0)
	if c.app != nil && c.app.cliMgr != nil {
		if session, ok := c.app.cliMgr.Get(sessionID); ok && session != nil {
			_, outputBaselineOffset = session.WindowBytes(1)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.ensureSessionStateLocked(sessionID)
	requirementChanged := state.requirementID != requirementID
	if !requirementChanged && !replay && !state.lastPromptSentAt.IsZero() {
		if strings.TrimSpace(state.prompt) == "" {
			state.prompt = prompt
		}
		state.lastPromptSentAt = at
		if state.health == "" {
			state.health = requirementAutomationHealthRunning
			state.healthUpdatedAt = at
		}
		if state.lastOutputAt.IsZero() {
			state.lastOutputAt = at
		}
		return
	}
	if requirementChanged {
		state.cleanOutput = ""
		state.pending = nil
		state.outputBaselineOffset = outputBaselineOffset
		state.replayCount = 0
		state.lastReplayAt = time.Time{}
		state.lastConfirmationAt = time.Time{}
		state.lastConfirmationKey = ""
		state.lastReconnectAt = time.Time{}
		state.nextReconnectAfter = time.Time{}
		state.reconnectCount = 0
	}
	state.requirementID = requirementID
	state.prompt = prompt
	state.lastPromptSentAt = at
	state.lastOutputAt = at
	state.lastMeaningfulAt = time.Time{}
	state.progressSeen = false
	state.replayPending = false
	state.replayPendingReason = ""
	state.dispatchBlockedReason = ""
	state.health = requirementAutomationHealthRunning
	state.healthReason = ""
	state.healthUpdatedAt = at
	if replay {
		state.replayCount++
		state.lastReplayAt = at
	}
	if replay || requirementChanged {
		state.lastReconnectAt = time.Time{}
		state.nextReconnectAfter = time.Time{}
		state.reconnectCount = 0
	}
}

func (c *RequirementAutomationCoordinator) recordRequirementOutput(sessionID, chunk string, at time.Time) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	chunk = normalizeRequirementAutomationText(chunk)
	if chunk == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.ensureSessionStateLocked(sessionID)
	if !state.lastPromptSentAt.IsZero() && at.Before(state.lastPromptSentAt) {
		return
	}
	state.lastOutputAt = at
	if strings.TrimSpace(state.requirementID) == "" || strings.TrimSpace(state.prompt) == "" {
		return
	}
	if requirementAutomationOutputShowsProgress(chunk, state.prompt) {
		state.progressSeen = true
		state.lastMeaningfulAt = at
		state.pending = nil
	}
	state.dispatchBlockedReason = ""
	state.health = requirementAutomationHealthRunning
	state.healthReason = ""
	state.healthUpdatedAt = at
	state.lastReconnectAt = time.Time{}
	state.nextReconnectAfter = time.Time{}
	state.reconnectCount = 0
}

type requirementAutomationSessionSnapshot struct {
	RequirementID         string
	Prompt                string
	OutputBaselineOffset  int64
	LastPromptSentAt      time.Time
	LastOutputAt          time.Time
	LastMeaningfulAt      time.Time
	LastConfirmationAt    time.Time
	LastConfirmationKey   string
	LastReplayAt          time.Time
	ReplayCount           int
	ReplayPending         bool
	ReplayPendingReason   string
	DispatchBlockedReason string
	ProgressSeen          bool
	Health                string
	HealthReason          string
	HealthUpdatedAt       time.Time
	LastReconnectAt       time.Time
	NextReconnectAfter    time.Time
	ReconnectCount        int
}

func (c *RequirementAutomationCoordinator) getSessionSnapshot(sessionID string) requirementAutomationSessionSnapshot {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || c == nil {
		return requirementAutomationSessionSnapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureSessionStateLocked(sessionID)
	return requirementAutomationSessionSnapshot{
		RequirementID:         state.requirementID,
		Prompt:                state.prompt,
		OutputBaselineOffset:  state.outputBaselineOffset,
		LastPromptSentAt:      state.lastPromptSentAt,
		LastOutputAt:          state.lastOutputAt,
		LastMeaningfulAt:      state.lastMeaningfulAt,
		LastConfirmationAt:    state.lastConfirmationAt,
		LastConfirmationKey:   state.lastConfirmationKey,
		LastReplayAt:          state.lastReplayAt,
		ReplayCount:           state.replayCount,
		ReplayPending:         state.replayPending,
		ReplayPendingReason:   state.replayPendingReason,
		DispatchBlockedReason: state.dispatchBlockedReason,
		ProgressSeen:          state.progressSeen,
		Health:                state.health,
		HealthReason:          state.healthReason,
		HealthUpdatedAt:       state.healthUpdatedAt,
		LastReconnectAt:       state.lastReconnectAt,
		NextReconnectAfter:    state.nextReconnectAfter,
		ReconnectCount:        state.reconnectCount,
	}
}

func (c *RequirementAutomationCoordinator) reconnectBaseDelay() time.Duration {
	if c == nil || c.config.ReconnectBaseSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.config.ReconnectBaseSeconds) * time.Second
}

func (c *RequirementAutomationCoordinator) reconnectMaxDelay() time.Duration {
	if c == nil || c.config.ReconnectMaxSeconds <= 0 {
		return 10 * time.Minute
	}
	return time.Duration(c.config.ReconnectMaxSeconds) * time.Second
}

func (c *RequirementAutomationCoordinator) reconnectDelay(attempt int) time.Duration {
	base := c.reconnectBaseDelay()
	maxDelay := c.reconnectMaxDelay()
	if attempt <= 0 {
		return base
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func (c *RequirementAutomationCoordinator) maxRequirementRetryAttempts() int {
	if c == nil || c.config.MaxRequirementRetryAttempts <= 0 {
		return 5
	}
	return c.config.MaxRequirementRetryAttempts
}

func (c *RequirementAutomationCoordinator) ensureRequirementStateLocked(requirementID string) *requirementAutomationRequirementState {
	state, ok := c.requirementState[requirementID]
	if !ok {
		state = &requirementAutomationRequirementState{}
		c.requirementState[requirementID] = state
	}
	return state
}

func (c *RequirementAutomationCoordinator) noteRequirementDispatchAttempt(requirementID string) bool {
	if c == nil {
		return false
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureRequirementStateLocked(requirementID)
	alreadyAttempted := state.dispatchAttempted
	state.dispatchAttempted = true
	state.updatedAt = time.Now()
	return alreadyAttempted
}

func (c *RequirementAutomationCoordinator) setRequirementDispatchBlockedReason(requirementID, reason string) {
	if c == nil {
		return
	}
	requirementID = strings.TrimSpace(requirementID)
	reason = strings.TrimSpace(reason)
	if requirementID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureRequirementStateLocked(requirementID)
	state.dispatchBlockedReason = reason
	state.updatedAt = time.Now()
	for _, sessionState := range c.sessionState {
		if strings.TrimSpace(sessionState.requirementID) != requirementID {
			continue
		}
		sessionState.dispatchBlockedReason = reason
	}
}

func (c *RequirementAutomationCoordinator) clearRequirementDiagnostics(requirementID string) {
	if c == nil {
		return
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearRequirementOwnerByRequirementLocked(requirementID)
	delete(c.requirementState, requirementID)
	delete(c.completionPending, requirementID)
	for sessionID, sessionState := range c.sessionState {
		if strings.TrimSpace(sessionState.requirementID) != requirementID {
			continue
		}
		sessionState.dispatchBlockedReason = ""
		if sessionState.health == requirementAutomationHealthCompleted || sessionState.health == requirementAutomationHealthFailed {
			delete(c.sessionState, sessionID)
		}
	}
}

func (c *RequirementAutomationCoordinator) requirementDispatchBlockedReason(requirementID string) string {
	if c == nil {
		return ""
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.requirementState[requirementID]
	if !ok {
		return ""
	}
	return strings.TrimSpace(state.dispatchBlockedReason)
}

func (c *RequirementAutomationCoordinator) beginProjectSync(projectID string) bool {
	if c == nil {
		return false
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if c.projectSync[projectID] {
		return false
	}
	c.projectSync[projectID] = true
	return true
}

func (c *RequirementAutomationCoordinator) endProjectSync(projectID string) {
	if c == nil {
		return
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return
	}
	c.syncMu.Lock()
	delete(c.projectSync, projectID)
	c.syncMu.Unlock()
}

func (c *RequirementAutomationCoordinator) markConfirmationHandled(sessionID string, match *cliConfirmationMatch) bool {
	if c == nil || match == nil {
		return false
	}
	fingerprint := cliConfirmationFingerprint(match)
	if fingerprint == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureSessionStateLocked(sessionID)
	now := time.Now()
	if state.lastConfirmationKey == fingerprint && now.Sub(state.lastConfirmationAt) < 2*time.Second {
		return false
	}
	state.lastConfirmationKey = fingerprint
	state.lastConfirmationAt = now
	return true
}

func (c *RequirementAutomationCoordinator) markReplaySkipped(sessionID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureSessionStateLocked(sessionID)
	state.replayPending = false
	state.replayPendingReason = ""
}

func (c *RequirementAutomationCoordinator) updateSessionHealth(sessionID, health, reason string, at time.Time) {
	if c == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	health = strings.TrimSpace(health)
	reason = strings.TrimSpace(reason)
	if sessionID == "" || health == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureSessionStateLocked(sessionID)
	state.health = health
	state.healthReason = reason
	state.healthUpdatedAt = at
}

type RequirementExecutionHealth struct {
	SessionID     string
	RequirementID string
	State         string
	Reason        string
	LastOutputAt  time.Time
	LastUpdatedAt time.Time
}

func (c *RequirementAutomationCoordinator) GetSessionExecutionHealth(sessionID string) (RequirementExecutionHealth, bool) {
	snapshot := c.getSessionSnapshot(sessionID)
	if strings.TrimSpace(snapshot.RequirementID) == "" {
		return RequirementExecutionHealth{}, false
	}
	return RequirementExecutionHealth{
		SessionID:     strings.TrimSpace(sessionID),
		RequirementID: snapshot.RequirementID,
		State:         snapshot.Health,
		Reason:        snapshot.HealthReason,
		LastOutputAt:  snapshot.LastOutputAt,
		LastUpdatedAt: snapshot.HealthUpdatedAt,
	}, true
}

func (c *RequirementAutomationCoordinator) GetRequirementExecutionHealth(requirementID string) (RequirementExecutionHealth, bool) {
	if c == nil {
		return RequirementExecutionHealth{}, false
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return RequirementExecutionHealth{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for sessionID, state := range c.sessionState {
		if strings.TrimSpace(state.requirementID) != requirementID {
			continue
		}
		return RequirementExecutionHealth{
			SessionID:     sessionID,
			RequirementID: state.requirementID,
			State:         state.health,
			Reason:        state.healthReason,
			LastOutputAt:  state.lastOutputAt,
			LastUpdatedAt: state.healthUpdatedAt,
		}, true
	}
	return RequirementExecutionHealth{}, false
}

func (c *RequirementAutomationCoordinator) findTrackedSessionIDForRequirement(requirementID string) string {
	if c == nil {
		return ""
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for sessionID, state := range c.sessionState {
		if strings.TrimSpace(state.requirementID) == requirementID {
			return sessionID
		}
	}
	return ""
}

func (c *RequirementAutomationCoordinator) resolveObservedSessionIDForRequirement(requirement Requirement) string {
	if c == nil {
		return ""
	}
	if sessionID := c.findTrackedSessionIDForRequirement(requirement.ID); strings.TrimSpace(sessionID) != "" {
		return strings.TrimSpace(sessionID)
	}
	if requirement.Status != RequirementStatusRunning {
		return ""
	}
	if requirement.PromptSentAt == nil || requirement.PromptSentAt.IsZero() {
		return ""
	}
	return strings.TrimSpace(c.resolveTrackedRequirementSessionID(requirement, ""))
}

func (c *RequirementAutomationCoordinator) scheduleReconnectAttempt(sessionID string, at time.Time) (int, time.Time, bool) {
	if c == nil {
		return 0, time.Time{}, false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, time.Time{}, false
	}
	if at.IsZero() {
		at = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.ensureSessionStateLocked(sessionID)
	if !state.nextReconnectAfter.IsZero() && at.Before(state.nextReconnectAfter) {
		return state.reconnectCount, state.nextReconnectAfter, false
	}
	state.reconnectCount++
	state.lastReconnectAt = at
	delay := c.reconnectDelay(state.reconnectCount)
	state.nextReconnectAfter = at.Add(delay)
	return state.reconnectCount, state.nextReconnectAfter, true
}

// SyncProject evaluates one project queue and starts/distributes automatic requirements when possible.
func (c *RequirementAutomationCoordinator) SyncProject(projectID, preferredSessionID string) {
	if c == nil || c.app == nil {
		return
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return
	}
	if c.isProjectAutomationPaused(projectID) {
		return
	}
	if !c.beginProjectSync(projectID) {
		return
	}

	completionSessionID := c.syncProjectLocked(projectID, preferredSessionID)
	c.endProjectSync(projectID)

	if completionSessionID != "" {
		c.completeAutoRequirementForSession(completionSessionID)
	}
}

func (c *RequirementAutomationCoordinator) runOutputWatcher() {
	defer c.wg.Done()

	events := c.app.cliMgr.Events()
	if events == nil {
		return
	}
	subscription := events.Subscribe(CLIEventFilter{}, 0)
	defer subscription.Close()

	for {
		select {
		case <-c.stopCh:
			return
		case event, ok := <-subscription.Events:
			if !ok {
				return
			}
			if event.Type != "output" || strings.TrimSpace(event.Output) == "" {
				continue
			}
			completed := c.handleOutputEvent(event)
			c.recordRequirementOutput(event.SessionID, event.Output, event.Timestamp)
			if c.advanceRequirementSession(event.SessionID) {
				completed = true
			}
			if !completed {
				snapshot := c.getSessionSnapshot(event.SessionID)
				if c.sessionWindowShowsRequirementCompletion(event.SessionID, snapshot.Prompt) {
					completed = true
				}
			}
			if !completed {
				continue
			}
			c.completeAutoRequirementForSession(event.SessionID)
		}
	}
}

func (c *RequirementAutomationCoordinator) runScheduler() {
	defer c.wg.Done()
	ticker := time.NewTicker(requirementAutomationScanInterval)
	defer ticker.Stop()

	c.syncAllProjects()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.syncAllProjects()
		}
	}
}

func (c *RequirementAutomationCoordinator) syncAllProjects() {
	if c == nil || c.app == nil || c.app.projectSvc == nil {
		return
	}
	scanAt := time.Now()
	c.syncMu.Lock()
	c.lastScanAt = scanAt
	c.nextScanAt = scanAt.Add(requirementAutomationScanInterval)
	c.scanCount++
	c.syncMu.Unlock()
	projects, err := c.app.projectSvc.List()
	if err != nil {
		requirementAutomationLogger().Warn("list projects for automation scan failed", zap.Error(err))
		return
	}
	for _, project := range projects {
		if project.AutomationPaused {
			continue
		}
		c.SyncProject(project.ID, "")
	}
}

func (c *RequirementAutomationCoordinator) handleOutputEvent(event CLIEvent) bool {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" {
		return false
	}
	text := normalizeCLIConfirmationWindow(event.Output)
	if text == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.ensureSessionStateLocked(sessionID)
	if !event.Timestamp.IsZero() && !state.lastPromptSentAt.IsZero() && event.Timestamp.Before(state.lastPromptSentAt) {
		return false
	}
	state.cleanOutput = trimRequirementAutomationBuffer(state.cleanOutput + text)
	state.pending = pruneTrackedOutboundInputs(state.pending)

	if len(state.pending) > 0 {
		cleaned, pending := consumeTrackedOutboundInputs(state.cleanOutput, state.pending)
		state.cleanOutput = cleaned
		state.pending = pending
	}
	if requirementAutomationHasUnresolvedPending(state.pending) {
		return false
	}
	completed := requirementAutomationHasCompletionSignal(state.cleanOutput)
	return completed
}

func (c *RequirementAutomationCoordinator) completeAutoRequirementForSession(sessionID string) {
	if c == nil || c.app == nil || c.app.store == nil {
		return
	}
	record, err := c.app.store.GetCLISessionRecord(sessionID)
	if err != nil {
		requirementAutomationLogger().Warn("load cli session for auto completion failed", zap.String("session_id", sessionID), zap.Error(err))
		return
	}
	projectID := strings.TrimSpace(record.ProjectID)
	if projectID == "" {
		return
	}
	if !c.beginProjectSync(projectID) {
		return
	}

	requirements, err := c.app.requirementSvc.ListByProject(projectID)
	if err != nil {
		c.endProjectSync(projectID)
		requirementAutomationLogger().Warn("list project requirements for completion failed", zap.String("project_id", projectID), zap.Error(err))
		return
	}
	ordered := sortRequirementsForAutomation(requirements)
	var current *Requirement
	for _, item := range ordered {
		if item.Status == RequirementStatusRunning && item.ExecutionMode == RequirementExecutionModeAuto {
			candidate := item
			current = &candidate
			break
		}
	}
	if current == nil {
		c.endProjectSync(projectID)
		c.clearSessionDetectionState(sessionID)
		return
	}

	if c.app.workflowSvc != nil {
		canComplete, reason, workflowErr := c.app.workflowSvc.CanAutoComplete(current.ID)
		if workflowErr != nil {
			c.endProjectSync(projectID)
			requirementAutomationLogger().Warn(
				"evaluate workflow blockers before auto completion failed",
				zap.String("project_id", projectID),
				zap.String("requirement_id", current.ID),
				zap.String("session_id", sessionID),
				zap.Error(workflowErr),
			)
			return
		}
		if !canComplete {
			requirementAutomationLogger().Info(
				"skip auto requirement completion because workflow still has blockers",
				zap.String("project_id", projectID),
				zap.String("requirement_id", current.ID),
				zap.String("session_id", sessionID),
				zap.String("reason", reason),
			)
			// Record that the completion marker was seen but blocked, so
			// TryCompleteRunningRequirement can finalize when the blocker clears.
			c.mu.Lock()
			c.completionPending[current.ID] = true
			c.mu.Unlock()
			c.endProjectSync(projectID)
			c.clearSessionDetectionState(sessionID)
			return
		}
	}

	// Clear any pending completion flag now that we're actually completing.
	c.mu.Lock()
	delete(c.completionPending, current.ID)
	c.mu.Unlock()

	if _, err := c.app.requirementSvc.Transition(current.ID, "done"); err != nil {
		c.endProjectSync(projectID)
		requirementAutomationLogger().Warn(
			"mark auto requirement done failed",
			zap.String("project_id", projectID),
			zap.String("requirement_id", current.ID),
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return
	}
	c.clearRequirementDiagnostics(current.ID)

	requirementAutomationLogger().Info(
		"auto requirement completed from cli output marker",
		zap.String("project_id", projectID),
		zap.String("requirement_id", current.ID),
		zap.String("session_id", sessionID),
	)

	if current.AutoClearSession {
		if session, ok := c.app.cliMgr.Get(sessionID); ok && isRuntimeSessionRunning(session) {
			if err := session.WriteInput("/clear\n"); err != nil {
				requirementAutomationLogger().Warn(
					"send /clear to session after requirement done failed",
					zap.String("session_id", sessionID),
					zap.String("requirement_id", current.ID),
					zap.Error(err),
				)
			} else {
				c.app.cliSessionSvc.Touch(sessionID)
				requirementAutomationLogger().Info(
					"sent /clear to session after requirement done",
					zap.String("session_id", sessionID),
					zap.String("requirement_id", current.ID),
				)
			}
		}
	}

	c.updateSessionHealth(sessionID, requirementAutomationHealthCompleted, requirementAutomationCompletionMarker, time.Now())
	nextCompletionSessionID := c.syncProjectLocked(projectID, sessionID)
	c.endProjectSync(projectID)

	if nextCompletionSessionID != "" {
		c.completeAutoRequirementForSession(nextCompletionSessionID)
	}
}

func (c *RequirementAutomationCoordinator) syncProjectLocked(projectID, preferredSessionID string) string {
	if c.isProjectAutomationPaused(projectID) {
		return ""
	}
	requirements, err := c.app.requirementSvc.ListByProject(projectID)
	if err != nil {
		requirementAutomationLogger().Warn("list project requirements failed", zap.String("project_id", projectID), zap.Error(err))
		return ""
	}
	ordered := sortRequirementsForAutomation(requirements)

	if active := pickActiveRequirement(ordered); active != nil {
		if active.Status == RequirementStatusRunning {
			if active.ExecutionMode == RequirementExecutionModeAuto && active.PromptSentAt == nil {
				if err := c.dispatchRequirementPrompt(*active, preferredSessionID); err != nil {
					requirementAutomationLogger().Warn(
						"dispatch running requirement prompt failed",
						zap.String("project_id", projectID),
						zap.String("requirement_id", active.ID),
						zap.String("execution_mode", active.ExecutionMode),
						zap.Error(err),
					)
				}
				return ""
			}
			sessionID := c.ensureRequirementSessionTracking(*active, preferredSessionID)
			if active.ExecutionMode == RequirementExecutionModeAuto && sessionID == "" {
				if err := c.recoverRunningRequirementSession(*active, preferredSessionID); err != nil {
					requirementAutomationLogger().Warn(
						"recover running auto requirement without active session failed",
						zap.String("project_id", projectID),
						zap.String("requirement_id", active.ID),
						zap.Error(err),
					)
				}
				return ""
			}
			if sessionID != "" {
				if active.ExecutionMode == RequirementExecutionModeAuto &&
					c.sessionWindowShowsRequirementCompletion(sessionID, buildRequirementDispatchPrompt(*active)) {
					return sessionID
				}
				if c.advanceRequirementSession(sessionID) {
					return sessionID
				}
				c.evaluateRequirementWatchdog(*active, sessionID, preferredSessionID)
			}
		}
		return ""
	}

	next := pickNextPlannedRequirement(ordered)
	if next == nil || next.ExecutionMode != RequirementExecutionModeAuto {
		return ""
	}
	started, err := c.app.requirementSvc.Transition(next.ID, "start")
	if err != nil {
		requirementAutomationLogger().Warn(
			"start next auto requirement failed",
			zap.String("project_id", projectID),
			zap.String("requirement_id", next.ID),
			zap.Error(err),
		)
		return ""
	}
	if err := c.dispatchRequirementPrompt(*started, preferredSessionID); err != nil {
		requirementAutomationLogger().Warn(
			"dispatch next auto requirement prompt failed",
			zap.String("project_id", projectID),
			zap.String("requirement_id", started.ID),
			zap.Error(err),
		)
	}
	return ""
}

func (c *RequirementAutomationCoordinator) isProjectAutomationPaused(projectID string) bool {
	if c == nil || c.app == nil || c.app.projectSvc == nil {
		return false
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	project, err := c.app.projectSvc.Get(projectID)
	if err != nil || project == nil {
		return false
	}
	return project.AutomationPaused
}

func (c *RequirementAutomationCoordinator) evaluateRequirementWatchdog(requirement Requirement, sessionID, preferredSessionID string) {
	if c == nil || c.app == nil || c.app.store == nil {
		return
	}
	if requirement.Status != RequirementStatusRunning || requirement.NoResponseTimeoutMinutes <= 0 {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	snapshot := c.getSessionSnapshot(sessionID)
	triggerKind, triggerReason, action := c.classifyRequirementWatchdog(requirement, sessionID, snapshot)
	if triggerKind == "" || action == "" || action == RequirementNoResponseActionNone {
		return
	}
	if c.shouldSuppressRequirementWatchdog(requirement.ID, triggerKind, snapshot.LastOutputAt) {
		return
	}
	if err := c.executeRequirementWatchdogAction(requirement, sessionID, preferredSessionID, triggerKind, triggerReason, action); err != nil {
		requirementAutomationLogger().Warn(
			"execute requirement watchdog action failed",
			zap.String("project_id", requirement.ProjectID),
			zap.String("requirement_id", requirement.ID),
			zap.String("session_id", sessionID),
			zap.String("trigger_kind", triggerKind),
			zap.String("trigger_reason", triggerReason),
			zap.String("action", action),
			zap.Error(err),
		)
	}
}

func (c *RequirementAutomationCoordinator) classifyRequirementWatchdog(requirement Requirement, sessionID string, snapshot requirementAutomationSessionSnapshot) (string, string, string) {
	if requirement.Status != RequirementStatusRunning || requirement.NoResponseTimeoutMinutes <= 0 {
		return "", "", ""
	}
	if strings.TrimSpace(snapshot.RequirementID) != requirement.ID {
		return "", "", ""
	}
	if snapshot.ReplayPending || snapshot.Health == requirementAutomationHealthAwaitingConfirmation {
		return "", "", ""
	}

	session, ok := c.app.cliMgr.Get(sessionID)
	sessionRunning := ok && isRuntimeSessionRunning(session)
	if !sessionRunning {
		action := strings.TrimSpace(requirement.NoResponseErrorAction)
		if action == "" || action == RequirementNoResponseActionNone {
			return "", "", ""
		}
		reason := strings.TrimSpace(snapshot.HealthReason)
		if reason == "" {
			reason = "session_disconnected"
		}
		if diagnosis := c.app.diagnoseSessionFailure(sessionID, reason); strings.TrimSpace(diagnosis.Summary) != "" {
			reason = diagnosis.Summary
		}
		return RequirementWatchdogTriggerCLIError, reason, action
	}

	switch snapshot.Health {
	case requirementAutomationHealthStalledNetwork,
		requirementAutomationHealthStalledQuota,
		requirementAutomationHealthStalledInterrupted:
		action := strings.TrimSpace(requirement.NoResponseErrorAction)
		if action == "" || action == RequirementNoResponseActionNone {
			return "", "", ""
		}
		reason := strings.TrimSpace(snapshot.HealthReason)
		if reason == "" {
			reason = "watchdog_cli_error"
		}
		return RequirementWatchdogTriggerCLIError, reason, action
	case requirementAutomationHealthStalledNoOutput:
		action := strings.TrimSpace(requirement.NoResponseIdleAction)
		if action == "" || action == RequirementNoResponseActionNone {
			return "", "", ""
		}
		reason := strings.TrimSpace(snapshot.HealthReason)
		if reason == "" {
			reason = "watchdog_idle_timeout"
		}
		return RequirementWatchdogTriggerCLIIdle, reason, action
	default:
		return "", "", ""
	}
}

func (c *RequirementAutomationCoordinator) shouldSuppressRequirementWatchdog(requirementID, triggerKind string, lastOutputAt time.Time) bool {
	if c == nil || c.app == nil || c.app.store == nil {
		return false
	}
	if lastOutputAt.IsZero() {
		return false
	}
	event, err := c.app.store.GetLatestRequirementWatchdogEvent(requirementID)
	if err != nil || event == nil {
		return false
	}
	if strings.TrimSpace(event.TriggerKind) != strings.TrimSpace(triggerKind) {
		return false
	}
	if strings.TrimSpace(event.Status) != RequirementWatchdogEventStatusSucceeded {
		return false
	}
	return !event.CreatedAt.Before(lastOutputAt)
}

func (c *RequirementAutomationCoordinator) executeRequirementWatchdogAction(
	requirement Requirement,
	sessionID,
	preferredSessionID,
	triggerKind,
	triggerReason,
	action string,
) error {
	if c == nil || c.app == nil || c.app.store == nil {
		return nil
	}
	current, ok, err := c.reloadRunningRequirement(requirement.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	requirement = current
	now := time.Now()
	diagnosis := c.app.diagnoseSessionFailure(sessionID, triggerReason)
	if strings.TrimSpace(diagnosis.Summary) != "" {
		triggerReason = diagnosis.Summary
	}
	event, err := c.app.store.CreateRequirementWatchdogEvent(RequirementWatchdogEvent{
		RequirementID: requirement.ID,
		SessionID:     sessionID,
		TriggerKind:   triggerKind,
		TriggerReason: triggerReason,
		Action:        action,
		Status:        RequirementWatchdogEventStatusPending,
		CreatedAt:     now,
	})
	if err != nil {
		return err
	}

	finish := func(status, detail string) error {
		_, finishErr := c.app.store.FinishRequirementWatchdogEvent(event.ID, status, detail, time.Now())
		return finishErr
	}

	if triggerKind == RequirementWatchdogTriggerCLIError && diagnosis.Matched && !diagnosis.Retryable {
		reason := strings.TrimSpace(firstNonEmpty(firstNonEmpty(diagnosis.Summary, triggerReason), "session_failure"))
		detail := "non-retryable cli failure detected"
		if reason != "" {
			detail += ": " + reason
		}
		if failErr := c.failRequirement(requirement, sessionID, reason); failErr != nil {
			_ = finish(RequirementWatchdogEventStatusFailed, detail+": "+failErr.Error())
			return failErr
		}
		return finish(RequirementWatchdogEventStatusFailed, detail)
	}

	retryReason := automationRetryReasonWatchdogResend
	if action == RequirementNoResponseActionCloseAndResendRequirement {
		retryReason = automationRetryReasonWatchdogRebuild
	}
	currentAttempt, exhausted, err := c.app.requirementSvc.ConsumeRetryBudget(requirement.ID, c.maxRequirementRetryAttempts(), retryReason, now)
	if err != nil {
		return err
	}

	if exhausted {
		detail := fmt.Sprintf(
			"requirement retry budget exhausted: attempt %d blocked after %d retries, requirement marked failed",
			currentAttempt,
			c.maxRequirementRetryAttempts(),
		)
		if failErr := c.failRequirementForRetryBudget(requirement, sessionID, automationRetryReasonBudgetExhausted); failErr != nil {
			_ = finish(RequirementWatchdogEventStatusFailed, detail+": "+failErr.Error())
			return failErr
		}
		return finish(RequirementWatchdogEventStatusFailed, detail)
	}

	var actionErr error
	switch action {
	case RequirementNoResponseActionResendRequirement:
		actionErr = c.sendRequirementPrompt(requirement, firstNonEmpty(preferredSessionID, sessionID), true, "watchdog:"+triggerKind+":"+triggerReason)
	case RequirementNoResponseActionCloseAndResendRequirement:
		if resetErr := c.resetRequirementSession(sessionID); resetErr != nil {
			actionErr = resetErr
			break
		}
		actionErr = c.sendRequirementPrompt(requirement, firstNonEmpty(preferredSessionID, sessionID), true, "watchdog:"+triggerKind+":"+triggerReason)
	default:
		actionErr = nil
	}
	if actionErr != nil {
		_ = finish(RequirementWatchdogEventStatusFailed, actionErr.Error())
		return actionErr
	}
	detail := "watchdog action completed"
	if action == RequirementNoResponseActionCloseAndResendRequirement {
		detail = fmt.Sprintf("session rebuilt and requirement resent (attempt %d/%d)", currentAttempt, c.maxRequirementRetryAttempts())
	} else if action == RequirementNoResponseActionResendRequirement {
		detail = fmt.Sprintf("requirement resent to running cli session (attempt %d/%d)", currentAttempt, c.maxRequirementRetryAttempts())
	}
	return finish(RequirementWatchdogEventStatusSucceeded, detail)
}

func (c *RequirementAutomationCoordinator) recoverRunningRequirementSession(requirement Requirement, preferredSessionID string) error {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return nil
	}
	now := time.Now()
	recoverySessionID := strings.TrimSpace(c.resolveTrackedRequirementSessionID(requirement, preferredSessionID))
	if recoverySessionID != "" {
		diagnosis := c.app.diagnoseSessionFailure(recoverySessionID, "session_disconnected")
		if diagnosis.Matched && !diagnosis.Retryable {
			return c.failRequirement(requirement, recoverySessionID, strings.TrimSpace(firstNonEmpty(diagnosis.Summary, "session_failure")))
		}
	}
	if recoverySessionID != "" {
		attempt, nextAttemptAt, allowed := c.scheduleReconnectAttempt(recoverySessionID, now)
		if !allowed {
			c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedWaitingReconnect)
			requirementAutomationLogger().Info(
				"skip auto requirement session recovery until reconnect backoff elapses",
				zap.String("project_id", requirement.ProjectID),
				zap.String("requirement_id", requirement.ID),
				zap.String("session_id", recoverySessionID),
				zap.Int("attempt", attempt),
				zap.Time("next_attempt_at", nextAttemptAt),
			)
			return nil
		}
	}

	err := c.sendRequirementPrompt(requirement, preferredSessionID, true, automationRetryReasonSessionRecovery)
	blockedReason := strings.TrimSpace(c.requirementDispatchBlockedReason(requirement.ID))
	recoveryBlocked := blockedReason == automationDispatchBlockedNoSessionBound ||
		blockedReason == automationDispatchBlockedSessionNotRunning ||
		strings.HasPrefix(blockedReason, automationDispatchBlockedPromptDispatchFailed)
	if err == nil && !recoveryBlocked {
		return nil
	}
	return c.recordRequirementRecoveryFailure(requirement, recoverySessionID, blockedReason, err, now)
}

func (c *RequirementAutomationCoordinator) recordRequirementRecoveryFailure(
	requirement Requirement,
	sessionID,
	blockedReason string,
	attemptErr error,
	at time.Time,
) error {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return attemptErr
	}
	current, ok, err := c.reloadRunningRequirement(requirement.ID)
	if err != nil {
		return err
	}
	if ok {
		requirement = current
	}

	attempt, exhausted, err := c.app.requirementSvc.ConsumeRetryBudget(requirement.ID, c.maxRequirementRetryAttempts(), automationRetryReasonSessionRecovery, at)
	if err != nil {
		return err
	}
	if exhausted {
		return c.failRequirementForRetryBudget(requirement, sessionID, automationRetryReasonBudgetExhausted)
	}

	reason := strings.TrimSpace(blockedReason)
	if reason == "" && attemptErr != nil {
		reason = automationDispatchFailureReason(attemptErr)
	}
	if reason == "" {
		reason = automationDispatchBlockedNoSessionBound
	}
	c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedWaitingReconnect)
	requirementAutomationLogger().Warn(
		"auto requirement session recovery attempt failed; waiting for next retry window",
		zap.String("project_id", requirement.ProjectID),
		zap.String("requirement_id", requirement.ID),
		zap.String("session_id", sessionID),
		zap.String("blocked_reason", reason),
		zap.Int("attempt", attempt),
		zap.Int("max_attempts", c.maxRequirementRetryAttempts()),
		zap.Error(attemptErr),
	)
	return nil
}

func (c *RequirementAutomationCoordinator) failRequirement(requirement Requirement, sessionID, reason string) error {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return nil
	}
	if requirement.Status != RequirementStatusRunning && requirement.Status != RequirementStatusPaused {
		return nil
	}
	if _, err := c.app.requirementSvc.Transition(requirement.ID, "fail"); err != nil {
		return err
	}
	c.clearRequirementDiagnostics(requirement.ID)
	c.clearSessionDetectionState(sessionID)
	if sessionID != "" {
		c.updateSessionHealth(sessionID, requirementAutomationHealthFailed, strings.TrimSpace(reason), time.Now())
	}
	requirementAutomationLogger().Warn(
		"auto requirement marked failed after watchdog retry limit",
		zap.String("project_id", requirement.ProjectID),
		zap.String("requirement_id", requirement.ID),
		zap.String("session_id", sessionID),
		zap.String("reason", strings.TrimSpace(reason)),
	)
	return nil
}

func (c *RequirementAutomationCoordinator) failRequirementForRetryBudget(requirement Requirement, sessionID, reason string) error {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return nil
	}
	now := time.Now()
	if err := c.app.requirementSvc.MarkRetryBudgetExhausted(requirement.ID, reason, now); err != nil {
		return err
	}
	c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedRetryBudgetExhausted)
	return c.failRequirement(requirement, sessionID, reason)
}

func (c *RequirementAutomationCoordinator) resetRequirementSession(sessionID string) error {
	if c == nil || c.app == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	agentID := ""
	if runtimeSession, ok := c.app.cliMgr.Get(sessionID); ok {
		agentID = strings.TrimSpace(runtimeSession.AgentID)
		if err := runtimeSession.Terminate(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not running") {
			return err
		}
		if err := c.app.cliMgr.Destroy(sessionID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
			return err
		}
	} else if view, err := c.app.cliSessionSvc.GetView(sessionID); err == nil {
		agentID = strings.TrimSpace(view.AgentID)
	}
	if events := c.app.cliMgr.Events(); events != nil {
		events.ClearSession(sessionID, agentID)
	}
	if c.app.cliArchive != nil {
		_ = c.app.cliArchive.DeleteSession(sessionID)
	}
	_ = c.app.cliSessionSvc.MarkTerminated(sessionID)
	c.clearSessionDetectionState(sessionID)
	return nil
}

func (c *RequirementAutomationCoordinator) dispatchRequirementPrompt(requirement Requirement, preferredSessionID string) error {
	reason := "initial-dispatch"
	if c.noteRequirementDispatchAttempt(requirement.ID) {
		reason = automationRetryReasonSchedulerRedispatch
	}
	return c.sendRequirementPrompt(requirement, preferredSessionID, false, reason)
}

func (c *RequirementAutomationCoordinator) sendRequirementPrompt(requirement Requirement, preferredSessionID string, replay bool, reason string) error {
	current, ok, err := c.reloadRunningRequirement(requirement.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	requirement = current
	dispatchedAt := time.Now()
	prompt := buildRequirementDispatchPrompt(requirement)
	if !replay && strings.TrimSpace(reason) == automationRetryReasonSchedulerRedispatch {
		attempt, exhausted, err := c.app.requirementSvc.ConsumeRetryBudget(requirement.ID, c.maxRequirementRetryAttempts(), reason, dispatchedAt)
		if err != nil {
			c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedPromptDispatchFailed+":"+automationDispatchFailureReason(err))
			return err
		}
		if exhausted {
			_ = attempt
			return c.failRequirementForRetryBudget(requirement, "", automationRetryReasonBudgetExhausted)
		}
	}
	if prompt == "" {
		if replay {
			return nil
		}
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedNone)
		return c.app.requirementSvc.MarkPromptDispatched(requirement.ID, dispatchedAt)
	}

	sessionID, rehydrated, blockedReason := c.resolveRequirementDispatchSession(requirement, preferredSessionID)
	if blockedReason != "" {
		c.setRequirementDispatchBlockedReason(requirement.ID, blockedReason)
	}
	if sessionID == "" {
		return nil
	}
	session, ok := c.app.cliMgr.Get(sessionID)
	if !ok || !isRuntimeSessionRunning(session) {
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedSessionNotRunning)
		return nil
	}
	if rehydrated {
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedNone)
		return nil
	}
	if err := prepareSessionForInput(session, 12*time.Second, true); err != nil {
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedPromptDispatchFailed+":"+automationDispatchFailureReason(err))
		return err
	}
	if replay {
		if err := c.app.requirementSvc.MarkPromptReplayed(requirement.ID, dispatchedAt); err != nil {
			c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedPromptDispatchFailed+":"+automationDispatchFailureReason(err))
			return err
		}
	} else {
		if err := c.app.requirementSvc.MarkPromptDispatched(requirement.ID, dispatchedAt); err != nil {
			c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedPromptDispatchFailed+":"+automationDispatchFailureReason(err))
			return err
		}
	}
	payload := prompt
	if !strings.HasSuffix(payload, "\n") {
		payload += "\n"
	}
	c.setRequirementOwner(requirement, sessionID, dispatchedAt)
	c.bindRequirementSession(sessionID, requirement.ID, prompt, dispatchedAt, replay)
	c.TrackOutboundInput(sessionID, payload)
	if err := session.WriteInput(payload); err != nil {
		c.clearSessionDetectionState(sessionID)
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedPromptDispatchFailed+":"+automationDispatchFailureReason(err))
		return err
	}
	c.app.cliSessionSvc.Touch(sessionID)
	c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedNone)
	if replay {
		requirementAutomationLogger().Info(
			"replay auto requirement prompt",
			zap.String("project_id", requirement.ProjectID),
			zap.String("requirement_id", requirement.ID),
			zap.String("session_id", sessionID),
			zap.String("reason", strings.TrimSpace(reason)),
		)
	}
	return nil
}

func (c *RequirementAutomationCoordinator) reloadRunningRequirement(requirementID string) (Requirement, bool, error) {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return Requirement{}, false, nil
	}
	requirementID = strings.TrimSpace(requirementID)
	if requirementID == "" {
		return Requirement{}, false, nil
	}
	requirement, err := c.app.requirementSvc.Get(requirementID)
	if err != nil {
		return Requirement{}, false, err
	}
	if requirement.Status != RequirementStatusRunning {
		return Requirement{}, false, nil
	}
	return *requirement, true, nil
}

func (c *RequirementAutomationCoordinator) resolveRequirementDispatchSession(requirement Requirement, preferredSessionID string) (string, bool, string) {
	if requirement.RetryBudgetExhaustedAt != nil && !requirement.RetryBudgetExhaustedAt.IsZero() {
		return "", false, automationDispatchBlockedRetryBudgetExhausted
	}
	sessionID, rehydrated, blockedReason := c.ensureOwnedRequirementSession(requirement, preferredSessionID)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", false, blockedReason
	}
	if session, ok := c.app.cliMgr.Get(sessionID); !ok || !isRuntimeSessionRunning(session) {
		return sessionID, false, automationDispatchBlockedSessionNotRunning
	}
	return sessionID, rehydrated, automationDispatchBlockedNone
}

func (c *RequirementAutomationCoordinator) resolveTrackedRequirementSessionID(requirement Requirement, preferredSessionID string) string {
	preferredSessionID = strings.TrimSpace(preferredSessionID)
	return c.findRunningRequirementOwnedSession(requirement, preferredSessionID)
}

func (c *RequirementAutomationCoordinator) ensureRequirementSessionTracking(requirement Requirement, preferredSessionID string) string {
	prompt := buildRequirementDispatchPrompt(requirement)
	if strings.TrimSpace(prompt) == "" {
		return ""
	}
	sessionID := c.resolveTrackedRequirementSessionID(requirement, preferredSessionID)
	if sessionID == "" {
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedNoSessionBound)
		return ""
	}
	if session, ok := c.app.cliMgr.Get(sessionID); !ok || !isRuntimeSessionRunning(session) {
		c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedWaitingReconnect)
		return ""
	}
	dispatchedAt := time.Now()
	if requirement.PromptSentAt != nil && !requirement.PromptSentAt.IsZero() {
		dispatchedAt = *requirement.PromptSentAt
	}
	c.bindRequirementSession(sessionID, requirement.ID, prompt, dispatchedAt, false)
	c.setRequirementDispatchBlockedReason(requirement.ID, automationDispatchBlockedNone)
	return sessionID
}

func (c *RequirementAutomationCoordinator) sessionWindowShowsRequirementCompletion(sessionID, prompt string) bool {
	if c == nil || c.app == nil || c.app.cliMgr == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	prompt = strings.TrimSpace(prompt)
	if sessionID == "" || prompt == "" {
		return false
	}
	session, ok := c.app.cliMgr.Get(sessionID)
	if !ok || !isRuntimeSessionRunning(session) {
		return false
	}
	snapshot := c.getSessionSnapshot(sessionID)
	window := sessionWindowSinceOffset(session, snapshot.OutputBaselineOffset, 64*1024)
	if !requirementAutomationHasCompletionSignal(window) {
		return false
	}
	return requirementAutomationOutputShowsProgress(window, prompt)
}

func (c *RequirementAutomationCoordinator) advanceRequirementSession(sessionID string) bool {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	snapshot := c.getSessionSnapshot(sessionID)
	if strings.TrimSpace(snapshot.RequirementID) == "" || strings.TrimSpace(snapshot.Prompt) == "" {
		return false
	}

	requirement, err := c.app.requirementSvc.Get(snapshot.RequirementID)
	if err != nil {
		requirementAutomationLogger().Warn("load running requirement for session advance failed", zap.String("session_id", sessionID), zap.String("requirement_id", snapshot.RequirementID), zap.Error(err))
		return false
	}
	if requirement.Status != RequirementStatusRunning {
		c.clearSessionDetectionState(sessionID)
		return false
	}

	session, ok := c.app.cliMgr.Get(sessionID)
	if !ok || !isRuntimeSessionRunning(session) {
		reason := "session_disconnected"
		if diagnosis := c.app.diagnoseSessionFailure(sessionID, reason); strings.TrimSpace(diagnosis.Summary) != "" {
			reason = diagnosis.Summary
		}
		c.updateSessionHealth(sessionID, requirementAutomationHealthStalledInterrupted, reason, time.Now())
		return false
	}

	window := sessionWindowSinceOffset(session, snapshot.OutputBaselineOffset, 64*1024)
	now := time.Now()
	health, reason := classifyRequirementAutomationHealth(window, snapshot.LastOutputAt, now, effectiveRequirementNoOutputTimeout(*requirement))
	completionVisible := requirementAutomationHasCompletionSignal(window)
	if completionVisible {
		health = requirementAutomationHealthCompleted
		reason = requirementAutomationCompletionMarker
	}
	if requirement.ExecutionMode == RequirementExecutionModeAuto && completionVisible && (!snapshot.LastMeaningfulAt.IsZero() || snapshot.ProgressSeen) {
		c.updateSessionHealth(sessionID, health, reason, now)
		return true
	}
	if match := detectCLIConfirmation(window); match != nil {
		fingerprint := cliConfirmationFingerprint(match)
		staleHandledPrompt := fingerprint != "" &&
			fingerprint == snapshot.LastConfirmationKey &&
			!snapshot.LastConfirmationAt.IsZero() &&
			now.Sub(snapshot.LastConfirmationAt) >= cliConfirmationSettleDelay
		if !staleHandledPrompt {
			c.updateSessionHealth(sessionID, requirementAutomationHealthAwaitingConfirmation, match.Key, now)
			if c.markConfirmationHandled(sessionID, match) {
				if err := session.WriteRawBytes(match.Response); err != nil {
					requirementAutomationLogger().Warn(
						"auto confirm cli prompt failed",
						zap.String("project_id", requirement.ProjectID),
						zap.String("requirement_id", requirement.ID),
						zap.String("session_id", sessionID),
						zap.String("confirmation_key", match.Key),
						zap.Error(err),
					)
				} else {
					c.app.cliSessionSvc.Touch(sessionID)
				}
			}
			return false
		}
	}
	c.updateSessionHealth(sessionID, health, reason, now)
	if requirement.ExecutionMode != RequirementExecutionModeAuto {
		return false
	}
	return false
}

func buildRequirementDispatchPrompt(requirement Requirement) string {
	prompt := strings.TrimSpace(requirement.Description)
	if prompt == "" {
		return ""
	}
	if requirement.ExecutionMode != RequirementExecutionModeAuto || strings.Contains(prompt, requirementAutomationCompletionMarker) {
		return prompt
	}
	return prompt + "\n\n" + requirementAutomationCompletionPrompt
}

func buildRequirementAutomationPrompt(description string) string {
	return buildRequirementDispatchPrompt(Requirement{
		Description:   description,
		ExecutionMode: RequirementExecutionModeAuto,
	})
}

func (c *RequirementAutomationCoordinator) pickPreferredRunningSessionID(projectID, preferredSessionID string) string {
	projectID = strings.TrimSpace(projectID)
	preferredSessionID = strings.TrimSpace(preferredSessionID)
	if projectID == "" {
		return ""
	}
	if preferredSessionID != "" {
		if session, ok := c.app.cliMgr.Get(preferredSessionID); ok && isRuntimeSessionRunning(session) {
			return preferredSessionID
		}
	}

	views, err := c.app.cliSessionSvc.ListProjectViews(projectID)
	if err != nil {
		return ""
	}
	sort.SliceStable(views, func(i, j int) bool {
		if !views[i].LastActiveAt.Equal(views[j].LastActiveAt) {
			return views[i].LastActiveAt.After(views[j].LastActiveAt)
		}
		return views[i].CreatedAt.After(views[j].CreatedAt)
	})
	for _, view := range views {
		if view.State != CLISessionStateRunning {
			continue
		}
		session, ok := c.app.cliMgr.Get(view.ID)
		if !ok || !isRuntimeSessionRunning(session) {
			continue
		}
		return view.ID
	}
	return ""
}

func sessionWindowSinceOffset(session *CLISession, offset int64, maxBytes int) string {
	if session == nil {
		return ""
	}
	window, end := session.WindowBytes(maxBytes)
	if len(window) == 0 {
		return ""
	}
	if offset <= 0 {
		return string(window)
	}
	windowStart := end - int64(len(window))
	if offset <= windowStart {
		return string(window)
	}
	if offset >= end {
		return ""
	}
	start := int(offset - windowStart)
	if start < 0 {
		start = 0
	}
	if start >= len(window) {
		return ""
	}
	return string(window[start:])
}

func (c *RequirementAutomationCoordinator) clearSessionDetectionState(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	c.clearRequirementOwnerBySessionLocked(sessionID)
	delete(c.sessionState, sessionID)
	c.mu.Unlock()
}

// TryCompleteRunningRequirement checks whether the running auto requirement for a project
// can now be completed (all blockers resolved) and marks it done if so. Called after a
// decision or review blocker is resolved.
func (c *RequirementAutomationCoordinator) TryCompleteRunningRequirement(projectID string) {
	if c == nil || c.app == nil || c.app.requirementSvc == nil {
		return
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return
	}
	if !c.beginProjectSync(projectID) {
		return
	}
	defer c.endProjectSync(projectID)

	requirements, err := c.app.requirementSvc.ListByProject(projectID)
	if err != nil {
		return
	}
	ordered := sortRequirementsForAutomation(requirements)
	var current *Requirement
	for _, item := range ordered {
		if item.Status == RequirementStatusRunning && item.ExecutionMode == RequirementExecutionModeAuto {
			candidate := item
			current = &candidate
			break
		}
	}
	if current == nil {
		return
	}

	if c.app.workflowSvc == nil {
		return
	}

	// Only complete if the agent has already output the completion marker but was
	// previously blocked. Without this guard we could mark a requirement done
	// while the agent is still actively working.
	c.mu.Lock()
	markerSeen := c.completionPending[current.ID]
	c.mu.Unlock()
	if !markerSeen {
		return
	}

	canComplete, _, err := c.app.workflowSvc.CanAutoComplete(current.ID)
	if err != nil || !canComplete {
		return
	}

	// Marker was seen and all blockers are now cleared — finalize.
	c.mu.Lock()
	delete(c.completionPending, current.ID)
	c.mu.Unlock()

	if _, err := c.app.requirementSvc.Transition(current.ID, "done"); err != nil {
		requirementAutomationLogger().Warn(
			"mark auto requirement done after blocker resolved failed",
			zap.String("project_id", projectID),
			zap.String("requirement_id", current.ID),
			zap.Error(err),
		)
		return
	}
	c.clearRequirementDiagnostics(current.ID)
	requirementAutomationLogger().Info(
		"auto requirement completed after blocker was resolved",
		zap.String("project_id", projectID),
		zap.String("requirement_id", current.ID),
	)
	if health, ok := c.GetRequirementExecutionHealth(current.ID); ok {
		c.updateSessionHealth(health.SessionID, requirementAutomationHealthCompleted, requirementAutomationCompletionMarker, time.Now())
	}
	c.syncProjectLocked(projectID, "")
}

func (c *RequirementAutomationCoordinator) ensureSessionStateLocked(sessionID string) *requirementAutomationSessionState {
	state, ok := c.sessionState[sessionID]
	if !ok {
		state = &requirementAutomationSessionState{}
		c.sessionState[sessionID] = state
	}
	return state
}

func buildTrackedOutboundInput(text, actualSent string) *trackedOutboundInput {
	raw := strings.TrimSpace(normalizeRequirementAutomationText(text))
	actualSent = strings.TrimSpace(normalizeRequirementAutomationText(actualSent))
	if raw == "" || !strings.Contains(raw, requirementAutomationCompletionMarker) {
		return nil
	}
	lines := make([]string, 0, 2)
	contextLines := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			if strings.Contains(trimmed, requirementAutomationCompletionMarker) {
				continue
			}
		} else {
			seen[trimmed] = struct{}{}
			contextLines = append(contextLines, trimmed)
		}
		if !strings.Contains(trimmed, requirementAutomationCompletionMarker) {
			continue
		}
		lines = append(lines, trimmed)
	}
	return &trackedOutboundInput{
		raw:          raw,
		actualSent:   actualSent,
		markerLines:  lines,
		contextLines: contextLines,
		recordedAt:   time.Now(),
	}
}

func pruneTrackedOutboundInputs(items []trackedOutboundInput) []trackedOutboundInput {
	if len(items) == 0 {
		return nil
	}
	now := time.Now()
	filtered := items[:0]
	for _, item := range items {
		if item.recordedAt.IsZero() {
			continue
		}
		if !item.consumedAt.IsZero() {
			if now.Sub(item.consumedAt) > requirementAutomationConsumedTTL {
				continue
			}
		} else if now.Sub(item.recordedAt) > requirementAutomationPendingTTL {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return nil
	}
	return append([]trackedOutboundInput(nil), filtered...)
}

func consumeTrackedOutboundInputs(output string, items []trackedOutboundInput) (string, []trackedOutboundInput) {
	if len(items) == 0 || output == "" {
		return output, items
	}
	remaining := make([]trackedOutboundInput, 0, len(items))
	for _, item := range items {
		consumedActual := false
		consumedRaw := false
		consumedMarker := false
		if item.actualSent != "" {
			for {
				idx := strings.Index(output, item.actualSent)
				if idx < 0 {
					break
				}
				output = output[:idx] + output[idx+len(item.actualSent):]
				consumedActual = true
			}
		}
		if consumedActual && item.consumedAt.IsZero() {
			item.consumedAt = time.Now()
		}
		if item.raw != "" {
			for {
				idx := strings.Index(output, item.raw)
				if idx < 0 {
					break
				}
				output = output[:idx] + output[idx+len(item.raw):]
				consumedRaw = true
			}
		}
		if consumedRaw && item.consumedAt.IsZero() {
			item.consumedAt = time.Now()
		}
		for _, line := range item.markerLines {
			for {
				var removed bool
				output, removed = removeFirstMatchingMarkerWithNearbyContext(output, line, item.contextLines)
				if !removed {
					break
				}
				consumedMarker = true
			}
		}
		output = removeMatchingTrackedContextLines(output, item.contextLines)
		if (consumedActual || consumedRaw || consumedMarker) && item.consumedAt.IsZero() {
			item.consumedAt = time.Now()
		}
		remaining = append(remaining, item)
	}
	return trimRequirementAutomationBuffer(output), remaining
}

func requirementAutomationHasUnresolvedPending(items []trackedOutboundInput) bool {
	for _, item := range items {
		if item.consumedAt.IsZero() {
			return true
		}
	}
	return false
}

func removeFirstMatchingMarkerWithNearbyContext(output, target string, contextLines []string) (string, bool) {
	if output == "" || strings.TrimSpace(target) == "" {
		return output, false
	}
	lines := strings.Split(output, "\n")
	target = strings.TrimSpace(target)
	contextSet := buildRequirementAutomationLineSet(contextLines, target)
	contextFragments := buildRequirementAutomationContextFragments(contextLines, target)
	if len(contextSet) == 0 {
		if len(contextFragments) == 0 {
			return output, false
		}
	}

	for i, line := range lines {
		if strings.TrimSpace(line) != target {
			continue
		}
		if !requirementAutomationHasNearbyContext(lines, i, contextSet, contextFragments, 3) {
			continue
		}
		lines = append(lines[:i], lines[i+1:]...)
		return strings.Join(lines, "\n"), true
	}
	return output, false
}

func removeMatchingTrackedContextLines(output string, contextLines []string) string {
	if output == "" || len(contextLines) == 0 {
		return output
	}
	contextSet := buildRequirementAutomationLineSet(contextLines, requirementAutomationCompletionMarker)
	contextFragments := buildRequirementAutomationContextFragments(contextLines, requirementAutomationCompletionMarker)
	if len(contextSet) == 0 && len(contextFragments) == 0 {
		return output
	}
	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !requirementAutomationCompletionRegex.MatchString(trimmed) {
			if _, ok := contextSet[trimmed]; ok {
				removed = true
				continue
			}
			if requirementAutomationLineUsesOnlyPromptFragments(trimmed, contextFragments) {
				removed = true
				continue
			}
		}
		filtered = append(filtered, line)
	}
	if !removed {
		return output
	}
	return strings.Join(filtered, "\n")
}

func buildRequirementAutomationLineSet(lines []string, exclude string) map[string]struct{} {
	set := make(map[string]struct{})
	exclude = strings.TrimSpace(exclude)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == exclude {
			continue
		}
		set[trimmed] = struct{}{}
	}
	return set
}

func buildRequirementAutomationContextFragments(lines []string, exclude string) []string {
	if len(lines) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(lines))
	exclude = strings.TrimSpace(exclude)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == exclude {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	if len(filtered) == 0 {
		return nil
	}
	return buildRequirementAutomationPromptFragments(strings.Join(filtered, "\n"))
}

func requirementAutomationHasNearbyContext(lines []string, index int, contextSet map[string]struct{}, contextFragments []string, radius int) bool {
	if len(lines) == 0 || index < 0 || index >= len(lines) {
		return false
	}
	if len(contextSet) == 0 && len(contextFragments) == 0 {
		return false
	}
	if radius <= 0 {
		radius = 1
	}
	requiredMatches := 1

	start := index - radius
	if start < 0 {
		start = 0
	}
	end := index + radius
	if end >= len(lines) {
		end = len(lines) - 1
	}

	matched := make(map[string]struct{}, requiredMatches)
	for i := start; i <= end; i++ {
		if i == index {
			continue
		}
		trimmed := strings.TrimSpace(lines[i])
		if _, ok := contextSet[trimmed]; ok {
			matched[trimmed] = struct{}{}
			if len(matched) >= requiredMatches {
				return true
			}
			continue
		}
		if requirementAutomationLineUsesOnlyPromptFragments(trimmed, contextFragments) {
			matched[trimmed] = struct{}{}
			if len(matched) >= requiredMatches {
				return true
			}
		}
	}
	return false
}

func effectiveRequirementNoOutputTimeout(requirement Requirement) time.Duration {
	if requirement.NoResponseTimeoutMinutes > 0 {
		return time.Duration(requirement.NoResponseTimeoutMinutes) * time.Minute
	}
	return requirementAutomationNoOutputTimeout
}

func classifyRequirementAutomationHealth(window string, lastOutputAt, now time.Time, noOutputTimeout time.Duration) (string, string) {
	normalized := normalizeCLIConfirmationWindow(window)
	lower := strings.ToLower(normalized)
	if noOutputTimeout <= 0 {
		noOutputTimeout = requirementAutomationNoOutputTimeout
	}

	switch {
	case !lastOutputAt.IsZero() && now.Sub(lastOutputAt) >= noOutputTimeout:
		return requirementAutomationHealthStalledNoOutput, "no_output_timeout"
	case containsAnyFailureNeedle(lower, quotaFailureNeedles):
		return requirementAutomationHealthStalledQuota, "quota_or_rate_limit"
	case containsAnyFailureNeedle(lower, networkFailureNeedles):
		return requirementAutomationHealthStalledNetwork, "network_timeout"
	case cliWindowShowsInterruption(normalized):
		return requirementAutomationHealthStalledInterrupted, "interrupted"
	default:
		return requirementAutomationHealthRunning, ""
	}
}

func containsAnyFailureNeedle(lower string, needles []string) bool {
	if lower == "" || len(needles) == 0 {
		return false
	}
	for _, needle := range needles {
		needle = strings.TrimSpace(strings.ToLower(needle))
		if needle == "" {
			continue
		}
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func requirementAutomationOutputShowsProgress(chunk, prompt string) bool {
	normalized := normalizeCLIConfirmationWindow(chunk)
	if strings.TrimSpace(normalized) == "" {
		return false
	}
	promptText := strings.ToLower(strings.ReplaceAll(normalizeCLIConfirmationWindow(prompt), "\n", " "))
	promptFragments := buildRequirementAutomationPromptFragments(prompt)

	promptLines := make(map[string]struct{})
	for _, line := range strings.Split(normalizeCLIConfirmationWindow(prompt), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		promptLines[strings.ToLower(trimmed)] = struct{}{}
	}

	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, "\u203a"); idx > 0 && strings.Contains(strings.ToLower(line), "gpt-") {
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}
		if utf8.RuneCountInString(line) <= 2 {
			continue
		}
		lower := strings.ToLower(line)
		if _, ok := promptLines[lower]; ok {
			continue
		}
		if strings.Contains(promptText, lower) && utf8.RuneCountInString(line) <= 32 {
			continue
		}
		if requirementAutomationLineUsesOnlyPromptFragments(line, promptFragments) {
			continue
		}
		if lower == strings.ToLower(requirementAutomationCompletionPrompt) {
			continue
		}
		if detectCLIConfirmation(line) != nil {
			continue
		}
		if lower == "y" || lower == "yes" {
			continue
		}
		if lower == ">" || lower == "❯" || strings.HasPrefix(lower, "> ") || strings.HasPrefix(lower, "❯ ") {
			continue
		}
		if strings.Contains(line, "\u203a") && strings.Contains(lower, "gpt-") {
			continue
		}
		if strings.HasPrefix(lower, "openai codex") ||
			strings.HasPrefix(lower, "model:") ||
			strings.HasPrefix(lower, "directory:") ||
			strings.HasPrefix(lower, "tip:") ||
			strings.HasPrefix(lower, "gpt-") ||
			lower == "hi. what do you need?" ||
			lower == "what do you need?" ||
			strings.Contains(lower, "esc to interrupt") {
			continue
		}
		return true
	}
	return false
}

func requirementAutomationHasCompletionSignal(text string) bool {
	normalized := normalizeCLIConfirmationWindow(text)
	if strings.TrimSpace(normalized) == "" {
		return false
	}
	for _, rawLine := range strings.Split(normalized, "\n") {
		if requirementAutomationLineIndicatesCompletion(rawLine) {
			return true
		}
	}
	return false
}

func requirementAutomationLineIndicatesCompletion(line string) bool {
	line = strings.TrimSpace(normalizeCLIConfirmationWindow(line))
	if line == "" {
		return false
	}
	line = requirementAutomationCompletionPrefixRegex.ReplaceAllString(line, "")
	if strings.TrimSpace(line) == "" {
		return false
	}
	compacted := requirementAutomationCompactCompletionLine(line)
	if compacted == "" {
		return false
	}
	_, ok := requirementAutomationCompletionVariants[compacted]
	return ok
}

func requirementAutomationCompactCompletionLine(line string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(line))
}

func buildRequirementAutomationPromptFragments(prompt string) []string {
	seen := make(map[string]struct{})
	fragments := make([]string, 0)
	for _, line := range strings.Split(normalizeCLIConfirmationWindow(prompt), "\n") {
		compact := requirementAutomationCompactPromptFragment(line)
		if compact == "" {
			continue
		}
		if _, ok := seen[compact]; ok {
			continue
		}
		seen[compact] = struct{}{}
		fragments = append(fragments, compact)
	}
	sort.SliceStable(fragments, func(i, j int) bool {
		return len(fragments[i]) > len(fragments[j])
	})
	return fragments
}

func requirementAutomationLineUsesOnlyPromptFragments(line string, promptFragments []string) bool {
	if len(promptFragments) == 0 {
		return false
	}
	remaining := requirementAutomationCompactPromptFragment(line)
	if remaining == "" {
		return false
	}
	for {
		previous := remaining
		for _, fragment := range promptFragments {
			if fragment == "" || !strings.Contains(remaining, fragment) {
				continue
			}
			remaining = strings.ReplaceAll(remaining, fragment, "")
		}
		remaining = requirementAutomationTrimPromptEchoNoise(remaining)
		if remaining == "" {
			return true
		}
		if remaining == previous {
			return false
		}
	}
}

func requirementAutomationCompactPromptFragment(text string) string {
	if text == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(text))
}

func requirementAutomationTrimPromptEchoNoise(text string) string {
	if text == "" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, text)
}

func normalizeRequirementAutomationText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

func normalizeAutomationCodexTypedInput(text string) string {
	if text == "" || text == "\x03" {
		return text
	}
	if strings.HasSuffix(text, "\r\n") {
		text = strings.TrimSuffix(text, "\r\n")
	} else if strings.HasSuffix(text, "\n") {
		text = strings.TrimSuffix(text, "\n")
	} else if strings.HasSuffix(text, "\r") {
		text = strings.TrimSuffix(text, "\r")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !strings.Contains(text, "\n") {
		return text
	}
	parts := strings.Split(text, "\n")
	collapsed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		collapsed = append(collapsed, part)
	}
	return strings.Join(collapsed, " ")
}

func isCodexAutomationCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	binary := strings.Trim(fields[0], `"'`)
	if idx := strings.LastIndexAny(binary, `/\`); idx >= 0 && idx+1 < len(binary) {
		binary = binary[idx+1:]
	}
	return strings.EqualFold(binary, "codex")
}

func automationDispatchFailureReason(err error) string {
	if err == nil {
		return "unknown"
	}
	message := strings.TrimSpace(strings.ToLower(err.Error()))
	if message == "" {
		return "unknown"
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range message {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if lastUnderscore {
				continue
			}
			builder.WriteRune('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return "unknown"
	}
	return result
}

func trimRequirementAutomationBuffer(text string) string {
	if len(text) <= requirementAutomationMaxBufferBytes {
		return text
	}
	return text[len(text)-requirementAutomationMaxBufferBytes:]
}

func sortRequirementsForAutomation(items []Requirement) []Requirement {
	list := append([]Requirement(nil), items...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].SortOrder != list[j].SortOrder {
			return list[i].SortOrder < list[j].SortOrder
		}
		if !list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].CreatedAt.Before(list[j].CreatedAt)
		}
		return list[i].ID < list[j].ID
	})
	return list
}

func pickActiveRequirement(items []Requirement) *Requirement {
	for _, item := range items {
		if item.Status == RequirementStatusRunning {
			candidate := item
			return &candidate
		}
	}
	for _, item := range items {
		if item.Status == RequirementStatusPaused {
			candidate := item
			return &candidate
		}
	}
	for _, item := range items {
		if item.Status == RequirementStatusFailed {
			candidate := item
			return &candidate
		}
	}
	return nil
}

func pickNextPlannedRequirement(items []Requirement) *Requirement {
	for _, item := range items {
		if item.Status == RequirementStatusPlanning {
			candidate := item
			return &candidate
		}
	}
	return nil
}
