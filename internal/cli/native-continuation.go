package cli

import (
	"encoding/json"
	"errors"
)

type nativeGitHubDiagnostic struct {
	Kind       string `json:"kind"`
	HTTPStatus *int   `json:"httpStatus,omitempty"`
}

type nativeContinuation struct {
	Status    string                  `json:"status"`
	Results   []json.RawMessage       `json:"results"`
	Code      string                  `json:"code,omitempty"`
	GitHubAPI *nativeGitHubDiagnostic `json:"githubApi,omitempty"`
}

// Payload validation is independent of the enclosing command's process status.
// A replacement activation can succeed while its nested continuation is pending.
func nativeContinuationPayload(data []byte) (nativeContinuation, error) {
	var result nativeContinuation
	var raw struct {
		Status    string            `json:"status"`
		Results   []json.RawMessage `json:"results"`
		Code      string            `json:"code"`
		GitHubAPI json.RawMessage   `json:"githubApi"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Results == nil ||
		(raw.Status != "dispatched" && raw.Status != "paused" && raw.Status != "pending") {
		return result, errors.New("NATIVE_CONTINUATION_REQUIRES_READBACK")
	}
	result.Status, result.Results, result.Code = raw.Status, raw.Results, raw.Code
	result.GitHubAPI = nativeGitHubCause(raw.GitHubAPI)
	return result, nil
}

func nativeGitHubCause(raw json.RawMessage) *nativeGitHubDiagnostic {
	var diagnostic nativeGitHubDiagnostic
	if json.Unmarshal(raw, &diagnostic) == nil {
		switch diagnostic.Kind {
		case "http":
			if diagnostic.HTTPStatus != nil {
				return &diagnostic
			}
		case "network", "invalid-response", "read":
			diagnostic.HTTPStatus = nil
			return &diagnostic
		}
	}
	return nil
}

func decodeNativeContinuation(data []byte, callErr error) (nativeContinuation, error) {
	result, err := nativeContinuationPayload(data)
	if err != nil ||
		(result.Status == "pending" && (callErr == nil ||
			callErr.Error() != "CORE_PENDING_RESULT")) ||
		(result.Status != "pending" && callErr != nil) {
		return result, errors.New("NATIVE_CONTINUATION_REQUIRES_READBACK")
	}
	return result, nil
}
