package cli

import (
	"encoding/json"
	"errors"
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
		return f.fakeAPI.Call(method, path, input, output)
	}
	f.writes = append(f.writes, method+" "+path)
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
		return assign(output, pull)
	case base + "/labels":
		if f.failedLabel {
			return errors.New("PERMISSION_DENIED")
		}
		body := input.(map[string]string)
		f.data[base+"/labels/"+body["name"]] = body
	default:
		return errors.New("UNEXPECTED_WRITE")
	}
	return nil
}

func installFixture(t *testing.T) *installingAPI {
	t.Helper()
	f := initFixture()
	plan, err := planInit(f, "example/project")
	if err != nil {
		t.Fatal(err)
	}
	f.data["repos/example/project/git/commits/"+plan.Head] = map[string]any{
		"tree": map[string]string{"sha": strings.Repeat("d", 40)}}
	return &installingAPI{fakeAPI: f, plan: plan}
}

func TestInstallationCreatesReadBackPRNeverWritesMain(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		f := installFixture(t)
		f.lostRef = lostResponse
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
	}
}

func TestBadReadbackRevokedPermissionAndDuplicateBranchStayIncomplete(t *testing.T) {
	for _, kind := range []string{"readback", "permission", "duplicate"} {
		f := installFixture(t)
		switch kind {
		case "readback":
			f.wrongFile = true
		case "permission":
			f.failedLabel = true
		case "duplicate":
			f.data["repos/example/project/git/ref/heads/nibfin%2Finit-"+
				f.plan.Head[:12]] = reference{}
		}
		result, err := applyInit(f, f.plan)
		if err == nil || result.Status == "completed" || f.refPosts > 1 {
			t.Fatalf("%s cannot succeed or retry writes: %+v %v", kind, result, err)
		}
	}
}
