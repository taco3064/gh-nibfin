package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

func selectProject(t *testing.T, s *Service, name string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"repository": name})
	r, _ := http.NewRequest("POST", s.URL()+"/api/repository",
		strings.NewReader(string(body)))
	r.Header.Set(SessionHeader, strings.Split(proof(s), ":")[0])
	r.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func TestProjectReadsRequireCurrentInventoryAndStableIdentity(t *testing.T) {
	for _, mode := range []string{"empty-issues", "retained", "expired", "removed",
		"replaced", "native-mismatch", "native-unavailable", "removed-during-read"} {
		t.Run(mode, func(t *testing.T) {
			fixture := overviewFixture{make(chan struct{}), make(chan struct{}),
				overviewValue()}
			close(fixture.release)
			var s *Service
			reads := 0
			provider := &recordingCollector{collect: func(string) snapshot.Snapshot {
				reads++
				value := projectSnapshot()
				if mode == "native-mismatch" {
					value.Repository.Items[0].ID = 99
				}
				if mode == "native-unavailable" {
					value.Repository.Status.Availability = snapshot.Denied
				}
				if mode == "removed-during-read" {
					s.mu.Lock()
					s.overview.current.Repositories.Items = nil
					s.mu.Unlock()
				}
				return value
			}}
			s = launch(t, Config{Overview: fixture, Provider: provider})
			token := strings.Split(proof(s), ":")[0]
			response := overviewGET(t, s, "/api/updates", token, "")
			response.Body.Close()
			if selectProject(t, s, "unknown/project") != 403 || reads != 0 {
				t.Fatal("forged selection reached collector")
			}
			if selectProject(t, s, "example/project") != 200 {
				t.Fatal("verified project with no required Issue was rejected")
			}
			s.mu.Lock()
			switch mode {
			case "retained":
				s.overview.current = reconcileOverview(s.overview.current,
					snapshot.Overview{ObservedAt: time.Now(),
						ExpiresAt: time.Now().Add(time.Minute)})
			case "expired":
				s.overview.current.ExpiresAt = time.Now().Add(-time.Second)
			case "removed":
				s.overview.current.Repositories.Items = nil
			case "replaced":
				s.overview.current.Repositories.Items[0].ID = 99
			}
			s.mu.Unlock()
			code, data := request(t, s, token)
			if mode == "empty-issues" {
				if code != 200 || reads != 1 {
					t.Fatal("authorized empty project unavailable", code, reads)
				}
			} else if code == 200 || strings.Contains(data, `"schema_version"`) {
				t.Fatal("ineligible repository disclosed detail", code, data)
			}
			if mode == "retained" || mode == "expired" || mode == "removed" {
				if reads != 0 || selectProject(t, s, "example/project") != 403 {
					t.Fatal("display inventory granted access", reads)
				}
			}
		})
	}
}
