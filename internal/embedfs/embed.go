package embedfs

import (
	"embed"
	"io/fs"
)

//go:embed frontend_dist
var webFS embed.FS

// FrontendDist returns the embedded React build filesystem mirrored during packaging.
func FrontendDist() (fs.FS, error) {
	return fs.Sub(webFS, "frontend_dist")
}
