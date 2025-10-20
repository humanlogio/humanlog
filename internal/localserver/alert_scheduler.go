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
	"golang.org/x/sync/singleflight"
)

// AlertScheduler manages per-alert-group goroutines that evaluate rules
// at their specified intervals, plus a reconciliation loop that discovers
// new/changed/deleted alert groups.
//
// Error handling philosophy:
// - System errors (corrupted DB, unrecoverable state) cause Start() to return error
// - User errors (bad alert config, temporary API failures) are logged but don't kill the service
// - Reconciliation is debounced via singleflight to handle concurrent triggers
type AlertScheduler struct {
	ll                  *slog.Logger
	state               projectService
	storage             localstorage.Storage
	timeNow             func() time.Time
	getReconcileTrigger func() <-chan time.Time
	getEvalTrigger      func(projectName, groupName string, interval time.Duration) <-chan time.Time
	onStateTransition   localalert.OnStateTransition

	// Hooks for observability and user feedback
	onReconcileComplete  func()
	onReconcileError     func(err error)
	onEvaluationComplete func(projectName, groupName string)
	onEvaluationError    func(projectName, groupName string, err error, errorCount int)
	onEvaluatorWillStart func(projectName, groupName string)
	onEvaluatorStarted   func(projectName, groupName string)
	onEvaluatorStopped   func(projectName, groupName string)

	reconcileSF singleflight.Group

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

func withOnReconcileError(fn func(err error)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onReconcileError = fn
	}
}

func withOnEvaluationComplete(fn func(projectName, groupName string)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onEvaluationComplete = fn
	}
}

func withOnEvaluationError(fn func(projectName, groupName string, err error, errorCount int)) alertSchedulerOption {
	return func(s *AlertScheduler) {
		s.onEvaluationError = fn
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
	onStateTransition localalert.OnStateTransition,
	opts ...alertSchedulerOption,
) *AlertScheduler {
	s := &AlertScheduler{
		ll:                  ll,
		state:               state,
		storage:             storage,
		timeNow:             timeNow,
		getReconcileTrigger: getReconcileTrigger,
		getEvalTrigger:      getEvalTrigger,
		onStateTransition:   onStateTransition,
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
	onStateTransition localalert.OnStateTransition,
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
		onStateTransition,
		opts...,
	)
}

// Start begins the scheduler. Blocks until context is cancelled.
// Returns error only for system-level failures that should kill the background service.
// User configuration errors are logged but don't stop the scheduler.
func (s *AlertScheduler) Start(ctx context.Context, state projectService) error {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer close(s.done)

	s.ll.InfoContext(ctx, "alert scheduler starting")

	// Initial reconciliation
	if err := s.doReconcile(ctx); err != nil {
		s.ll.ErrorContext(ctx, "initial reconciliation failed", slog.Any("err", err))
	}

	for {
		reconcileTrigger := s.getReconcileTrigger()
		select {
		case <-ctx.Done():
			// Create detached context for shutdown: inherit values but not cancellation
			shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer shutdownCancel()
			s.stopAll(shutdownCtx)
			s.ll.InfoContext(ctx, "alert scheduler stopped")
			return nil
		case <-reconcileTrigger:
			if err := s.doReconcile(ctx); err != nil {
				s.ll.ErrorContext(ctx, "reconciliation failed", slog.Any("err", err))
			}
		}
	}
}

// TriggerReconcile forces an immediate reconciliation (used when user calls SyncProject).
// Concurrent calls are deduplicated via singleflight.
func (s *AlertScheduler) TriggerReconcile(ctx context.Context) error {
	return s.doReconcile(ctx)
}

// doReconcile wraps reconcile with singleflight for deduplication
func (s *AlertScheduler) doReconcile(ctx context.Context) error {
	_, err, _ := s.reconcileSF.Do("reconcile", func() (interface{}, error) {
		err := s.reconcile(ctx)
		if err != nil && s.onReconcileError != nil {
			s.onReconcileError(err)
		}
		return nil, err
	})
	return err
}

func (s *AlertScheduler) reconcile(ctx context.Context) error {
	s.ll.DebugContext(ctx, "reconciling alert groups")

	discovered := make(map[string]*alertGroupConfig)

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

	for key, eval := range s.evaluators {
		cfg, exists := discovered[key]
		if exists && cfg.interval == eval.interval {
			continue
		}

		ll := s.ll.With(
			slog.String("project", eval.projectName),
			slog.String("group", eval.groupName),
		)

		reason := "interval changed"
		if !exists {
			reason = "deleted"
		}

		ll.InfoContext(ctx, "stopping alert group evaluator", slog.String("reason", reason))
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

		errorCount := 0
		for {
			trigger := s.getEvalTrigger(eval.projectName, eval.groupName, eval.interval)
			select {
			case <-ctx.Done():
				return
			case <-trigger:
				err := s.evaluateGroup(ctx, eval)

				if err != nil {
					errorCount++
					ll.ErrorContext(ctx, "failed to evaluate alert group",
						slog.Any("err", err),
						slog.Int("error_count", errorCount),
					)

					if s.onEvaluationError != nil {
						s.onEvaluationError(eval.projectName, eval.groupName, err, errorCount)
					}
				} else {
					errorCount = 0
				}

				if s.onEvaluationComplete != nil {
					s.onEvaluationComplete(eval.projectName, eval.groupName)
				}
			}
		}
	}()

	return eval
}

func (s *AlertScheduler) evaluateGroup(ctx context.Context, eval *alertGroupEvaluator) error {
	projectResp, err := s.state.GetProject(ctx, &projectv1.GetProjectRequest{Name: eval.projectName})
	if err != nil {
		return fmt.Errorf("getting project %q: %v", eval.projectName, err)
	}

	alertGroupResp, err := s.state.GetAlertGroup(ctx, &alertv1.GetAlertGroupRequest{
		ProjectName: eval.projectName,
		Name:        eval.groupName,
	})
	if err != nil {
		return fmt.Errorf("getting alert group %q: %v", eval.groupName, err)
	}

	evaluator := localalert.NewEvaluator(s.storage, s.timeNow)
	return evaluator.EvaluateRules(ctx, projectResp.Project, alertGroupResp.AlertGroup, s.onStateTransition)
}

func (s *AlertScheduler) stopAll(ctx context.Context) {
	s.mu.Lock()
	evals := make([]*alertGroupEvaluator, 0, len(s.evaluators))
	for _, eval := range s.evaluators {
		evals = append(evals, eval)
		eval.cancel()
	}
	s.evaluators = make(map[string]*alertGroupEvaluator)
	s.mu.Unlock()

	for _, eval := range evals {
		select {
		case <-eval.done:
		case <-ctx.Done():
			return
		}
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
