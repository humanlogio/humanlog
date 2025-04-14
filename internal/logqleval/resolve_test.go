package logqleval

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
)

func TestResolveVal(t *testing.T) {
	tests := []struct {
		name    string
		in      *typesv1.Val
		mkMap   MakeMap
		mkSlice MakeSlice
		want    any
	}{
		{
			name: "flatmap",
			in: typesv1.ValObj(
				typesv1.KeyVal("composite", typesv1.ValObj(
					typesv1.KeyVal("depth1", typesv1.ValObj(
						typesv1.KeyVal("depth2", typesv1.ValStr("deep-value")),
					)),
				)),
				typesv1.KeyVal("simple", typesv1.ValStr("value")),
			),
			mkMap:   MakeFlatGoMap,
			mkSlice: MakeFlatMapGoSlice,
			want: map[string]any{
				"composite.depth1.depth2": "deep-value",
				"simple":                  "value",
			},
		},
		{
			name: "flatmapslice",
			in: typesv1.ValArr(
				typesv1.ValStr("value1"),
				typesv1.ValStr("value2"),
				typesv1.ValObj(
					typesv1.KeyVal("composite", typesv1.ValObj(
						typesv1.KeyVal("depth1", typesv1.ValObj(
							typesv1.KeyVal("depth2", typesv1.ValStr("deep-value")),
						)),
					)),
					typesv1.KeyVal("simple", typesv1.ValStr("value")),
				),
			),
			mkMap:   MakeFlatGoMap,
			mkSlice: MakeFlatMapGoSlice,
			want: map[string]any{
				"0":                         "value1",
				"1":                         "value2",
				"2.composite.depth1.depth2": "deep-value",
				"2.simple":                  "value",
			},
		},
		{
			name: "flatslice",
			in: typesv1.ValArr(
				typesv1.ValStr("value1"),
				typesv1.ValStr("value2"),
				typesv1.ValArr(typesv1.ValStr("el0"), typesv1.ValStr("el1")),
				typesv1.ValObj(
					typesv1.KeyVal("composite", typesv1.ValObj(
						typesv1.KeyVal("depth1", typesv1.ValObj(
							typesv1.KeyVal("depth2", typesv1.ValStr("deep-value")),
						)),
					)),
					typesv1.KeyVal("simple", typesv1.ValStr("value")),
				),
			),
			mkMap:   MakeFlatGoMap,
			mkSlice: MakeFlatGoSlice,
			want: []any{
				"value1",
				"value2",
				[]any{"el0", "el1"},
				map[string]any{
					"composite.depth1.depth2": "deep-value",
					"simple":                  "value",
				},
			},
		},
		{
			name:    "string",
			in:      typesv1.ValStr("a-string"),
			mkMap:   MakeFlatGoMap,
			mkSlice: MakeFlatGoSlice,
			want:    "a-string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveVal(tt.in, tt.mkMap, tt.mkSlice)
			require.NoError(t, err)
			require.Empty(t, cmp.Diff(tt.want, got))
		})
	}
}
