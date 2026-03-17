# Alert Architecture - Complete Analysis

## Overview

The alert system follows a **separation of concerns** pattern where:
- **Spec** (configuration) is stored in filesystem YAML files
- **Status** (runtime state) is stored separately in a state storage layer
- These are **merged at read time** when serving API responses

## Component Architecture

### 1. AlertScheduler (`internal/localserver/alert_scheduler.go`)

**Responsibility**: Orchestrates alert evaluation across all projects

**Key behaviors**:
- Reconciles every N seconds (discovers new/changed/deleted alert groups)
- Spawns one goroutine per alert group
- Each goroutine evaluates rules at the group's configured interval
- Uses singleflight for debounced reconciliation

**Dependencies**:
```go
type AlertScheduler struct {
    state               projectService    // Query projects/alert groups
    storage             localstorage.Storage  // Persist alert status
    timeNow             func() time.Time  // Injected for testing
    getReconcileTrigger func() <-chan time.Time  // Injected for testing
    getEvalTrigger      func(...) <-chan time.Time  // Injected for testing
    onStateTransition   localalert.OnStateTransition  // Fire alerts
}

type projectService interface {
    ListProject(...)
    GetProject(...)
    ListAlertGroup(...)
    GetAlertGroup(...)
}
```

**Reconciliation flow**:
1. List all projects
2. For each project, list all alert groups
3. Compare discovered groups to running evaluators
4. Stop evaluators for deleted/changed groups
5. Start evaluators for new groups

### 2. Evaluator (`internal/localalert/alert.go`)

**Responsibility**: Evaluates rules within an alert group

**Evaluation flow** (for each rule):
```go
func (ev *Evaluator) EvaluateRules(...) error {
    for each rule:
        // 1. Get current status from storage
        status := ev.db.AlertGetOrCreate(ctx, projectName, groupName, ruleID, ...)

        // 2. Record evaluation timestamp
        status.LastEvaluatedAt = timestamppb.New(ev.timeNow())

        // 3. Evaluate the alert condition
        err := check(ctx, rule, ev.db, ev.timeNow(), onStateTransition, status)

        // 4. Persist updated status
        ev.db.AlertUpdateState(ctx, projectName, groupName, ruleID, status)

    // 5. Clean up deleted rules
    ev.db.AlertDeleteStateNotInList(ctx, projectName, groupName, keeplist)
}
```

### 3. Storage Layer (`pkg/localstorage/localstorage.go`)

**Alertable interface**:
```go
type Alertable interface {
    // Get or lazily create alert status
    AlertGetOrCreate(
        ctx context.Context,
        projectName, groupName, ruleID string,
        create func() *typesv1.AlertRuleStatus,
    ) (*typesv1.AlertRuleStatus, error)

    // Update alert status (called after evaluation)
    AlertUpdateState(
        ctx context.Context,
        projectName, groupName, ruleID string,
        status *typesv1.AlertRuleStatus,
    ) error

    // Delete orphaned alert status (cleanup)
    AlertDeleteStateNotInList(
        ctx context.Context,
        projectName, groupName string,
        keeplist []string,
    ) error
}
```

**Implementations**:
- `DuckDB`: Persists to database
- `mockStorage` (tests): In-memory map

### 4. Project/AlertGroup Discovery (`internal/localproject/`)

**watch.go** (`localstate.DB` implementation):
```go
func (wt *watch) GetAlertGroup(ctx, req) {
    // 1. Get alert group from storage (filesystem)
    storage.getAlertGroup(ctx, wt.alertState, projectName, ptr, groupName, ...)

    // In the callback from storage:
    // 2. Status is already hydrated by storage layer
    out = alertGroup  // Contains spec + status
}
```

**fs.go** (`localGitStorage` - filesystem storage):
```go
func (store *localGitStorage) getAlertGroup(..., alertState, ...) {
    // 1. Parse alert group YAML from filesystem (spec only)
    alertGroups := parseProjectAlertGroups(ctx, store.fs, ...)

    // 2. Hydrate status for each rule
    ag.Status.Rules = make(...)
    for each rule:
        status := alertState.AlertGetOrCreate(ctx, projectName, groupName, ruleID, ...)
        ag.Status.Rules = append(..., status)

    // 3. Return complete alert group (spec + status)
    return onAlertGroup(ag)
}
```

