package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_int64toLightRGB(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{
			in:   62,
			want: "#3aef45",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := int64toLightRGB(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_int64toDarkRGB(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{
			in:   62,
			want: "#1b6f20",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := int64toDarkRGB(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}

func Test_getPrefix(t *testing.T) {
	type args struct {
		machine int64
		session int64
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			args: args{machine: 1, session: 2},
			want: "1║2║",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getPrefix(tt.args.machine, tt.args.session); got != tt.want {
				t.Errorf("getPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}
