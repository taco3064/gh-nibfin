package cli

import "strings"

const managedIssueWorkflow = ".github/workflows/nibfin-issue.yml"

func matchesUntitledCaller(path, maintained, observed string, allowed bool) bool {
	return allowed && (path == managedIssueWorkflow ||
		path == ".github/workflows/nibfin-wake.yml") && observed == untitledCaller(maintained)
}

func initIssueRecipe(plan InitPlan) string {
	if plan.IssueRecipe == "" {
		return "issue"
	}
	return plan.IssueRecipe
}

func recognizeIssueRecipe(pins Producers, member int64, recipes map[string][]string,
	observed string) string {
	return recognizeIssueRecipeVariant(pins, member, recipes, observed, false)
}

func recognizeIssueRecipeVariant(pins Producers, member int64,
	recipes map[string][]string, observed string, allowUntitled bool) string {
	for _, recipe := range []string{"issue", "issue-managed"} {
		if len(recipes[recipe]) == 0 {
			continue
		}
		expected := templatesForRecipe(pins, member, recipes, recipe)[managedIssueWorkflow]
		if observed == expected ||
			matchesUntitledCaller(managedIssueWorkflow, expected, observed, allowUntitled) {
			return recipe
		}
	}
	return ""
}

// Derive the historical layout from the maintained template. Never remove
// title-like content from an observed caller to make it appear managed.
func untitledCaller(maintained string) string {
	lines := strings.Split(maintained, "\n")
	for start, line := range lines {
		if line != "run-name: >-" {
			continue
		}
		end := start + 1
		for end < len(lines) && strings.HasPrefix(lines[end], "  ") {
			end++
		}
		return strings.Join(append(lines[:start], lines[end:]...), "\n")
	}
	return maintained
}

// A merged, exact managed upgrade may supersede CLI-1's legacy PR intent.
// Preserve that original intent; Core independently verifies the adoption refresh.
func managedIntentRecipeCompatible(original, current InitPlan) bool {
	return initIssueRecipe(original) == initIssueRecipe(current) ||
		(initIssueRecipe(original) == "issue" &&
			initIssueRecipe(current) == "issue-managed" && len(current.Files) == 0)
}
