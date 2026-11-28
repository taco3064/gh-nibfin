package coveragegate

import (
	"bufio"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
)

// Thresholds, in percent of statements (Proposal R8.25, Issue #518).
const (
	MinTotalPercent   = 85.0
	MinPackagePercent = 80.0
)

// Stmts counts statements and how many of them ran.
type Stmts struct {
	Total   int
	Covered int
}

// Percent returns the covered share in percent; 100 when there are no
// statements.
func (s Stmts) Percent() float64 {
	if s.Total == 0 {
		return 100
	}
	return 100 * float64(s.Covered) / float64(s.Total)
}

type block struct {
	pkg     string
	stmts   int
	covered bool
}

// ParseProfile reads a Go coverage profile and returns the statements per
// package (the import path of each file's directory). It counts each block
// ("file:range") once, however many test binaries list it, and treats it as
// covered when any listing has a count above 0.
func ParseProfile(profile string) (map[string]Stmts, error) {
	blocks := map[string]*block{}
	sc := bufio.NewScanner(strings.NewReader(profile))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "mode:") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return nil, fmt.Errorf("profile line %d: want \"file:range stmts count\", got %q", line, text)
		}
		key := fields[0]
		colon := strings.LastIndex(key, ":")
		if colon <= 0 {
			return nil, fmt.Errorf("profile line %d: no file:range in %q", line, key)
		}
		stmts, err := strconv.Atoi(fields[1])
		if err != nil || stmts < 0 {
			return nil, fmt.Errorf("profile line %d: bad statement count %q", line, fields[1])
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("profile line %d: bad count %q", line, fields[2])
		}
		b, ok := blocks[key]
		if !ok {
			b = &block{pkg: path.Dir(key[:colon]), stmts: stmts}
			blocks[key] = b
		} else if b.stmts != stmts {
			return nil, fmt.Errorf("profile line %d: block %s has %d statements, earlier %d", line, key, stmts, b.stmts)
		}
		if count > 0 {
			b.covered = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	pkgs := map[string]Stmts{}
	for _, b := range blocks {
		s := pkgs[b.pkg]
		s.Total += b.stmts
		if b.covered {
			s.Covered += b.stmts
		}
		pkgs[b.pkg] = s
	}
	for p, s := range pkgs {
		if s.Total == 0 {
			delete(pkgs, p)
		}
	}
	return pkgs, nil
}

// ParseExclude reads exclude.txt: one import path per line, blank lines and
// lines starting with # ignored.
func ParseExclude(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// Check drops the excluded packages and returns one message per failure: a
// stale exclusion entry, a package below MinPackagePercent, or a total below
// MinTotalPercent. It also returns the total over the remaining packages.
func Check(pkgs map[string]Stmts, exclude []string) (Stmts, []string) {
	var problems []string
	kept := map[string]Stmts{}
	for p, s := range pkgs {
		kept[p] = s
	}
	for _, e := range exclude {
		if _, ok := kept[e]; !ok {
			problems = append(problems, fmt.Sprintf("exclusion %s matches no package with code in the profile; remove it from exclude.txt", e))
			continue
		}
		delete(kept, e)
	}
	var total Stmts
	for _, p := range slices.Sorted(maps.Keys(kept)) {
		s := kept[p]
		total.Total += s.Total
		total.Covered += s.Covered
		if s.Percent() < MinPackagePercent {
			problems = append(problems, fmt.Sprintf("package %s: %.1f%% of %d statements, below the %.0f%% package threshold", p, s.Percent(), s.Total, MinPackagePercent))
		}
	}
	if total.Percent() < MinTotalPercent {
		problems = append(problems, fmt.Sprintf("total: %.1f%% of %d statements, below the %.0f%% total threshold", total.Percent(), total.Total, MinTotalPercent))
	}
	return total, problems
}
