package localalert

import (
	"context"
	"testing"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestAlertState(t *testing.T) {
	start := time.Date(2025, 7, 18, 17, 8, 41, 0, time.UTC)
	tests := []struct {
		name  string
		init  AlertState
		now   time.Time
		input *typesv1.Table
		check CheckFunc
		want  AlertState
	}{
		{
			name: "unknown to ok",
			init: AlertState{
				Rule:   mkrule("nothing special"),
				Status: AlertStatusUnknown,
			},
			now:   start,
			input: table(tableType(tableCol("my_rule", typesv1.TypeBool()))),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusOK, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start,
			},
		},
		{
			name: "unknown to pending",
			init: AlertState{
				Rule:   mkrule("nothing special", setFor(time.Second)),
				Status: AlertStatusUnknown,
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusPending, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special", setFor(time.Second)),
				Status:         AlertStatusPending,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
		},
		{
			name: "unknown to firing",
			init: AlertState{
				Rule:   mkrule("nothing special"),
				Status: AlertStatusUnknown,
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusFiring, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
		},
		{
			name: "ok to ok",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start,
			},
			now:   start,
			input: table(tableType(tableCol("my_rule", typesv1.TypeBool()))),
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start,
			},
		},

		{
			name: "ok to pending",
			init: AlertState{
				Rule:   mkrule("nothing special", setFor(time.Second)),
				Status: AlertStatusOK,
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusPending, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special", setFor(time.Second)),
				Status:         AlertStatusPending,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
		},
		{
			name: "ok to firing",
			init: AlertState{
				Rule:   mkrule("nothing special"),
				Status: AlertStatusOK,
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusFiring, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
		},
		{
			name: "pending to ok - no value",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusPending,
				TransitionedAt: start,
			},
			now:   start.Add(time.Second),
			input: table(tableType(tableCol("my_rule", typesv1.TypeBool()))),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusOK, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start.Add(time.Second),
			},
		},
		{
			name: "pending to ok - value but false",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(false)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusOK, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start.Add(time.Second),
			},
		},
		{
			name: "pending to pending (not yet long enough)",
			init: AlertState{
				Rule:           mkrule("nothing special", setFor(2*time.Second)),
				Status:         AlertStatusPending,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(time.Second),
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			want: AlertState{
				Rule:           mkrule("nothing special", setFor(2*time.Second)),
				Status:         AlertStatusPending,
				TransitionedAt: start,
				LastFiringAt:   start.Add(time.Second),
			},
		},
		{
			name: "pending to firing (long enough)",
			init: AlertState{
				Rule:           mkrule("nothing special", setFor(2*time.Second)),
				Status:         AlertStatusPending,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(2 * time.Second),
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusFiring, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special", setFor(2*time.Second)),
				Status:         AlertStatusFiring,
				TransitionedAt: start.Add(2 * time.Second),
				LastFiringAt:   start.Add(2 * time.Second),
			},
		},
		{
			name: "pending to firing (no for, updated?)",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusPending,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(2 * time.Second),
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusFiring, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start.Add(2 * time.Second),
				LastFiringAt:   start.Add(2 * time.Second),
			},
		},
		{
			name: "firing to ok - no value",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusOK, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start.Add(time.Second),
			},
		},
		{
			name: "firing to ok - value but false",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(false)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusOK, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusOK,
				TransitionedAt: start.Add(time.Second),
			},
		},
		{
			name: "keep firing because alert still true",
			init: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			want: AlertState{
				Rule:           mkrule("nothing special"),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start.Add(time.Second),
			},
		},
		{
			name: "keep firing even though value false",
			init: AlertState{
				Rule:           mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(false)),
			),
			want: AlertState{
				Rule:           mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
		},
		{
			name: "keep firing even though no value",
			init: AlertState{
				Rule:           mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
			),
			want: AlertState{
				Rule:           mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
		},
		{
			name: "stop firing after long enough",
			init: AlertState{
				Rule:           mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status:         AlertStatusFiring,
				TransitionedAt: start,
				LastFiringAt:   start,
			},
			now: start.Add(2 * time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
				require.Equal(t, AlertStatusOK, as)
				return nil
			},
			want: AlertState{
				Rule:           mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status:         AlertStatusOK,
				TransitionedAt: start.Add(2 * time.Second),
				LastFiringAt:   start,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			state := tt.init
			check := tt.check
			if check == nil {
				check = func(ctx context.Context, ar *typesv1.AlertRule, as AlertStatus, o *typesv1.Obj) error {
					assert.Nil(t, ar)
					assert.Nil(t, as)
					assert.Nil(t, o)
					require.Fail(t, "should not have changed state")
					return nil
				}
			}
			err := state.apply(ctx, tt.input, tt.now, check)
			require.NoError(t, err)
			require.Equal(t, tt.want, state)
		})
	}
}

func mkrule(name string, opts ...func(*typesv1.AlertRule)) *typesv1.AlertRule {
	ar := &typesv1.AlertRule{
		Name: name,
	}
	for _, opt := range opts {
		opt(ar)
	}
	return ar
}

func setFor(v time.Duration) func(*typesv1.AlertRule) {
	return func(ar *typesv1.AlertRule) {
		ar.For = durationpb.New(v)
	}
}

func setKeepFiringFor(v time.Duration) func(*typesv1.AlertRule) {
	return func(ar *typesv1.AlertRule) {
		ar.KeepFiringFor = durationpb.New(v)
	}
}

func addLabel(k string, v *typesv1.Val) func(*typesv1.AlertRule) {
	return func(ar *typesv1.AlertRule) {
		if ar.Labels == nil {
			ar.Labels = &typesv1.Obj{}
		}
		ar.Labels.Kvs = append(ar.Labels.Kvs, typesv1.KeyVal(k, v))
	}
}

func addAnnotation(k string, v *typesv1.Val) func(*typesv1.AlertRule) {
	return func(ar *typesv1.AlertRule) {
		if ar.Annotations == nil {
			ar.Annotations = &typesv1.Obj{}
		}
		ar.Annotations.Kvs = append(ar.Annotations.Kvs, typesv1.KeyVal(k, v))
	}
}

func table(tableType *typesv1.TableType, rows ...*typesv1.Arr) *typesv1.Table {
	return &typesv1.Table{
		Type: tableType,
		Rows: rows,
	}
}

func tableType(cols ...*typesv1.TableType_Column) *typesv1.TableType {
	return &typesv1.TableType{Columns: cols}
}

func tableCol(name string, typ *typesv1.VarType) *typesv1.TableType_Column {
	return &typesv1.TableType_Column{Name: name, Type: typ}
}

func arr(vals ...*typesv1.Val) *typesv1.Arr {
	return &typesv1.Arr{Items: vals}
}

func boolean(v bool) *typesv1.Val {
	return typesv1.ValBool(v)
}

func str(v string) *typesv1.Val {
	return typesv1.ValStr(v)
}

func dur(v time.Duration) *typesv1.Val {
	return typesv1.ValDuration(v)
}

func f64(v float64) *typesv1.Val {
	return typesv1.ValF64(v)
}

func i64(v int64) *typesv1.Val {
	return typesv1.ValI64(v)
}

func akv(k string, v *typesv1.Val) *typesv1.KV {
	return &typesv1.KV{Key: k, Value: v}
}

func obj(kvs ...*typesv1.KV) *typesv1.Obj {
	return &typesv1.Obj{Kvs: kvs}
}
