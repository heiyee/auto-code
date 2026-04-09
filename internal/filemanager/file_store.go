package filemanager

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"auto-code/internal/domain"
)

const defaultMaxEditableFileBytes int64 = 1024 * 1024

// FileStore provides safe filesystem operations under one project root.
type FileStore struct {
	guard            *PathGuard
	maxEditableBytes int64
}

// NewFileStore creates one safe filesystem store.
func NewFileStore(guard *PathGuard) *FileStore {
	if guard == nil {
		guard = NewPathGuard()
	}
	return &FileStore{
		guard:            guard,
		maxEditableBytes: defaultMaxEditableFileBytes,
	}
}

// ListDirectory returns direct children nodes of one directory path.
func (s *FileStore) ListDirectory(projectRoot, requestPath string) ([]domain.FileNode, error) {
	if s == nil {
		s = NewFileStore(nil)
	}
	dirPath, relDir, err := s.guard.Resolve(projectRoot, requestPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, domain.ErrDirectoryExpected
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	nodes := make([]domain.FileNode, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		relPath := joinRelativePath(relDir, name)
		if s.guard.IsForbidden(relPath) {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			continue
		}

		node := domain.FileNode{
			Path:    relPath,
			Name:    name,
			ModTime: entryInfo.ModTime(),
		}
		if entryInfo.IsDir() {
			node.Type = domain.FileNodeTypeDirectory
			node.HasChildren = s.hasVisibleChildren(filepath.Join(dirPath, name), relPath)
		} else {
			node.Type = domain.FileNodeTypeFile
			node.Size = entryInfo.Size()
		}
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Type != nodes[j].Type {
			return nodes[i].Type == domain.FileNodeTypeDirectory
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

// ReadFile reads one text file with revision hash for optimistic locking.
func (s *FileStore) ReadFile(projectRoot, requestPath string) (domain.FileContent, error) {
	if s == nil {
		s = NewFileStore(nil)
	}
	filePath, relPath, err := s.guard.Resolve(projectRoot, requestPath)
	if err != nil {
		return domain.FileContent{}, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return domain.FileContent{}, err
	}
	if info.IsDir() {
		return domain.FileContent{}, domain.ErrDirectoryExpected
	}
	if info.Size() > s.maxEditableBytes {
		return domain.FileContent{}, domain.ErrFileTooLarge
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return domain.FileContent{}, err
	}
	if !isTextContent(raw) {
		return domain.FileContent{}, domain.ErrTextFileOnly
	}
	return domain.FileContent{
		Path:     relPath,
		Content:  string(raw),
		Revision: contentRevision(raw),
	}, nil
}

// SaveFile writes one text file when base revision matches current file revision.
func (s *FileStore) SaveFile(projectRoot string, input domain.FileSaveInput) (domain.FileContent, error) {
	if s == nil {
		s = NewFileStore(nil)
	}
	input.Path = strings.TrimSpace(input.Path)
	input.BaseRevision = strings.TrimSpace(input.BaseRevision)
	if input.Path == "" {
		return domain.FileContent{}, domain.ErrInvalidFilePath
	}
	if input.BaseRevision == "" {
		return domain.FileContent{}, domain.ErrFileRevisionRequired
	}

	filePath, relPath, err := s.guard.Resolve(projectRoot, input.Path)
	if err != nil {
		return domain.FileContent{}, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return domain.FileContent{}, err
	}
	if info.IsDir() {
		return domain.FileContent{}, domain.ErrDirectoryExpected
	}

	current, err := os.ReadFile(filePath)
	if err != nil {
		return domain.FileContent{}, err
	}
	if !isTextContent(current) {
		return domain.FileContent{}, domain.ErrTextFileOnly
	}
	currentRevision := contentRevision(current)
	if currentRevision != input.BaseRevision {
		return domain.FileContent{}, &domain.FileRevisionConflictError{
			Path:            relPath,
			CurrentRevision: currentRevision,
		}
	}

	nextContent := []byte(input.Content)
	if int64(len(nextContent)) > s.maxEditableBytes {
		return domain.FileContent{}, domain.ErrFileTooLarge
	}
	if !isTextContent(nextContent) {
		return domain.FileContent{}, domain.ErrTextFileOnly
	}
	if err := writeFileAtomically(filePath, nextContent, info.Mode().Perm()); err != nil {
		return domain.FileContent{}, err
	}
	return domain.FileContent{
		Path:     relPath,
		Content:  input.Content,
		Revision: contentRevision(nextContent),
	}, nil
}

// CreateFile creates one new file under project root.
func (s *FileStore) CreateFile(projectRoot string, input domain.FileCreateInput) (domain.FileContent, error) {
	if s == nil {
		s = NewFileStore(nil)
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return domain.FileContent{}, domain.ErrInvalidFilePath
	}
	filePath, relPath, err := s.guard.Resolve(projectRoot, input.Path)
	if err != nil {
		return domain.FileContent{}, err
	}
	if relPath == "" {
		return domain.FileContent{}, domain.ErrInvalidFilePath
	}
	if _, err := os.Stat(filePath); err == nil {
		return domain.FileContent{}, fmt.Errorf("path already exists: %s", relPath)
	}

	parent := filepath.Dir(filePath)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return domain.FileContent{}, err
	}
	if !parentInfo.IsDir() {
		return domain.FileContent{}, domain.ErrDirectoryExpected
	}

	content := []byte(input.Content)
	if int64(len(content)) > s.maxEditableBytes {
		return domain.FileContent{}, domain.ErrFileTooLarge
	}
	if !isTextContent(content) {
		return domain.FileContent{}, domain.ErrTextFileOnly
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return domain.FileContent{}, err
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		return domain.FileContent{}, err
	}
	if err := file.Sync(); err != nil {
		return domain.FileContent{}, err
	}
	return domain.FileContent{
		Path:     relPath,
		Content:  input.Content,
		Revision: contentRevision(content),
	}, nil
}

// CreateDirectory creates one directory path under project root.
func (s *FileStore) CreateDirectory(projectRoot string, input domain.DirectoryCreateInput) error {
	if s == nil {
		s = NewFileStore(nil)
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return domain.ErrInvalidFilePath
	}
	dirPath, relPath, err := s.guard.Resolve(projectRoot, input.Path)
	if err != nil {
		return err
	}
	if relPath == "" {
		return domain.ErrInvalidFilePath
	}
	if _, err := os.Stat(dirPath); err == nil {
		return fmt.Errorf("path already exists: %s", relPath)
	}
	return os.MkdirAll(dirPath, 0o755)
}

// Rename renames one file or directory path.
func (s *FileStore) Rename(projectRoot string, input domain.FileRenameInput) (string, error) {
	if s == nil {
		s = NewFileStore(nil)
	}
	input.OldPath = strings.TrimSpace(input.OldPath)
	input.NewPath = strings.TrimSpace(input.NewPath)
	if input.OldPath == "" || input.NewPath == "" {
		return "", domain.ErrInvalidFilePath
	}

	oldAbs, oldRel, err := s.guard.Resolve(projectRoot, input.OldPath)
	if err != nil {
		return "", err
	}
	newAbs, newRel, err := s.guard.Resolve(projectRoot, input.NewPath)
	if err != nil {
		return "", err
	}
	if oldRel == "" || newRel == "" {
		return "", domain.ErrInvalidFilePath
	}
	if oldRel == newRel {
		return "", nil
	}

	if _, err := os.Stat(oldAbs); err != nil {
		return "", err
	}
	if _, err := os.Stat(newAbs); err == nil {
		return "", fmt.Errorf("target already exists: %s", newRel)
	}

	parent := filepath.Dir(newAbs)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return "", err
	}
	if !parentInfo.IsDir() {
		return "", domain.ErrDirectoryExpected
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return "", err
	}
	return newRel, nil
}

// Delete removes one file or directory path under project root.
func (s *FileStore) Delete(projectRoot, requestPath string) error {
	if s == nil {
		s = NewFileStore(nil)
	}
	targetAbs, relPath, err := s.guard.Resolve(projectRoot, requestPath)
	if err != nil {
		return err
	}
	if relPath == "" {
		return domain.ErrInvalidFilePath
	}
	info, err := os.Stat(targetAbs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(targetAbs)
	}
	return os.Remove(targetAbs)
}

func (s *FileStore) hasVisibleChildren(dirAbsPath, relPath string) bool {
	entries, err := os.ReadDir(dirAbsPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		childRel := joinRelativePath(relPath, name)
		if s.guard.IsForbidden(childRel) {
			continue
		}
		return true
	}
	return false
}

func joinRelativePath(parent, child string) string {
	parent = strings.Trim(strings.TrimSpace(parent), "/")
	child = strings.Trim(strings.TrimSpace(child), "/")
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	return path.Join(parent, child)
}

func isTextContent(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	if !utf8.Valid(content) {
		return false
	}
	for _, value := range content {
		if value == 0 {
			return false
		}
	}
	return true
}

func contentRevision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func writeFileAtomically(targetPath string, content []byte, mode os.FileMode) error {
	parent := filepath.Dir(targetPath)
	tempFile, err := os.CreateTemp(parent, ".auto-code-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempName)
		}
	}()

	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, targetPath); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

// IsPathErrorNotExist reports whether err indicates missing file path.
func IsPathErrorNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
