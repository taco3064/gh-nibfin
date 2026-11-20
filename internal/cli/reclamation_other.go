//go:build !windows

package cli

import "context"

func (NativeProcess) ObserveSparse(string) sparseObservation {
	return sparseObservation{Setting: "unknown", Capability: "unavailable"}
}

func (NativeProcess) EnableSparse(context.Context, string) string {
	return "unavailable"
}
