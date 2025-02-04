//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/sys/execabs"

	"github.com/humanlogio/humanlog/internal/pkg/state"
)

func (hdl *serviceHandler) Stop(ctx context.Context) error {
	confPath, err := hdl.getServiceFilePath()
	if err != nil {
		return err
	}

	cmd := execabs.CommandContext(ctx, "launchctl", "unload", confPath)
	return cmd.Run()
}

func (hdl *serviceHandler) Start(ctx context.Context) error {
	confPath, err := hdl.getServiceFilePath()
	if err != nil {
		return err
	}
	cmd := execabs.CommandContext(ctx, "launchctl", "load", confPath)
	return cmd.Run()
}

func (hdl *serviceHandler) Uninstall() error {
	confPath, err := hdl.getServiceFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(confPath); !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (hdl *serviceHandler) Install() error {
	confPath, err := hdl.getServiceFilePath()
	if err != nil {
		return err
	}
	// Ensure that ~/Library/LaunchAgents exists.
	err = os.MkdirAll(filepath.Dir(confPath), 0700)
	if err != nil {
		return fmt.Errorf("ensuring service directory exists: %v", err)
	}

	f, err := os.Create(confPath)
	if err != nil {
		return fmt.Errorf("creating service file: %v", err)
	}
	defer f.Close()

	path, err := os.Executable()
	if err != nil {
		return fmt.Errorf("looking up own executable path: %v", err)
	}

	var logdir string
	if hdl.localhostCfg.LogDir != nil {
		logdir = *hdl.localhostCfg.LogDir
	} else {
		stateDir, err := state.GetDefaultStateDirpath()
		if err != nil {
			return fmt.Errorf("looking up default state dir")
		}
		logdir = filepath.Join(stateDir, "logs")
	}
	if err := os.MkdirAll(logdir, 0700); err != nil {
		return fmt.Errorf("ensuring log dir exists: %v", err)
	}

	var to = &struct {
		Name      string
		Path      string
		Arguments []string
		EnvVars   map[string]string
		UserName  string

		KeepAlive, RunAtLoad bool
		SessionCreate        bool
		StandardOutPath      string
		StandardErrorPath    string
	}{
		Name:              hdl.svcCfg.Name,
		Path:              path,
		Arguments:         hdl.svcCfg.Arguments,
		EnvVars:           nil,
		UserName:          hdl.svcCfg.UserName,
		KeepAlive:         true,
		RunAtLoad:         true,
		SessionCreate:     true,
		StandardOutPath:   filepath.Join(logdir, "log.out"),
		StandardErrorPath: filepath.Join(logdir, "log.err"),
	}
	return hdl.template().Execute(f, to)
}

func (hdl *serviceHandler) getHomeDir() (string, error) {
	u, err := user.Current()
	if err == nil {
		return u.HomeDir, nil
	}

	// alternate methods
	homeDir := os.Getenv("HOME") // *nix
	if homeDir == "" {
		return "", errors.New("User home directory not found.")
	}
	return homeDir, nil
}

func (hdl *serviceHandler) getServiceFilePath() (string, error) {
	name := hdl.svcCfg.Name
	homeDir, err := hdl.getHomeDir()
	if err != nil {
		return "", err
	}
	return homeDir + "/Library/LaunchAgents/" + name + ".plist", nil
}

func (hdl *serviceHandler) template() *template.Template {
	functions := template.FuncMap{
		"bool": func(v bool) string {
			if v {
				return "true"
			}
			return "false"
		},
	}
	return template.Must(template.New("").Funcs(functions).Parse(launchdConfig))
}

var launchdConfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Disabled</key>
	<false/>
	{{- if .EnvVars}}
	<key>EnvironmentVariables</key>
	<dict>
		{{- range $k, $v := .EnvVars}}
		<key>{{html $k}}</key>
		<string>{{html $v}}</string>
		{{- end}}
	</dict>
	{{- end}}
	<key>KeepAlive</key>
	<{{bool .KeepAlive}}/>
	<key>Label</key>
	<string>{{html .Name}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{html .Path}}</string>
		{{- if .Arguments}}
		{{- range .Arguments}}
		<string>{{html .}}</string>
		{{- end}}
	{{- end}}
	</array>
	<key>RunAtLoad</key>
	<{{bool .RunAtLoad}}/>
	<key>SessionCreate</key>
	<{{bool .SessionCreate}}/>
	{{- if .StandardErrorPath}}
	<key>StandardErrorPath</key>
	<string>{{html .StandardErrorPath}}</string>
	{{- end}}
	{{- if .StandardOutPath}}
	<key>StandardOutPath</key>
	<string>{{html .StandardOutPath}}</string>
	{{- end}}
	{{- if .UserName}}
	<key>UserName</key>
	<string>{{html .UserName}}</string>
	{{- end}}
</dict>
</plist>
`
