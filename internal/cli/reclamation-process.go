package cli

import (
	"os"
	"strings"
)

func sparseOperationResult(err error, output []byte) string {
	if err == nil {
		return "succeeded"
	}
	if os.IsPermission(err) {
		return "permission_denied"
	}
	// Match native HRESULT identifiers without exposing provider diagnostics.
	text := string(output)
	if strings.ContainsRune(text, 0) {
		if decoded, decodeErr := decodeWSL(output); decodeErr == nil {
			text = decoded
		}
	}
	text = strings.ToLower(text)
	if strings.Contains(text, "0x80070005") || strings.Contains(text, "e_accessdenied") {
		return "permission_denied"
	}
	if strings.Contains(text, "0x80070032") || strings.Contains(text, "e_notimpl") {
		return "unsupported"
	}
	return "operation_failed"
}
