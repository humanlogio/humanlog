package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/urfave/cli"
)

const (
	configCmdName = "config"
)

func configCmd(
	getCfg func(cctx *cli.Context) *config.Config,
) cli.Command {

	return cli.Command{
		Name:      configCmdName,
		ShortName: "cfg",
		Usage:     "Manipulate humanlog's configuration.",
		Subcommands: []cli.Command{
			{
				Name: "reset-to-defaults",
				Action: func(cctx *cli.Context) error {
					fp, err := config.GetDefaultConfigFilepath()
					if err != nil {
						return fmt.Errorf("getting default config filepath: %v", err)
					}
					cfg, err := config.GetDefaultConfig(defaultReleaseChannel)
					if err != nil {
						return fmt.Errorf("preparing default config: %v", err)
					}
					if err := config.WriteConfigFile(fp, cfg); err != nil {
						return fmt.Errorf("writing default config to filepath: %v", err)
					}
					loginfo("reset config to defaults: %v", fp)
					return nil
				},
			},
			{
				Name: "show",
				Action: func(cctx *cli.Context) error {
					cfg := getCfg(cctx)
					out, err := json.MarshalIndent(cfg, "", "   ")
					if err != nil {
						return err
					}
					_, err = os.Stdout.Write(out)
					return err
				},
			},
			{
				Name: "show-defaults",
				Action: func(cctx *cli.Context) error {
					cfg, err := config.GetDefaultConfig(defaultReleaseChannel)
					if err != nil {
						return err
					}

					out, err := json.MarshalIndent(cfg, "", "   ")
					if err != nil {
						return err
					}
					_, err = os.Stdout.Write(out)
					return err
				},
			},
			{
				Name: "set",
				Action: func(cctx *cli.Context) error {
					cfg := getCfg(cctx)
					for _, directive := range cctx.Args() {
						if err := applySetDirective(cfg, directive); err != nil {
							return fmt.Errorf("applying directive %q: %v", directive, err)
						}
					}
					return cfg.WriteBack()
				},
			},
		},
	}
}

func applySetDirective(cfg *config.Config, directive string) error {
	pathElements, value, err := parseSetDirective(directive)
	if err != nil {
		return fmt.Errorf("parsing directive: %v", err)
	}
	if err := setValue(cfg, pathElements, value); err != nil {
		return fmt.Errorf("applying directive %q: %v", directive, err)
	}
	return nil
}

func parseSetDirective(directive string) (pathElements []string, value any, err error) {
	path, valueStr, found := strings.Cut(directive, "=")
	if !found {
		return nil, value, fmt.Errorf("no `=` found in directive")
	}
	if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
		return nil, value, fmt.Errorf("parsing value in directive (%q): %v", valueStr, err)
	}
	pathElements = strings.Split(path, ".")

	return pathElements, value, nil
}

func setValue(cfg *config.Config, pathElements []string, value any) error {
	buf, err := json.Marshal(cfg.CurrentConfig)
	if err != nil {
		return err
	}
	mutatable := make(map[string]any)
	if err := json.Unmarshal(buf, &mutatable); err != nil {
		return err
	}

	pos := mutatable
	for i, el := range pathElements {
		nextPos, ok := pos[el]
		if !ok {
			nextPos = make(map[string]any)
			pos[el] = nextPos
		}
		if i == len(pathElements)-1 {
			pos[el] = value
			break
		}
		nextTypedPos, ok := nextPos.(map[string]any)
		if !ok {
			pathSoFar := strings.Join(pathElements[:i], ".")
			return fmt.Errorf("invalid path, not indexable (not an object, but a %T): %v", pos[el], pathSoFar)
		}
		pos = nextTypedPos
	}

	newBuf, err := json.Marshal(mutatable)
	if err != nil {
		return err
	}

	return json.Unmarshal(newBuf, cfg.CurrentConfig)
}
