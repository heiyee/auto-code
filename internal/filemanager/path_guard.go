package filemanager

import (
	"errors"
	"path"
	"path/filepath"
	"strings"

	"auto-code/internal/domain"
)

var defaultForbiddenPatterns = []string{
	".git/config",
	".env",
	".env.*",
	"*.key",
	"*.pem",
}

// PathGuard validates project-relative paths and protects sensitive files.
type PathGuard struct {
	forbiddenPatterns []string
}

// NewPathGuard builds one path guard with default forbidden patterns.
func NewPathGuard() *PathGuard {
	return &PathGuard{
		forbiddenPatterns: append([]string(nil), defaultForbiddenPatterns...),
	}
}

// Resolve resolves one request path into canonical absolute/relative forms.
func (g *PathGuard) Resolve(projectRoot, requestPath string) (string, string, error) {
	if g == nil {
		g = NewPathGuard()
	}
	root, err := canonicalProjectRoot(projectRoot)
	if err != nil {
		return "", "", err
	}
	normalized, err := normalizeRequestPath(requestPath)
	if err != nil {
		return "", "", err
	}
	if normalized != "" && g.IsForbidden(normalized) {
		return "", "", domain.ErrForbiddenPath
	}

	target := root
	if normalized != "" {
		target = filepath.Join(root, filepath.FromSlash(normalized))
	}
	canonicalTarget, err := resolveWithParentFallback(target)
	if err != nil {
		return "", "", err
	}

	rel, err := filepath.Rel(root, canonicalTarget)
	if err != nil {
		return "", "", domain.ErrInvalidFilePath
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", domain.ErrPathOutsideProject
	}
	if rel == "." {
		rel = ""
	}
	rel = filepath.ToSlash(rel)
	if rel != "" && g.IsForbidden(rel) {
		return "", "", domain.ErrForbiddenPath
	}
	return canonicalTarget, rel, nil
}

// IsForbidden reports whether one project-relative path should be blocked.
func (g *PathGuard) IsForbidden(relPath string) bool {
	if g == nil {
		g = NewPathGuard()
	}
	relPath = strings.TrimSpace(filepath.ToSlash(relPath))
	relPath = strings.Trim(relPath, "/")
	if relPath == "" {
		return false
	}
	if relPath == ".git" ||
		strings.HasPrefix(relPath, ".git/") ||
		strings.HasSuffix(relPath, "/.git") ||
		strings.Contains(relPath, "/.git/") {
		return true
	}

	base := path.Base(relPath)
	for _, pattern := range g.forbiddenPatterns {
		if matched, _ := path.Match(pattern, relPath); matched {
			return true
		}
		if matched, _ := path.Match(pattern, base); matched {
			return true
		}
	}
	return false
}

func canonicalProjectRoot(projectRoot string) (string, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return "", domain.ErrInvalidFilePath
	}
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", domain.ErrInvalidFilePath
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if errors.Is(err, filepath.ErrBadPattern) {
			return "", domain.ErrInvalidFilePath
		}
		return "", err
	}
	return filepath.Clean(realPath), nil
}

func normalizeRequestPath(requestPath string) (string, error) {
	requestPath = strings.TrimSpace(requestPath)
	if strings.Contains(requestPath, "\x00") {
		return "", domain.ErrInvalidFilePath
	}
	requestPath = strings.ReplaceAll(requestPath, "\\", "/")
	if requestPath == "" {
		return "", nil
	}
	cleaned := path.Clean(requestPath)
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." {
		return "", nil
	}
	if strings.TrimSpace(cleaned) == "" {
		return "", domain.ErrInvalidFilePath
	}
	return cleaned, nil
}

func resolveWithParentFallback(targetPath string) (string, error) {
	targetPath = filepath.Clean(targetPath)
	if _, err := filepath.EvalSymlinks(targetPath); err == nil {
		realPath, evalErr := filepath.EvalSymlinks(targetPath)
		if evalErr != nil {
			return "", evalErr
		}
		return filepath.Clean(realPath), nil
	}

	parent := filepath.Dir(targetPath)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(realParent, filepath.Base(targetPath))), nil
}
