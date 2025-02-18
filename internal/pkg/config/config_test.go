package config

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestConfig_populateEmpty(t *testing.T) {

	tests := []struct {
		name  string
		input Config
		other *Config
		want  *Config
	}{
		{
			name: "ignore empty update",
			input: Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"hello"},
					},
				},
			},
			other: &Config{},
			want: &Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"hello"},
					},
				},
			},
		},
		{
			name:  "replace empty",
			input: Config{},
			other: &Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"hello"},
					},
				},
			},
			want: &Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"hello"},
					},
				},
			},
		},
		{
			name: "respect update",
			input: Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"hello"},
					},
				},
			},
			other: &Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"world"},
					},
				},
			},
			want: &Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Formatter: &typesv1.FormatConfig{
						SkipFields: []string{"world"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originput, err := json.Marshal(tt.input)
			require.NoError(t, err)

			origother, err := json.Marshal(tt.other)
			require.NoError(t, err)

			got := tt.input.populateEmpty(tt.other)
			require.Empty(t, cmp.Diff(tt.want.CurrentConfig, got.CurrentConfig, protocmp.Transform()), "config should not differ")

			afterinput, err := json.Marshal(tt.input)
			require.NoError(t, err)
			require.Equal(t, originput, afterinput, "input shouldn't be changed")

			afterother, err := json.Marshal(tt.other)
			require.NoError(t, err)
			require.Equal(t, origother, afterother, "other shouldn't be changed")
		})
	}
}
