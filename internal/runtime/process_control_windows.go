//go:build windows

package runtime

import (
	"errors"
	"os"
	"os/exec"
)

func applyCommandProcessGroup(cmd *exec.Cmd) {
	_ = cmd
}

func resolveProcessGroupID(pid int) int {
	if pid <= 0 {
		return 0
	}
	return pid
}

// TerminateDetachedProcess terminates one stale CLI process from persisted metadata.
func TerminateDetachedProcess(pid, pgid int) error {
	return terminateProcessTree(pid, pgid)
}

func terminateProcessTree(pid, pgid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
