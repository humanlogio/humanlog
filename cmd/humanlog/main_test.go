package main

import (
	"encoding/json"
	"os"
	"testing"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestApplyConfigFromConfigFile_when_one_of_skip_or_keep_is_given(t *testing.T) {

	cfg := config.Config{
		CurrentConfig: &typesv1.LocalhostConfig{
			Formatter: &typesv1.FormatConfig{
				SkipFields: []string{"foo", "bar"},
			},
		},
	}

	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "00065-apply-config.json")
	require.NoError(t, err)

	err = json.NewEncoder(f).Encode(cfg)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	args := []string{"program-path", "--config", f.Name()}

	app := newApp()
	if err := app.Run(args); err != nil {
		t.Fatal(err)
	}
}
