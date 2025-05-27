package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestSetValue(t *testing.T) {
	tests := []struct {
		directive string
		cfg       *config.Config
		want      *config.Config
	}{
		{
			directive: "runtime.skip_check_for_updates=false",
			cfg: &config.Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Runtime: &typesv1.RuntimeConfig{
						SkipCheckForUpdates: ptr(true),
					},
				},
			},
			want: &config.Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Runtime: &typesv1.RuntimeConfig{
						SkipCheckForUpdates: ptr(false),
					},
				},
			},
		},
		{
			directive: "runtime.skip_check_for_updates=false",
			cfg: &config.Config{
				CurrentConfig: &typesv1.LocalhostConfig{},
			},
			want: &config.Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Runtime: &typesv1.RuntimeConfig{
						SkipCheckForUpdates: ptr(false),
					},
				},
			},
		},
		{
			directive: "runtime.skip_check_for_updates=null",
			cfg: &config.Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Runtime: &typesv1.RuntimeConfig{
						SkipCheckForUpdates: ptr(true),
					},
				},
			},
			want: &config.Config{
				CurrentConfig: &typesv1.LocalhostConfig{
					Runtime: &typesv1.RuntimeConfig{
						SkipCheckForUpdates: nil,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.directive, func(t *testing.T) {

			err := applySetDirective(tt.cfg, tt.directive)
			require.NoError(t, err)

			want := tt.want
			got := tt.cfg
			require.Empty(t, cmp.Diff(want, got, protocmp.Transform()))
		})
	}
}
