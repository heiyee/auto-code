package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"auto-code/internal/domain"
	"auto-code/internal/filemanager"
)

const defaultProjectFileGitTimeout = 10 * time.Second

// ProjectFileService provides file-manager business orchestration per project.
type ProjectFileService struct {
	projectService     *ProjectService
	requirementService *RequirementService
	fileStore          *filemanager.FileStore
	gitClient          *filemanager.GitClient
	executor           *filemanager.ProjectExecutor
}

// NewProjectFileService builds one file manager service.
func NewProjectFileService(projectService *ProjectService, requirementService *RequirementService) *ProjectFileService {
	guard := filemanager.NewPathGuard()
	return &ProjectFileService{
		projectService:     projectService,
		requirementService: requirementService,
		fileStore:          filemanager.NewFileStore(guard),
		gitClient:          filemanager.NewGitClient(defaultProjectFileGitTimeout),
		executor:           filemanager.NewProjectExecutor(),
	}
}

// CurrentRequirement returns one running/paused requirement for status bar display.
func (s *ProjectFileService) CurrentRequirement(projectID string) (*domain.CurrentRequirementInfo, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project id is required")
	}

	requirements, err := s.requirementService.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	candidate := pickCurrentRequirement(requirements)
	if candidate == nil {
		return nil, nil
	}
	return &domain.CurrentRequirementInfo{
		RequirementID: candidate.ID,
		Title:         candidate.Title,
		Status:        candidate.Status,
		ProjectName:   candidate.ProjectName,
		StartedAt:     candidate.StartedAt,
	}, nil
}

// ListFiles lists one directory under project with git status decorations.
func (s *ProjectFileService) ListFiles(ctx context.Context, projectID, requestPath string) ([]domain.FileNode, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	root, err := s.resolveProjectRoot(projectID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.fileStore.ListDirectory(root, requestPath)
	if err != nil {
		return nil, err
	}

	gitStatus, err := s.gitClient.Status(ctx, root)
	if err != nil {
		// File listing should still work in non-git directories.
		return nodes, nil
	}
	lookup := gitStatusLookup(gitStatus)
	for i := range nodes {
		nodes[i].GitStatus = lookupForNode(nodes[i], lookup)
	}
	return nodes, nil
}

// ReadFile reads one text file under project root.
func (s *ProjectFileService) ReadFile(projectID, requestPath string) (domain.FileContent, error) {
	if err := s.ensureReady(); err != nil {
		return domain.FileContent{}, err
	}
	root, err := s.resolveProjectRoot(projectID)
	if err != nil {
		return domain.FileContent{}, err
	}
	return s.fileStore.ReadFile(root, requestPath)
}

// SaveFile saves one text file with optimistic-lock conflict detection.
func (s *ProjectFileService) SaveFile(projectID string, input domain.FileSaveInput) (domain.FileContent, error) {
	if err := s.ensureReady(); err != nil {
		return domain.FileContent{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.FileContent{}, errors.New("project id is required")
	}

	var result domain.FileContent
	err := s.executor.Run(projectID, func() error {
		root, err := s.resolveProjectRoot(projectID)
		if err != nil {
			return err
		}
		result, err = s.fileStore.SaveFile(root, input)
		return err
	})
	return result, err
}

// CreateFile creates one new file under project root.
func (s *ProjectFileService) CreateFile(projectID string, input domain.FileCreateInput) (domain.FileContent, error) {
	if err := s.ensureReady(); err != nil {
		return domain.FileContent{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.FileContent{}, errors.New("project id is required")
	}

	var result domain.FileContent
	err := s.executor.Run(projectID, func() error {
		root, err := s.resolveProjectRoot(projectID)
		if err != nil {
			return err
		}
		result, err = s.fileStore.CreateFile(root, input)
		return err
	})
	return result, err
}

// CreateDirectory creates one new directory under project root.
func (s *ProjectFileService) CreateDirectory(projectID string, input domain.DirectoryCreateInput) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("project id is required")
	}

	return s.executor.Run(projectID, func() error {
		root, err := s.resolveProjectRoot(projectID)
		if err != nil {
			return err
		}
		return s.fileStore.CreateDirectory(root, input)
	})
}

// Rename renames one file or directory path under project root.
func (s *ProjectFileService) Rename(projectID string, input domain.FileRenameInput) (string, error) {
	if err := s.ensureReady(); err != nil {
		return "", err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", errors.New("project id is required")
	}

	var result string
	err := s.executor.Run(projectID, func() error {
		root, err := s.resolveProjectRoot(projectID)
		if err != nil {
			return err
		}
		result, err = s.fileStore.Rename(root, input)
		return err
	})
	return result, err
}

// Delete removes one file/directory path under project root.
func (s *ProjectFileService) Delete(projectID, requestPath string) error {
	if err := s.ensureReady(); err != nil {
		return err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("project id is required")
	}

	return s.executor.Run(projectID, func() error {
		root, err := s.resolveProjectRoot(projectID)
		if err != nil {
			return err
		}
		return s.fileStore.Delete(root, requestPath)
	})
}

// GitStatus returns git working-tree status for one project.
func (s *ProjectFileService) GitStatus(ctx context.Context, projectID string) (domain.GitStatus, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitStatus{}, err
	}
	root, err := s.resolveProjectRoot(projectID)
	if err != nil {
		return domain.GitStatus{}, err
	}
	return s.gitClient.Status(ctx, root)
}

