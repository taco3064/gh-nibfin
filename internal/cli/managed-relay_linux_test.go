package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// These drivers simulate provider, installed Core and systemd responses. They
// exercise CLI ordering and reconciliation without a real dispatch or token.
type relayDriver struct {
	api          *fakeAPI
	plan         InitPlan
	snapshot     relayLocalSnapshot
	config       relayConfiguration
	paused       bool
	enabled      bool
	busy         bool
	unknown      bool
	writeLost    bool
	variableLost bool
	calls        []string
}

func newRelayDriver(t *testing.T) *relayDriver {
	t.Helper()
	api := initFixture()
	recipes := recipeFixture()
	recipes["issue-managed"] = []string{"name: managed CORE_COMMIT", "mode: managed", ""}
	pin := Pins().Core
	api.data["repos/"+pin.Repository+"/contents/src/caller-recipes.json?ref="+
		pin.Commit] = encoded(jsonRelay(recipes))
	plan := InitPlan{Repository: api.data["repos/example/project"].(Repository),
		Member: api.data["user"].(User), IssueRecipe: "issue-managed",
		Execution: &SharedExecution{"example/project", 10}}
	driver := &relayDriver{api: api, plan: plan}
	driver.addAdoption(10, "example/project", 40)
	driver.snapshot.Config = json.RawMessage("null")
	return driver
}

func (d *relayDriver) addAdoption(id int64, name string, workflow int64) {
	recipes, _ := callerRecipes(d.api, Pins().Core)
	body := templatesForRecipe(Pins(), 7, recipes, "issue-managed")[managedIssueWorkflow]
	head := strings.Repeat("b", 40)
	repo := d.plan.Repository
	repo.ID, repo.FullName = id, name
	d.api.data["repos/"+name] = repo
	d.api.data["repos/"+name+"/branches/main"] = map[string]any{
		"name": "main", "protected": true, "commit": map[string]string{"sha": head},
	}
	d.api.data[fmt.Sprintf("repos/%s/actions/workflows/%d", name, workflow)] =
		map[string]any{"id": workflow, "path": managedIssueWorkflow, "state": "active"}
	d.api.data["repos/"+name+"/contents/"+managedIssueWorkflow+"?ref="+head] =
		encoded([]byte(body))
	var bindings map[string]any
	_ = json.Unmarshal(d.snapshot.Bindings, &bindings)
	if bindings == nil {
		bindings = map[string]any{"memberId": 7, "pin": Pins().Core, "origins": []any{}}
	}
	origins := bindings["origins"].([]any)
	bindings["origins"] = append(origins, map[string]any{
		"repository": name, "repositoryId": id,
		"event": map[string]any{
			"repository": name, "repositoryId": id, "memberId": 7,
			"workflowId": workflow, "workflowPath": managedIssueWorkflow,
			"sourceCommit": head, "workflowBlob": blobHash([]byte(body)),
			"reusable": map[string]string{"repository": Pins().Core.Repository,
				"path": ".github/workflows/issue.yml", "commit": Pins().Core.Commit},
		},
	})
	d.snapshot.Bindings = jsonRelay(bindings)
	binding, _ := json.Marshal(*d.plan.Execution)
	d.api.data["repos/"+name+"/actions/variables/"+sharedDestinationVariable] =
		actionsVariable{sharedDestinationVariable, string(binding)}
}

func (d *relayDriver) observation(operation string) []byte {
	resources := []map[string]string{}
	units := []map[string]string{}
	for _, name := range []string{"nibfin-notification-relay.service",
		"nibfin-notification-relay.timer"} {
		resources = append(resources, map[string]string{"name": name, "status": "prepared"})
		active, enabled := "inactive", "disabled"
		if d.enabled && strings.HasSuffix(name, ".timer") {
			active, enabled = "active", "enabled"
		}
		units = append(units, map[string]string{"Id": name, "LoadState": "loaded",
			"ActiveState": active, "UnitFileState": enabled})
	}
	recovery := map[string]any{"status": "observed",
		"control": map[string]any{"schemaVersion": 1, "paused": d.paused},
		"records": []map[string]string{}}
	if d.unknown {
		recovery["records"] = []map[string]any{
			{"status": "pending", "code": "RELAY_EFFECT_UNKNOWN"},
			{"status": "consumed", "code": "RELAY_DISPATCH_READ_BACK",
				"origin": map[string]int64{"repositoryId": d.plan.Repository.ID}},
		}
	}
	status := "inspected"
	if operation == "prepare" {
		status = "prepared"
	}
	return jsonRelay(map[string]any{
		"status": status, "resources": resources, "servicesStarted": false,
		"control": map[string]any{"schemaVersion": 1, "paused": d.paused},
		"github": map[string]any{"status": "verified", "origins": d.config.Origins,
			"controller": map[string]int64{"id": 7}, "destination": d.plan.Execution},
		"recovery": recovery, "service": map[string]any{"status": "observed",
			"units": units},
	})
}

