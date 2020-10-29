// +build !windows

package cli

// Add default constants for non-windows environments
const (
	defaultPrompt             = "$"
	defaultEnvSetCmd          = "export"
	defaultAssignmentOperator = "="
	defaultDisableHistory     = "$ set +o history"
	defaultEnableHistory      = "$ set -o history"
)
