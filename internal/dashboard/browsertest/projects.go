package main

import (
	"strings"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// Each synthetic repository has its own identity and collections. Unknown names
// never fall back to the primary fixture, even when the browser forges a value.
type projectFixtures struct {
	primary  dashboard.Collector
	projects map[string]snapshot.Snapshot
}

func (p projectFixtures) Collect(name string) snapshot.Snapshot {
	for project, value := range p.projects {
		if strings.EqualFold(project, name) {
			return value
		}
	}
	if strings.EqualFold(name, "example/project") {
		return p.primary.Collect(name)
	}
	return (snapshot.Provider{}).Collect(name)
}
