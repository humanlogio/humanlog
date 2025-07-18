package localalert

import (
	"context"
	"fmt"
	"time"

	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localstate"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
	"github.com/humanlogio/humanlog/pkg/localstorage"
	"golang.org/x/sync/errgroup"
)

func NewEvaluator(ruleSource localstate.DB, db localstorage.Queryable, timeNow func() time.Time) *Evaluator {
	return &Evaluator{
		ruleSource:  ruleSource,
		db:          db,
		timeNow:     timeNow,
		alertStates: make(map[int64]*alertState),
	}
}

type Evaluator struct {
	ruleSource localstate.DB
	db         localstorage.Queryable
	timeNow    func() time.Time

	alertStates map[int64]*alertState
}

type AlertStatus string

const (
	AlertStatusUnknown AlertStatus = "unknown"
	AlertStatusOK      AlertStatus = "ok"
	AlertStatusPending AlertStatus = "pending"
	AlertStatusFiring  AlertStatus = "firing"
	AlertStatusDeleted AlertStatus = "deleted"
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

func (ev *Evaluator) EvaluateRules(ctx context.Context, onStateChange CheckFunc) error {
	iterator := iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) (items []*typesv1.AlertRule, next *typesv1.Cursor, err error) {
		out, err := ev.ruleSource.ListAlertRule(ctx, &alertv1.ListAlertRuleRequest{Cursor: cursor, Limit: limit})
		if err != nil {
			return items, next, err
		}
		next = out.Next
		for _, el := range out.Items {
			items = append(items, el.AlertRule)
		}
		return items, next, err
	})

	eg, ctx := errgroup.WithContext(ctx)
	seen := make(map[int64]struct{})
	for iterator.Next() {
		alert := iterator.Current()
		seen[alert.Id] = struct{}{}
		eg.Go(func() error {
			state, ok := ev.alertStates[alert.Id]
			if !ok {
				state = newAlertState(alert)
			}
			err := state.check(ctx, ev.db, ev.timeNow(), onStateChange)
			if err != nil {
				return err
			}
			ev.alertStates[alert.Id] = state
			return nil
		})
	}
	for _, state := range ev.alertStates {
		if _, ok := seen[state.rule.Id]; !ok {
			if err := onStateChange(ctx, state.rule, AlertStatusDeleted, nil); err != nil {
				return err
			}
		}
	}

	if err := iterator.Err(); err != nil {
		return fmt.Errorf("listing rules: %v", err)
	}

	return eg.Wait()
}

type alertState struct {
	rule           *typesv1.AlertRule
	transitionedAt time.Time
	lastFiringAt   time.Time
	status         AlertStatus
}

func newAlertState(rule *typesv1.AlertRule) *alertState {
	return &alertState{rule: rule, status: AlertStatusUnknown}
}

func (as *alertState) check(
	ctx context.Context,
	db localstorage.Queryable,
	now time.Time,
	onStateChange CheckFunc,
) error {
	data, _, err := db.Query(ctx, as.rule.Expr, nil, 100)
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

func (as *alertState) apply(ctx context.Context, table *typesv1.Table, now time.Time, onStateChange CheckFunc) error {
	transitionToOk := func(labels *typesv1.Obj) error {
		as.status = AlertStatusOK
		as.transitionedAt = now
		return onStateChange(ctx, as.rule, as.status, labels)
	}
	transitionToPending := func(labels *typesv1.Obj) error {
		as.status = AlertStatusPending
		as.transitionedAt = now
		return onStateChange(ctx, as.rule, as.status, labels)
	}
	transitionToFiring := func(labels *typesv1.Obj) error {
		as.status = AlertStatusFiring
		as.transitionedAt = now
		return onStateChange(ctx, as.rule, as.status, labels)
	}

	onOk := func(labels *typesv1.Obj) error {
		switch as.status {
		case AlertStatusUnknown:
			return transitionToOk(labels)
		case AlertStatusOK:
			// we're already ok
			return nil
		case AlertStatusPending:
			return transitionToOk(labels)
		case AlertStatusFiring:
			if as.rule.KeepFiringFor == nil {
				// we're done
				return transitionToOk(labels)
			}
			firingFor := as.rule.KeepFiringFor.AsDuration()
			mustBeOkUntil := as.lastFiringAt.Add(firingFor)
			if now.Before(mustBeOkUntil) {
				return nil // still firing
			}
			// we're done firing
			return transitionToOk(labels)
		default:
			return fmt.Errorf("unhandled case: %T (%#v)", as.status, as.status)
		}
	}
	onFiring := func(labels *typesv1.Obj) error {
		as.lastFiringAt = now // always record the last firing
		switch as.status {
		case AlertStatusUnknown:
			if as.rule.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case AlertStatusOK:
			if as.rule.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case AlertStatusPending:
			if as.rule.For == nil {
				return transitionToFiring(labels)
			}
			pendingFor := as.rule.For.AsDuration()
			firesAt := as.transitionedAt.Add(pendingFor)
			if now.Before(firesAt) {
				// still pending
				return nil
			}
			return transitionToFiring(labels)
		case AlertStatusFiring:
			// we're already firing
			return nil
		default:
			return fmt.Errorf("unhandled case: %T (%#v)", as.status, as.status)
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
