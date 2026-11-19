//go:build !linux

package cli

import "os"

func sourceFileIdentity(os.FileInfo) string { return "" }

func currentSourceOwner() sourceOwner { return sourceOwner{} }

func validSourceOwner(sourceOwner) bool { return false }

func observeSourceUse(sourceRecord) string {
	return "native process identity and surviving-consumer observation unavailable"
}