func (d *relayDriver) operations() managedRelayOperations {
	return managedRelayOperations{
		local: func(request relayLocalRequest) ([]byte, error) {
			d.calls = append(d.calls, request.Operation)
			switch request.Operation {
			case "lock":
				return jsonRelay(map[string]string{"status": "locked"}), nil
			case "unlock":
				return jsonRelay(map[string]string{"status": "unlocked"}), nil
			case "read":
				return jsonRelay(d.snapshot), nil
			case "write":
				if !reflect.DeepEqual(*request.Expected, d.snapshot) {
					return nil, errors.New("snapshot changed")
				}
				d.config = *request.Config
				d.snapshot.Config = jsonRelay(d.config)
				if d.writeLost {
					return nil, errors.New("lost response")
				}
				return jsonRelay(map[string]string{"status": "written"}), nil
			}
			return nil, errors.New("unexpected operation")
		},
		core: func(operation string) ([]byte, error) {
			d.calls = append(d.calls, operation)
			switch operation {
			case "pause", "resume":
				if operation == "pause" && d.busy {
					return jsonRelay(map[string]string{"status": "pending"}), nil
				}
				d.paused = operation == "pause"
				status := "resumed"
				if d.paused {
					status = "paused"
				}
				return jsonRelay(map[string]string{"status": status}), nil
			case "prepare":
				d.paused = true
			}
			return d.observation(operation), nil
		},
		system: func(args ...string) error {
			d.calls = append(d.calls, strings.Join(args, " "))
			if args[0] == "enable" {
				d.enabled = true
			}
			return nil
		},
		refresh: func() error { d.calls = append(d.calls, "refresh"); return nil },
		recheck: func() error { d.calls = append(d.calls, "recheck"); return nil },
	}
}

type relayVariableAPI struct{ *relayDriver }

func (a relayVariableAPI) Call(method, path string, input, output any) error {
	if method != "GET" {
		a.calls = append(a.calls, method+" "+path)
		a.api.writes = append(a.api.writes, method+" "+path)
		if a.variableLost {
			return errors.New("lost before write")
		}
		variable := input.(actionsVariable)
		a.api.data["repos/"+a.plan.Repository.FullName+"/actions/variables/"+
			variable.Name] = variable
		return errors.New("lost after write")
	}
	return a.api.Call(method, path, input, output)
}

func TestManagedRelayFreshPeerAndResume(t *testing.T) {
	d := newRelayDriver(t)
	result, err := configureManagedRelay(relayVariableAPI{d}, d.plan, d.operations())
	if err != nil || result.Mode != "managed" || result.DispatchObserved ||
		len(d.config.Origins) != 1 || !d.enabled || d.paused {
		t.Fatalf("fresh route: %+v %v; calls %v", result, err, d.calls)
	}
	before := len(d.api.writes)
	d.calls = nil
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
		d.operations()); err != nil {
		t.Fatal(err)
	}
	if len(d.api.writes) != before ||
		strings.Contains(strings.Join(d.calls, ","), "resume") {
		t.Fatal("resume of a configured route must only inspect")
	}
	d.addAdoption(20, "example/peer", 50)
	d.plan.Repository = d.api.data["repos/example/peer"].(Repository)
	d.calls = nil
	d.unknown = true
	result, err = configureManagedRelay(relayVariableAPI{d}, d.plan, d.operations())
	if err != nil || len(d.config.Origins) != 2 || result.UnknownEffects != 1 ||
		!result.DispatchObserved || result.Consumed != 1 {
		t.Fatalf("peer route or recovery: %+v %v; %v", result, err, d.calls)
	}
	sequence := strings.Join(d.calls, ",")
	if !strings.Contains(sequence, "pause,write") ||
		!strings.Contains(sequence, "enable --now nibfin-notification-relay.timer,resume") {
		t.Fatalf("activation order: %s", sequence)
	}
}

