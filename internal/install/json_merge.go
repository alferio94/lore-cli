package install

import (
	"encoding/json"
	"fmt"
	"strings"
)

func mergeJSONObject(existing, desired []byte, existingLabel, desiredLabel, mergedLabel string) ([]byte, error) {
	base := map[string]any{}
	if len(strings.TrimSpace(string(existing))) > 0 {
		if err := json.Unmarshal(existing, &base); err != nil {
			return nil, fmt.Errorf("decode existing %s: %w", existingLabel, err)
		}
	}
	overlay := map[string]any{}
	if err := json.Unmarshal(desired, &overlay); err != nil {
		return nil, fmt.Errorf("decode rendered %s: %w", desiredLabel, err)
	}
	merged := mergeMaps(base, overlay)
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode merged %s: %w", mergedLabel, err)
	}
	return append(data, '\n'), nil
}

func mergeAntigravityMCPConfig(existing, desired []byte) ([]byte, error) {
	return mergeJSONObject(existing, desired, "mcp_config.json", "mcp_config.json", "mcp_config.json")
}
