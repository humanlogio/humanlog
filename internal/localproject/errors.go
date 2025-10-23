package localproject

func errDashboardNotFound(projectName, id string) error {
	return errInvalid("project %q has no dashboard with ID %q", projectName, id)
}

func errAlertGroupNotFound(projectName, groupName string) error {
	return errInvalid("project %q has no alert group with name %q", projectName, groupName)
}

func errAlertRuleNotFound(projectName, groupName, ruleName string) error {
	return errInvalid("project %q has no alert rule in group %q with name %q", projectName, groupName, ruleName)
}

func errProjectDashboardDirMissing(path string) error {
	return errInvalid("project doesn't contain a dashboard directory at %q", path)
}

func errProjectAlertDirMissing(path string) error {
	return errInvalid("project doesn't contain an alert directory at %q", path)
}

func errDashboardParse(filename string, err error) error {
	return errInvalid("failed to parse dashboard %q: %v", filename, err)
}

func errAlertGroupParse(filename string, err error) error {
	return errInvalid("failed to parse alert group file %q: %v", filename, err)
}
