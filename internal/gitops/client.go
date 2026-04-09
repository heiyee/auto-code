package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// OperationListBranches lists available branch names for a repository.
	OperationListBranches = "list-branches"
	// OperationInspectLocalRepo inspects one local git repository path.
	OperationInspectLocalRepo = "inspect-local-repo"
)

const defaultCommandTimeout = 5 * time.Second

// QueryRequest describes one Git operation request.
type QueryRequest struct {
	Operation  string `json:"operation"`
	Repository string `json:"repository"`
	Limit      int    `json:"limit,omitempty"`
}

// QueryResult is a normalized response payload returned by Git operations.
type QueryResult struct {
	Operation     string   `json:"operation"`
	Repository    string   `json:"repository"`
	Values        []string `json:"values"`
	CurrentBranch string   `json:"current_branch,omitempty"`
	RemoteURL     string   `json:"remote_url,omitempty"`
}

// Client defines the generic Git operation interface used by upper layers.
type Client interface {
	Query(ctx context.Context, req QueryRequest) (QueryResult, error)
}

// CLIClient executes Git operations by invoking the local `git` command.
type CLIClient struct {
	timeout time.Duration
}

// NewCLIClient creates a command-based Git client with a default timeout fallback.
func NewCLIClient(timeout time.Duration) *CLIClient {
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	return &CLIClient{timeout: timeout}
}

// Query executes one supported Git operation and returns normalized values.
func (c *CLIClient) Query(ctx context.Context, req QueryRequest) (QueryResult, error) {
	operation := strings.ToLower(strings.TrimSpace(req.Operation))
	repository := normalizeRepository(req.Repository)
	if operation == "" {
		return QueryResult{}, errors.New("operation is required")
	}
	if repository == "" {
		return QueryResult{}, errors.New("repository is required")
	}
	if c == nil {
		return QueryResult{}, errors.New("git client is not initialized")
	}

	switch operation {
	case OperationListBranches:
		values, err := c.listBranches(ctx, repository, req.Limit)
		if err != nil {
			return QueryResult{}, err
		}
		return QueryResult{
			Operation:  operation,
			Repository: repository,
			Values:     values,
		}, nil
	case OperationInspectLocalRepo:
		return c.inspectLocalRepo(ctx, repository, req.Limit)
	default:
		return QueryResult{}, fmt.Errorf("unsupported operation: %s", operation)
	}
}

// inspectLocalRepo reads origin remote url, current branch and local branches from one local repository.
func (c *CLIClient) inspectLocalRepo(ctx context.Context, repository string, limit int) (QueryResult, error) {
	if !isLocalRepository(repository) {
		return QueryResult{}, errors.New("inspect-local-repo only supports local repository path")
	}

	branches, err := c.listLocalBranches(ctx, repository)
	if err != nil {
		return QueryResult{}, err
	}
	branches = uniqueSortedValues(branches)
	if limit > 0 && len(branches) > limit {
		branches = branches[:limit]
	}

	currentBranch, err := c.currentLocalBranch(ctx, repository)
	if err != nil {
		return QueryResult{}, err
	}

	remoteURL := c.originRemoteURL(ctx, repository)

	return QueryResult{
		Operation:     OperationInspectLocalRepo,
		Repository:    repository,
		Values:        branches,
		CurrentBranch: currentBranch,
		RemoteURL:     remoteURL,
	}, nil
}

// listBranches resolves branch names for local or remote repositories.
func (c *CLIClient) listBranches(ctx context.Context, repository string, limit int) ([]string, error) {
	var (
		values []string
		err    error
	)

	if isLocalRepository(repository) {
		values, err = c.listLocalBranches(ctx, repository)
	} else {
		values, err = c.listRemoteBranches(ctx, repository)
	}
	if err != nil {
		return nil, err
	}
	values = uniqueSortedValues(values)
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

// listLocalBranches queries branch names from a local Git repository path.
func (c *CLIClient) listLocalBranches(ctx context.Context, repository string) ([]string, error) {
	info, err := os.Stat(repository)
	if err != nil {
		return nil, fmt.Errorf("stat repository: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("repository must be a directory for local query")
	}
	stdout, err := c.runGitCommand(ctx, "git", "-C", repository, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	return parseLocalBranchOutput(stdout), nil
}

// currentLocalBranch returns current checked-out branch.
func (c *CLIClient) currentLocalBranch(ctx context.Context, repository string) (string, error) {
	current, err := c.runGitCommand(ctx, "git", "-C", repository, "branch", "--show-current")
	if err == nil {
		current = strings.TrimSpace(current)
		if current != "" {
			return current, nil
		}
	}
	fallback, fallbackErr := c.runGitCommand(ctx, "git", "-C", repository, "rev-parse", "--abbrev-ref", "HEAD")
	if fallbackErr != nil {
		return "", fallbackErr
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return "", errors.New("current branch is empty")
	}
	return fallback, nil
}

// originRemoteURL returns configured remote.origin.url. Empty means not configured.
func (c *CLIClient) originRemoteURL(ctx context.Context, repository string) string {
	remote, err := c.runGitCommand(ctx, "git", "-C", repository, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(remote)
}

// listRemoteBranches queries branch names by reading remote refs.
func (c *CLIClient) listRemoteBranches(ctx context.Context, repository string) ([]string, error) {
	stdout, err := c.runGitCommand(ctx, "git", "ls-remote", "--heads", repository)
	if err != nil {
		return nil, err
	}
	return parseRemoteBranchOutput(stdout), nil
}

// runGitCommand executes one git command with timeout and captures stdout.
func (c *CLIClient) runGitCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, c.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
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

// normalizeRepository converts repository path text into an absolute local path when possible.
func normalizeRepository(repository string) string {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return ""
	}
	if strings.Contains(repository, "://") || strings.HasPrefix(repository, "git@") {
		return repository
	}
	if filepath.IsAbs(repository) {
		return filepath.Clean(repository)
	}
	if strings.HasPrefix(repository, ".") {
		if abs, err := filepath.Abs(repository); err == nil {
			return filepath.Clean(abs)
		}
	}
	return repository
}

// isLocalRepository detects whether a repository identifier should be queried as local path.
func isLocalRepository(repository string) bool {
	if repository == "" {
		return false
	}
	if strings.Contains(repository, "://") || strings.HasPrefix(repository, "git@") {
		return false
	}
	if strings.Contains(repository, "@") && strings.Contains(repository, ":") {
		return false
	}
	if filepath.IsAbs(repository) {
		return true
	}
	if strings.HasPrefix(repository, ".") || strings.HasPrefix(repository, "/") {
		return true
	}
	if info, err := os.Stat(repository); err == nil && info.IsDir() {
		return true
	}
	return false
}

// parseLocalBranchOutput parses one-branch-per-line local output.
func parseLocalBranchOutput(raw string) []string {
	lines := strings.Split(raw, "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		values = append(values, name)
	}
	return values
}

// parseRemoteBranchOutput parses `git ls-remote --heads` output into branch names.
func parseRemoteBranchOutput(raw string) []string {
	lines := strings.Split(raw, "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		ref := strings.TrimSpace(fields[1])
		if ref == "" {
			continue
		}
		ref = strings.TrimPrefix(ref, "refs/heads/")
		if ref == "" {
			continue
		}
		values = append(values, ref)
	}
	return values
}

// uniqueSortedValues removes duplicates and sorts branch names alphabetically.
func uniqueSortedValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
