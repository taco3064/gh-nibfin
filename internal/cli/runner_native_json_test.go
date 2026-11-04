package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOfficialRunnerUTF8BOMIdentityRemainsChecked(t *testing.T) {
	root := t.TempDir()
	record := RunnerInstall{RunnerID: 42, Name: "native-windows",
		Repository: "example/project"}
	body := []byte(`{"agentId":42,"agentName":"native-windows",` +
		`"gitHubUrl":"https://github.com/example/project"}`)
	for _, bom := range []bool{false, true} {
		data := append([]byte{}, body...)
		if bom {
			data = append([]byte{0xef, 0xbb, 0xbf}, data...)
		}
		if err := os.WriteFile(filepath.Join(root, ".runner"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := localRunnerMatches(root, record); err != nil {
			t.Fatal("official UTF-8 settings must parse with and without BOM")
		}
		wrong := record
		wrong.RunnerID = 7
		if localRunnerMatches(root, wrong) == nil {
			t.Fatal("BOM support must preserve native identity rejection")
		}
	}
}
