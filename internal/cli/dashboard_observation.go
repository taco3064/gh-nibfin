package cli

import (
	"encoding/json"
	"errors"
	"strings"
)

// JSON escaping can expand each of the 16 KiB token bytes sixfold.
const observationInputLimit = 6*16384 + 32

func observationInput(token []byte) ([]byte, error) {
	credential := strings.TrimSpace(string(token))
	if len(token) > 16385 || len(credential) > 16384 || credential == "" ||
		strings.ContainsAny(credential, " \t\r\n\x00") {
		return nil, errors.New("CREDENTIAL_UNAVAILABLE")
	}
	return json.Marshal(struct {
		Token string `json:"token"`
	}{credential})
}

func observationCommands(data []byte) ([]string, error) {
	var help struct {
		Commands []string `json:"commands"`
	}
	if len(data) > 65536 || json.Unmarshal(data, &help) != nil ||
		!overviewCommand(help.Commands, "observe-local-work") {
		return nil, errors.New("UNSUPPORTED_CORE")
	}
	return help.Commands, nil
}

// Only this finite denial survives a supported nonzero observation exit.
func observationDenial(data []byte) ([]byte, bool) {
	var denial struct {
		SchemaVersion int    `json:"schemaVersion"`
		Status        string `json:"status"`
	}
	if len(data) <= 1024 && json.Unmarshal(data, &denial) == nil &&
		denial.SchemaVersion == 1 && denial.Status == "denied" {
		return []byte(`{"schemaVersion":1,"status":"denied"}`), true
	}
	return nil, false
}

func overviewAdoptedIDs(host *sharedHost, member int64) ([]int64, bool) {
	if host == nil || host.MemberID != member || len(host.AdoptedRepositoryIDs) > 32 {
		return nil, true
	}
	for _, id := range host.AdoptedRepositoryIDs {
		if id <= 0 {
			return nil, true
		}
	}
	return host.AdoptedRepositoryIDs, false
}