## Data Flow Diagram

```
┌─────────────────────────────────────────────────────────┐
│ AlertScheduler                                          │
│  ┌────────────┐       ┌────────────────┐              │
│  │ Reconcile  │──────>│ Per-Group      │              │
│  │ Loop       │       │ Evaluators     │              │
│  │ (60s)      │       │ (interval)     │              │
│  └────────────┘       └────────┬───────┘              │
└─────────────────────────────────┼────────────────────────┘
                                  │
                        ┌─────────▼──────────┐
                        │ Evaluator          │
                        │ EvaluateRules()    │
                        └─┬──────────────┬───┘
                          │              │
        ┌─────────────────▼─┐         ┌──▼──────────────────┐
        │ projectService    │         │ localstorage        │
        │ (localstate.DB)   │         │ (Alertable)         │
        │                   │         │                     │
        │ GetAlertGroup()   │         │ AlertGetOrCreate()  │
        │   │               │         │ AlertUpdateState()  │
        │   │               │         └──────────────────────┘
        │   ▼               │
        │ localGitStorage   │
        │   │               │
        │   ▼               │
        │ Filesystem YAML   │
        │ (spec only)       │
        │   │               │
        │   ├──────────┐    │
        │   │          ▼    │
        │   │     Hydrate   │────> Uses alertState to get status
        │   │     Status    │
        │   │          │    │
        │   └──────────┘    │
        └───────────────────┘
```

## Key Patterns

### 1. Spec/Status Separation

**Why?**
- Spec (configuration) changes infrequently → stored in git
- Status (runtime state) changes every evaluation → stored in database
- Keeps git history clean (no status churn)

**How?**
- Filesystem YAML contains only spec
- Status fetched from alertState storage
- Merged at read time in storage layer

### 2. Lazy Status Initialization

```go
AlertGetOrCreate(ctx, projectName, groupName, ruleID, func() *typesv1.AlertRuleStatus {
    return &typesv1.AlertRuleStatus{Status: &typesv1.AlertRuleStatus_Unknown{}}
})
```

- Status created on first access
- Default state: Unknown
- Avoids need to pre-create status for all rules

### 3. Status Hydration in Storage Layer

**Not in watch layer**:
```go
// ❌ Bad: watch.go hydrating status
func (wt *watch) GetAlertGroup(...) {
    storage.getAlertGroup(...)  // Get spec
    for each rule:
        status := wt.alertState.AlertGetOrCreate(...)  // Hydrate status
}
```

**In storage layer**:
```go
// ✅ Good: storage layer hydrates status
func (store *localGitStorage) getAlertGroup(..., alertState, ...) {
    ag := parseFromFilesystem()  // Get spec
    for each rule:
        status := alertState.AlertGetOrCreate(...)  // Hydrate status
    return ag  // Complete alert group
}
```

**Why?**
- Storage layer knows the data structure
- Keeps hydration logic close to data source
- watch layer just routes requests

### 4. Constructor Injection for Testing

**Production**:
```go
scheduler := NewAlertScheduler(
    logger,
    state,  // Real DB
    storage,  // Real DuckDB
    time.Now,
    onStateTransition,
    60*time.Second,
)
```

**Testing**:
```go
scheduler := newAlertScheduler(
    logger,
    mockProjectService,  // Test mock
    mockStorage,  // In-memory storage
    func() time.Time { return fixedTime },  // Deterministic time
    func() <-chan time.Time { return testChan },  // Controlled triggers
    func(...) <-chan time.Time { return testChan },
    onStateTransition,
)
```

### 5. Callback-Based Storage Interface

```go
type projectStorage interface {
    getAlertGroup(
        ctx context.Context,
        alertState localstorage.Alertable,
        projectName string,
        ptr *typesv1.ProjectPointer,
        groupName string,
        onAlertGroup func(*typesv1.AlertGroup) error,  // Callback!
    ) error
}
```

