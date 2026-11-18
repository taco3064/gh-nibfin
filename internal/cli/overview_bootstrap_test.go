package cli

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"testing"
)

// Execute the maintained credential-free bootstrap against controlled filesystem
// metadata. These are simulated permissions, not an installed-host certification.
func TestOverviewBootstrapTrustPolicy(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("guest bootstrap coverage runs with Linux system Python")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	source, _ := json.Marshal(overviewBootstrap)
	harness := `
import io
import contextlib
from pathlib import PurePosixPath
from types import SimpleNamespace
from unittest.mock import patch
ns = {'__name__': 'controlled_fixture'}
exec(SOURCE, ns)
unsafe = {}
git = set()
links = {}
ns['os'] = SimpleNamespace(getuid=lambda: 1000, environ={})
uid = 1000
class FakePath(PurePosixPath):
    def resolve(self, strict=False):
        if str(self) == '/missing':
            raise FileNotFoundError()
        return FakePath(links.get(str(self), str(self)))
    def stat(self):
        mode = 0o100755 if self.name == 'gh-nibfin' else 0o040755
        owner, mode = unsafe.get(str(self), (uid, mode))
        return SimpleNamespace(st_uid=owner, st_mode=mode)
    def is_dir(self):
        return self.name != 'gh-nibfin'
    def lstat(self):
        if str(self) not in git:
            raise FileNotFoundError()
        return self.stat()
    @classmethod
    def home(cls):
        return cls('/home/reader')
ns['Path'] = FakePath
entry = '/home/reader/.local/share/gh/extensions/gh-nibfin/gh-nibfin'
assert str(ns['trusted'](FakePath(entry))) == entry
# User ownership is valid; root ownership is also valid.
unsafe[entry] = (0, 0o100755)
assert str(ns['trusted'](FakePath(entry))) == entry
unsafe.clear()
for unsafe_path, metadata in [
    (entry, (uid + 1001, 0o100755)),
    (entry, (uid, 0o100775)),
    ('/home/reader', (uid, 0o040777)),
    ('/home/reader/.local', (uid + 1001, 0o040755)),
    (entry, (uid, 0o100644)),
]:
    unsafe[unsafe_path] = metadata
    try:
        ns['trusted'](FakePath(entry))
        raise AssertionError('unsafe path accepted')
    except ValueError:
        pass
    unsafe.clear()
for bad in ['/tmp/gh-nibfin', '/var/tmp/gh-nibfin']:
    try:
        ns['trusted'](FakePath(bad))
        raise AssertionError('temporary path accepted')
    except ValueError:
        pass
git.add('/home/reader/.git')
try:
    ns['trusted'](FakePath(entry))
    raise AssertionError('repository path accepted')
except ValueError:
    pass
git.clear()
links[entry] = '/tmp/gh-nibfin'
try:
    ns['trusted'](FakePath(entry))
    raise AssertionError('temporary symlink accepted')
except ValueError:
    pass
links.clear()
with patch.dict(ns['os'].environ, {}, clear=True):
    with contextlib.redirect_stdout(io.StringIO()) as output:
        ns['main']()
    assert entry in output.getvalue()
with patch.dict(ns['os'].environ, {'XDG_DATA_HOME': 'relative'}, clear=True):
    try:
        ns['main']()
        raise AssertionError('relative installation accepted')
    except ValueError:
        pass
`
	harness = "SOURCE = " + string(source) + "\n" + harness
	command := exec.Command(python, "-I", "-B", "-c", harness)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal("bootstrap filesystem fixture failed", err, string(output))
	}
}
