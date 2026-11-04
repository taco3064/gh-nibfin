package cli

import (
	"strings"
	"testing"
)

func TestGeneratedSourcesMeetWholeFileLimits(t *testing.T) {
	for path, content := range templates(Pins(), 127829492, recipeFixture()) {
		lines := strings.Split(content, "\n")
		if len(lines) > 1000 {
			t.Fatal("generated source exceeds file limit: " + path)
		}
		for _, line := range lines {
			if len([]rune(line)) > 90 {
				t.Fatal("generated source exceeds line limit: " + path)
			}
		}
	}
}
