package cli

// Default constants for windows environments
const (
	defaultPrompt             = "C:\\>"
	defaultEnvSetCmd          = "set"
	defaultAssignmentOperator = "="
	defaultDisableHistory     = "For security reasons, disable Windows history activity momentarily.\n" +
		"     Go to \"Settings/Privacy/Activity history\" and click on check boxes,\n" +
		"     \"Store my activity on this device\" and \"Send my activity history to\n" +
		"     Microsoft\" to deselect and disable the history activity."
	defaultEnableHistory = "Click and select \"Store my activity on this device\" check box to enable history activity."
)
