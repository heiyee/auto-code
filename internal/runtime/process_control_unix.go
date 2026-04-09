//go:build !windows

package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	processTerminateWait = 2 * time.Second
	processPollInterval  = 80 * time.Millisecond
)

func applyCommandProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func resolveProcessGroupID(pid int) int {
	if pid <= 0 {
		return 0
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0
	}
	return pgid
}

// TerminateDetachedProcess terminates one stale CLI process from persisted metadata.
func TerminateDetachedProcess(pid, pgid int) error {
	return terminateProcessTree(pid, pgid)
}

func terminateProcessTree(pid, pgid int) error {
	if pid <= 0 {
		return nil
	}
	if !isProcessAlive(pid) {
		return nil
	}

	targetPGID := sanitizeTargetPGID(pid, pgid)
	if targetPGID > 0 {
		if err := signalProcessGroup(targetPGID, syscall.SIGTERM); err != nil {
			return err
		}
		if waitProcessExit(pid, processTerminateWait) {
			return nil
		}
		if err := signalProcessGroup(targetPGID, syscall.SIGKILL); err != nil {
			return err
		}
		if waitProcessExit(pid, processTerminateWait) {
			return nil
		}
		return fmt.Errorf("process %d did not exit after process-group kill", pid)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if waitProcessExit(pid, processTerminateWait) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if waitProcessExit(pid, processTerminateWait) {
		return nil
	}
	return fmt.Errorf("process %d did not exit after kill", pid)
}

func sanitizeTargetPGID(pid, pgid int) int {
	if pid <= 0 {
		return 0
	}
	if pgid <= 0 {
		pgid = resolveProcessGroupID(pid)
	}
	if pgid <= 0 {
		return 0
	}
	// Only use group-kill for dedicated groups created by Setpgid=true.
	// For shared process groups this could terminate unrelated processes.
	if pgid != pid {
		return 0
	}
	selfPGID, err := syscall.Getpgid(os.Getpid())
	if err == nil && selfPGID > 0 && selfPGID == pgid {
		return 0
	}
	return pgid
}

func signalProcessGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return nil
	}
	err := syscall.Kill(-pgid, sig)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func waitProcessExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}
	if timeout <= 0 {
		timeout = processTerminateWait
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return true
		}
		time.Sleep(processPollInterval)
	}
	return !isProcessAlive(pid)
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but cannot be signaled with current user.
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}
