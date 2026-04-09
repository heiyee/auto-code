package runtime

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// CreateWithIdentity recreates a runtime session with fixed session and agent identity.
func (m *CLISessionManager) CreateWithIdentity(sessionID, agentID, command, workDir string) (*CLISession, error) {
	return m.CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir, nil, 0, 0)
}

// CreateWithIdentityAndEnv recreates one runtime session with fixed identity and env overrides.
func (m *CLISessionManager) CreateWithIdentityAndEnv(sessionID, agentID, command, workDir string, env map[string]string) (*CLISession, error) {
	return m.CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir, env, 0, 0)
}

// CreateWithIdentityAndEnvAndSize recreates one runtime session with explicit initial PTY size.
func (m *CLISessionManager) CreateWithIdentityAndEnvAndSize(sessionID, agentID, command, workDir string, env map[string]string, cols, rows int) (*CLISession, error) {
	if m == nil {
		return nil, errors.New("session manager is not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = newAgentID()
	}
	command = strings.TrimSpace(command)
	if command == "" {
		command = m.defaultCommand
	}
	workDir = strings.TrimSpace(workDir)

	if seq, ok := parseCLISessionSequence(sessionID); ok {
		m.EnsureNextIDAtLeast(seq)
	}

	m.mu.RLock()
	_, exists := m.sessions[sessionID]
	m.mu.RUnlock()
	if exists {
		return nil, errors.New("session already exists")
	}

	session := NewCLISessionWithSize(sessionID, agentID, command, workDir, env, cols, rows)
	session.setOutputHook(func(chunk []byte, at time.Time) {
		m.emitOutputChunk(sessionID, agentID, chunk, at)
	})
	if err := session.start(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()
	return session, nil
}

// parseCLISessionSequence parses `cli-000001` into numeric sequence.
func parseCLISessionSequence(sessionID string) (uint64, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if !strings.HasPrefix(sessionID, "cli-") {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(sessionID, "cli-"))
	if raw == "" {
		return 0, false
	}
	seq, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return seq, true
}
