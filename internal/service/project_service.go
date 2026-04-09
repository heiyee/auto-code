package service

import (
	"errors"
	"path/filepath"
	"strings"

	"auto-code/internal/domain"
	"auto-code/internal/persistence"
)

// ProjectService encapsulates project domain rules and delegates persistence to SQLiteStore.
type ProjectService struct {
	store   *persistence.SQLiteStore
	appRoot string
}

// NewProjectService builds a project service with required dependencies.
func NewProjectService(store *persistence.SQLiteStore, appRoot string) *ProjectService {
	return &ProjectService{store: store, appRoot: appRoot}
}

// List returns all projects with counters.
func (s *ProjectService) List() ([]domain.ProjectSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("project service is not initialized")
	}
	list, err := s.store.ListProjectSummaries()
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].WorkDir = s.NormalizeWorkDir(list[i].WorkDir)
	}
	return list, nil
}

// Get returns one project by id.
func (s *ProjectService) Get(projectID string) (*domain.Project, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("project service is not initialized")
	}
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	project.WorkDir = s.NormalizeWorkDir(project.WorkDir)
	return project, nil
}

// Create validates input and creates a project.
func (s *ProjectService) Create(input domain.ProjectMutation) (*domain.Project, error) {
	normalized, err := s.normalizeMutation(input)
	if err != nil {
		return nil, err
	}
	project, err := s.store.CreateProject(normalized)
	if err != nil {
		return nil, err
	}
	project.WorkDir = s.NormalizeWorkDir(project.WorkDir)
	return project, nil
}

// Update validates input and updates a project.
func (s *ProjectService) Update(projectID string, input domain.ProjectMutation) (*domain.Project, error) {
	normalized, err := s.normalizeMutation(input)
	if err != nil {
		return nil, err
	}
	project, err := s.store.UpdateProject(projectID, normalized)
	if err != nil {
		return nil, err
	}
	project.WorkDir = s.NormalizeWorkDir(project.WorkDir)
	return project, nil
}

// Delete removes a project and returns cascade statistics.
func (s *ProjectService) Delete(projectID string) (domain.DeleteProjectStats, error) {
	if s == nil || s.store == nil {
		return domain.DeleteProjectStats{}, errors.New("project service is not initialized")
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return domain.DeleteProjectStats{}, errors.New("project id is required")
	}
	return s.store.DeleteProject(projectID)
}

// EffectiveWorkDir resolves actual work dir with default fallback strategy.
func (s *ProjectService) EffectiveWorkDir(project domain.Project) string {
	return project.EffectiveWorkDir(s.appRoot)
}

// NormalizeWorkDir converts empty work_dir into computed default path.
func (s *ProjectService) NormalizeWorkDir(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return ""
	}
	if filepath.IsAbs(workDir) {
		return filepath.Clean(workDir)
	}
	return filepath.Clean(filepath.Join(s.appRoot, workDir))
}

// normalizeMutation validates and normalizes project mutation payload.
func (s *ProjectService) normalizeMutation(input domain.ProjectMutation) (domain.ProjectMutation, error) {
	if s == nil || s.store == nil {
		return domain.ProjectMutation{}, errors.New("project service is not initialized")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Repository = strings.TrimSpace(input.Repository)
	input.Branch = strings.TrimSpace(input.Branch)
	input.WorkDir = strings.TrimSpace(input.WorkDir)

	if input.Name == "" {
		return domain.ProjectMutation{}, errors.New("project name is required")
	}
	if input.Repository == "" {
		return domain.ProjectMutation{}, errors.New("repository is required")
	}
	if input.Branch == "" {
		return domain.ProjectMutation{}, errors.New("branch is required")
	}

	if input.WorkDir != "" {
		if !filepath.IsAbs(input.WorkDir) {
			input.WorkDir = filepath.Clean(filepath.Join(s.appRoot, input.WorkDir))
		} else {
			input.WorkDir = filepath.Clean(input.WorkDir)
		}
	}
	return input, nil
}
