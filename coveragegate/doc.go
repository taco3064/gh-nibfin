// Package coveragegate holds the Go unit-test coverage gate that Proposal
// R8.25 requires of gh-nibfin's pull request CI (Issue #518). It has no code of
// its own; TestCoverage runs as part of the ordinary go test ./..., so CI and
// local runs enforce it without a separate step.
//
// TestCoverage runs
//
//	go test -coverpkg=./internal/... -coverprofile=<tmp> ./internal/...
//
// from the module root, so code that one package's tests exercise in another
// package counts. It then reads the profile and fails when the total over the
// measured packages is below MinTotalPercent or any one package is below
// MinPackagePercent.
//
// The merged profile lists most blocks once per test binary. A block is
// therefore counted once, keyed by its "file:range" field, with its statement
// count taken once, and it is covered when any listing has a count above 0.
// Summing the profile's lines directly would count each block many times.
//
// Packages with no statements never appear in the profile and need no entry.
// Packages that only embed files are listed in exclude.txt, one import path
// per line; an entry that matches no package in the profile fails the gate so
// the list cannot go stale.
//
// Reading a failure: the message names the total or each package below its
// threshold with its percentage. Add tests for that package's code; do not
// lower the thresholds or exclude a package that has real logic. When the
// coverage run itself fails, its output is shown and the failing package's
// own test fails as well.
package coveragegate