// GitDiff returns one file unified diff for git mode center preview.
func (s *ProjectFileService) GitDiff(ctx context.Context, projectID, requestPath string) (domain.GitFileDiff, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitFileDiff{}, err
	}
	root, err := s.resolveProjectRoot(projectID)
	if err != nil {
		return domain.GitFileDiff{}, err
	}

	guard := filemanager.NewPathGuard()
	_, relPath, err := guard.Resolve(root, requestPath)
	if err != nil {
		return domain.GitFileDiff{}, err
	}
	if strings.TrimSpace(relPath) == "" {
		return domain.GitFileDiff{}, domain.ErrInvalidFilePath
	}

	statusSnapshot, err := s.gitClient.Status(ctx, root)
	if err != nil {
		return domain.GitFileDiff{}, err
	}
	statusLookup := gitStatusLookup(statusSnapshot)
	status := strings.TrimSpace(statusLookup[relPath])

	if status == domain.GitFileStatusUntracked {
		file, readErr := s.fileStore.ReadFile(root, relPath)
		if readErr != nil {
			return domain.GitFileDiff{}, readErr
		}
		return domain.GitFileDiff{
			Path:   relPath,
			Status: status,
			Diff:   buildUntrackedFileDiff(relPath, file.Content),
		}, nil
	}

	diffText, diffErr := s.gitClient.DiffAgainstHEAD(ctx, root, relPath)
	if diffErr != nil || strings.TrimSpace(diffText) == "" {
		if cachedDiff, cachedErr := s.gitClient.DiffCached(ctx, root, relPath); cachedErr == nil && strings.TrimSpace(cachedDiff) != "" {
			diffText = cachedDiff
			diffErr = nil
		}
	}
	if (diffErr != nil || strings.TrimSpace(diffText) == "") && status != domain.GitFileStatusDeleted {
		if workingDiff, workingErr := s.gitClient.DiffWorkingTree(ctx, root, relPath); workingErr == nil && strings.TrimSpace(workingDiff) != "" {
			diffText = workingDiff
			diffErr = nil
		}
	}
	if diffErr != nil {
		return domain.GitFileDiff{}, diffErr
	}
	if strings.TrimSpace(diffText) == "" {
		diffText = "(no diff)"
	}
	return domain.GitFileDiff{
		Path:   relPath,
		Status: status,
		Diff:   diffText,
	}, nil
}

