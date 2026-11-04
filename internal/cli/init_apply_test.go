package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

type installingAPI struct {
	*fakeAPI
	plan        InitPlan
	lostRef     bool
	wrongFile   bool
	refPosts    int
	failedLabel bool
	branchRace  bool
	branchReads int
	deniedPath  string
	lostLabel   bool
	lostPR      bool
}

func assign(output, value any) error {
	if output == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, output)
}

func (f *installingAPI) Call(method, path string, input, output any) error {
	if method == "GET" {
		if strings.Contains(path, "/git/ref/heads/nibfin%2F") {
			f.branchReads++
			if f.branchRace && f.branchReads == 3 {
				ref := reference{}
				ref.Object.SHA = strings.Repeat("e", 40)
				return assign(output, ref)
			}
		}
		return f.fakeAPI.Call(method, path, input, output)
	}
	f.writes = append(f.writes, method+" "+path)
	if path == f.deniedPath {
		return errors.New("PERMISSION_DENIED")
	}
	base := "repos/example/project"
	commit := strings.Repeat("c", 40)
	branch := "nibfin/init-" + f.plan.Head[:12]
	switch path {
	case base + "/git/trees":
		return assign(output, map[string]string{"sha": strings.Repeat("b", 40)})
	case base + "/git/commits":
		return assign(output, map[string]string{"sha": commit})
	case base + "/git/refs":
		f.refPosts++
		ref := reference{Ref: "refs/heads/" + branch}
		ref.Object.SHA = commit
		f.data[base+"/git/ref/heads/nibfin%2Finit-"+f.plan.Head[:12]] = ref
		for _, file := range f.plan.Files {
			body := file.Content
			if f.wrongFile {
				body = "wrong provider content"
			}
			f.data[base+"/contents/"+file.Path+"?ref="+commit] = encoded([]byte(body))
		}
		if f.lostRef {
			return errors.New("PROCESS_RESULT_UNAVAILABLE")
		}
	case base + "/pulls":
		pull := installPR{Number: 8, State: "open",
			URL: "https://github.com/example/project/pull/8"}
		pull.Head.Ref, pull.Head.SHA = branch, commit
		pull.Base.Ref = "main"
		f.data[base+"/pulls/8"] = pull
		if f.lostPR {
			query := "?state=open&head=" + url.QueryEscape("example:"+branch)
			f.data[base+"/pulls"+query] = []installPR{pull}
			return errors.New("PROCESS_RESULT_UNAVAILABLE")
		}
		return assign(output, pull)
	case base + "/labels":
		if f.failedLabel {
			return errors.New("PERMISSION_DENIED")
		}
		body := input.(map[string]string)
		f.data[base+"/labels/"+body["name"]] = body
		if f.lostLabel {
			return errors.New("PROCESS_RESULT_UNAVAILABLE")
		}
	default:
		return errors.New("UNEXPECTED_WRITE")
	}
	return nil
}

func installFixture(t *testing.T, ownerType string) *installingAPI {
	t.Helper()
	f := initFixture()
	repo := f.data["repos/example/project"].(Repository)
	repo.Owner.Type = ownerType
	repo.Permissions = map[string]bool{"push": true}
	f.data["repos/example/project"] = repo
	plan, err := planInit(f, "example/project")
	if err != nil {
		t.Fatal(err)
	}
	f.data["repos/example/project/git/commits/"+plan.Head] = map[string]any{
		"tree": map[string]string{"sha": strings.Repeat("d", 40)}}
	return &installingAPI{fakeAPI: f, plan: plan}
}

func TestInstallationCreatesReadBackPRNeverWritesMain(t *testing.T) {
	for _, ownerType := range []string{"User", "Organization"} {
		for _, lostResponse := range []bool{false, true} {
			f := installFixture(t, ownerType)
			f.lostRef = lostResponse
			f.lostLabel = lostResponse
			f.lostPR = lostResponse
			result, err := applyInit(f, f.plan)
			if err != nil || result.Status != "pending" || result.PR == "" {
				t.Fatalf("installation must hand off a real PR: %+v %v", result, err)
			}
			if f.refPosts != 1 {
				t.Fatal("unknown writes must be read back, not repeated")
			}
			for _, write := range f.writes {
				if strings.Contains(write, "heads/main") || strings.HasSuffix(write, "/merge") {
					t.Fatal("installation must never update main or merge")
				}
			}
			for _, label := range f.plan.Labels {
				if _, ok := f.data["repos/example/project/labels/"+label]; !ok {
					t.Fatal("each label effect must exist for native read-back")
				}
			}
			ref := reference{}
			ref.Object.SHA = strings.Repeat("c", 40)
			f.data["repos/example/project/git/ref/heads/main"] = ref
			plan, err := planInit(f, "example/project")
			if err != nil || len(plan.Files) != 0 || len(plan.Labels) != 0 {
				t.Fatal("merged installation must yield an unchanged plan")
			}
			writes := len(f.writes)
			result, err = applyInit(f, plan)
			if err != nil || result.Status != "completed" || len(f.writes) != writes {
				t.Fatal("unchanged apply must read back without further writes")
			}
		}
	}
}

func TestBadReadbackRevokedPermissionAndDuplicateBranchStayIncomplete(t *testing.T) {
	for _, ownerType := range []string{"User", "Organization"} {
		for _, kind := range []string{"readback", "permission", "duplicate", "branch-race"} {
			f := installFixture(t, ownerType)
			switch kind {
			case "readback":
				f.wrongFile = true
			case "permission":
				f.failedLabel = true
			case "branch-race":
				f.branchRace = true
			case "duplicate":
				f.data["repos/example/project/git/ref/heads/nibfin%2Finit-"+
					f.plan.Head[:12]] = reference{}
			}
			result, err := applyInit(f, f.plan)
			if err == nil || result.Status == "completed" || f.refPosts > 1 {
				t.Fatalf("%s cannot succeed or retry writes: %+v %v", kind, result, err)
			}
			if kind == "branch-race" {
				for _, write := range f.writes {
					if strings.HasSuffix(write, "/pulls") {
						t.Fatal("changed installation branch must not become a PR")
					}
				}
			}
		}
	}
}
