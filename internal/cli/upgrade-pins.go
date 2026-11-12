package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func decodeUpgradePins(data []byte, pins *Producers) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(pins); err != nil {
		return errors.New("UNSUPPORTED_UPGRADE_PRODUCER_METADATA")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("UNSUPPORTED_UPGRADE_PRODUCER_METADATA")
	}
	// encoding/json accepts duplicate object keys; source selection must not.
	if err := uniqueUpgradeJSON(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return errors.New("AMBIGUOUS_UPGRADE_PRODUCER_METADATA")
	}
	return nil
}

func uniqueUpgradeJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			name = strings.ToLower(name)
			if !ok || keys[name] {
				return errors.New("DUPLICATE_JSON_KEY")
			}
			keys[name] = true
		}
		if err := uniqueUpgradeJSON(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
