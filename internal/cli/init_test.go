package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeAPI struct {
	data   map[string]any
	writes []string
}

func (f *fakeAPI) Call(method, path string, input, output any) error {
	if method != "GET" {
		f.writes = append(f.writes, method+" "+path)
		return errors.New("UNEXPECTED_WRITE")
	}
	value, ok := f.data[path]
	if !ok {
		return errors.New("NOT_FOUND")
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, output)
}

func encoded(data []byte) Content {
	return Content{Type: "file", Encoding: "base64",
		Content: base64.StdEncoding.EncodeToString(data), SHA: blobHash(data)}
}

func initFixture() *fakeAPI {
	pins := Pins()
	head := strings.Repeat("a", 40)
	f := &fakeAPI{data: map[string]any{
		"user": User{ID: 7, Login: "operator", Type: "User"},
		"repos/example/project": Repository{ID: 10, FullName: "example/project",
			DefaultBranch: "main", Owner: User{ID: 1, Type: "User"},
			Permissions: map[string]bool{"push": true, "admin": true}},
		"repos/example/project/git/ref/heads/main": map[string]any{
			"ref": "refs/heads/main", "object": map[string]string{"sha": head}},
	}}
	for _, pin := range []SourcePin{pins.Core, pins.Action} {
		f.data["repos/"+pin.Repository] = Repository{FullName: pin.Repository}
		f.data["repos/"+pin.Repository+"/commits/"+pin.Commit] =
			map[string]string{"sha": pin.Commit}
	}
	bound, _ := json.Marshal(pins.Core)
	f.data["repos/"+pins.Action.Repository+"/contents/core-source.json?ref="+
		pins.Action.Commit] = encoded(bound)
	return f
}

func TestPublicPersonalAndOrganizationPreview(t *testing.T) {
	for _, ownerType := range []string{"User", "Organization"} {
		f := initFixture()
		repo := f.data["repos/example/project"].(Repository)
		repo.Owner.Type = ownerType
		f.data["repos/example/project"] = repo
		plan, err := planInit(f, repo.FullName)
		if err != nil || len(plan.Files) != 3 || len(plan.Labels) != 3 {
			t.Fatalf("invalid preview: %+v %v", plan, err)
		}
		if len(f.writes) != 0 {
			t.Fatal("preview must not write")
		}
		for _, file := range plan.Files {
			if strings.Contains(file.Content, "pull_request_target") ||
				strings.Contains(file.Content, "secrets: inherit") {
				t.Fatal("unsafe template trigger or credential propagation")
			}
		}
	}
}

func TestPrivatePermissionSourceAndExistingEditRejectBeforeWrites(t *testing.T) {
	for _, change := range []string{"private", "permission", "source", "edit"} {
		f := initFixture()
		repo := f.data["repos/example/project"].(Repository)
		switch change {
		case "private":
			repo.Private = true
		case "permission":
			repo.Permissions["push"] = false
		case "source":
			pins := Pins()
			f.data["repos/"+pins.Core.Repository+"/commits/"+pins.Core.Commit] =
				map[string]string{"sha": strings.Repeat("b", 40)}
		case "edit":
			path := "repos/example/project/contents/.github/workflows/"
			f.data[path+"nibfin-wake.yml?ref="+strings.Repeat("a", 40)] =
				encoded([]byte("user-owned workflow"))
		}
		f.data["repos/example/project"] = repo
		if _, err := planInit(f, repo.FullName); err == nil || len(f.writes) != 0 {
			t.Fatalf("%s must reject without writes", change)
		}
	}
}

func TestUnchangedInstallationHasNoFileOrLabelChanges(t *testing.T) {
	f := initFixture()
	for path, body := range templates(Pins(), 7) {
		f.data["repos/example/project/contents/"+path+"?ref="+
			strings.Repeat("a", 40)] = encoded([]byte(body))
	}
	for _, label := range []string{"scope", "shaped", "delivered"} {
		f.data["repos/example/project/labels/"+label] = map[string]string{"name": label}
	}
	plan, err := planInit(f, "example/project")
	if err != nil {
		t.Fatal(err)
	}
	result, err := applyInit(f, plan)
	if err != nil || result.Status != "completed" || len(f.writes) != 0 {
		t.Fatalf("unchanged initialization must be non-destructive: %+v %v", result, err)
	}
}
