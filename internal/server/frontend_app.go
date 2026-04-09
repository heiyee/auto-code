package server

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// handleFrontendApp serves the built React workbench and falls back to index.html for client routes.
func (a *App) handleFrontendApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	frontendFS, err := a.frontendAppFS()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			http.Error(w, "frontend build is missing, run `npm run build` in frontend first", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.URL.Path == "/app" || r.URL.Path == "/app/" {
		if err := serveFrontendFile(w, r, frontendFS, "index.html"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	relativeURLPath := strings.TrimPrefix(r.URL.Path, "/app/")
	cleanedURLPath := strings.TrimPrefix(path.Clean("/"+relativeURLPath), "/")
	if cleanedURLPath != "" && cleanedURLPath != "." {
		if fs.ValidPath(cleanedURLPath) {
			info, statErr := fs.Stat(frontendFS, cleanedURLPath)
			if statErr == nil && !info.IsDir() {
				if err := serveFrontendFile(w, r, frontendFS, cleanedURLPath); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
				http.Error(w, statErr.Error(), http.StatusInternalServerError)
				return
			}
		}
		if path.Ext(cleanedURLPath) != "" {
			http.NotFound(w, r)
			return
		}
	}

	if err := serveFrontendFile(w, r, frontendFS, "index.html"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) frontendAppFS() (fs.FS, error) {
	distDir := strings.TrimSpace(a.frontendDistDir)
	if distDir != "" {
		diskFS := os.DirFS(distDir)
		if _, err := fs.Stat(diskFS, "index.html"); err == nil {
			return diskFS, nil
		}
	}

	if a.frontendEmbeddedFS != nil {
		if _, err := fs.Stat(a.frontendEmbeddedFS, "index.html"); err == nil {
			return a.frontendEmbeddedFS, nil
		}
	}

	return nil, fs.ErrNotExist
}

func serveFrontendFile(w http.ResponseWriter, r *http.Request, frontendFS fs.FS, name string) error {
	file, err := frontendFS.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fs.ErrNotExist
	}

	if seeker, ok := file.(io.ReadSeeker); ok {
		http.ServeContent(w, r, info.Name(), info.ModTime(), seeker)
		return nil
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), bytes.NewReader(data))
	return nil
}
