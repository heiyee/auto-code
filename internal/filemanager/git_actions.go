package filemanager

import (
	"context"
	"strings"
)

// CurrentBranch returns current checked-out branch name.
func (c *GitClient) CurrentBranch(ctx context.Context, projectRoot string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	output, err := c.runGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// ShortHeadCommit returns short commit hash of HEAD.
func (c *GitClient) ShortHeadCommit(ctx context.Context, projectRoot string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	output, err := c.runGit(ctx, root, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// ListLocalBranches returns all local branches.
func (c *GitClient) ListLocalBranches(ctx context.Context, projectRoot string) ([]string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	output, err := c.runGit(ctx, root, "branch", "--list", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(output, "\n")
	branches := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		branches = append(branches, name)
	}
	return branches, nil
}

// ValidateBranchName validates one branch name with git check-ref-format.
func (c *GitClient) ValidateBranchName(ctx context.Context, projectRoot, name string) error {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return err
	}
	_, err = c.runGit(ctx, root, "check-ref-format", "--branch", strings.TrimSpace(name))
	return err
}

// ValidateTagName validates one tag name with git check-ref-format.
func (c *GitClient) ValidateTagName(ctx context.Context, projectRoot, name string) error {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return err
	}
	tagName := strings.TrimSpace(name)
	_, err = c.runGit(ctx, root, "check-ref-format", "refs/tags/"+tagName)
	return err
}

// CheckoutBranch checks out one existing branch.
func (c *GitClient) CheckoutBranch(ctx context.Context, projectRoot, branch string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return c.runGit(ctx, root, "checkout", strings.TrimSpace(branch))
}

// CreateBranch creates one new local branch.
func (c *GitClient) CreateBranch(ctx context.Context, projectRoot, branch string, checkout bool) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	branch = strings.TrimSpace(branch)
	if checkout {
		return c.runGit(ctx, root, "checkout", "-b", branch)
	}
	return c.runGit(ctx, root, "branch", branch)
}

// CreateTag creates one new lightweight tag at HEAD.
func (c *GitClient) CreateTag(ctx context.Context, projectRoot, tag string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return c.runGit(ctx, root, "tag", strings.TrimSpace(tag))
}

// AddAll stages all working tree changes.
func (c *GitClient) AddAll(ctx context.Context, projectRoot string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return c.runGit(ctx, root, "add", "-A")
}

// AddPath stages one path in working tree.
func (c *GitClient) AddPath(ctx context.Context, projectRoot, relPath string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return c.runGit(ctx, root, "add", "--", strings.TrimSpace(relPath))
}

// UnstagePath removes one path from staged index while keeping work tree.
func (c *GitClient) UnstagePath(ctx context.Context, projectRoot, relPath string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(relPath)
	output, restoreErr := c.runGit(ctx, root, "restore", "--staged", "--", path)
	if restoreErr == nil {
		return output, nil
	}
	return c.runGit(ctx, root, "reset", "HEAD", "--", path)
}

// Commit creates one commit from staged changes.
func (c *GitClient) Commit(ctx context.Context, projectRoot, message string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	return c.runGit(ctx, root, "commit", "--no-gpg-sign", "-m", strings.TrimSpace(message))
}

// Pull executes git pull for given remote/branch with ff-only strategy.
func (c *GitClient) Pull(ctx context.Context, projectRoot, remote, branch string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	args := []string{"pull", "--ff-only"}
	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if remote != "" {
		args = append(args, remote)
		if branch != "" {
			args = append(args, branch)
		}
	} else if branch != "" {
		args = append(args, "origin", branch)
	}
	return c.runGit(ctx, root, args...)
}

// Push executes git push for given remote/branch.
func (c *GitClient) Push(ctx context.Context, projectRoot, remote, branch string) (string, error) {
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}
	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if remote == "" {
		remote = "origin"
	}
	if branch != "" {
		return c.runGit(ctx, root, "push", "-u", remote, branch)
	}
	return c.runGit(ctx, root, "push", remote)
}
