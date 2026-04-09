package filemanager

import "sync"

// ProjectExecutor serializes write operations per project id.
type ProjectExecutor struct {
	locks sync.Map
}

// NewProjectExecutor creates one project-scoped mutex executor.
func NewProjectExecutor() *ProjectExecutor {
	return &ProjectExecutor{}
}

// Run executes fn under a per-project lock.
func (e *ProjectExecutor) Run(projectID string, fn func() error) error {
	if e == nil {
		e = NewProjectExecutor()
	}
	lockValue, _ := e.locks.LoadOrStore(projectID, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}
