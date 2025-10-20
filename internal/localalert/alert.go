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

type OnStateTransition func(
	ctx context.Context,
	rule *typesv1.AlertRule,
	labels *typesv1.Obj,
) error

func (ev *Evaluator) EvaluateRules(ctx context.Context, project *typesv1.Project, group *typesv1.AlertGroup, onStateTransition OnStateTransition) error {
	projectName := project.Spec.Name
	spec := group.Spec

	evaluateRule := func(named *typesv1.AlertGroupSpec_NamedAlertRuleSpec) error {
		status, err := ev.db.AlertGetOrCreate(ctx, projectName, spec.Name, named.Id, func() *typesv1.AlertRuleStatus {
			return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{}}
		})
		if err != nil {
			return fmt.Errorf("getting alert state for %q: %w", named.Id, err)
		}

		status.LastEvaluatedAt = timestamppb.New(ev.timeNow())

		as := &typesv1.AlertRule{
			Meta:   &typesv1.AlertRuleMeta{Id: named.Id},
			Spec:   named.Spec,
			Status: status,
		}
		err = check(ctx, as, ev.db, ev.timeNow(), onStateTransition, status)
		if err != nil {
			errMsg := fmt.Sprintf("evaluating alert: %v", err)
			status.Error = &errMsg
			// Persist error status so user can see it
			if persistErr := ev.db.AlertUpdateState(ctx, projectName, spec.Name, named.Id, status); persistErr != nil {
				return fmt.Errorf("persisting error status for alert %q: %w", named.Id, persistErr)
			}
			return nil
		}

		// Clear any previous error
		status.Error = nil

		err = ev.db.AlertUpdateState(ctx, projectName, spec.Name, named.Id, status)
		if err != nil {
			return fmt.Errorf("persisting alert state for %q: %w", named.Id, err)
		}

		return nil
	}

	var keeplist []string
	for _, named := range spec.Rules {
		keeplist = append(keeplist, named.Id)
		if err := evaluateRule(named); err != nil {
			return err
		}
	}
	if err := ev.db.AlertDeleteStateNotInList(ctx, projectName, spec.Name, keeplist); err != nil {
		return err
	}
	return nil
}

func check(
	ctx context.Context,
	as *typesv1.AlertRule,
	db localstorage.Queryable,
	now time.Time,
	onStateTransition OnStateTransition,
	status *typesv1.AlertRuleStatus,
) error {
	data, _, metrics, err := db.Query(ctx, as.Spec.Expr, nil, 100)
	status.LastEvaluationMetrics = metrics
	if err != nil {
		return fmt.Errorf("evaluating alert rule expression: %v", err)
	}
	freeform, ok := data.Shape.(*typesv1.Data_FreeForm)
	if !ok {
		return fmt.Errorf("invalid query, result is not a table")
	}
	table := freeform.FreeForm
	return apply(ctx, as, table, now, onStateTransition)
}

func apply(ctx context.Context, as *typesv1.AlertRule, table *typesv1.Table, now time.Time, onStateTransition OnStateTransition) error {
	transitionToOk := func(labels *typesv1.Obj) error {
		as.Status.Status = &typesv1.AlertRuleStatus_Ok{Ok: &typesv1.AlertOk{}}
		as.Status.TransitionedAt = timestamppb.New(now)
		return onStateTransition(ctx, as, labels)
	}
	transitionToPending := func(labels *typesv1.Obj) error {
		as.Status.Status = &typesv1.AlertRuleStatus_Pending{Pending: &typesv1.AlertPending{}}
		as.Status.TransitionedAt = timestamppb.New(now)
		return onStateTransition(ctx, as, labels)
	}
	transitionToFiring := func(labels *typesv1.Obj) error {
		as.Status.Status = &typesv1.AlertRuleStatus_Firing{Firing: &typesv1.AlertFiring{}}
		as.Status.TransitionedAt = timestamppb.New(now)
		return onStateTransition(ctx, as, labels)
	}

	onOk := func(labels *typesv1.Obj) error {
		switch as.Status.Status.(type) {
		case *typesv1.AlertRuleStatus_Unknown:
			return transitionToOk(labels)
		case *typesv1.AlertRuleStatus_Ok:
			// we're already ok
			return nil
		case *typesv1.AlertRuleStatus_Pending:
			return transitionToOk(labels)
		case *typesv1.AlertRuleStatus_Firing:
			if as.Spec.KeepFiringFor == nil {
				// we're done
				return transitionToOk(labels)
			}
			firingFor := as.Spec.KeepFiringFor.AsDuration()
			mustBeOkUntil := as.Status.LastFiringAt.AsTime().Add(firingFor)
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
		as.Status.LastFiringAt = timestamppb.New(now) // always record the last firing
		switch as.Status.Status.(type) {
		case *typesv1.AlertRuleStatus_Unknown:
			if as.Spec.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case *typesv1.AlertRuleStatus_Ok:
			if as.Spec.For == nil {
				return transitionToFiring(labels)
			}
			return transitionToPending(labels)
		case *typesv1.AlertRuleStatus_Pending:
			if as.Spec.For == nil {
				return transitionToFiring(labels)
			}
			pendingFor := as.Spec.For.AsDuration()
			firesAt := as.Status.TransitionedAt.AsTime().Add(pendingFor)
			if now.Before(firesAt) {
				// still pending
				return nil
			}
			return transitionToFiring(labels)
		case *typesv1.AlertRuleStatus_Firing:
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
