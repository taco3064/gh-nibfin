package cli

import "errors"

// Native rebase retains the legacy message but replaces its parent. Recover the
// original source binding from that parent's authoritative ancestry, rather than
// treating the new parent's plan ID as evidence of the original source intent.
func verifyLegacyUpgradeBinding(api API, parent upgradePlan, version,
	planID string) error {
	pending := []string{parent.Base}
	seen := map[string]bool{}
	var planningErr error
	base := "repos/" + parent.Repository.FullName
	for len(pending) != 0 {
		revision := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[revision] {
			continue
		}
		seen[revision] = true
		candidate := parent
		var err error
		if revision != parent.Base {
			candidate, err = replanUpgrade(api, parent, version, revision)
		}
		// A side ancestry may predate installation or carry a different intent.
		// Only a matching original plan proves the binding; other candidates
		// must not prevent discovering that proof on another ancestry path.
		if err == nil && candidate.ID == planID && sameUpgradeIntent(parent, candidate) {
			return nil
		}
		if err != nil && planningErr == nil {
			planningErr = err
		}
		var commit upgradeCommit
		if err := api.Call("GET", base+"/git/commits/"+revision,
			nil, &commit); err != nil {
			return err
		}
		if commit.SHA != revision {
			return errors.New("UPGRADE_ANCESTRY_UNAVAILABLE")
		}
		for _, ancestor := range commit.Parents {
			if !commitPattern.MatchString(ancestor.SHA) || ancestor.SHA == revision {
				return errors.New("UPGRADE_ANCESTRY_UNAVAILABLE")
			}
			pending = append(pending, ancestor.SHA)
		}
	}
	if planningErr != nil {
		return planningErr
	}
	return errors.New("UPGRADE_BRANCH_SOURCE_INTENT_UNVERIFIABLE")
}
