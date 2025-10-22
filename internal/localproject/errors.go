package localproject

import "fmt"

func errDashboardNotFound(projectName, id string) error {
	return errInvalid("project %q has no dashboard with ID %q", projectName, id)
}

func errAlertGroupNotFound(projectName, groupName string) error {
	return errInvalid("project %q has no alert group with name %q", projectName, groupName)
}

func errAlertRuleNotFound(projectName, groupName, ruleName string) error {
	return errInvalid("project %q has no alert rule in group %q with name %q", projectName, groupName, ruleName)
}

func projectErrDashboardDirMissing(path string) string {
	return fmt.Sprintf("project doesn't contain a dashboard directory at %q", path)
}

func projectErrAlertDirMissing(path string) string {
	return fmt.Sprintf("project doesn't contain an alert directory at %q", path)
}

func dashboardParseErr(filename string, err error) string {
	return fmt.Sprintf("failed to parse dashboard %q: %v", filename, err)
}

func alertGroupParseErr(filename string, err error) string {
	return fmt.Sprintf("failed to parse alert group file %q: %v", filename, err)
}
