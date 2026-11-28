package op

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path"
)

// depsInstaller is the official runner's dependency installer inside its
// archive, and maxDepsInstaller caps how much of it is read.
const (
	depsInstaller    = "bin/installdependencies.sh"
	maxDepsInstaller = 1 << 20
)

// runnerArchive is the runner archive of one Apply: downloaded and checked
// against its sha256 on the first get, reused by later ones and removed by
// close.
type runnerArchive struct {
	download func(context.Context) (runnerDownload, error)
	path     string
	name     string
	cleanup  func()
}

// get returns the path of the downloaded archive, downloading it first when
// this Apply has not yet.
func (r *runnerArchive) get(ctx context.Context) (string, error) {
	if r.path != "" {
		return r.path, nil
	}
	d, err := r.download(ctx)
	if err != nil {
		return "", err
	}
	p, cleanup, err := downloadArchive(ctx, d)
	if err != nil {
		return "", err
	}
	r.path, r.name, r.cleanup = p, d.Filename, cleanup
	return p, nil
}

// close removes the downloaded archive, if any.
func (r *runnerArchive) close() {
	if r.cleanup != nil {
		r.cleanup()
	}
	r.path, r.cleanup = "", nil
}

// archiveFile returns the regular file want (a cleaned, slash-separated
// path) from the .tar.gz at p; name is the archive's name for the error.
func archiveFile(p, name, want string) ([]byte, error) {
	fail := func(format string, args ...any) ([]byte, error) {
		return nil, Errorf(CodeRunnerInstallFailed, "the runner archive %s: "+format, append([]any{name}, args...)...)
	}
	f, err := os.Open(p)
	if err != nil {
		return fail("%v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fail("%v", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fail("has no %s", want)
		}
		if err != nil {
			return fail("%v", err)
		}
		if !h.FileInfo().Mode().IsRegular() || path.Clean("/"+h.Name) != "/"+want {
			continue
		}
		if h.Size > maxDepsInstaller {
			return fail("%s is larger than %d bytes", want, maxDepsInstaller)
		}
		var b bytes.Buffer
		if _, err := io.Copy(&b, io.LimitReader(tr, maxDepsInstaller+1)); err != nil {
			return fail("read %s: %v", want, err)
		}
		if b.Len() > maxDepsInstaller {
			return fail("%s is larger than %d bytes", want, maxDepsInstaller)
		}
		return b.Bytes(), nil
	}
}

// installDeps installs the official runner's system dependencies inside the
// podman machine VM machine with the runner's own bin/installdependencies.sh
// from this Apply's archive (depsScript). A failure names what to run inside
// the VM.
func (a *runnerAdd) installDeps(ctx context.Context, arc *runnerArchive, machine string) error {
	p, err := arc.get(ctx)
	if err != nil {
		return err
	}
	body, err := archiveFile(p, arc.name, depsInstaller)
	if err != nil {
		return err
	}
	if _, err := a.script(ctx, "install runner dependencies", depsScript, bytes.NewReader(body), ""); err != nil {
		return Errorf(CodeRunnerInstallFailed, "%w; inside the VM (podman machine ssh %s), run sudo ./bin/installdependencies.sh from an extracted runner directory, and where libssl.so.3 is still missing, sudo dnf install -y openssl3-libs; then run gh nibfin runner add again", err, machine)
	}
	return nil
}