**Why callbacks?**
- Storage layer controls data loading
- Consumer extracts what they need via callback
- No need to expose internal types
- Follows existing codebase pattern

## Testing Patterns

### Test Structure (from `alert_scheduler_test.go`)

```go
type tick struct {
    at           time.Duration
    reconcile    bool
    evals        []string  // "project/group"
    addGroups    map[string][]*typesv1.AlertGroup
    removeGroups map[string][]string
    updateGroups map[string][]*typesv1.AlertGroup
}

tests := []struct {
    name  string
    init  schedulerState  // Initial state
    ticks []tick          // State transitions
    check func(t *testing.T, events []event)  // Verify outcomes
}
```

### Event Recording via Hooks

```go
var events []event

scheduler := newAlertScheduler(
    ...,
    withOnReconcileComplete(func() {
        recordEvent("reconcile-complete", ...)
    }),
    withOnEvaluationComplete(func(project, group string) {
        recordEvent("eval-complete", project, group, ...)
    }),
    withOnEvaluatorStarted(func(project, group string) {
        recordEvent("evaluator-started", project, group, ...)
    }),
)

// Later: verify events
require.Equal(t, 2, countEvals(events, "proj1", "group1"))
require.True(t, hasEvent(events, "evaluator-stopped", "proj1", "group2"))
```

### Mock Storage with Alert State

```go
type mockStorage struct {
    mu     sync.Mutex
    alerts map[string]map[string]map[string]*typesv1.AlertRuleStatus
}

func (m *mockStorage) AlertGetOrCreate(...) (*typesv1.AlertRuleStatus, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Lazy init
    if status, ok := m.alerts[projectName][groupName][ruleID]; ok {
        return status, nil
    }

    status := create()
    m.alerts[projectName][groupName][ruleID] = status
    return status, nil
}
```

## Implications for Dashboard Implementation

### Pattern to Follow

Dashboards should **NOT** follow the spec/status separation pattern because:
1. Dashboards don't have runtime status (no evaluation loop)
2. Dashboard metadata (management, codegen markers) is **part of the spec**
3. All dashboard data should be in one place (YAML file)

### Correct Pattern for Dashboards

```go
// Dashboard YAML contains EVERYTHING
# managed-by: humanlog
kind: Dashboard
metadata:
  name: my-dashboard
spec:
  display:
    name: My Dashboard
  # ... dashboard config

// No separate status storage needed
```

### Discovery Pattern (Similar to Alerts)

```go
func (store *localGitStorage) getDashboard(...) {
    // 1. Read YAML from filesystem
    dashboard := parseProjectDashboard(ctx, store.fs, ...)

    // 2. Detect management markers (in the parsing step)
    managedBy, _ := extractManagedByMarker(yamlData)
    codegenMarkers := detectCodegenMarkers(yamlData)

    // 3. Build complete dashboard proto
    dashboard.Spec.Management = &typesv1.DashboardManagement{
        Origin:         origin,
        CodegenMarkers: codegenMarkers,
    }

    // 4. Return via callback (same pattern as alerts)
    return onDashboard(dashboard)
}
```

### Testing Pattern (Same as Alerts)

```go
type transition struct {
    name            string
    at              time.Duration
    operation       func(context.Context, *testing.T, localstate.DB) error
    expectFile      *fileExpectation
    expectDashboard *dashboardExpectation
}

tests := []struct {
    name        string
    initFS      fsState
    initProject *typesv1.ProjectsConfig_Project
    transitions []transition
}
```

## Key Takeaways

1. **Alert Status**: Separate storage layer (DuckDB), hydrated at read time
2. **Dashboard Metadata**: Stored in YAML comments, parsed at read time
3. **Storage Interface**: Callback-based, follows existing pattern
4. **Testing**: Declarative state machines, externalized non-determinism
5. **Constructor Injection**: All dependencies via `newXxx()`, no setters

This architecture enables:
- Clean git history (no runtime state in YAML)
- Testable components (inject mocks)
- Flexible storage backends (filesystem, git, database)
- GitOps workflows (everything in version control)
