package localalert

import (
	"context"
	"fmt"
	"time"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/pkg/localstorage"
)

type AlertStorage interface {
	GetOrCreate(ctx context.Context, groupName, alertName string, create func() *AlertState) (*AlertState, error)
	UpdateState(ctx context.Context, groupName, alertName string, state *AlertState) error
	DeleteStateNotInList(ctx context.Context, groupName string, keeplist []string) error
}

func NewEvaluator(db localstorage.Queryable, alertStorage AlertStorage, timeNow func() time.Time) *Evaluator {
	return &Evaluator{
		db:           db,
		timeNow:      timeNow,
		alertStorage: alertStorage,
	}
}

type Evaluator struct {
	db      localstorage.Queryable
	timeNow func() time.Time

	alertStorage AlertStorage
}

type AlertStatus string

const (
	AlertStatusUnknown AlertStatus = "unknown"
	AlertStatusOK      AlertStatus = "ok"
	AlertStatusPending AlertStatus = "pending"
	AlertStatusFiring  AlertStatus = "firing"
)

type AlertFiring struct {
	Labels *typesv1.Obj
}

type CheckFunc func(
	context.Context,
	*typesv1.AlertRule,
	AlertStatus,
	*typesv1.Obj,
) error

func (ev *Evaluator) EvaluateRules(ctx context.Context, group *typesv1.AlertGroup, onStateChange CheckFunc) error {
	var keeplist []string
	for _, alert := range group.Rules {
		keeplist = append(keeplist, alert.Name)
		state, err := ev.alertStorage.GetOrCreate(ctx, group.Name, alert.Name, func() *AlertState {
			return newAlertState(alert)
		})
		if err != nil {
			return fmt.Errorf("getting alert state for group %q, alert %q: %v", group.Name, alert.Name, err)
		}
		err = state.check(ctx, ev.db, ev.timeNow(), onStateChange)
		if err != nil {
			return fmt.Errorf("checking alert state for group %q, alert %q: %v", group.Name, alert.Name, err)
		}
		err = ev.alertStorage.UpdateState(ctx, group.Name, alert.Name, state)
		if err != nil {
			return fmt.Errorf("updating alert state for group %q, alert %q: %v", group.Name, alert.Name, err)
		}
	}
	if err := ev.alertStorage.DeleteStateNotInList(ctx, group.Name, keeplist); err != nil {
		return err
	}
	return nil
}

type AlertState struct {
	Rule           *typesv1.AlertRule
	TransitionedAt time.Time
	LastFiringAt   time.Time
	Status         AlertStatus
}

func newAlertState(rule *typesv1.AlertRule) *AlertState {
	return &AlertState{Rule: rule, Status: AlertStatusUnknown}
}

func (as *AlertState) check(
	ctx context.Context,
	db localstorage.Queryable,
	now time.Time,
	onStateChange CheckFunc,
) error {
	data, _, err := db.Query(ctx, as.Rule.Expr, nil, 100)
	if err != nil {
		return fmt.Errorf("evaluating alert rule expression: %v", err)
	}
	freeform, ok := data.Shape.(*typesv1.Data_FreeForm)
	if !ok {
		return fmt.Errorf("invalid query, result is not a table")
	}
	table := freeform.FreeForm
	return as.apply(ctx, table, now, onStateChange)
}

func (as *AlertState) apply(ctx context.Context, table *typesv1.Table, now time.Time, onStateChange CheckFunc) error {
	transitionToOk := func(labels *typesv1.Obj) error {
		as.Status = AlertStatusOK
		as.TransitionedAt = now
		return onStateChange(ctx, as.Rule, as.Status, labels)
	}
	transitionToPending := func(labels *typesv1.Obj) error {
		as.Status = AlertStatusPending
		as.TransitionedAt = now
		return onStateChange(ctx, as.Rule, as.Status, labels)
	}
	transitionToFiring := func(labels *typesv1.Obj) error {
		as.Status = AlertStatusFiring
		as.TransitionedAt = now
		return onStateChange(ctx, as.Rule, as.Status, labels)
	}

	onOk := func(labels *typesv1.Obj) error {
		switch as.Status {
		case AlertStatusUnknown:
			return transitionToOk(labels)
		case AlertStatusOK:
			// we're already ok
			return nil
		case AlertStatusPending:
			return transitionToOk(labels)
		case AlertStatusFiring:
			if as.Rule.KeepFiringFor == nil {
				// we're done
				return transitionToOk(labels)
			}
			firingFor := as.Rule.KeepFiringFor.AsDuration()
			mustBeOkUntil := as.LastFiringAt.Add(firingFor)
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
		as.LastFiringAt = now // always record the last firing
		switch as.Status {
		case AlertStatusUnknown:
			if as.Rule.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case AlertStatusOK:
			if as.Rule.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case AlertStatusPending:
			if as.Rule.For == nil {
				return transitionToFiring(labels)
			}
			pendingFor := as.Rule.For.AsDuration()
			firesAt := as.TransitionedAt.Add(pendingFor)
			if now.Before(firesAt) {
				// still pending
				return nil
			}
			return transitionToFiring(labels)
		case AlertStatusFiring:
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
