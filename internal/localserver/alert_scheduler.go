package localserver

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	projectv1 "github.com/humanlogio/api/go/svc/project/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/humanlogio/humanlog/internal/localalert"
	"github.com/humanlogio/humanlog/internal/pkg/iterapi"
	"github.com/humanlogio/humanlog/pkg/localstorage"
)

// AlertScheduler manages per-alert-group goroutines that evaluate rules
// at their specified intervals, plus a reconciliation loop that discovers
// new/changed/deleted alert groups.
type AlertScheduler struct {
	ll                  *slog.Logger
	state               projectService
	storage             localstorage.Storage
	timeNow             func() time.Time
	getReconcileTrigger func() <-chan time.Time
	getEvalTrigger      func(projectName, groupName string, interval time.Duration) <-chan time.Time
	notifyAlert         localalert.CheckFunc

	// Test hooks for happens-before relationships
	onReconcileComplete  func()
	onEvaluationComplete func(projectName, groupName string)
	onEvaluatorWillStart func(projectName, groupName string)
	onEvaluatorStarted   func(projectName, groupName string)
	onEvaluatorStopped   func(projectName, groupName string)

	mu         sync.Mutex
	evaluators map[string]*alertGroupEvaluator // key: "projectName/groupName"
	cancel     context.CancelFunc
	done       chan struct{}
}

type projectService interface {
	ListProject(context.Context, *projectv1.ListProjectRequest) (*projectv1.ListProjectResponse, error)
	GetProject(context.Context, *projectv1.GetProjectRequest) (*projectv1.GetProjectResponse, error)
	ListAlertGroup(context.Context, *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error)
	GetAlertGroup(context.Context, *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error)
}

type alertGroupEvaluator struct {
	projectName string
	groupName   string
	interval    time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

type alertSchedulerOption func(*AlertScheduler)

func withOnReconcileComplete(fn func()) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onReconcileComplete = fn
	}
}

func withOnEvaluationComplete(fn func(projectName, groupName string)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onEvaluationComplete = fn
	}
}

func withOnEvaluatorWillStart(fn func(projectName, groupName string)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onEvaluatorWillStart = fn
	}
}

func withOnEvaluatorStarted(fn func(projectName, groupName string)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onEvaluatorStarted = fn
	}
}

func withOnEvaluatorStopped(fn func(projectName, groupName string)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onEvaluatorStopped = fn
	}
}

