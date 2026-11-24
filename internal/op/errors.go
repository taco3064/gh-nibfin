package op

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error codes. A code names the original cause; callers that add context keep
// the innermost code.
const (
	CodeNotImplemented       = "not_implemented"
	CodeInvalidRepo          = "invalid_repo"
	CodeRepoUnresolved       = "repo_unresolved"
	CodeConfirmationRequired = "confirmation_required"
	CodeCancelled            = "cancelled"
	CodeInvalidArgument      = "invalid_argument"
	CodeInvalidTemplateInput = "invalid_template_input"
	CodeWorkflowsUnreadable  = "workflows_unreadable"
	CodeImageUnresolved      = "image_unresolved"
	CodeImageUnverified      = "image_unverified"
	CodePodmanMissing        = "podman_missing"
	CodeMachineMissing       = "machine_missing"
	CodeMachineStartFailed   = "machine_start_failed"
	CodeUnsupportedHost      = "unsupported_host"
	CodePodmanFailed         = "podman_failed"
	CodeLoginFailed          = "login_failed"
	CodeLoginCheckFailed     = "login_check_failed"
	CodeNotLoggedIn          = "not_logged_in"
	CodeNotInitialized       = "not_initialized"
	CodeRepoUnreadable       = "repo_unreadable"
	CodeRepoWriteFailed      = "repo_write_failed"
	CodeNotFound             = "not_found"
	CodeLabelMissing         = "label_missing"
	CodeGitHubError          = "github_error"
	CodeAuthUnavailable      = "auth_unavailable"
	CodeNotApplied           = "not_applied"
)

// Error is an error with a machine-readable cause code.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// MarshalJSON renders the error as {"code","message"}.
func (e *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code(e), e.Error()})
}

// Wrap returns err with the given code. If err already carries a code, that
// innermost code is kept and only the context is added.
func Wrap(code string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Err: err}
}

// Errorf builds a coded error from a format string; %w keeps the wrapped cause.
func Errorf(code, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// NotImplemented reports that the named command has no behaviour yet.
func NotImplemented(name string) error {
	return Errorf(CodeNotImplemented, "%s is not implemented", name)
}

// Code returns the innermost cause code in err's chain, or "" when none.
func Code(err error) string {
	code := ""
	for err != nil {
		var e *Error
		if !errors.As(err, &e) {
			break
		}
		code = e.Code
		err = e.Err
	}
	return code
}

// AsError returns err as an *Error carrying its innermost code. An error
// without a code gets the code "error".
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	code := Code(err)
	if code == "" {
		code = "error"
	}
	return &Error{Code: code, Err: err}
}
