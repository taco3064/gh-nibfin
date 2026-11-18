package dashboard

import (
	"strings"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// Called under mu. Retained display observations never grant detail access.
func (s *Service) eligibleRepository(name string) (snapshot.Repository, bool) {
	if s.overview != nil {
		value := s.overview.current
		if !s.now().Before(value.ObservedAt) && s.now().Before(value.ExpiresAt) {
			for _, repo := range value.Repositories.Items {
				if repo.ID > 0 && !repo.Retained && strings.EqualFold(repo.Name, name) {
					return repo, true
				}
			}
		}
	}
	return snapshot.Repository{}, false
}

type repositoryCollector interface {
	CollectRepository(snapshot.Repository) snapshot.Snapshot
}

func matchesRepository(value snapshot.Snapshot, repo snapshot.Repository) bool {
	if value.Repository.Status.Availability != snapshot.Known ||
		len(value.Repository.Items) != 1 {
		return false
	}
	actual := value.Repository.Items[0]
	return actual.ID == repo.ID && strings.EqualFold(actual.Name, repo.Name)
}
