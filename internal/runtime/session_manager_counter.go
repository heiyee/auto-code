package runtime

import "sync/atomic"

// EnsureNextIDAtLeast advances the internal id counter to at least the provided value.
func (m *CLISessionManager) EnsureNextIDAtLeast(value uint64) {
	if m == nil || value == 0 {
		return
	}
	for {
		current := atomic.LoadUint64(&m.nextID)
		if current >= value {
			return
		}
		if atomic.CompareAndSwapUint64(&m.nextID, current, value) {
			return
		}
	}
}