func TestManagedRelayStopsBeforeUnsafeEffects(t *testing.T) {
	for _, scenario := range []string{"busy", "paused", "legacy", "unknown-config",
		"lost-config", "lost-variable", "changed-workflow"} {
		t.Run(scenario, func(t *testing.T) {
			d := newRelayDriver(t)
			if scenario == "busy" || scenario == "paused" {
				if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
					d.operations()); err != nil {
					t.Fatal(err)
				}
				d.addAdoption(20, "example/peer", 50)
				d.plan.Repository = d.api.data["repos/example/peer"].(Repository)
				d.busy, d.paused = scenario == "busy", scenario == "paused"
			}
			switch scenario {
			case "legacy":
				d.api.data["repos/example/project/actions/secrets/NIBFIN_WAKE_TOKEN"] =
					map[string]string{"name": "NIBFIN_WAKE_TOKEN"}
			case "unknown-config":
				d.snapshot.Config = json.RawMessage(`{"schemaVersion":1,"unknown":true}`)
			case "lost-config":
				d.writeLost = true
			case "lost-variable":
				d.variableLost = true
			case "changed-workflow":
				d.api.data["repos/example/project/actions/workflows/40"] =
					map[string]any{"id": 41, "path": managedIssueWorkflow, "state": "active"}
			}
			d.calls = nil
			before, _ := json.Marshal(d.snapshot.Config)
			_, err := configureManagedRelay(relayVariableAPI{d}, d.plan, d.operations())
			if err == nil || strings.Contains(strings.Join(d.calls, ","), "resume") {
				t.Fatalf("unsafe activation: %v %v", err, d.calls)
			}
			if scenario == "busy" || scenario == "paused" || scenario == "legacy" {
				after, _ := json.Marshal(d.snapshot.Config)
				if string(before) != string(after) {
					t.Fatal("existing route configuration changed")
				}
			}
		})
	}
}

func TestManagedRelayReadinessRequiresEveryObservation(t *testing.T) {
	d := newRelayDriver(t)
	config, err := relayBindings(d.api, d.snapshot.Bindings, d.plan, relayConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	d.config, d.enabled = config, true
	for _, scenario := range []string{"github", "service", "timer", "control",
		"recovery", "origin", "destination"} {
		t.Run(scenario, func(t *testing.T) {
			var value map[string]any
			_ = json.Unmarshal(d.observation("inspect"), &value)
			switch scenario {
			case "github":
				value["github"].(map[string]any)["status"] = "unverified"
			case "service":
				value["service"].(map[string]any)["status"] = "unknown"
			case "timer":
				units := value["service"].(map[string]any)["units"].([]any)
				units[1].(map[string]any)["ActiveState"] = "inactive"
			case "control":
				value["recovery"].(map[string]any)["control"] = nil
			case "recovery":
				value["recovery"].(map[string]any)["status"] = "unknown"
			case "origin":
				value["github"].(map[string]any)["origins"] = []any{}
			case "destination":
				value["github"].(map[string]any)["destination"] = nil
			}
			observed, err := decodeRelayObservation(jsonRelay(value))
			if err == nil {
				err = observed.verify(config, d.plan)
			}
			if err == nil {
				_, err = observed.configured(d.plan.Repository.ID)
			}
			if err == nil {
				t.Fatal("incomplete readback claimed configuration")
			}
		})
	}
}

func TestManagedRelayInterruptedPreparationRequiresExplicitInspection(t *testing.T) {
	d := newRelayDriver(t)
	d.variableLost = true
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
		d.operations()); err == nil || !d.paused || d.enabled {
		t.Fatalf("uncertain activation must retain pause: %v", err)
	}
	d.variableLost = false
	d.calls = nil
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
		d.operations()); err == nil || strings.Contains(strings.Join(d.calls, ","), "resume") {
		t.Fatal("ordinary retry silently cleared pause")
	}
	// Simulate the existing administrator inspection/resume interface. Init
	// must pause again before finishing the interrupted variable write/startup.
	d.paused = false
	d.calls = nil
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
		d.operations()); err != nil {
		t.Fatal(err, d.calls)
	}
	if !strings.Contains(strings.Join(d.calls, ","), "pause") || !d.enabled {
		t.Fatal("resumed preparation bypassed staged activation")
	}
}

func TestReplacementRelayResumesKnownPausedConfiguration(t *testing.T) {
	d := newRelayDriver(t)
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
		d.operations()); err != nil {
		t.Fatal(err)
	}
	d.paused = true
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan,
		d.operations()); err == nil {
		t.Fatal("ordinary init must not resume an administrator-paused relay")
	}
	ops := d.operations()
	ops.replacement = true
	if _, err := configureManagedRelay(relayVariableAPI{d}, d.plan, ops); err != nil {
		t.Fatal(err)
	}
	if d.paused {
		t.Fatal("verified replacement relay remained paused")
	}
}
