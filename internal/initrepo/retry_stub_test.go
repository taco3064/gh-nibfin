package initrepo

import (
	"context"
	"io"
	"time"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
)

// Retries in this package's tests neither wait nor log.
func init() {
	ghretry.Sleep = func(context.Context, time.Duration) error { return nil }
	ghretry.Log = io.Discard
}
