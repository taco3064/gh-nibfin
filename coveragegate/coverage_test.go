package coveragegate

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCoverage is the gate: it measures ./internal/... and fails below the
// thresholds. See the package comment.
func TestCoverage(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	excludeText, err := os.ReadFile("exclude.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpRoot := filepath.Join(root, ".tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(tmpRoot, "coveragegate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	profile := filepath.Join(dir, "cover.out")

	cmd := exec.Command("go", "test", "-coverpkg=./internal/...", "-coverprofile="+profile, "./internal/...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("coverage run failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := ParseProfile(string(data))
	if err != nil {
		t.Fatal(err)
	}
	total, problems := Check(pkgs, ParseExclude(string(excludeText)))
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		t.Logf("%-60s %5.1f%% of %d statements", p, pkgs[p].Percent(), pkgs[p].Total)
	}
	t.Logf("total (without exclusions): %.1f%% of %d statements", total.Percent(), total.Total)
	if len(problems) > 0 {
		t.Errorf("coverage below the thresholds (total %.0f%%, each package %.0f%%):\n%s",
			MinTotalPercent, MinPackagePercent, strings.Join(problems, "\n"))
	}
}

func TestParseProfileMergesListings(t *testing.T) {
	const profile = `mode: set
example.com/m/a/x.go:1.1,2.2 3 0
example.com/m/a/x.go:1.1,2.2 3 1
example.com/m/a/x.go:1.1,2.2 3 0
example.com/m/a/x.go:3.1,4.2 2 0
example.com/m/a/x.go:3.1,4.2 2 0
example.com/m/a/y.go:1.1,2.2 5 1
example.com/m/b/z.go:1.1,2.2 4 0
example.com/m/c/e.go:1.1,2.2 0 0
`
	got, err := ParseProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Stmts{
		"example.com/m/a": {Total: 10, Covered: 8},
		"example.com/m/b": {Total: 4, Covered: 0},
	}
	if !maps.Equal(got, want) {
		t.Fatalf("ParseProfile = %v, want %v", got, want)
	}
}

func TestParseProfileRejectsBadLines(t *testing.T) {
	for _, line := range []string{
		"x.go:1.1,2.2 3",
		"x.go:1.1,2.2 a 1",
		"x.go:1.1,2.2 3 -1",
		"nofile 3 1",
	} {
		if _, err := ParseProfile("mode: set\n" + line + "\n"); err == nil {
			t.Errorf("ParseProfile(%q): no error", line)
		}
	}
	if _, err := ParseProfile("mode: set\nm/a/x.go:1.1,2.2 3 0\nm/a/x.go:1.1,2.2 4 0\n"); err == nil {
		t.Error("ParseProfile: no error for one block with two statement counts")
	}
}

func TestCheck(t *testing.T) {
	pkgs := map[string]Stmts{
		"m/good":  {Total: 100, Covered: 90},
		"m/low":   {Total: 100, Covered: 79},
		"m/embed": {Total: 6, Covered: 4},
	}
	total, problems := Check(pkgs, []string{"m/embed", "m/gone"})
	if total != (Stmts{Total: 200, Covered: 169}) {
		t.Errorf("total = %v", total)
	}
	want := []string{"m/gone", "m/low", "total: 84.5%"}
	if len(problems) != len(want) {
		t.Fatalf("problems = %q, want %d", problems, len(want))
	}
	for i, w := range want {
		if !strings.Contains(problems[i], w) {
			t.Errorf("problem %d = %q, want it to mention %q", i, problems[i], w)
		}
	}
	if _, problems := Check(map[string]Stmts{"m/a": {Total: 10, Covered: 9}}, nil); len(problems) != 0 {
		t.Errorf("passing profile: problems = %q", problems)
	}
}

func TestParseExclude(t *testing.T) {
	got := ParseExclude("# c\n\n  m/a  \n#m/b\nm/c\n")
	if !slices.Equal(got, []string{"m/a", "m/c"}) {
		t.Errorf("ParseExclude = %q", got)
	}
}
