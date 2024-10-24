package main

import (
	"os"
	"strings"
	"testing"
)

func TestApplyConfigFromConfigFile_when_one_of_skip_or_keep_is_given(t *testing.T) {

	wd, _ := os.Getwd()
	dirs := strings.Split(wd, "/")
	root := strings.Join(dirs[:len(dirs)-2], "/")
	configFilePath := root + "/test/cases/00065-apply-config/config.json"
	t.Logf("config file path: %v", configFilePath)

	args := []string{"program-path"}
	args = append(args, "--config", configFilePath)

	app := newApp()
	if err := app.Run(args); err != nil {
		t.Fatal(err)
	}
}
