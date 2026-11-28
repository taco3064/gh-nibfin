package runnerreq_test

import (
	"regexp"
	"strings"
	"testing"

	"filippo.io/age/armor"

	"github.com/taco3064/gh-nibfin/internal/runnerreq"
)

// The token block is decrypted with filippo.io/age/armor (internal/op), so its
// delimiters must be armor's own.
func TestTokenDelimitersMatchAgeArmor(t *testing.T) {
	if runnerreq.RunnerTokenBegin != armor.Header {
		t.Errorf("RunnerTokenBegin = %q, want armor.Header %q", runnerreq.RunnerTokenBegin, armor.Header)
	}
	if runnerreq.RunnerTokenEnd != armor.Footer {
		t.Errorf("RunnerTokenEnd = %q, want armor.Footer %q", runnerreq.RunnerTokenEnd, armor.Footer)
	}
}

// Markers are whole body or comment lines, hidden in rendered Markdown.
func TestMarkersAreOneHTMLCommentLine(t *testing.T) {
	re := regexp.MustCompile(`^<!-- nibfin:[a-z-]+ -->$`)
	for name, m := range map[string]string{
		"RunnerRequestMarker":      runnerreq.RunnerRequestMarker,
		"RunnerRegistrationMarker": runnerreq.RunnerRegistrationMarker,
		"RunnerRefusedMarker":      runnerreq.RunnerRefusedMarker,
	} {
		if !re.MatchString(m) {
			t.Errorf("%s = %q, want one line like <!-- nibfin:name -->", name, m)
		}
	}
}

// Fields are read as "<field>: <value>" lines, so a field name cannot hold
// a colon or whitespace.
func TestFieldNamesAreSingleTokens(t *testing.T) {
	for name, f := range map[string]string{
		"RunnerKeyIDField":     runnerreq.RunnerKeyIDField,
		"RunnerDeployKeyField": runnerreq.RunnerDeployKeyField,
		"RunnerRequesterField": runnerreq.RunnerRequesterField,
		"RunnerExpiresField":   runnerreq.RunnerExpiresField,
		"RunnerReasonField":    runnerreq.RunnerReasonField,
	} {
		if f == "" || strings.ContainsAny(f, ": \t\r\n") {
			t.Errorf("%s = %q, want a non-empty name without ':' or whitespace", name, f)
		}
	}
}