// newAlertScheduler creates a scheduler with explicit trigger functions.
// This is the private constructor used by tests.
func newAlertScheduler(
	ll *slog.Logger,
	state projectService,
	storage localstorage.Storage,
	timeNow func() time.Time,
	getReconcileTrigger func() <-chan time.Time,
	getEvalTrigger func(projectName, groupName string, interval time.Duration) <-chan time.Time,
	notifyAlert localalert.CheckFunc,
	opts ...alertSchedulerOption,
) *AlertScheduler {
	s := &AlertScheduler{
		ll:                  ll,
		state:               state,
		storage:             storage,
		timeNow:             timeNow,
		getReconcileTrigger: getReconcileTrigger,
		getEvalTrigger:      getEvalTrigger,
		notifyAlert:         notifyAlert,
		evaluators:          make(map[string]*alertGroupEvaluator),
		done:                make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewAlertScheduler creates a scheduler that:
// - Reconciles alert groups every `reconcileEvery` (e.g., 60s)
// - Launches per-group goroutines that evaluate rules at AlertGroup.interval
func NewAlertScheduler(
	ll *slog.Logger,
	state projectService,
	storage localstorage.Storage,
	timeNow func() time.Time,
	notifyAlert localalert.CheckFunc,
	reconcileEvery time.Duration,
	opts ...alertSchedulerOption,
) *AlertScheduler {
	return newAlertScheduler(
		ll,
		state,
		storage,
		timeNow,
		func() <-chan time.Time { return time.After(reconcileEvery) },
		func(projectName, groupName string, interval time.Duration) <-chan time.Time {
			return time.After(interval)
		},
		notifyAlert,
		opts...,
	)
}

// Start begins the scheduler. Blocks until context is cancelled.
func (s *AlertScheduler) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer close(s.done)

	s.ll.InfoContext(ctx, "alert scheduler starting")

	// Initial reconciliation
	if err := s.reconcile(ctx); err != nil {
		s.ll.ErrorContext(ctx, "initial reconciliation failed", slog.Any("err", err))
	}

	for {
		reconcileTrigger := s.getReconcileTrigger()
		select {
		case <-ctx.Done():
			s.stopAll()
			s.ll.InfoContext(ctx, "alert scheduler stopped")
			return nil
		case <-reconcileTrigger:
			if err := s.reconcile(ctx); err != nil {
				s.ll.ErrorContext(ctx, "reconciliation failed", slog.Any("err", err))
			}
		}
	}
}

// TriggerReconcile forces an immediate reconciliation (used when user calls SyncProject)
func (s *AlertScheduler) TriggerReconcile(ctx context.Context) error {
	return s.reconcile(ctx)
}

// reconcile discovers all alert groups and ensures each has a running evaluator
func (s *AlertScheduler) reconcile(ctx context.Context) error {
	s.ll.DebugContext(ctx, "reconciling alert groups")

	// Discover all projects and their alert groups
	discovered := make(map[string]*alertGroupConfig) // key: "projectName/groupName"

	projectIter := s.iteratorForProject(ctx)
	for projectIter.Next() {
		project := projectIter.Current()

		alertGroupIter := s.iteratorForAlertGroup(ctx, project.Spec.Name)
		for alertGroupIter.Next() {
			alertGroup := alertGroupIter.Current()
			key := alertGroupKey(project.Spec.Name, alertGroup.Spec.Name)

			interval := alertGroup.Spec.Interval.AsDuration()
			if interval == 0 {
				interval = 60 * time.Second // default
			}

			discovered[key] = &alertGroupConfig{
				projectName: project.Spec.Name,
				groupName:   alertGroup.Spec.Name,
				interval:    interval,
			}
		}
		if err := alertGroupIter.Err(); err != nil {
			return fmt.Errorf("iterating alert groups for project %q: %v", project.Spec.Name, err)
		}
	}
	if err := projectIter.Err(); err != nil {
		return fmt.Errorf("iterating projects: %v", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop evaluators for groups that no longer exist or have changed intervals
	for key, eval := range s.evaluators {
		cfg, exists := discovered[key]
		if exists && cfg.interval == eval.interval {
			continue
		}

		ll := s.ll.With(
			slog.String("project", eval.projectName),
			slog.String("group", eval.groupName),
		)

		ll.InfoContext(ctx, "stopping alert group evaluator",
			slog.String("reason", func() string {
				if !exists {
					return "deleted"
				}
				return "interval changed"
			}()),
		)
		eval.cancel()
		select {
		case <-eval.done:
		case <-ctx.Done():
			ll.WarnContext(ctx, "context cancelled while waiting for evaluator to stop")
		}
		delete(s.evaluators, key)
		if s.onEvaluatorStopped != nil {
			s.onEvaluatorStopped(eval.projectName, eval.groupName)
		}
	}

	// Start evaluators for new groups
	for key, cfg := range discovered {
		if _, exists := s.evaluators[key]; exists {
			continue
		}

		ll := s.ll.With(
			slog.String("project", cfg.projectName),
			slog.String("group", cfg.groupName),
		)

		ll.InfoContext(ctx, "starting alert group evaluator",
			slog.Duration("interval", cfg.interval),
		)
		eval := s.startEvaluator(ctx, cfg)
		s.evaluators[key] = eval
		if s.onEvaluatorStarted != nil {
			s.onEvaluatorStarted(cfg.projectName, cfg.groupName)
		}
	}

	if s.onReconcileComplete != nil {
		s.onReconcileComplete()
	}

	return nil
}

type alertGroupConfig struct {
	projectName string
	groupName   string
	interval    time.Duration
}

func alertGroupKey(projectName, groupName string) string {
	return projectName + "/" + groupName
}

func (s *AlertScheduler) startEvaluator(parentCtx context.Context, cfg *alertGroupConfig) *alertGroupEvaluator {
	ctx, cancel := context.WithCancel(parentCtx)
	eval := &alertGroupEvaluator{
		projectName: cfg.projectName,
		groupName:   cfg.groupName,
		interval:    cfg.interval,
		cancel:      cancel,
		done:        make(chan struct{}),
	}

	if s.onEvaluatorWillStart != nil {
		s.onEvaluatorWillStart(cfg.projectName, cfg.groupName)
	}

	go func() {
		defer close(eval.done)

		ll := s.ll.With(
			slog.String("project", eval.projectName),
			slog.String("group", eval.groupName),
		)

		for {
			trigger := s.getEvalTrigger(eval.projectName, eval.groupName, eval.interval)
			select {
			case <-ctx.Done():
				return
			case <-trigger:
				if err := s.evaluateGroup(ctx, eval.projectName, eval.groupName); err != nil {
					ll.ErrorContext(ctx, "failed to evaluate alert group", slog.Any("err", err))
				}
				if s.onEvaluationComplete != nil {
					s.onEvaluationComplete(eval.projectName, eval.groupName)
				}
			}
		}
	}()

	return eval
}

func (s *AlertScheduler) evaluateGroup(ctx context.Context, projectName, groupName string) error {
	// Fetch the project
	projectResp, err := s.state.GetProject(ctx, &projectv1.GetProjectRequest{Name: projectName})
	if err != nil {
		return fmt.Errorf("getting project %q: %v", projectName, err)
	}

	// Fetch the alert group
	alertGroupResp, err := s.state.GetAlertGroup(ctx, &alertv1.GetAlertGroupRequest{
		ProjectName: projectName,
		Name:        groupName,
	})
	if err != nil {
		return fmt.Errorf("getting alert group %q: %v", groupName, err)
	}

	// Evaluate rules
	evaluator := localalert.NewEvaluator(s.storage, s.timeNow)
	return evaluator.EvaluateRules(ctx, projectResp.Project, alertGroupResp.AlertGroup, s.notifyAlert)
}

func (s *AlertScheduler) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, eval := range s.evaluators {
		ll := s.ll.With(
			slog.String("project", eval.projectName),
			slog.String("group", eval.groupName),
		)

		eval.cancel()
		select {
		case <-eval.done:
		case <-time.After(5 * time.Second):
			ll.Warn("timeout waiting for evaluator to stop")
		}
		delete(s.evaluators, key)
	}
}

func (s *AlertScheduler) iteratorForProject(ctx context.Context) *iterapi.Iter[*typesv1.Project] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*typesv1.Project, *typesv1.Cursor, error) {
		out, err := s.state.ListProject(ctx, &projectv1.ListProjectRequest{Cursor: cursor, Limit: limit})
		if err != nil {
			return nil, nil, err
		}
		var items []*typesv1.Project
		for _, el := range out.Items {
			items = append(items, el.Project)
		}
		return items, out.Next, nil
	})
}

func (s *AlertScheduler) iteratorForAlertGroup(ctx context.Context, projectName string) *iterapi.Iter[*typesv1.AlertGroup] {
	return iterapi.New(ctx, 100, func(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*typesv1.AlertGroup, *typesv1.Cursor, error) {
		out, err := s.state.ListAlertGroup(ctx, &alertv1.ListAlertGroupRequest{ProjectName: projectName, Cursor: cursor, Limit: limit})
		if err != nil {
			return nil, nil, err
		}
		var items []*typesv1.AlertGroup
		for _, el := range out.Items {
			items = append(items, el.AlertGroup)
		}
		return items, out.Next, nil
	})
}
