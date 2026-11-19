package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixtureRetirementID = "12345678-1234-1234-1234-123456789abc"

// Controlled operation seams simulate Core settlement, provider setup, native
// registrations and relay. No fixture reaches credentials or a live host.
type rebuildDriver struct {
	api                 *fakeAPI
	old                 sharedHost
	host                *sharedHost
	repositories        []preparationRepository
	retirement          *rebuildRetirement
	binding             agentBinding
	runners             []RunnerInfo
	paths               []string
	calls               []string
	deletes             []int64
	pending             bool
	denied              bool
	incompatible        bool
	lostRemoval         bool
	lostAdoption        bool
	lostActivation      bool
	continuationPending bool
	failure             string
}

func newRebuildDriver(t *testing.T) *rebuildDriver {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	host, err := decodeSharedHost([]byte(`{
 "schemaVersion":1,"repository":"example/project","repositoryId":10,"memberId":7,
 "adoptedRepositoryIds":[10,11],
 "runners":[{"id":25,"name":"old-one"},{"id":26,"name":"old-two"}]
}`))
	if err != nil {
		t.Fatal(err)
	}
	host.Agent = &agentBinding{"codex", "gpt-6.1", "linux-rootful-docker-v1"}
	d := &rebuildDriver{api: initFixture(), old: *host, host: host,
		binding: agentBinding{"claude", "fixture-model", "fixture-profile"},
		repositories: []preparationRepository{
			{"example/project", 10}, {"example/peer", 11},
		}}
	peer := d.api.data["repos/example/project"].(Repository)
	peer.ID, peer.FullName = 11, "example/peer"
	d.api.data["repos/example/peer"] = peer
	d.native(d.old.Runners[0].ID, d.old.Runners[0].Name)
	d.native(d.old.Runners[1].ID, d.old.Runners[1].Name)
	for n := 0; n < 2; n++ {
		path := t.TempDir()
		for _, name := range stagedSecrets {
			if err := os.WriteFile(filepath.Join(path, name),
				[]byte("fixture-staged-secret"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		d.paths = append(d.paths, path)
	}
	return d
}

func (d *rebuildDriver) native(id int64, name string) {
	labels := []struct {
		Name string `json:"name"`
	}{{"nibfin-user-7"}, {sharedRunnerLabel}}
	d.runners = append(d.runners, RunnerInfo{ID: id, Name: name,
		OS: "Linux", Status: "online", Labels: labels})
}

func (d *rebuildDriver) Call(method, path string, input, output any) error {
	base := "repos/example/project/actions/runners"
	if method == "GET" && strings.HasPrefix(path, base) {
		var result any
		if strings.Contains(path, "?per_page=") {
			result = map[string]any{"total_count": len(d.runners), "runners": d.runners}
		} else {
			for _, runner := range d.runners {
				if path == fmt.Sprintf("%s/%d", base, runner.ID) {
					result = runner
				}
			}
			if result == nil {
				return errors.New("NOT_FOUND")
			}
		}
		return json.Unmarshal(jsonRelay(result), output)
	}
	if method == "DELETE" && strings.HasPrefix(path, base+"/") {
		if d.pending || d.retirement == nil || d.retirement.Phase != "removal" {
			panic("removal without settlement")
		}
		for n, runner := range d.runners {
			if path == fmt.Sprintf("%s/%d", base, runner.ID) {
				d.deletes = append(d.deletes, runner.ID)
				d.runners = slices.Delete(d.runners, n, n+1)
				if d.lostRemoval {
					return errors.New("synthetic lost response")
				}
				return nil
			}
		}
		panic("absent registration removed twice")
	}
	return d.api.Call(method, path, input, output)
}

func (d *rebuildDriver) core(operation string) ([]byte, error) {
	d.calls = append(d.calls, operation)
	if operation == "complete-installed-lifecycle" {
		if d.continuationPending {
			return []byte(`{"status":"pending","results":[]}`),
				errors.New("CORE_PENDING_RESULT")
		}
		return []byte(`{"status":"dispatched","results":[]}`), nil
	}
	if operation == "activate-shared-replacement" {
		if d.failure == "activate" || len(d.host.AdoptedRepositoryIDs) != 2 {
			return nil, errors.New("synthetic activation failure")
		}
		d.retirement.Release = "released"
		if d.lostActivation {
			d.lostActivation = false
			return nil, errors.New("synthetic lost activation response")
		}
		pool, _ := json.Marshal(d.host)
		var result map[string]any
		_ = json.Unmarshal(pool, &result)
		result["status"], result["retirementId"] = "active", fixtureRetirementID
		status := "dispatched"
		if d.continuationPending {
			status = "pending"
		}
		result["continuation"] = map[string]any{"status": status, "results": []any{}}
		return jsonRelay(result), nil
	}
	if d.retirement == nil {
		d.retirement = &rebuildRetirement{ID: fixtureRetirementID, Phase: "removal",
			Pool: &d.old, Repositories: d.repositories}
	}
	status := "pending"
	permitted := !d.pending
	if d.pending {
		d.retirement.Phase = "settling"
	} else if len(d.runners) == 0 || d.retirement.Phase == "retired" {
		d.retirement.Phase, d.retirement.Settled = "retired", true
		status, permitted = "retired", false
	}
	r := rebuildRetirementResult{Version: 1, ID: fixtureRetirementID,
		Status: status, Phase: d.retirement.Phase}
	r.Removal.SharedExecution, r.Removal.MemberID = d.old.SharedExecution, 7
	r.Removal.Permitted = permitted
	for _, old := range d.old.Runners {
		r.Removal.Runners = append(r.Removal.Runners,
			sharedRegistration{ID: old.ID, Name: old.Name})
	}
	if status == "pending" {
		return jsonRelay(r), errors.New("CORE_PENDING_RESULT")
	}
	return jsonRelay(r), nil
}

func (d *rebuildDriver) operations() rebuildOperations {
	stage := func(InitPlan, rebuildIntent) ([]string, error) { return d.paths, nil }
	return rebuildOperations{
		snapshot: func() (rebuildSnapshot, error) {
			repos := []preparationRepository{}
			for _, repo := range d.repositories {
				if slices.Contains(d.host.AdoptedRepositoryIDs, repo.ID) {
					repos = append(repos, repo)
				}
			}
			return rebuildSnapshot{Host: d.host, Repositories: repos,
				Draining:   d.retirement != nil && d.retirement.Release != "released",
				Retirement: d.retirement}, nil
		},
		compatible: func() error {
			if d.incompatible {
				return errors.New("synthetic incompatible producer")
			}
			return nil
		},
		capability: func(string) (agentBinding, error) { return d.binding, nil },
		core:       d.core,
		plan: func(repo preparationRepository, i *rebuildIntent) (InitPlan, error) {
			if d.denied {
				return InitPlan{}, errors.New("PERMISSION_DENIED")
			}
			return InitPlan{Repository: d.api.data["repos/"+repo.Repository].(Repository),
				Member: d.api.data["user"].(User), Execution: &i.Old.SharedExecution,
				Agent: &i.Agent, IssueRecipe: "issue-managed"}, nil
		},
		prepare: func(InitPlan) error {
			if d.retirement.Phase != "retired" || slices.ContainsFunc(d.runners,
				func(r RunnerInfo) bool { return r.ID == 25 || r.ID == 26 }) {
				panic("fresh preparation before complete retirement")
			}
			d.calls = append(d.calls, "prepare")
			return nil
		},
		stage:    stage,
		packages: func(InitPlan, rebuildIntent) error { return nil },
		register: d.register,
		host:     d.hostOperations(),
	}
}

func (d *rebuildDriver) register(_ InitPlan, i rebuildIntent,
	save func([]sharedRegistration) error) error {
	d.calls = append(d.calls, "register")
	if len(d.runners) == 0 {
		d.native(31, "fresh-one")
	}
	if len(d.runners) == 1 && d.failure == "register" {
		return errors.New("synthetic registration interruption")
	}
	if len(d.runners) == 1 {
		d.native(32, "fresh-two")
	}
	fresh := []sharedRegistration{}
	for _, runner := range d.runners {
		fresh = append(fresh, sharedRegistration{ID: runner.ID, Name: runner.Name})
	}
	return save(fresh)
}

func (d *rebuildDriver) hostOperations() managedHostOperations {
	return managedHostOperations{
		setupAgent: func(p InitPlan, token []byte) error {
			if p.Agent.Provider != "claude" || !bytes.Equal(token, []byte("fixture-secret")) {
				panic("provider setup boundary changed")
			}
			return nil
		},
		credentials: func(int64, []byte) error { panic("Claude in Codex envelope") },
		readAgent:   verifyPlannedAgent,
		adopt: func(p InitPlan, slots []sharedRegistration) (*sharedHost, error) {
			host := d.old
			host.Runners = slices.Clone(d.old.Runners)
			for n, runner := range slots {
				host.Runners[n].ID, host.Runners[n].Name = runner.ID, runner.Name
			}
			host.Agent = p.Agent
			host.AdoptedRepositoryIDs = []int64{10}
			d.host = &host
			d.retirement.Replacement, d.retirement.Release = &host, "prepared"
			if d.lostAdoption {
				d.lostAdoption = false
				if d.failure == "publishing" {
					d.retirement.Release = "publishing"
				}
				return nil, errors.New("synthetic lost adoption response")
			}
			return &host, nil
		},
		adoptPeer: func(_ sharedHost, p InitPlan) error {
			if d.failure == "peer" {
				return errors.New("synthetic peer interruption")
			}
			d.host.AdoptedRepositoryIDs = append(d.host.AdoptedRepositoryIDs, p.Repository.ID)
			return nil
		},
		start: func([]string) error {
			d.calls = append(d.calls, "start")
			if d.failure == "start" {
				return errors.New("synthetic start interruption")
			}
			return nil
		},
		activate: func(InitPlan, []sharedRegistration) (managedRelayResult, error) {
			d.calls = append(d.calls, "wake")
			if d.failure == "wake" {
				return managedRelayResult{}, errors.New("synthetic wake interruption")
			}
			return managedRelayResult{Github: "verified", Control: "resumed"}, nil
		},
	}
}
