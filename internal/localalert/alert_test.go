package localalert

import (
	"context"
	"testing"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAlertState(t *testing.T) {
	start := time.Date(2025, 7, 18, 17, 8, 41, 0, time.UTC)
	startts := timestamppb.New(start)
	tests := []struct {
		name  string
		init  *typesv1.AlertRule
		now   time.Time
		input *typesv1.Table
		check OnStateTransition
		want  *typesv1.AlertRule
	}{
		{
			name: "unknown to ok",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStateUnknown(),
			},
			now:   start,
			input: table(tableType(tableCol("my_rule", typesv1.TypeBool()))),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateOk().Ok, ar.Status.GetOk())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(startts),
			},
		},
		{
			name: "unknown to pending",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(time.Second)),
				Status: alertStateUnknown(),
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStatePending().Pending, ar.Status.GetPending())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(time.Second)),
				Status: alertStatusPending(startts, startts),
			},
		},
		{
			name: "unknown to firing",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStateUnknown(),
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateFiring(nil).Firing, ar.Status.GetFiring())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, startts),
			},
		},
		{
			name: "ok to ok",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(startts),
			},
			now:   start,
			input: table(tableType(tableCol("my_rule", typesv1.TypeBool()))),
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(startts),
			},
		},

		{
			name: "ok to pending",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(time.Second)),
				Status: alertStatusOk(nil),
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStatePending().Pending, ar.Status.GetPending())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(time.Second)),
				Status: alertStatusPending(startts, startts),
			},
		},
		{
			name: "ok to firing",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(nil),
			},
			now: start,
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateFiring(nil).Firing, ar.Status.GetFiring())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, startts),
			},
		},
		{
			name: "pending to ok - no value",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusPending(startts, nil),
			},
			now:   start.Add(time.Second),
			input: table(tableType(tableCol("my_rule", typesv1.TypeBool()))),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateOk().Ok, ar.Status.GetOk())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(timestamppb.New(start.Add(time.Second))),
			},
		},
		{
			name: "pending to ok - value but false",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, nil),
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(false)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateOk().Ok, ar.Status.GetOk())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(timestamppb.New(start.Add(time.Second))),
			},
		},
		{
			name: "pending to pending (not yet long enough)",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(2*time.Second)),
				Status: alertStatusPending(startts, startts),
			},
			now: start.Add(time.Second),
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(2*time.Second)),
				Status: alertStatusPending(startts, timestamppb.New(start.Add(time.Second))),
			},
		},
		{
			name: "pending to firing (long enough)",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(2*time.Second)),
				Status: alertStatusPending(startts, startts),
			},
			now: start.Add(2 * time.Second),
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateFiring(nil).Firing, ar.Status.GetFiring())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setFor(2*time.Second)),
				Status: alertStatusFiring(timestamppb.New(start.Add(2*time.Second)), timestamppb.New(start.Add(2*time.Second))),
			},
		},
		{
			name: "pending to firing (no for, updated?)",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusPending(startts, startts),
			},
			now: start.Add(2 * time.Second),
			input: table(tableType(
				tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateFiring(nil).Firing, ar.Status.GetFiring())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(timestamppb.New(start.Add(2*time.Second)), timestamppb.New(start.Add(2*time.Second))),
			},
		},
		{
			name: "firing to ok - no value",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, nil),
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateOk().Ok, ar.Status.GetOk())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(timestamppb.New(start.Add(time.Second))),
			},
		},
		{
			name: "firing to ok - value but false",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, nil),
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(false)),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateOk().Ok, ar.Status.GetOk())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusOk(timestamppb.New(start.Add(time.Second))),
			},
		},
		{
			name: "keep firing because alert still true",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, startts),
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(true)),
			),
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special"),
				Status: alertStatusFiring(startts, timestamppb.New(start.Add(time.Second))),
			},
		},
		{
			name: "keep firing even though value false",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status: alertStatusFiring(startts, startts),
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
				arr(boolean(false)),
			),
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status: alertStatusFiring(startts, startts),
			},
		},
		{
			name: "keep firing even though no value",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status: alertStatusFiring(startts, startts),
			},
			now: start.Add(time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
			),
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status: alertStatusFiring(startts, startts),
			},
		},
		{
			name: "stop firing after long enough",
			init: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status: alertStatusFiring(startts, startts),
			},
			now: start.Add(2 * time.Second),
			input: table(
				tableType(tableCol("my_rule", typesv1.TypeBool())),
			),
			check: func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
				require.Equal(t, alertStateOk().Ok, ar.Status.GetOk())
				return nil
			},
			want: &typesv1.AlertRule{
				Spec:   mkrule("nothing special", setKeepFiringFor(2*time.Second)),
				Status: alertStatusOkWithLastFiring(timestamppb.New(start.Add(2*time.Second)), startts),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			state := tt.init
			check := tt.check
			if check == nil {
				check = func(ctx context.Context, ar *typesv1.AlertRule, o *typesv1.Obj) error {
					assert.Nil(t, ar)
					assert.Nil(t, o)
					require.Fail(t, "should not have changed state")
					return nil
				}
			}
			err := apply(ctx, state, tt.input, tt.now, check)
			require.NoError(t, err)
			require.Equal(t, tt.want, state)
		})
	}
}

