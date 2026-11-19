package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNativeContinuationOptionalDiagnosticsAreSafeAndAdditive(t *testing.T) {
	for _, diagnostic := range []string{
		`{"kind":"http","httpStatus":503,"token":"secret","body":"secret"}`,
		`{"kind":"network","message":"secret"}`,
		`{"kind":"invalid-response","resource":"secret"}`,
		`{"kind":"read","httpStatus":401}`,
		`{"kind":"future","message":"secret"}`,
		`{"kind":"http","httpStatus":"bad"}`,
		`{"kind":44}`, `"bad"`, `null`, `[]`,
	} {
		data := []byte(`{"status":"pending","results":[],"githubApi":` + diagnostic + `}`)
		result, err := decodeNativeContinuation(data, errors.New("CORE_PENDING_RESULT"))
		if err != nil || result.Status != "pending" || result.Results == nil {
			t.Fatal("optional diagnostics changed continuation semantics", diagnostic, err)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "secret") {
			t.Fatal("unapproved diagnostic data was forwarded", string(encoded))
		}
	}
}

func TestNativeContinuationSeparatesNestedPayloadFromDirectExit(t *testing.T) {
	data := []byte(`{"status":"pending","results":[]}`)
	if _, err := nativeContinuationPayload(data); err != nil {
		t.Fatal("valid nested pending rejected", err)
	}
	if _, err := decodeNativeContinuation(data, nil); err == nil {
		t.Fatal("direct pending must use Core exit 2")
	}
	for _, invalid := range []string{
		`{"status":"pending"}`, `{"status":"completed","results":[]}`,
		`{"status":"paused","results":null}`, `{"status":22,"results":[]}`,
	} {
		if _, err := nativeContinuationPayload([]byte(invalid)); err == nil {
			t.Fatal("invalid lifecycle payload accepted", invalid)
		}
	}
}
