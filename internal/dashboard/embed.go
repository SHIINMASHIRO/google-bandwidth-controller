package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed all:static
var staticFiles embed.FS

// GetStaticFS returns the embedded static files filesystem
func GetStaticFS() (fs.FS, error) {
	return fs.Sub(staticFiles, "static")
}
