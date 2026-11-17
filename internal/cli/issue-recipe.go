package cli

const managedIssueWorkflow = ".github/workflows/nibfin-issue.yml"

func initIssueRecipe(plan InitPlan) string {
	if plan.IssueRecipe == "" {
		return "issue"
	}
	return plan.IssueRecipe
}

func recognizeIssueRecipe(pins Producers, member int64, recipes map[string][]string,
	observed string) string {
	for _, recipe := range []string{"issue", "issue-managed"} {
		if len(recipes[recipe]) != 0 &&
			observed == templatesForRecipe(pins, member, recipes, recipe)[managedIssueWorkflow] {
			return recipe
		}
	}
	return ""
}

// A merged, exact managed upgrade may supersede CLI-1's legacy PR intent.
// Preserve that original intent; Core independently verifies the adoption refresh.
func managedIntentRecipeCompatible(original, current InitPlan) bool {
	return initIssueRecipe(original) == initIssueRecipe(current) ||
		(initIssueRecipe(original) == "issue" &&
			initIssueRecipe(current) == "issue-managed" && len(current.Files) == 0)
}
