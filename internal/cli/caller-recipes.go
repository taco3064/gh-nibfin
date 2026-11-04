package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Core owns complete caller recipes. Preview reads their immutable public
// source; the CLI neither installs a cache nor invents another wake policy.
func callerRecipes(api API, pin SourcePin) (map[string][]string, error) {
	data, _, err := content(api, pin.Repository, "src/caller-recipes.json", pin.Commit)
	if err != nil {
		return nil, err
	}
	var recipes map[string][]string
	if json.Unmarshal(data, &recipes) != nil || len(recipes) != 2 {
		return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
	}
	for _, name := range []string{"wake", "issue"} {
		lines := recipes[name]
		if len(lines) < 2 || len(lines) > 1000 || lines[len(lines)-1] != "" {
			return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
		}
		for _, line := range lines {
			rendered := strings.ReplaceAll(line, "CORE_COMMIT", pin.Commit)
			if strings.ContainsAny(line, "\r\n") || utf8.RuneCountInString(rendered) > 90 {
				return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
			}
		}
	}
	return recipes, nil
}