// GitBranches lists local branches and current branch for git sidebar actions.
func (s *ProjectFileService) GitBranches(ctx context.Context, projectID string) (domain.GitBranchList, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitBranchList{}, err
	}
	root, err := s.resolveProjectRoot(projectID)
	if err != nil {
		return domain.GitBranchList{}, err
	}

	current, err := s.gitClient.CurrentBranch(ctx, root)
	if err != nil {
		return domain.GitBranchList{}, err
	}
	branches, err := s.gitClient.ListLocalBranches(ctx, root)
	if err != nil {
		return domain.GitBranchList{}, err
	}

	items := make([]domain.GitBranchInfo, 0, len(branches)+1)
	hasCurrent := false
	for _, name := range branches {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		isCurrent := name == current
		if isCurrent {
			hasCurrent = true
		}
		items = append(items, domain.GitBranchInfo{
			Name:    name,
			Current: isCurrent,
		})
	}
	if strings.TrimSpace(current) != "" && !hasCurrent {
		items = append(items, domain.GitBranchInfo{
			Name:    current,
			Current: true,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Current != items[j].Current {
			return items[i].Current
		}
		return items[i].Name < items[j].Name
	})
	return domain.GitBranchList{
		CurrentBranch: current,
		Branches:      items,
	}, nil
}

