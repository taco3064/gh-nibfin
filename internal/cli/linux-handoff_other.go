//go:build !linux

package cli

import "flag"

func linuxIdentity() string { return "" }

func (a App) linuxHandoffGuard(*flag.FlagSet) func() error {
	return func() error { return nil }
}
