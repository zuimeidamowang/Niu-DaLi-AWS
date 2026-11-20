package service

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// This shape is reserved for input-media capabilities. Generated video/CDN
// URLs and ordinary upstream fields remain unchanged, including usage facts.
var mediaSourceCapabilityPattern = regexp.MustCompile(`https?://[^\s"'<>\\]+/v1/media/assets/media_[A-Za-z0-9_-]{32}/source\?(?:[^\s"'<>\\]|\\u[0-9a-fA-F]{4})*`)

// RedactMediaAssetCapabilities prevents upstream echoes of scoped input URLs
// from reaching task snapshots, failure messages, or polling debug logs.
func RedactMediaAssetCapabilities(body []byte) []byte {
	if !bytes.Contains(body, []byte("/v1/media/assets/")) && !bytes.Contains(body, []byte(`\/v1\/media\/assets\/`)) && !bytes.Contains(body, []byte(`\u`)) {
		return body
	}
	var value common.RawMessage
	if common.Unmarshal(body, &value) != nil {
		text := strings.ReplaceAll(string(body), `\/`, "/")
		redacted := mediaSourceCapabilityPattern.ReplaceAllString(text, "[media source redacted]")
		if redacted == text {
			return body
		}
		return []byte(redacted)
	}
	redacted, changed, err := redactMediaAssetCapabilityJSON(value)
	if err != nil {
		return []byte(`"[media source redacted]"`)
	}
	if !changed {
		return body
	}
	return redacted
}

// Raw JSON leaves numeric tokens intact: a privacy filter must not change
// vendor counters, seeds, IDs or decimals by routing them through float64.
func redactMediaAssetCapabilityJSON(value common.RawMessage) (common.RawMessage, bool, error) {
	changed := false
	switch common.GetJsonType(value) {
	case "string":
		var text string
		if err := common.Unmarshal(value, &text); err != nil {
			return nil, false, err
		}
		redacted := mediaSourceCapabilityPattern.ReplaceAllString(text, "[media source redacted]")
		if redacted == text {
			return value, false, nil
		}
		encoded, err := common.Marshal(redacted)
		return encoded, true, err
	case "object":
		var object map[string]common.RawMessage
		if err := common.Unmarshal(value, &object); err != nil {
			return nil, false, err
		}
		for key, item := range object {
			redactedKey := mediaSourceCapabilityPattern.ReplaceAllString(key, "[media source redacted]")
			if redactedKey != key {
				delete(object, key)
				changed = true
			}
			redacted, itemChanged, err := redactMediaAssetCapabilityJSON(item)
			if err != nil {
				return nil, false, err
			}
			object[redactedKey] = redacted
			changed = changed || itemChanged
		}
		if changed {
			encoded, err := common.Marshal(object)
			return encoded, true, err
		}
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(value, &items); err != nil {
			return nil, false, err
		}
		for index, item := range items {
			redacted, itemChanged, err := redactMediaAssetCapabilityJSON(item)
			if err != nil {
				return nil, false, err
			}
			items[index] = redacted
			changed = changed || itemChanged
		}
		if changed {
			encoded, err := common.Marshal(items)
			return encoded, true, err
		}
	}
	return value, false, nil
}
