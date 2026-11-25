//go:build dashboardscreenshots

package dashboard

import (
	"os"
	"path/filepath"
	"runtime"
)

// With -tags dashboardscreenshots, which only npm run screenshots in
// dashboard/ uses, the server serves the screenshot build in
// dashboard/.screenshots-dist/ instead of the embedded UI. No release or CI
// build has the tag, so no product binary reads UI files from disk.
func init() {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("dashboardscreenshots: cannot locate the source folder")
	}
	appFiles = os.DirFS(filepath.Join(filepath.Dir(file), "..", "..", "dashboard", ".screenshots-dist"))
}
