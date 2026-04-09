package filemanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"auto-code/internal/domain"
)

const defaultGitCommandTimeout = 10 * time.Second

// GitClient reads git status snapshots from local repositories.
type GitClient struct {
	timeout time.Duration
}

// NewGitClient creates one command-based git client.
func NewGitClient(timeout time.Duration) *GitClient {
	if timeout <= 0 {
		timeout = defaultGitCommandTimeout
	}
	return &GitClient{timeout: timeout}
}

// Status returns parsed `git status --porcelain --branch` results.
func (c *GitClient) Status(ctx context.Context, projectRoot string) (domain.GitStatus, error) {
	if c == nil {
		c = NewGitClient(defaultGitCommandTimeout)
	}
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return domain.GitStatus{}, err
	}
	output, err := c.runGit(ctx, root, "status", "--porcelain=1", "--branch")
	if err != nil {
		return domain.GitStatus{}, err
	}
	return parseGitStatusPorcelain(output), nil
}

// DiffAgainstHEAD returns unified diff against HEAD for one file path.
func (c *GitClient) DiffAgainstHEAD(ctx context.Context, projectRoot, relPath string) (string, error) {
	return c.runGit(ctx, projectRoot, "diff", "--no-color", "HEAD", "--", relPath)
}

// DiffCached returns unified staged diff for one file path.
func (c *GitClient) DiffCached(ctx context.Context, projectRoot, relPath string) (string, error) {
	return c.runGit(ctx, projectRoot, "diff", "--no-color", "--cached", "--", relPath)
}

// DiffWorkingTree returns unified unstaged diff for one file path.
func (c *GitClient) DiffWorkingTree(ctx context.Context, projectRoot, relPath string) (string, error) {
	return c.runGit(ctx, projectRoot, "diff", "--no-color", "--", relPath)
}

func (c *GitClient) runGit(ctx context.Context, projectRoot string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, c.timeout)
		defer cancel()
	}

	commandArgs := append([]string{"-C", projectRoot}, args...)
	cmd := exec.CommandContext(runCtx, "git", commandArgs...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("git command timeout")
		}
		if text == "" {
			return "", fmt.Errorf("git command failed: %w", err)
		}
		return "", fmt.Errorf("git command failed: %s", text)
	}
	return text, nil
}

func parseGitStatusPorcelain(output string) domain.GitStatus {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	result := domain.GitStatus{
		Changes:   make([]domain.FileChange, 0),
		Staged:    make([]domain.FileChange, 0),
		Untracked: make([]string, 0),
	}
	if len(lines) == 0 {
		return result
	}
	stagedSeen := make(map[string]struct{})
	changesSeen := make(map[string]struct{})
	untrackedSeen := make(map[string]struct{})

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "##") {
			result.CurrentBranch = parseCurrentBranch(line)
			continue
		}
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		pathText := parseStatusPath(strings.TrimSpace(line[3:]))
		if pathText == "" {
			continue
		}

		if code == "??" {
			if _, exists := untrackedSeen[pathText]; !exists {
				untrackedSeen[pathText] = struct{}{}
				result.Untracked = append(result.Untracked, pathText)
			}
			continue
		}

		xStatus := statusCode(code[0])
		yStatus := statusCode(code[1])
		if isConflictStatus(code[0], code[1]) {
			if _, exists := changesSeen[pathText]; !exists {
				changesSeen[pathText] = struct{}{}
				result.Changes = append(result.Changes, domain.FileChange{
					Path:   pathText,
					Status: domain.GitFileStatusConflict,
				})
			}
			continue
		}
		if xStatus != "" {
			if _, exists := stagedSeen[pathText]; !exists {
				stagedSeen[pathText] = struct{}{}
				result.Staged = append(result.Staged, domain.FileChange{
					Path:   pathText,
					Status: xStatus,
				})
			}
		}
		if yStatus != "" {
			if _, exists := changesSeen[pathText]; !exists {
				changesSeen[pathText] = struct{}{}
				result.Changes = append(result.Changes, domain.FileChange{
					Path:   pathText,
					Status: yStatus,
				})
			}
		}
	}
	return result
}

func parseCurrentBranch(line string) string {
	line = strings.TrimSpace(strings.TrimPrefix(line, "##"))
	if line == "" {
		return ""
	}
	if strings.HasPrefix(line, "HEAD") {
		return "HEAD"
	}
	if idx := strings.Index(line, "..."); idx >= 0 {
		return strings.TrimSpace(line[:idx])
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

func parseStatusPath(pathField string) string {
	pathField = strings.TrimSpace(pathField)
	if pathField == "" {
		return ""
	}
	if strings.Contains(pathField, " -> ") {
		parts := strings.Split(pathField, " -> ")
		pathField = strings.TrimSpace(parts[len(parts)-1])
	}
	pathField = strings.Trim(pathField, "\"")
	pathField = strings.ReplaceAll(pathField, "\\", "/")
	return strings.TrimSpace(pathField)
}

func statusCode(marker byte) string {
	switch marker {
	case 'M':
		return domain.GitFileStatusModified
	case 'A':
		return domain.GitFileStatusAdded
	case 'D':
		return domain.GitFileStatusDeleted
	case 'U':
		return domain.GitFileStatusConflict
	default:
		return ""
	}
}

func isConflictStatus(x, y byte) bool {
	if x == 'U' || y == 'U' {
		return true
	}
	if x == 'A' && y == 'A' {
		return true
	}
	if x == 'D' && y == 'D' {
		return true
	}
	return false
}
