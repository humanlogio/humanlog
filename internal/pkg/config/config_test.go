package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_populateEmpty(t *testing.T) {

	tests := []struct {
		name  string
		input Config
		other *Config
		want  *Config
	}{
		{
			input: Config{},
			other: &Config{
				Skip: ptr([]string{"hello"}),
			},
			want: &Config{
				Skip: ptr([]string{"hello"}),
			},
		},
		{
			input: Config{
				Skip: ptr([]string{"hello"}),
			},
			other: &Config{},
			want: &Config{
				Skip: ptr([]string{"hello"}),
			},
		},
		{
			input: Config{
				Skip: ptr([]string{"hello"}),
			},
			other: &Config{
				Skip: ptr([]string{"world"}),
			},
			want: &Config{
				Skip: ptr([]string{"hello"}),
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
			require.Equal(t, tt.want, got)

			afterinput, err := json.Marshal(tt.input)
			require.NoError(t, err)
			require.Equal(t, originput, afterinput, "input shouldn't be changed")

			afterother, err := json.Marshal(tt.other)
			require.NoError(t, err)
			require.Equal(t, origother, afterother, "other shouldn't be changed")
		})
	}
}
