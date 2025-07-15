package localstorage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog"
	"github.com/humanlogio/humanlog/pkg/sink"
	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ServiceName = "antoine's service"
	TestRes     = typesv1.NewResource("res_schema_url", []*typesv1.KV{
		typesv1.KeyVal(string(semconv.ServiceNameKey), typesv1.ValStr(ServiceName)),
	})
	TestScope = typesv1.NewScope("scope_schema_url", "test-scope", "v0.0.0-test", []*typesv1.KV{
		typesv1.KeyVal("component", typesv1.ValStr("database")),
	})
)

func RunTest(t *testing.T, constructor func(t *testing.T, timeNow func() time.Time, newUlid func() string) Storage) {
	tests := []struct {
		name  string
		q     string
		limit int
		input []*typesv1.Log
		want  []*typesv1.Data
	}{
		{
			name:  "nothing",
			q:     `{from==2006-01-02T15:04:06.00500000Z}`,
			limit: 4,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs(nil),
			},
		},
		{
			name:  "all",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 5,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
					reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
				}),
			},
		},

		{
			name:  "all",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 5,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
					reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
				}),
			},
		},
		{
			name:  "all with pagination",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 3,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				}),
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
				}),
			},
		},
		{
			name:  "all with pagination overflow",
			q:     `{to==2006-01-02T15:04:06.00500000Z}`,
			limit: 2,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				}),
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
					reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
				}),
				dataLogs(nil),
			},
		},
		{
			name:  "skip last",
			q:     `{to==2006-01-02T15:04:06.004000000Z}`,
			limit: 4,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				}),
			},
		},
		{
			name:  "skip first",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.005000000Z}`,
			limit: 4,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
					reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
				}),
			},
		},
		{
			name:  "from only",
			q:     `{from==2006-01-02T15:04:06.002000000Z}`,
			limit: 4,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
					reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
				}),
			},
		},
		{
			name:  "slice",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.004000000Z}`,
			limit: 4,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl": "info", "msg":"hello world 1"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.004"), `{"ts":"2006-01-02T15:04:06.004", "lvl": "info", "msg":"hello world 4"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.002"), `{"ts":"2006-01-02T15:04:06.002", "lvl": "info", "msg":"hello world 2"}`),
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl": "info", "msg":"hello world 3"}`),
				}),
			},
		},
		{
			name:  "simple query on `severity_text`",
			q:     `{from==2006-01-02T15:04:06.002000000Z to==2006-01-02T15:04:06.004000000Z} filter severity_text=="error"`,
			limit: 4,
			input: []*typesv1.Log{
				reallog(TestRes, TestScope, "1", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl":"error", "msg":"some sort of problem"}`),
				reallog(TestRes, TestScope, "2", ServiceName, musttime("2006-01-02T15:04:06.001"), `{"ts":"2006-01-02T15:04:06.001", "lvl":"info", "msg":"no problem, all is fine"}`),
				reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl":"error", "msg":"some sort of problem a bit later"}`),
				reallog(TestRes, TestScope, "4", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl":"info", "msg":"no problem, all is fine a bit later"}`),
				reallog(TestRes, TestScope, "5", ServiceName, musttime("2006-01-02T15:04:06.006"), `{"ts":"2006-01-02T15:04:06.006", "lvl":"error", "msg":"some sort of problem too late"}`),
				reallog(TestRes, TestScope, "6", ServiceName, musttime("2006-01-02T15:04:06.006"), `{"ts":"2006-01-02T15:04:06.006", "lvl":"info", "msg":"no problem, all is fine too late"}`),
			},
			want: []*typesv1.Data{
				dataLogs([]*typesv1.Log{
					reallog(TestRes, TestScope, "3", ServiceName, musttime("2006-01-02T15:04:06.003"), `{"ts":"2006-01-02T15:04:06.003", "lvl":"error", "msg":"some sort of problem a bit later"}`),
				}),
			},
		},
	}
	type sinkid struct {
		resID   uint64
		scopeID uint64
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			now := time.Date(2025, 3, 10, 20, 55, 20, 0, time.UTC)
			timeNow := func() time.Time {
				return now
			}
			i := 0
			newUlid := func() string {
				i++
				return fmt.Sprintf("ulid-%d", i)
			}

			db := constructor(t, timeNow, newUlid)
			defer db.Close()

			snks := map[sinkid]sink.Sink{}
			defer func() {
				for _, snk := range snks {
					err := snk.Close(ctx)
					require.NoError(t, err)
				}
			}()
			for _, ev := range tt.input {
				res := ev.Resource
				scope := ev.Scope
				snkid := sinkid{resID: res.ResourceHash_64, scopeID: scope.ScopeHash_64}
				snk, ok := snks[snkid]
				if !ok {
					var err error
					snk, err = db.SinkFor(ctx, res, scope)
					require.NoError(t, err)
					snks[snkid] = snk
				}
				err := snk.Receive(ctx, ev)
				require.NoError(t, err)
			}

			q, err := db.Parse(ctx, tt.q)
			require.NoError(t, err)

			var (
				got []*typesv1.Data
				c   *typesv1.Cursor
			)
			for {
				out, next, err := db.Query(ctx, q, c, int(tt.limit))
				require.NoError(t, err)
				got = append(got, out)
				c = next
				if next == nil {
					break
				}
				require.LessOrEqual(t, len(got), len(tt.want))
			}

			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				diff := cmp.Diff(tt.want[i], got[i], protocmp.Transform())
				require.Empty(t, diff)
			}
		})
	}
}

func musttime(str string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000", str)
	if err != nil {
		panic(err)
	}
	return t
}

func dataLogs(logs []*typesv1.Log) *typesv1.Data {
	return &typesv1.Data{
		Shape: &typesv1.Data_Logs{
			Logs: &typesv1.Logs{Logs: logs},
		},
	}
}

func reallog(res *typesv1.Resource, scp *typesv1.Scope, ulid, svcname string, parsedAt time.Time, raw string) *typesv1.Log {
	ev := &typesv1.Log{Ulid: ulid, ObservedTimestamp: timestamppb.New(parsedAt), Resource: res, Scope: scp, Raw: []byte(raw)}

	opts := humanlog.DefaultOptions()
	opts.DetectTimestamp = true
	jsonHandler := humanlog.JSONHandler{Opts: opts}
	logfmtHandler := humanlog.LogfmtHandler{Opts: opts}

	handlers := []func([]byte, *typesv1.Log) bool{
		jsonHandler.TryHandle,
		logfmtHandler.TryHandle,
	}

	isHandled := false
	for _, hdlr := range handlers {
		if hdlr([]byte(raw), ev) {
			isHandled = true
			break
		}
	}
	if !isHandled {
		panic("raw doesn't parse with humanlog")
	}
	ev.ServiceName = svcname

	return ev
}