func alertStateUnknown() *typesv1.AlertRuleStatus {
	return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{Unknown: &typesv1.AlertUnknown{}}}
}

func alertStateOk() *typesv1.AlertRuleStatus_Ok {
	return &typesv1.AlertRuleStatus_Ok{Ok: &typesv1.AlertOk{}}
}

func alertStatePending() *typesv1.AlertRuleStatus_Pending {
	return &typesv1.AlertRuleStatus_Pending{Pending: &typesv1.AlertPending{}}
}
func alertStateFiring(labels *typesv1.Obj) *typesv1.AlertRuleStatus_Firing {
	return &typesv1.AlertRuleStatus_Firing{Firing: &typesv1.AlertFiring{}}
}

func alertStatusOk(transitionedAt *timestamppb.Timestamp) *typesv1.AlertRuleStatus {
	return &typesv1.AlertRuleStatus{
		TransitionedAt: transitionedAt,
		Status:         &typesv1.AlertRuleStatus_Ok{Ok: &typesv1.AlertOk{}},
	}
}

func alertStatusOkWithLastFiring(transitionedAt, lastFiringAt *timestamppb.Timestamp) *typesv1.AlertRuleStatus {
	return &typesv1.AlertRuleStatus{
		TransitionedAt: transitionedAt,
		LastFiringAt:   lastFiringAt,
		Status:         &typesv1.AlertRuleStatus_Ok{Ok: &typesv1.AlertOk{}},
	}
}

func alertStatusPending(transitionedAt, lastFiringAt *timestamppb.Timestamp) *typesv1.AlertRuleStatus {
	return &typesv1.AlertRuleStatus{
		TransitionedAt: transitionedAt,
		LastFiringAt:   lastFiringAt,
		Status:         &typesv1.AlertRuleStatus_Pending{Pending: &typesv1.AlertPending{}},
	}
}

func alertStatusFiring(transitionedAt, lastFiringAt *timestamppb.Timestamp) *typesv1.AlertRuleStatus {
	return &typesv1.AlertRuleStatus{
		TransitionedAt: transitionedAt,
		LastFiringAt:   lastFiringAt,
		Status:         &typesv1.AlertRuleStatus_Firing{Firing: &typesv1.AlertFiring{}},
	}
}

func mkrule(name string, opts ...func(*typesv1.AlertRuleSpec)) *typesv1.AlertRuleSpec {
	ar := &typesv1.AlertRuleSpec{
		Name: name,
	}
	for _, opt := range opts {
		opt(ar)
	}
	return ar
}

func setFor(v time.Duration) func(*typesv1.AlertRuleSpec) {
	return func(ar *typesv1.AlertRuleSpec) {
		ar.For = durationpb.New(v)
	}
}

func setKeepFiringFor(v time.Duration) func(*typesv1.AlertRuleSpec) {
	return func(ar *typesv1.AlertRuleSpec) {
		ar.KeepFiringFor = durationpb.New(v)
	}
}

func addLabel(k string, v *typesv1.Val) func(*typesv1.AlertRuleSpec) {
	return func(ar *typesv1.AlertRuleSpec) {
		if ar.Labels == nil {
			ar.Labels = &typesv1.Obj{}
		}
		ar.Labels.Kvs = append(ar.Labels.Kvs, typesv1.KeyVal(k, v))
	}
}

func addAnnotation(k string, v *typesv1.Val) func(*typesv1.AlertRuleSpec) {
	return func(ar *typesv1.AlertRuleSpec) {
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