// GitCheckoutBranch switches to one existing branch.
func (s *ProjectFileService) GitCheckoutBranch(ctx context.Context, projectID, branch string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}
	normalizedBranch, err := normalizeGitRefName(branch)
	if err != nil {
		return domain.GitActionResult{}, err
	}

	var result domain.GitActionResult
	err = s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}
		if validateErr := s.gitClient.ValidateBranchName(ctx, root, normalizedBranch); validateErr != nil {
			return domain.ErrGitRefNameInvalid
		}
		output, checkoutErr := s.gitClient.CheckoutBranch(ctx, root, normalizedBranch)
		if checkoutErr != nil {
			return checkoutErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Message:       "branch switched",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitCreateBranch creates one new branch and optionally checks it out.
func (s *ProjectFileService) GitCreateBranch(ctx context.Context, projectID, branch string, checkout bool) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}
	normalizedBranch, err := normalizeGitRefName(branch)
	if err != nil {
		return domain.GitActionResult{}, err
	}

	var result domain.GitActionResult
	err = s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}
		if validateErr := s.gitClient.ValidateBranchName(ctx, root, normalizedBranch); validateErr != nil {
			return domain.ErrGitRefNameInvalid
		}
		output, createErr := s.gitClient.CreateBranch(ctx, root, normalizedBranch, checkout)
		if createErr != nil {
			return createErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Message:       "branch created",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitCreateTag creates one lightweight tag at HEAD.
func (s *ProjectFileService) GitCreateTag(ctx context.Context, projectID, tag string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}
	normalizedTag, err := normalizeGitRefName(tag)
	if err != nil {
		return domain.GitActionResult{}, err
	}

	var result domain.GitActionResult
	err = s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}
		if validateErr := s.gitClient.ValidateTagName(ctx, root, normalizedTag); validateErr != nil {
			return domain.ErrGitRefNameInvalid
		}
		output, createErr := s.gitClient.CreateTag(ctx, root, normalizedTag)
		if createErr != nil {
			return createErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Tag:           normalizedTag,
			Message:       "tag created",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitStagePath stages one path into git index.
func (s *ProjectFileService) GitStagePath(ctx context.Context, projectID, requestPath string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}

	var result domain.GitActionResult
	err := s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}

		guard := filemanager.NewPathGuard()
		_, relPath, pathErr := guard.Resolve(root, requestPath)
		if pathErr != nil {
			return pathErr
		}
		if strings.TrimSpace(relPath) == "" {
			return domain.ErrInvalidFilePath
		}

		output, stageErr := s.gitClient.AddPath(ctx, root, relPath)
		if stageErr != nil {
			return stageErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Message:       "path staged",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitUnstagePath removes one path from staged index.
func (s *ProjectFileService) GitUnstagePath(ctx context.Context, projectID, requestPath string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}

	var result domain.GitActionResult
	err := s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}

		guard := filemanager.NewPathGuard()
		_, relPath, pathErr := guard.Resolve(root, requestPath)
		if pathErr != nil {
			return pathErr
		}
		if strings.TrimSpace(relPath) == "" {
			return domain.ErrInvalidFilePath
		}

		output, unstageErr := s.gitClient.UnstagePath(ctx, root, relPath)
		if unstageErr != nil {
			return unstageErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Message:       "path unstaged",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitCommit creates one commit from staged changes only.
func (s *ProjectFileService) GitCommit(ctx context.Context, projectID, message string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return domain.GitActionResult{}, domain.ErrGitCommitMessageRequired
	}

	var result domain.GitActionResult
	err := s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}
		status, statusErr := s.gitClient.Status(ctx, root)
		if statusErr != nil {
			return statusErr
		}
		if len(status.Staged) == 0 {
			return domain.ErrGitNoStagedChanges
		}
		output, commitErr := s.gitClient.Commit(ctx, root, message)
		if commitErr != nil {
			return commitErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		hash, hashErr := s.gitClient.ShortHeadCommit(ctx, root)
		if hashErr != nil {
			return hashErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			CommitHash:    strings.TrimSpace(hash),
			Message:       "commit created",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitPull executes git pull with optional remote/branch.
func (s *ProjectFileService) GitPull(ctx context.Context, projectID, remote, branch string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}

	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if branch != "" {
		normalizedBranch, normalizeErr := normalizeGitRefName(branch)
		if normalizeErr != nil {
			return domain.GitActionResult{}, normalizeErr
		}
		branch = normalizedBranch
	}
	if remote != "" {
		normalizedRemote, normalizeErr := normalizeGitRefName(remote)
		if normalizeErr != nil {
			return domain.GitActionResult{}, normalizeErr
		}
		remote = normalizedRemote
	}

	var result domain.GitActionResult
	err := s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}
		output, pullErr := s.gitClient.Pull(ctx, root, remote, branch)
		if pullErr != nil {
			return pullErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Message:       "pull completed",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

// GitPush executes git push with optional remote/branch.
func (s *ProjectFileService) GitPush(ctx context.Context, projectID, remote, branch string) (domain.GitActionResult, error) {
	if err := s.ensureReady(); err != nil {
		return domain.GitActionResult{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.GitActionResult{}, errors.New("project id is required")
	}

	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if branch != "" {
		normalizedBranch, normalizeErr := normalizeGitRefName(branch)
		if normalizeErr != nil {
			return domain.GitActionResult{}, normalizeErr
		}
		branch = normalizedBranch
	}
	if remote != "" {
		normalizedRemote, normalizeErr := normalizeGitRefName(remote)
		if normalizeErr != nil {
			return domain.GitActionResult{}, normalizeErr
		}
		remote = normalizedRemote
	}

	var result domain.GitActionResult
	err := s.executor.Run(projectID, func() error {
		root, resolveErr := s.resolveProjectRoot(projectID)
		if resolveErr != nil {
			return resolveErr
		}
		output, pushErr := s.gitClient.Push(ctx, root, remote, branch)
		if pushErr != nil {
			return pushErr
		}
		current, currentErr := s.gitClient.CurrentBranch(ctx, root)
		if currentErr != nil {
			return currentErr
		}
		result = domain.GitActionResult{
			CurrentBranch: strings.TrimSpace(current),
			Message:       "push completed",
			Output:        strings.TrimSpace(output),
		}
		return nil
	})
	return result, err
}

func (s *ProjectFileService) ensureReady() error {
	if s == nil {
		return errors.New("project file service is not initialized")
	}
	if s.projectService == nil || s.requirementService == nil || s.fileStore == nil || s.gitClient == nil || s.executor == nil {
		return errors.New("project file service is not initialized")
	}
	return nil
}

func (s *ProjectFileService) resolveProjectRoot(projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", errors.New("project id is required")
	}
	project, err := s.projectService.Get(projectID)
	if err != nil {
		return "", err
	}

	candidates := make([]string, 0, 3)
	if workDir := strings.TrimSpace(project.WorkDir); workDir != "" {
		candidates = append(candidates, s.projectService.NormalizeWorkDir(workDir))
	}
	if repoPath := strings.TrimSpace(project.Repository); isLikelyLocalRepositoryPath(repoPath) {
		candidates = append(candidates, s.projectService.NormalizeWorkDir(repoPath))
	}
	if fallback := strings.TrimSpace(s.projectService.EffectiveWorkDir(*project)); fallback != "" {
		candidates = append(candidates, s.projectService.NormalizeWorkDir(fallback))
	}

	if root, ok := firstExistingDirectory(candidates); ok {
		return root, nil
	}
	return "", fmt.Errorf("project %s work dir not found", projectID)
}

func firstExistingDirectory(candidates []string) (string, bool) {
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if _, duplicated := seen[candidate]; duplicated {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		return candidate, true
	}
	return "", false
}

func isLikelyLocalRepositoryPath(repository string) bool {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return false
	}
	lower := strings.ToLower(repository)
	if strings.Contains(lower, "://") {
		return false
	}
	if strings.HasPrefix(lower, "git@") {
		return false
	}
	return true
}

func buildUntrackedFileDiff(path, content string) string {
	path = strings.TrimSpace(path)
	lines := strings.Split(content, "\n")
	lineCount := len(lines)
	if lineCount > 0 && lines[lineCount-1] == "" {
		lineCount--
	}

	var builder strings.Builder
	builder.WriteString("diff --git a/")
	builder.WriteString(path)
	builder.WriteString(" b/")
	builder.WriteString(path)
	builder.WriteString("\n")
	builder.WriteString("new file mode 100644\n")
	builder.WriteString("--- /dev/null\n")
	builder.WriteString("+++ b/")
	builder.WriteString(path)
	builder.WriteString("\n")
	builder.WriteString("@@ -0,0 +1,")
	builder.WriteString(fmt.Sprintf("%d", lineCount))
	builder.WriteString(" @@\n")
	for i := 0; i < lineCount; i++ {
		builder.WriteString("+")
		builder.WriteString(lines[i])
		builder.WriteString("\n")
	}
	return builder.String()
}

func pickCurrentRequirement(items []domain.Requirement) *domain.Requirement {
	if len(items) == 0 {
		return nil
	}

	candidates := make([]domain.Requirement, 0, len(items))
	for _, item := range items {
		if item.Status == domain.RequirementStatusRunning || item.Status == domain.RequirementStatusPaused {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := requirementStatusOrder(candidates[i].Status)
		right := requirementStatusOrder(candidates[j].Status)
		if left != right {
			return left < right
		}
		if !candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
		}
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})
	picked := candidates[0]
	return &picked
}

