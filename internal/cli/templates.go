package cli

import (
	"encoding/json"
	"fmt"
)

func templates(pins Producers, memberID int64) map[string]string {
	core := pins.Core.Repository + "/.github/workflows/wake.yml@" + pins.Core.Commit
	action := pins.Action.Repository + "@" + pins.Action.Commit
	pinJSON, _ := json.MarshalIndent(pins, "", "  ")
	wake := fmt.Sprintf(`name: Nibfin adopted wake
on:
  workflow_dispatch:
    inputs:
      issue_number: { required: true, type: string }
      issue_id: { required: true, type: string }
      repository_id: { required: true, type: string }
permissions:
  contents: read
  id-token: write
concurrency:
  group: nibfin-${{ github.repository_id }}-${{ inputs.issue_number }}
  cancel-in-progress: false
jobs:
  wake:
    uses: %s
    with:
      core_commit: %s
    secrets:
      NIBFIN_WAKE_ADOPTION: ${{ secrets.NIBFIN_WAKE_ADOPTION }}
      NIBFIN_READ_TOKEN: ${{ secrets.NIBFIN_READ_TOKEN }}
`, core, pins.Core.Commit)
	stage := fmt.Sprintf(`name: Nibfin adopted stage
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
	return map[string]string{
		".github/workflows/nibfin-wake.yml":  wake,
		".github/workflows/nibfin-stage.yml": stage,
		".nibfin/producer-pins.json":         string(pinJSON) + "\n",
	}
}
