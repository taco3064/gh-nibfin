package dashboard

import (
	"bytes"
	"crypto/rand"
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

// noncePlaceholder is the CSP nonce Vite writes into the built HTML
// (html.cspNonce in dashboard/vite.config.ts). Each response replaces it with
// a fresh nonce, which the page passes to the <style> elements Radix creates.
const noncePlaceholder = "NIBFIN_CSP_NONCE"

// nonceCSP is appCSP with nonce also allowed for <style> elements; scripts
// stay same-origin only.
func nonceCSP(nonce string) string {
	return strings.Replace(appCSP, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1)
}

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
	serveHTML(w, r, "index.html")
}

// serveHTML serves one HTML file of the built UI. A file that carries the
// nonce placeholder gets a fresh nonce per response, in its body and in the
// style-src of its policy; any other is served as it is with appCSP.
func serveHTML(w http.ResponseWriter, r *http.Request, name string) {
	b, err := fs.ReadFile(appFiles, name)
	if err != nil {
		notFound(w, r)
		return
	}
	csp := appCSP
	if bytes.Contains(b, []byte(noncePlaceholder)) {
		nonce := rand.Text()
		b = bytes.ReplaceAll(b, []byte(noncePlaceholder), []byte(nonce))
		csp = nonceCSP(nonce)
	}
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Content-Type", assetTypes[".html"])
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

// asset serves one regular file of the built UI. Hidden paths, folders and
// missing files are the JSON 404 of any unknown route.
func (s *server) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if isAPI(r.URL.Path) || !fs.ValidPath(name) || hidden(name) || !regular(name) {
		notFound(w, r)
		return
	}
	if path.Ext(name) == ".html" {
		// Like http.ServeFileFS, the index by name redirects to its folder.
		if path.Base(name) == "index.html" {
			w.Header().Set("Content-Security-Policy", appCSP)
			w.Header().Set("Location", "./")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		serveHTML(w, r, name)
		return
	}
	w.Header().Set("Content-Security-Policy", appCSP)
	if ctype, ok := assetTypes[path.Ext(name)]; ok {
		w.Header().Set("Content-Type", ctype)
	}
	http.ServeFileFS(w, r, appFiles, name)
}

// assetTypes fixes the content type of the files the build emits. The mime
// package reads them from the Windows registry, which can differ per machine
// (for example application/javascript, or text/plain for .css, which the
// browser then refuses under nosniff). Other extensions fall back to it.
var assetTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".ico":   "image/x-icon",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".map":   "application/json",
	".png":   "image/png",
	".svg":   "image/svg+xml",
	".txt":   "text/plain; charset=utf-8",
	".webp":  "image/webp",
	".woff2": "font/woff2",
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
