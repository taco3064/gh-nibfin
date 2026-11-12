package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

const producerPath = ".nibfin/producer-pins.json"
const wakeWorkflowPath = ".github/workflows/nibfin-wake.yml"
const stageWorkflowPath = ".github/workflows/nibfin-stage.yml"
const issueWorkflowPath = ".github/workflows/nibfin-issue.yml"

func templates(
	pins Producers, memberID int64, recipes map[string][]string,
) map[string]string {
	action := pins.Action.Repository + "@" + pins.Action.Commit
	pinJSON, _ := json.MarshalIndent(pins, "", "  ")
	render := func(kind string) string {
		text := strings.ReplaceAll(strings.Join(recipes[kind], "\n"),
			"CORE_COMMIT", pins.Core.Commit)
		text = strings.ReplaceAll(text, "ACTION_COMMIT", pins.Action.Commit)
		return strings.ReplaceAll(text, "MEMBER_ID", fmt.Sprint(memberID))
	}
	files := map[string]string{
		issueWorkflowPath: render("issue"),
		producerPath:      string(pinJSON) + "\n",
	}
	if _, unified := recipes["action-dispatch"]; unified {
		files[wakeWorkflowPath] = render("action-dispatch")
		return files
	}
	files[wakeWorkflowPath] = render("action-wake")
	files[stageWorkflowPath] = fmt.Sprintf(`name: Nibfin adopted stage
on:
  workflow_dispatch:
    inputs:
      issue-number: { required: true, type: string }
      stage: { required: true, type: string }
permissions:
  contents: read
  id-token: write
jobs:
  adopted-stage:
    runs-on: [self-hosted, nibfin-user-%d]
    steps:
      - uses: %s
        with:
          issue-number: ${{ inputs.issue-number }}
          stage: ${{ inputs.stage }}
`, memberID, action)
	return files
}
