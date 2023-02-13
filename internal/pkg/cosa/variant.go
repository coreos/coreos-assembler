package cosa

import (
	"encoding/json"
	"fmt"
	"os"
)

const initConfigPath = "src/config.json"

type configVariant struct {
	Variant string `json:"coreos-assembler.config-variant"`
}

// GetVariant finds the configured variant, or "" if unset
func GetVariant() (string, error) {
	contents, err := os.ReadFile(initConfigPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}

	var variantData configVariant
	if err := json.Unmarshal(contents, &variantData); err != nil {
		return "", fmt.Errorf("parsing %s: %w", initConfigPath, err)
	}

	return variantData.Variant, nil
}
