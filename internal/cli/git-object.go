package cli

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
)

func gitObjectHash(kind string, body []byte) string {
	hash := sha1.New()
	fmt.Fprintf(hash, "%s %d\x00", kind, len(body))
	hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}
