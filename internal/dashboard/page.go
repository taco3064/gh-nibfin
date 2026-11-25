package dashboard

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
)

// apiCSP is the Content-Security-Policy of every response but the page.
const apiCSP = "default-src 'none'; frame-ancestors 'none'"

// pageScript exchanges the proof in the URL fragment for the session. It is
// the only script the page runs; it keeps the session in a variable only.
const pageScript = `(function () {
  var status = document.getElementById("status");
  var session = null;
  var m = /^#proof=([A-Z2-7]+)$/.exec(location.hash);
  if (!m) {
    status.textContent = "Open the session URL printed by gh nibfin dashboard.";
    return;
  }
  history.replaceState(null, "", location.pathname + location.search);
  var used = "This session URL was already used or is invalid. If you did not open it yourself, stop gh nibfin dashboard (Ctrl-C).";
  fetch("/api/session", { method: "POST", headers: { "X-Nibfin-Proof": m[1] } })
    .then(function (r) { return r.json(); })
    .then(function (b) {
      if (b.session) {
        session = b.session;
        status.textContent = "Session open.";
      } else if (b.error && b.error.code !== "proof_invalid") {
        status.textContent = b.error.code + ": " + b.error.message;
      } else {
        status.textContent = used;
      }
    })
    .catch(function (e) { status.textContent = "The session exchange failed: " + e; });
})();`

const pageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>gh nibfin dashboard</title>
</head>
<body>
<h1>gh nibfin dashboard</h1>
<p>The Dashboard UI is not built yet. The API is under <code>/api/</code>.</p>
<p id="status"></p>
<script>` + pageScript + `</script>
</body>
</html>
`

// pageCSP allows only pageScript, by its hash, and requests to this server.
var pageCSP = func() string {
	sum := sha256.Sum256([]byte(pageScript))
	return "default-src 'none'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
}()

// page serves the placeholder page; it holds no data.
func (s *server) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Security-Policy", pageCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(pageHTML))
}
