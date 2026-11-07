package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCoreCallerRecipesRejectBeforeWrites(t *testing.T) {
	for _, kind := range []string{"missing", "extra", "newline", "width", "terminal"} {
		f := initFixture()
		recipes := recipeFixture()
		switch kind {
		case "missing":
			delete(recipes, "issue")
		case "extra":
			recipes["other"] = []string{"unexpected", ""}
		case "newline":
			recipes["issue"][0] = "name: injected\nline"
		case "width":
			recipes["issue"][0] = strings.Repeat("x", 51) + "CORE_COMMIT"
		case "terminal":
			recipes["issue"] = []string{"name: incomplete", "no final newline"}
		}
		data, _ := json.Marshal(recipes)
		pin := Pins().Core
		path := "repos/" + pin.Repository + "/contents/src/caller-recipes.json?ref="
		f.data[path+pin.Commit] = encoded(data)
		if _, err := planInit(f, "example/project"); err == nil || len(f.writes) != 0 {
			t.Fatal(kind + ": malformed Core recipe must deny without writes")
		}
	}
}

func TestUnifiedCallerInstallsOneDispatchWorkflow(t *testing.T) {
	f := initFixture()
	recipes := recipeFixture()
	recipes["action-dispatch"] = []string{
		"name: Unified ACTION_COMMIT", "# Core source: CORE_COMMIT", "",
	}
	data, err := json.Marshal(recipes)
	if err != nil {
		t.Fatal(err)
	}
	pins := Pins()
	path := "repos/" + pins.Core.Repository + "/contents/src/caller-recipes.json?ref="
	f.data[path+pins.Core.Commit] = encoded(data)
	loaded, err := callerRecipes(f, pins.Core)
	if err != nil {
		t.Fatal(err)
	}
	files := templates(pins, 123, loaded)
	if len(files) != 3 || files[".github/workflows/nibfin-stage.yml"] != "" {
		t.Fatalf("unexpected install files: %v", files)
	}
	want := "name: Unified " + pins.Action.Commit + "\n# Core source: " +
		pins.Core.Commit + "\n"
	if got := files[".github/workflows/nibfin-wake.yml"]; got != want {
		t.Fatalf("unified caller = %q, want %q", got, want)
	}
}
