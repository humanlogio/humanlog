package stdiosink

import (
	"testing"
	"time"

	"github.com/humanlogio/api/go/pkg/logql"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
)

func TestPutKV(t *testing.T) {
	tests := []struct {
		name string
		args *typesv1.KV
		want map[string]string
	}{
		{
			name: "convert into map[string]string",
			args: typesv1.KeyVal("root", typesv1.ValObj(
				typesv1.KeyVal("a", typesv1.ValStr("lorem")),
				typesv1.KeyVal("b", typesv1.ValI64(int64(1))),
				typesv1.KeyVal("c", typesv1.ValF64(float64(3.14))),
				typesv1.KeyVal("d", typesv1.ValBool(true)),
				typesv1.KeyVal("e", typesv1.ValObj(
					typesv1.KeyVal("f", typesv1.ValStr("foo")),
				)),
				typesv1.KeyVal("g", typesv1.ValArr(
					typesv1.ValStr("bar"),
					typesv1.ValI64(int64(2)),
					typesv1.ValF64(float64(4.2)),
					typesv1.ValBool(false),
					typesv1.ValObj(
						typesv1.KeyVal("h", typesv1.ValStr("baz")),
					),
					typesv1.ValArr(
						typesv1.ValStr("qux"),
						typesv1.ValI64(int64(3)),
						typesv1.ValF64(float64(5.3)),
						typesv1.ValBool(true),
					),
					typesv1.ValTime(time.Date(2024, 12, 13, 19, 36, 0, 0, time.UTC)),
				),
				))),
			want: map[string]string{
				"root.a":     "\"lorem\"",
				"root.b":     "1",
				"root.c":     "3.14",
				"root.d":     "true",
				"root.e.f":   "\"foo\"",
				"root.g.0":   "\"bar\"",
				"root.g.1":   "2",
				"root.g.2":   "4.2",
				"root.g.3":   "false",
				"root.g.4.h": "\"baz\"",
				"root.g.5.0": "\"qux\"",
				"root.g.5.1": "3",
				"root.g.5.2": "5.3",
				"root.g.5.3": "true",
				"root.g.6":   "\"2024-12-13T19:36:00Z\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.args.Key
			value, err := logql.ResolveVal(tt.args.Value, logql.MakeFlatGoMap, logql.MakeFlatMapGoSlice)
			require.NoError(t, err)

			kvs := make(map[string]string)
			put(&kvs, key, value)

			got := kvs
			require.Equal(t, tt.want, got)
		})
	}
}
