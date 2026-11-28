package op

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
)

// PullMode chooses when PullImage contacts the registry. The two modes stand
// in for podman run's --pull=newer and --pull=missing, using only commands
// Podman 4.x has (podman pull has no --policy before 5.6), so the run that
// follows can use --pull=never and a pull failure is never confused with a
// container that started and failed.
type PullMode int

const (
	// PullNewer always pulls; when every try fails and the image is in local
	// storage, the local image is used, as --pull=newer does.
	PullNewer PullMode = iota
	// PullMissing pulls only when the image is not in local storage, as
	// podman run's default --pull=missing does.
	PullMissing
)

// pullCauseMax bounds the cause a retry line quotes from podman's stderr.
const pullCauseMax = 200

// pullStderrTail is how much of podman's stderr a final pull error keeps.
const pullStderrTail = 2000

// PullImage pulls image with podman, retrying a failed pull up to
// ghretry.Retries times whatever the error (Proposal R4.53, the retry model of
// the GitHub API retries), waiting 1 s, 2 s and 4 s with ghretry.Sleep and
// writing one line per retry to ghretry.Log. purpose says what the image is
// for, such as "for the claude login check". After the last try fails the
// error is podman_failed with podman's own stderr and the number of tries.
// A cancelled ctx is cancelled and not retried.
func PullImage(ctx context.Context, podman func(context.Context, ...string) *exec.Cmd, image, purpose string, mode PullMode) error {
	if mode == PullMissing {
		present, err := imageExists(ctx, podman, image)
		if err != nil {
			return err
		}
		if present {
			return nil
		}
	}
	tries, cause, stderr, err := pullRetried(ctx, podman, image)
	if err == nil {
		return nil
	}
	if Code(err) == CodeCancelled {
		return err
	}
	if mode == PullNewer {
		present, existsErr := imageExists(ctx, podman, image)
		if existsErr != nil {
			return existsErr
		}
		if present {
			fmt.Fprintf(ghretry.Log, "nibfin: podman pull %s failed after %d tries (%s); using the local image\n", image, tries, cause)
			return nil
		}
	}
	msg := stderr
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > pullStderrTail {
		msg = "…" + msg[len(msg)-pullStderrTail:]
	}
	return Errorf(CodePodmanFailed, "podman could not pull %s %s after %d tries: %s", image, purpose, tries, msg)
}

// pullRetried runs podman pull image until it succeeds or 1+Retries tries
// failed. It returns the number of tries, the last failure's short cause and
// podman's trimmed stderr, and the last error (nil on success).
func pullRetried(ctx context.Context, podman func(context.Context, ...string) *exec.Cmd, image string) (int, string, string, error) {
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return attempt - 1, "", "", Errorf(CodeCancelled, "cancelled")
		}
		var stderr bytes.Buffer
		c := podman(ctx, "pull", image)
		c.Stdout = io.Discard
		c.Stderr = &stderr
		err := c.Run()
		if err == nil {
			return attempt, "", "", nil
		}
		if ctx.Err() != nil {
			return attempt, "", "", Errorf(CodeCancelled, "cancelled")
		}
		text := strings.TrimSpace(stderr.String())
		cause := pullCause(text, err)
		if attempt > ghretry.Retries {
			return attempt, cause, text, err
		}
		fmt.Fprintf(ghretry.Log, "nibfin: podman pull %s failed (%s); retry %d of %d\n", image, cause, attempt, ghretry.Retries)
		if ghretry.Sleep(ctx, time.Duration(1<<(attempt-1))*time.Second) != nil {
			return attempt, cause, text, Errorf(CodeCancelled, "cancelled")
		}
	}
}

// pullCause is the last non-empty line of podman's stderr without terminal
// escape codes, capped; or the error itself (a start error or exit status N).
func pullCause(stderr string, err error) string {
	lines := strings.Split(ansiRE.ReplaceAllString(stderr, ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > pullCauseMax {
				l = l[:pullCauseMax] + "…"
			}
			return l
		}
	}
	return err.Error()
}

// imageExists reports whether image is in podman's local storage. Only exit
// status 0 means present; any other outcome is "not present" and the caller
// pulls, so broken local storage surfaces as podman's own pull or run error.
func imageExists(ctx context.Context, podman func(context.Context, ...string) *exec.Cmd, image string) (bool, error) {
	if ctx.Err() != nil {
		return false, Errorf(CodeCancelled, "cancelled")
	}
	c := podman(ctx, "image", "exists", image)
	c.Stdout, c.Stderr = io.Discard, io.Discard
	err := c.Run()
	if ctx.Err() != nil {
		return false, Errorf(CodeCancelled, "cancelled")
	}
	return err == nil, nil
}
