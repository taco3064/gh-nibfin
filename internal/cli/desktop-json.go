package cli

import (
	"bytes"
	"encoding/json"
	"io"
)

// Reject duplicate fields and excessive nesting before decoding provider structs.
func boundedDesktopJSON(data []byte) bool {
	if len(data) > desktopOutputLimit {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 12 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, composite := token.(json.Delim)
		if !composite {
			return true
		}
		if delimiter != '{' && delimiter != '[' {
			return false
		}
		seen := map[string]bool{}
		for decoder.More() {
			if delimiter == '{' {
				field, err := decoder.Token()
				key, ok := field.(string)
				if err != nil || !ok || seen[key] {
					return false
				}
				seen[key] = true
			}
			if !value(depth + 1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && (delimiter == '{' && end == json.Delim('}') ||
			delimiter == '[' && end == json.Delim(']'))
	}
	return value(0) && decoder.Decode(new(any)) == io.EOF
}
