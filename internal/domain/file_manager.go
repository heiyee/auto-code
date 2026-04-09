package domain

import (
	"errors"
	"fmt"
	"time"
)

const (
	// FileNodeTypeFile marks one normal file node.
	FileNodeTypeFile = "file"
	// FileNodeTypeDirectory marks one directory node.
	FileNodeTypeDirectory = "directory"
)

const (
	// GitFileStatusUntracked means the file is not tracked by git.
	GitFileStatusUntracked = "untracked"
	// GitFileStatusModified means the file has unstaged changes.
	GitFileStatusModified = "modified"
	// GitFileStatusAdded means the file was newly added.
	GitFileStatusAdded = "added"
	// GitFileStatusDeleted means the file was removed.
	GitFileStatusDeleted = "deleted"
	// GitFileStatusConflict means the file is in merge conflict.
	GitFileStatusConflict = "conflict"
	// GitFileStatusStaged means the file has staged changes.
	GitFileStatusStaged = "staged"
)

var (
	// ErrInvalidFilePath indicates the user supplied path is invalid.
	ErrInvalidFilePath = errors.New("invalid file path")
	// ErrPathOutsideProject indicates the target path escaped project root.
	ErrPathOutsideProject = errors.New("path is outside project directory")
	// ErrForbiddenPath indicates the path targets protected files.
	ErrForbiddenPath = errors.New("path is forbidden")
	// ErrFileRevisionRequired indicates save operation misses base revision.
	ErrFileRevisionRequired = errors.New("base revision is required")
	// ErrDirectoryExpected indicates one operation expects directory path.
	ErrDirectoryExpected = errors.New("directory path is required")
	// ErrTextFileOnly indicates binary files are not editable in web editor.
	ErrTextFileOnly = errors.New("only text files are editable")
	// ErrFileTooLarge indicates file exceeds editor size limit.
	ErrFileTooLarge = errors.New("file is too large to edit")
	// ErrGitRefNameRequired indicates git branch/tag name is missing.
	ErrGitRefNameRequired = errors.New("git ref name is required")
	// ErrGitRefNameInvalid indicates git branch/tag name is invalid.
	ErrGitRefNameInvalid = errors.New("invalid git ref name")
	// ErrGitCommitMessageRequired indicates commit message is missing.
	ErrGitCommitMessageRequired = errors.New("commit message is required")
	// ErrGitNoStagedChanges indicates commit requested with empty staged set.
	ErrGitNoStagedChanges = errors.New("no staged changes to commit")
)

// FileNode describes one file or directory node for project explorer.
type FileNode struct {
	Path        string      `json:"path"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Size        int64       `json:"size"`
	ModTime     time.Time   `json:"modTime"`
	GitStatus   string      `json:"gitStatus,omitempty"`
	HasChildren bool        `json:"hasChildren,omitempty"`
	Children    []*FileNode `json:"children,omitempty"`
}

// FileContent carries editable file content plus optimistic-lock revision.
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

// FileSaveInput describes one save request from frontend editor.
type FileSaveInput struct {
	Path         string
	Content      string
	BaseRevision string
}

// FileCreateInput describes one new-file create request.
type FileCreateInput struct {
	Path    string
	Content string
}

// DirectoryCreateInput describes one mkdir request.
type DirectoryCreateInput struct {
	Path string
}

// FileRenameInput describes one rename request.
type FileRenameInput struct {
	OldPath string
	NewPath string
}

// FileChange is one Git change entry.
type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// GitStatus describes repository working tree state.
type GitStatus struct {
	CurrentBranch string       `json:"currentBranch"`
	Changes       []FileChange `json:"changes"`
	Staged        []FileChange `json:"staged"`
	Untracked     []string     `json:"untracked"`
}

// GitFileDiff describes one file diff result for git mode preview.
type GitFileDiff struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Diff   string `json:"diff"`
}

// GitBranchInfo describes one local branch entry.
type GitBranchInfo struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// GitBranchList contains current branch and all local branches.
type GitBranchList struct {
	CurrentBranch string          `json:"currentBranch"`
	Branches      []GitBranchInfo `json:"branches"`
}

// GitActionResult describes one git write operation result.
type GitActionResult struct {
	CurrentBranch string `json:"currentBranch,omitempty"`
	CommitHash    string `json:"commitHash,omitempty"`
	Tag           string `json:"tag,omitempty"`
	Message       string `json:"message,omitempty"`
	Output        string `json:"output,omitempty"`
}

// CurrentRequirementInfo describes current running/paused requirement of one project.
type CurrentRequirementInfo struct {
	RequirementID string     `json:"requirementId"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	ProjectName   string     `json:"projectName"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
}

// FileRevisionConflictError indicates optimistic-lock conflict on file save.
type FileRevisionConflictError struct {
	Path            string
	CurrentRevision string
}

// Error returns human readable conflict message.
func (e *FileRevisionConflictError) Error() string {
	if e == nil {
		return "file revision conflict"
	}
	if e.Path == "" {
		return "file revision conflict"
	}
	return fmt.Sprintf("file revision conflict: %s", e.Path)
}
