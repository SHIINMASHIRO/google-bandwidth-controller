package dashboard

import (
	"io"
	"net/http"
	"strings"
)

// Handler serves the dashboard static files
type Handler struct {
	staticFS http.FileSystem
}

// NewHandler creates a new dashboard handler
func NewHandler() (*Handler, error) {
	staticFS, err := GetStaticFS()
	if err != nil {
		return nil, err
	}

	return &Handler{
		staticFS: http.FS(staticFS),
	}, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Serve index.html for root path
	if path == "/" || path == "" {
		h.serveFile(w, r, "index.html")
		return
	}

	// Handle /static/* paths - strip prefix
	if strings.HasPrefix(path, "/static/") {
		filePath := strings.TrimPrefix(path, "/static/")
		h.serveFile(w, r, filePath)
		return
	}

	// 404 for other paths
	http.NotFound(w, r)
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := h.staticFS.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set content type
	if strings.HasSuffix(name, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else if strings.HasSuffix(name, ".js") {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	} else if strings.HasSuffix(name, ".css") {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}

	http.ServeContent(w, r, name, stat.ModTime(), f.(io.ReadSeeker))
}

// RegisterRoutes registers dashboard routes on the given mux
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/", h)
}
