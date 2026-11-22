package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
)

// The owner's Claude credential stays in this process's memory only. It is read
// from the terminal, sent once through the private credential pipe and cleared.
type claudeCredential struct {
	kind  string
	value []byte
}

func (c *claudeCredential) clear() {
	if c != nil {
		clear(c.value)
	}
}

// Core stores and reads the credential with a 1024-byte bound and its kinds allow
// at most 513 characters; a longer line cannot be accepted, so reading stops.
const claudeCredentialLimit = 1024

var claudeCredentialBody = regexp.MustCompile(`^[0-9]{2}-[A-Za-z0-9_-]{20,500}$`)

func classifyClaudeCredential(value []byte) (string, error) {
	for _, k := range claudeCredentialKinds {
		if bytes.HasPrefix(value, []byte(k.prefix)) &&
			claudeCredentialBody.Match(value[len(k.prefix):]) {
			return k.kind, nil
		}
	}
	return "", errors.New("CLAUDE_CREDENTIAL_KIND_UNRECOGNIZED")
}

// Only the kind, the fixed 12-character prefix and the length are ever shown.
func claudeCredentialSummary(c claudeCredential) string {
	return fmt.Sprintf("%s %s... (%d characters)", c.kind, c.value[:12], len(c.value))
}

// readHiddenLine reads one line from a terminal with echo and line editing off.
// Its buffer never grows, so no partial copy of the secret is left behind.
func readHiddenLine(in io.Reader, limit int) ([]byte, error) {
	line := make([]byte, 0, limit)
	var b [1]byte
	for {
		if n, err := in.Read(b[:]); n == 0 || err != nil {
			clear(line[:cap(line)])
			return nil, errors.New("CLAUDE_CREDENTIAL_INPUT_CANCELLED")
		}
		switch c := b[0]; {
		case c == '\r' || c == '\n':
			return line, nil
		case c == 0x03 || c == 0x04:
			clear(line[:cap(line)])
			return nil, errors.New("CLAUDE_CREDENTIAL_INPUT_CANCELLED")
		case c == 0x7f || c == 0x08:
			if len(line) > 0 {
				line[len(line)-1] = 0
				line = line[:len(line)-1]
			}
		case len(line) == limit:
			clear(line[:cap(line)])
			return nil, errors.New("CLAUDE_CREDENTIAL_INPUT_INVALID")
		default:
			line = append(line, c)
		}
	}
}

// promptClaudeCredential asks once on the hidden terminal, names the kind
// locally and asks the owner to confirm the masked summary.
func promptClaudeCredential(tty io.ReadWriter) (*claudeCredential, error) {
	fmt.Fprint(tty, "Paste the Claude token from `claude setup-token` "+
		"or an Anthropic API key (input hidden): ")
	line, err := readHiddenLine(tty, claudeCredentialLimit)
	fmt.Fprint(tty, "\n")
	if err != nil {
		return nil, err
	}
	value := bytes.TrimSpace(line)
	kind, err := classifyClaudeCredential(value)
	if err != nil {
		clear(line)
		return nil, err
	}
	credential := &claudeCredential{kind: kind, value: value}
	fmt.Fprintf(tty, "Claude credential: %s. Continue? [y/N] ",
		claudeCredentialSummary(*credential))
	// The longest accepted answer is "yes"; anything longer is a refusal.
	answer, err := readHiddenLine(tty, 8)
	fmt.Fprint(tty, "\n")
	if err != nil || !slices.Contains([]string{"y", "Y", "yes"}, string(answer)) {
		credential.clear()
		return nil, errors.New("CLAUDE_CREDENTIAL_NOT_CONFIRMED")
	}
	return credential, nil
}

// The Claude envelope replaces `codex` with Core's advertised `claude` key.
func claudeCredentialEnvelope(token []byte, c claudeCredential) ([]byte, error) {
	if kind, err := classifyClaudeCredential(c.value); err != nil || kind != c.kind {
		return nil, errors.New("MANAGED_CREDENTIAL_INPUT_INVALID")
	}
	// The value holds only JSON-safe characters, so quoting it needs no escaping.
	quoted := make([]byte, 0, len(c.value)+2)
	quoted = append(append(append(quoted, '"'), c.value...), '"')
	defer clear(quoted)
	data, err := json.Marshal(struct {
		Version int             `json:"schemaVersion"`
		Token   string          `json:"githubToken"`
		Claude  json.RawMessage `json:"claude"`
	}{1, string(token), quoted})
	if err != nil || len(data) > 65536 {
		clear(data)
		return nil, errors.New("MANAGED_CREDENTIAL_INPUT_INVALID")
	}
	return data, nil
}

type managedCredentialResult struct {
	Status         string `json:"status"`
	Phase          string `json:"phase"`
	MemberID       int64  `json:"memberId"`
	Code           string `json:"code"`
	CredentialKind string `json:"credentialKind"`
}

// A pending result may arrive with Core's pending exit status.
func managedCredentialObservation(output []byte, err error) (
	managedCredentialResult, bool) {
	var result managedCredentialResult
	if json.Unmarshal(output, &result) != nil || err != nil && result.Status != "pending" {
		return managedCredentialResult{}, false
	}
	return result, true
}

// Runs one fixed `managed-credentials` operation with optional private input.
type managedCredentialAdmin func(input []byte, operation string) ([]byte, error)

// transferClaudeCredential renews the same kind, or switches from Codex or
// another Claude kind with Core's disconnect followed by intake.
func transferClaudeCredential(admin managedCredentialAdmin, plan InitPlan,
	token []byte, c claudeCredential) error {
	if plan.Agent == nil || plan.Agent.Provider != "claude" ||
		plan.Agent.CredentialKind != c.kind {
		return errors.New("REBUILD_CLAUDE_CREDENTIAL_KIND_CHANGED")
	}
	member := plan.Member.ID
	status, ok := managedCredentialObservation(admin(nil, "status"))
	var operation string
	switch {
	case !ok:
	case status.Status == "manual" || status.Status == "disconnected":
		operation = "intake"
	case status.MemberID != member:
	case status.Status == "active" && status.CredentialKind == c.kind:
		operation = "replace"
	case status.Status == "active",
		status.Status == "pending" && status.Phase == "disconnecting":
		operation = "disconnect"
	}
	if operation == "" {
		return errors.New("MANAGED_CREDENTIAL_STATUS_REQUIRES_READBACK")
	}
	if operation == "disconnect" {
		result, ok := managedCredentialObservation(admin(nil, "disconnect"))
		switch {
		case ok && result.Status == "pending":
			return errors.New("MANAGED_CREDENTIAL_SETTLEMENT_PENDING_RESUME_SAME_COMMAND")
		case !ok || result.Status != "disconnected":
			return errors.New("MANAGED_CREDENTIAL_DISCONNECT_REQUIRES_READBACK")
		}
		operation = "intake"
	}
	payload, err := claudeCredentialEnvelope(token, c)
	if err != nil {
		return err
	}
	defer clear(payload)
	output, err := admin(payload, operation)
	var result managedCredentialResult
	if err != nil || json.Unmarshal(output, &result) != nil ||
		result.Status != "active" || result.MemberID != member {
		switch result.Code {
		case "MANAGED_CLAUDE_CREDENTIAL_INVALID", "MANAGED_CREDENTIAL_PROVIDER_MISMATCH":
			return errors.New(result.Code)
		}
		return errors.New("MANAGED_CREDENTIAL_TRANSFER_REQUIRES_READBACK")
	}
	return nil
}
