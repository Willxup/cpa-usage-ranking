package httpserver

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// Frontend serves a built site from a filesystem root that cannot escape via symlinks.
// Keep it open until the HTTP server has drained; Close then releases the root handle.
type Frontend struct{ root *os.Root }

var fingerprintedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8}\.[a-z0-9]+$`)

func OpenFrontend(directory string) (*Frontend, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open frontend directory: %w", err)
	}
	file, err := root.Open("index.html")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("open frontend index: %w", err)
	}
	info, statErr := file.Stat()
	file.Close()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		root.Close()
		return nil, fmt.Errorf("frontend index must be a non-empty regular file")
	}
	return &Frontend{root: root}, nil
}

func (f *Frontend) Close() error { return f.root.Close() }

// Mount intentionally has no arbitrary SPA fallback: the current UI uses page anchors.
// Unknown APIs, resources and browser paths keep their real JSON 404/405 responses.
func (f *Frontend) Mount(router *gin.Engine) {
	for _, route := range []string{"/", "/index.html", "/favicon.svg", "/assets/*path"} {
		router.GET(route, f.serve)
		router.HEAD(route, f.serve)
	}
}

func (f *Frontend) serve(c *gin.Context) {
	name := strings.TrimPrefix(c.Request.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		writeError(c, http.StatusNotFound, "not_found", "Not found")
		return
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			writeError(c, http.StatusNotFound, "not_found", "Not found")
			return
		}
	}
	file, err := f.root.Open(name)
	if err != nil {
		writeError(c, http.StatusNotFound, "not_found", "Not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(c, http.StatusNotFound, "not_found", "Not found")
		return
	}
	c.Header("Cache-Control", "no-cache")
	// Only Vite's top-level generated assets use fingerprint caching. Public
	// subdirectories contain stable filenames (for example brand/icon-512.png).
	if strings.HasPrefix(name, "assets/") && strings.Count(name, "/") == 1 && fingerprintedAsset.MatchString(info.Name()) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}
