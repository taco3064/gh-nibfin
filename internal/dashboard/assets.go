package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// distFS holds the Dashboard UI that npm run build in dashboard/ writes to
// dist/. all: keeps dist/.gitignore, the committed placeholder that lets the
// folder embed without a build, and Rollup's _-prefixed chunks.
//
//go:embed all:dist
var distFS embed.FS

// appFiles is the built UI; tests replace it.
var appFiles fs.FS = func() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}()

// appCSP is the Content-Security-Policy of the built page and its files: only
// same-origin scripts, styles, fonts, images and requests, nothing inline.
const appCSP = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// built reports whether appFiles holds a built UI.
func built() bool {
	fi, err := fs.Stat(appFiles, "index.html")
	return err == nil && fi.Mode().IsRegular()
}

// page serves the built UI, or the placeholder in a binary built without it.
func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if !built() {
		s.placeholder(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", appCSP)
	http.ServeFileFS(w, r, appFiles, "index.html")
}

// asset serves one regular file of the built UI. Hidden paths, folders and
// missing files are the JSON 404 of any unknown route.
func (s *server) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if isAPI(r.URL.Path) || !fs.ValidPath(name) || hidden(name) || !regular(name) {
		notFound(w, r)
		return
	}
	w.Header().Set("Content-Security-Policy", appCSP)
	http.ServeFileFS(w, r, appFiles, name)
}

func hidden(name string) bool {
	for seg := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func regular(name string) bool {
	fi, err := fs.Stat(appFiles, name)
	return err == nil && fi.Mode().IsRegular()
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, op.Errorf(op.CodeNotFound, "no route %s %s", r.Method, r.URL.Path))
}

// isAPI reports whether p, as sent or cleaned, lies under /api/, the only
// routes that carry data or operations.
func isAPI(p string) bool {
	under := func(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }
	return under(p) || under(path.Clean(p))
}
