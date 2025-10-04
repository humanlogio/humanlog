package localalert

import (
	"context"
	"fmt"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func NewEvaluator(db localstorage.Storage, timeNow func() time.Time) *Evaluator {
	return &Evaluator{
		db:      db,
		timeNow: timeNow,
	}
}

type Evaluator struct {
	db      localstorage.Storage
	timeNow func() time.Time
}

type AlertFiring struct {
	Labels *typesv1.Obj
}

type CheckFunc func(
	context.Context,
	*typesv1.AlertRule,
	*typesv1.AlertState,
	*typesv1.Obj,
) error

func (ev *Evaluator) EvaluateRules(ctx context.Context, project *typesv1.Project, group *typesv1.AlertGroup, onStateChange CheckFunc) error {
	projectName := project.Spec.Name
	spec := group.Spec
	var keeplist []string
	for _, alert := range spec.Rules {
		keeplist = append(keeplist, alert.Name)
		state, err := ev.db.AlertGetOrCreate(ctx, projectName, spec.Name, alert.Name, func() *typesv1.AlertState {
			return newAlertState(alert)
		})
		if err != nil {
			return fmt.Errorf("getting alert state for group %q, alert %q: %v", spec.Name, alert.Name, err)
		}
		err = check(ctx, state, ev.db, ev.timeNow(), onStateChange)
		if err != nil {
			return fmt.Errorf("checking alert state for group %q, alert %q: %v", spec.Name, alert.Name, err)
		}
		err = ev.db.AlertUpdateState(ctx, projectName, spec.Name, alert.Name, state)
		if err != nil {
			return fmt.Errorf("updating alert state for group %q, alert %q: %v", spec.Name, alert.Name, err)
		}
	}
	if err := ev.db.AlertDeleteStateNotInList(ctx, projectName, spec.Name, keeplist); err != nil {
		return err
	}
	return nil
}

func newAlertState(rule *typesv1.AlertRule) *typesv1.AlertState {
	return &typesv1.AlertState{Rule: rule, Status: &typesv1.AlertState_Unknown{}}
}

func check(
	ctx context.Context,
	as *typesv1.AlertState,
	db localstorage.Queryable,
	now time.Time,
	onStateChange CheckFunc,
) error {
	data, _, _, err := db.Query(ctx, as.Rule.Expr, nil, 100)
	if err != nil {
		return fmt.Errorf("evaluating alert rule expression: %v", err)
	}
	freeform, ok := data.Shape.(*typesv1.Data_FreeForm)
	if !ok {
		return fmt.Errorf("invalid query, result is not a table")
	}
	table := freeform.FreeForm
	return apply(ctx, as, table, now, onStateChange)
}

func apply(ctx context.Context, as *typesv1.AlertState, table *typesv1.Table, now time.Time, onStateChange CheckFunc) error {
	transitionToOk := func(labels *typesv1.Obj) error {
		as.Status = &typesv1.AlertState_Ok{Ok: &typesv1.AlertOk{}}
		as.TransitionedAt = timestamppb.New(now)
		return onStateChange(ctx, as.Rule, as, labels)
	}
	transitionToPending := func(labels *typesv1.Obj) error {
		as.Status = &typesv1.AlertState_Pending{Pending: &typesv1.AlertPending{}}
		as.TransitionedAt = timestamppb.New(now)
		return onStateChange(ctx, as.Rule, as, labels)
	}
	transitionToFiring := func(labels *typesv1.Obj) error {
		as.Status = &typesv1.AlertState_Firing{Firing: &typesv1.AlertFiring{}}
		as.TransitionedAt = timestamppb.New(now)
		return onStateChange(ctx, as.Rule, as, labels)
	}

	onOk := func(labels *typesv1.Obj) error {
		switch as.Status.(type) {
		case *typesv1.AlertState_Unknown:
			return transitionToOk(labels)
		case *typesv1.AlertState_Ok:
			// we're already ok
			return nil
		case *typesv1.AlertState_Pending:
			return transitionToOk(labels)
		case *typesv1.AlertState_Firing:
			if as.Rule.KeepFiringFor == nil {
				// we're done
				return transitionToOk(labels)
			}
			firingFor := as.Rule.KeepFiringFor.AsDuration()
			mustBeOkUntil := as.LastFiringAt.AsTime().Add(firingFor)
			if now.Before(mustBeOkUntil) {
				return nil // still firing
			}
			// we're done firing
			return transitionToOk(labels)
		default:
			return fmt.Errorf("unhandled case: %T (%#v)", as.Status, as.Status)
		}
	}
	onFiring := func(labels *typesv1.Obj) error {
		as.LastFiringAt = timestamppb.New(now) // always record the last firing
		switch as.Status.(type) {
		case *typesv1.AlertState_Unknown:
			if as.Rule.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case *typesv1.AlertState_Ok:
			if as.Rule.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case *typesv1.AlertState_Pending:
			if as.Rule.For == nil {
				return transitionToFiring(labels)
			}
			pendingFor := as.Rule.For.AsDuration()
			firesAt := as.TransitionedAt.AsTime().Add(pendingFor)
			if now.Before(firesAt) {
				// still pending
				return nil
			}
			return transitionToFiring(labels)
		case *typesv1.AlertState_Firing:
			// we're already firing
			return nil
		default:
			return fmt.Errorf("unhandled case: %T (%#v)", as.Status, as.Status)
		}
	}
	return EvaluateTableForAlert(table, onOk, onFiring)
}

func EnsureTableTypeValidForAlerts(ttyp *typesv1.TableType) error {
	if len(ttyp.Columns) == 0 {
		return fmt.Errorf("table doesn't have any column")
	}
	first := ttyp.Columns[0]
	scalar, ok := first.Type.Type.(*typesv1.VarType_Scalar)
	if !ok {
		return fmt.Errorf("first column should be a boolean, was a %s", first.Type.String())
	}
	if scalar.Scalar != typesv1.ScalarType_bool {
		return fmt.Errorf("first column should be a boolean, was a %s", first.Type.String())
	}
	return nil
}

func EvaluateTableForAlert(table *typesv1.Table, onOk func(*typesv1.Obj) error, onFiring func(*typesv1.Obj) error) error {
	if err := EnsureTableTypeValidForAlerts(table.Type); err != nil {
		return err
	}
	colTypes := table.Type.Columns
	if len(table.Rows) < 1 {
		return onOk(nil)
	}
	checkRow := func(columns []*typesv1.Val) error {
		if len(columns) < 1 {
			return fmt.Errorf("not enough columns")
		}
		if len(colTypes) != len(columns) {
			return fmt.Errorf("table type announces %d columns but row contains %d", len(colTypes), len(columns))
		}
		isFiring, err := mustBool(columns[0])
		if err != nil {
			return err
		}
		// is firing, prepare the variables
		var kvs []*typesv1.KV
		for i, colType := range colTypes {
			colVal := columns[i]
			kvs = append(kvs, typesv1.KeyVal(colType.Name, colVal))
		}
		variables := &typesv1.Obj{Kvs: kvs}
		if !isFiring.Bool {
			return onOk(variables)
		}
		return onFiring(variables)
	}

	for i, row := range table.Rows {
		if err := checkRow(row.Items); err != nil {
			return fmt.Errorf("row %d: %v", i, err)
		}
	}

	return nil
}

func mustBool(v *typesv1.Val) (*typesv1.Val_Bool, error) {
	vb, ok := v.Kind.(*typesv1.Val_Bool)
	if !ok {
		return nil, fmt.Errorf("must be a bool, was a %T", v.Kind)
	}
	return vb, nil
}
