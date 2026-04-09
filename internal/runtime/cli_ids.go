package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// newAgentID creates a stable external identifier for one CLI runtime agent.
func newAgentID() string {
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return fmt.Sprintf("agent-%d", time.Now().UnixNano())
	}
	return "agent-" + hex.EncodeToString(token)
}