func requirementStatusOrder(status string) int {
	switch status {
	case domain.RequirementStatusRunning:
		return 0
	case domain.RequirementStatusPaused:
		return 1
	default:
		return 2
	}
}

func gitStatusLookup(status domain.GitStatus) map[string]string {
	lookup := make(map[string]string, len(status.Changes)+len(status.Staged)+len(status.Untracked))
	for _, path := range status.Untracked {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		lookup[path] = domain.GitFileStatusUntracked
	}
	for _, item := range status.Changes {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		lookup[path] = item.Status
	}
	for _, item := range status.Staged {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		lookup[path] = domain.GitFileStatusStaged
	}
	return lookup
}

func lookupForNode(node domain.FileNode, lookup map[string]string) string {
	if status, ok := lookup[node.Path]; ok {
		return status
	}
	if node.Type != domain.FileNodeTypeDirectory {
		return ""
	}
	prefix := node.Path + "/"
	best := ""
	for path, status := range lookup {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		if gitStatusPriority(status) > gitStatusPriority(best) {
			best = status
		}
	}
	return best
}

func gitStatusPriority(status string) int {
	switch status {
	case domain.GitFileStatusConflict:
		return 6
	case domain.GitFileStatusDeleted:
		return 5
	case domain.GitFileStatusModified:
		return 4
	case domain.GitFileStatusAdded:
		return 3
	case domain.GitFileStatusStaged:
		return 2
	case domain.GitFileStatusUntracked:
		return 1
	default:
		return 0
	}
}

func normalizeGitRefName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", domain.ErrGitRefNameRequired
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return "", domain.ErrGitRefNameInvalid
	}
	if strings.Contains(name, "\x00") {
		return "", domain.ErrGitRefNameInvalid
	}
	if strings.HasPrefix(name, "-") {
		return "", domain.ErrGitRefNameInvalid
	}
	return name, nil
}
