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
	return decodeCallerRecipes(data, pin)
}

func decodeCallerRecipes(data []byte, pin SourcePin) (map[string][]string, error) {
	var recipes map[string][]string
	if json.Unmarshal(data, &recipes) != nil {
		return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
	}
	names := []string{"wake", "issue", "action-wake"}
	if _, unified := recipes["action-dispatch"]; unified {
		names = append(names, "action-dispatch")
	}
	if len(recipes) != len(names) {
		return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
	}
	for _, name := range names {
		lines := recipes[name]
		if len(lines) < 2 || len(lines) > 1000 || lines[len(lines)-1] != "" {
			return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
		}
		for _, line := range lines {
			rendered := strings.ReplaceAll(line, "CORE_COMMIT", pin.Commit)
			rendered = strings.ReplaceAll(rendered, "ACTION_COMMIT", strings.Repeat("0", 40))
			rendered = strings.ReplaceAll(rendered, "MEMBER_ID", "9007199254740991")
			if strings.ContainsAny(line, "\r\n") || utf8.RuneCountInString(rendered) > 90 {
				return nil, errors.New("INVALID_CORE_CALLER_RECIPES")
			}
		}
	}
	return recipes, nil
}
