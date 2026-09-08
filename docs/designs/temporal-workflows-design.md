# Temporal Workflows Design

## Status

- Draft architecture, not scheduled for a release.
- The component model, runtime placement, and determinism boundary are the
  load-bearing decisions and are recommended as written.
- The package boundary decision and the metrics bridge are not
  implementation-ready until the Phase 0 spikes pass.

## Summary

GoForj should support durable workflow orchestration as a first-class
`workflows` component backed by Temporal.

The design has four durable boundaries:

1. `workflows` is a component in the canonical catalog, gated and rendered like
   Background Jobs, with a `workflows` resource whose only real driver is
   `temporal`.
2. Generated code uses `go.temporal.io/sdk` types directly. GoForj does not
   invent a portable workflow abstraction.
3. A `workflows.Runtime` joins the existing `RuntimeHost` beside HTTP,
   Scheduler, and Jobs. It is a fourth runtime, not a new process model.
4. Generated scaffolding makes the determinism boundary structural: workflows
   receive no dependencies, activities receive them through Wire as usual.

Temporal already ships in the developer-service catalog as a Compose profile
with no consumer. This design connects that profile to the App.

## Problem

A generated App today has no path to durable orchestration.

Background Jobs covers single-step, fire-and-forget work. The Scheduler covers
in-process recurrence. Neither covers work that must survive a deploy, span
hours or days, hold state between steps, compensate on failure, or wait on an
external signal.

A developer who wants that today must:

- start `temporalio/temporal` by hand or discover the undocumented `temporal`
  Compose profile
- add `go.temporal.io/sdk` to the App module
- hand-write client dialing, worker construction, registration, and shutdown
- decide alone where workflow code lives and which dependencies are safe to
  touch inside it
- reconcile all of that with `forj render` overwriting framework-owned files

The last two are the dangerous ones. Nothing in the framework signals that
injecting a repository into a workflow function silently corrupts replay, and
nothing tells the developer which files survive a rerender.

### Current Temporal surface

`internal/devservices/catalog.go` registers `KeyTemporal` at order 150, and
`templates/containers/developer-services/temporal.yml.tmpl` runs
`server start-dev` with gRPC on `7233` and the Web UI on `8233`, behind a
cluster health check.

That entry declares no `Providers` and no `DefaultFor`. It is an orphan
developer tool: the profile can be enabled, but no resource plans against it, no
environment references it, and no generated code consumes it.

## Goals

1. Make durable workflows a selectable component with the same render, gating,
   environment, and transition guarantees as every other primitive.
2. Reuse the existing runtime host, lifecycle phases, and timeout policy rather
   than introducing a parallel process model.
3. Make the determinism boundary hard to cross by accident.
4. Provide `make:workflow` and `make:activity` generators that update code and
   Wire registration together, matching `make:job`.
5. Give the local developer a working Temporal server, a reachable Web UI, and
   a first passing workflow test without manual setup.
6. Give operators a Lighthouse surface and metrics coverage consistent with
   Jobs and Scheduler.
7. Preserve a truthful disabled contract: a workflows-disabled render contains
   no workflow package, environment, command, module, or advertisement.

## Non-Goals

1. A portable workflow abstraction over multiple orchestration engines.
2. Replacing Background Jobs or the Scheduler.
3. Making Temporal a `goforj/queue` driver.
4. Managing production Temporal clusters, namespaces, or Temporal Cloud
   provisioning beyond configuration.
5. Generated workflow versioning policy. `workflow.GetVersion` is documented,
   not automated.
6. Workflow authoring in any language other than Go.
7. Shipping this as a third-party extension. The extension model in
   [forj-extension-design.md](forj-extension-design.md) is not released, and its
   non-goals exclude replacing first-party components with extensions.

## Why Workflows Is Not A Queue Driver

The cheapest possible implementation is a `temporal` driver inside
`goforj/queue`. Every existing job would then execute on Temporal with no new
component, no new runtime, and no new generators.

Reject it.

A queue driver can only expose Temporal's activity layer. Orchestration,
durable timers, signals, queries, child workflows, and compensation have no
representation in a `Dispatch`/`HandleTask` interface. The result is Temporal's
full operational cost — a server, a namespace, a UI, a retention policy — in
exchange for a slower Redis.

It also damages the queue abstraction. `queue.Job` policy is per dispatch:
timeout, retry, backoff, uniqueness. Workflow policy is per execution and per
activity, and is expressed in workflow code. Forcing one into the other makes
both worse.

The same argument rejects a portable `workflows` abstraction with Temporal as
one driver among several. `workflow.Context` is not `context.Context`,
determinism is not an implementation detail, and replay has no analogue in the
other candidates. Database is the precedent worth following: the component is
first-class, and the generated code uses GORM directly without pretending the
ORM is swappable.

If Cadence or another engine ever justifies the work, add a driver then, with a
real second implementation to test the abstraction against.

## Component Model

Add `ComponentWorkflows` with key `workflows` and label `Workflows`.

| Property | Value | Reason |
| --- | --- | --- |
| Key | `workflows` | Capability-first, not vendor-first. The driver names the vendor. |
| Label | `Workflows` | Matches `Background Jobs` phrasing: the capability, not the transport. |
| Description | `Run durable, multi-step workflows` | Distinguishes it from Background Jobs in the wizard. |
| `DefaultSelected` | `false` | Temporal is a real operational dependency. Opt in. |
| `Requires` | none | Workflows must be selectable without Jobs, Database, or Web API. |
| `Parent` | none | Not a child capability. |
| `ExclusiveGroup` | none | |

`DefaultSelected: false` is deliberate and is the one place this component
differs from Cache, Events, Storage, and Jobs. Those primitives all have a local
in-process default driver. Workflows has no meaningful in-process
implementation, so selecting it by default would add a required container to
every new project.

`Components.HasRuntime()` must include `Workflows`
(`project/config.go:389`). A workflows-only App is runtime-capable, so its bare
binary must enter the `run` host rather than printing CLI help.

## Resource And Driver Model

Add `ResourceWorkflows` with environment prefix `WORKFLOWS`.

| Driver | Service | Locally provisionable | Notes |
| --- | --- | --- | --- |
| `temporal` | `ServiceWorkflowsTemporal` | yes | Default and only real implementation. |
| `null` | none | n/a | Registers nothing, starts no worker, fails dispatch loudly. For tests, CI, and Apps that render the package but do not run it. |

`DefaultDriver` is `temporal`. `DefaultSupportedDrivers` is
`{"temporal", "null"}`.

The `null` driver is what keeps the component honest in test and render
matrices. It lets a Workflows-enabled App compile, boot, and run its
non-workflow test suite with no server reachable, which the integration and
smoke workflows in
[rendering-and-smoke-workflow.md](../context/rendering-and-smoke-workflow.md)
depend on. It must fail explicitly on any attempt to start a workflow rather
than silently discarding it — this is not `queue`'s `null`, which is legitimately
a black hole for fire-and-forget work.

Driver environment placeholders on `temporal`:

| Key | Example | Description |
| --- | --- | --- |
| `WORKFLOWS_ADDRESS` | `127.0.0.1:7233` | Temporal frontend gRPC address |
| `WORKFLOWS_NAMESPACE` | `default` | Temporal namespace |
| `WORKFLOWS_TASK_QUEUE` | `<app>-workflows` | Default task queue for this App |
| `WORKFLOWS_TLS_ENABLED` | `false` | Required for Temporal Cloud |
| `WORKFLOWS_TLS_CERT_PATH` | | Client certificate for mTLS |
| `WORKFLOWS_TLS_KEY_PATH` | | Client key for mTLS |
| `WORKFLOWS_API_KEY` | | Temporal Cloud API key when mTLS is unused |

Named resources follow the existing pattern where a second namespace or task
queue is genuinely a separate resource, and inherit the root driver
(`NamedDriversInheritRoot: true`).

## Local Development Service

Change the existing `KeyTemporal` entry in `internal/devservices/catalog.go:164`:

```go
{
	Key:        KeyTemporal,
	Label:      "Temporal",
	Profile:    "temporal",
	Providers:  []project.ServiceKey{project.ServiceWorkflowsTemporal},
	DefaultFor: []project.ComponentKey{project.ComponentWorkflows},
	Template:   "containers/developer-services/temporal.yml.tmpl",
	Order:      150,
},
```

That single edit is the entire local provisioning story. `Providers` makes the
service plan resolve the `temporal` driver onto this profile, and `DefaultFor`
enables the profile automatically when the component is selected. `forj dev`
startup, `forj dev status`, Compose profile reconciliation, and profile-aware
test rendering all follow from the existing catalog contract described in
[docker-compose-developer-service-catalog-design.md](completed/docker-compose-developer-service-catalog-design.md).

The Compose template itself needs no functional change. Two additions are worth
making while the file is open:

- a named volume so workflow history survives `forj dev` restarts, since the
  dev server is otherwise in-memory and loses every execution on restart
- `TEMPORAL_UI_PORT` surfaced through the resource registry so the Web UI is a
  discoverable link rather than a remembered port

The in-memory default is the right choice for the first release: a fresh
namespace per `forj dev` session sidesteps the non-determinism panic that
follows from editing a workflow with executions in flight. Persistence should be
opt-in through `WORKFLOWS_DEV_PERSIST=true`.

## Generated Package Layout

`internal/workflows` is framework-owned and mirrors `internal/jobs` file for
file. The parallel is deliberate: a developer who has read the Jobs package
should be able to predict this one.

| Jobs | Workflows | Role |
| --- | --- | --- |
| `internal/queues/manager_gen.go` | `internal/workflows/client_gen.go` | Generated by `internal/generate/workflows.go`. Environment to `client.Options`, root and named clients. |
| `internal/jobs/worker.go` | `internal/workflows/worker.go` | Owns one `worker.Worker` per task queue. Start, drain, stop. |
| `internal/jobs/runtime.go` | `internal/workflows/runtime.go` | `runtime.Runtime` implementation hosted by `RunCmd`. |
| `internal/jobs/worker_cmd.go` | `internal/workflows/worker_cmd.go` | `workflow:work` standalone process command. |
| `internal/jobs/lighthouse.go` | `internal/workflows/lighthouse.go` | Operator surface and command registration. |
| `internal/jobs/example_hello_job.go` | `internal/workflows/example_greeting_workflow.go` | Rendered example with a passing test. |
| `app/wire/inject_jobs.go` | `app/wire/inject_workflows.go` | Framework-owned Wire set. |
| `app/wire/inject_jobs_app.go` | `app/wire/inject_workflows_app.go` | App-owned registration. Render-once. |

App-owned registration mirrors `registerJobHandlers` exactly:

```go
// registerWorkflows binds every application workflow and activity to the worker.
func registerWorkflows(
	workers *workflows.Registry,
	greetingWorkflow *workflows.ExampleGreetingWorkflow,
	greetingActivities *workflows.ExampleGreetingActivities,
) *workflowRegistration {
	workers.RegisterWorkflow(workflows.ExampleGreetingWorkflowName, greetingWorkflow.Execute)
	workers.RegisterActivities(greetingActivities)
	return &workflowRegistration{}
}
```

The registration token is a Wire prerequisite of App construction, so
registration cannot be skipped or ordered after worker start. This is the same
mechanism that guarantees job handler registration today.

## Runtime And Lifecycle Model

### Runtime host participation

```
http.Runtime      ─┐
schedules.Runtime ─┼─→ RuntimeHost ─→ ./bin/app  (or `run`)
jobs.Runtime      ─┤
workflows.Runtime ─┘   NEW — RuntimeIdentity{Name: "workflows", Label: "workflows"}
```

`workflows.Runtime` implements `Identity()` and `Run(ctx)` and gains a
`RunWithConfig(ctx, RuntimeConfig)` for the metrics-endpoint suppression that
`RunCmd` applies to every hosted runtime. `templates/internal/cmd/run_cmd.go.tmpl`
gains a fourth conditional block identical in shape to the Jobs block.

Failure semantics come free from `RuntimeHost`: a worker that cannot reach
Temporal fails, cancels its siblings, and takes the process down with a named
error. That is correct. A worker silently not polling is the failure mode to
avoid.

### Client and worker construction

The two roles must be separated, and they resolve the lazy-initialization
tension differently.

**Client** — used by HTTP handlers, jobs, schedules, and CLI commands to start,
signal, and query workflows. Construction must not dial, per
[cli-lazy-infrastructure-initialization-design.md](cli-lazy-infrastructure-initialization-design.md).
`forj make:controller` must not fail because Temporal is down, and an App that
starts workflows from one endpoint must not refuse to boot when the cluster is
briefly unreachable. The generated client is a lazily-dialed handle that
connects on first use and fails loudly at that point.

**Worker** — the process that executes workflows and activities. It connects
during `Startup` and fails fast. A worker that cannot reach Temporal is not
degraded, it is dead, and pretending otherwise produces a healthy-looking
process that executes nothing. This matches `jobs.Runtime`.

The split means a Web API App and a worker App can share one generated package
with different failure postures, which is what makes the multi-App deployment
topology in [app-structure.md](../context/app-structure.md) work.

### Shutdown ordering and drain budget

Registration order is client first, worker second. The existing lifecycle
semantics — reverse registration order within each phase — then produce the
correct sequence with no special casing:

```
BeforeShutdown : worker stops polling; no new workflow or activity tasks accepted
Shutdown       : worker.Stop() drains in-flight activities, bounded by
                 Timeouts().WorkflowShutdownTimeout()
                 client.Close() releases the connection
AfterShutdown  : final lifecycle log
```

`worker.Options.WorkerStopTimeout` must be set from the same resolved timeout so
the SDK's internal budget and the App's budget cannot disagree.

Three properties matter here and should be covered by tests:

- Stopping polling before draining is what makes a rolling deploy safe. A worker
  that drains while still accepting tasks never finishes draining.
- Workflow tasks are cheap to abandon; the server reschedules them on another
  worker within seconds. Activity tasks are the expensive ones, and are what the
  drain budget is actually protecting.
- An activity that outlives the budget is killed regardless. Long activities
  must heartbeat with `activity.RecordHeartbeat` to be resumable, and the
  generated activity template should include the call commented with that
  reason.

### Timeout policy

Extend `runtime.Timeouts` (`templates/internal/runtime/timeouts.go.tmpl`) with
`WorkflowShutdownTimeout()`, resolved once from `WORKFLOWS_SHUTDOWN_TIMEOUT` and
falling back to `ShutdownTimeout()`, matching `QueueShutdownTimeout()`.

The default must be longer than the queue's `10s`. Temporal activities are
routinely minute-scale, and a 10-second budget converts every ordinary deploy
into a wave of activity timeouts and retries. Default to `60s`.

Note that `APP_SHUTDOWN_TIMEOUT` defaults to `30s`, so a `60s` workflow budget
exceeds the app-level default. Either raise the app default for
Workflows-enabled renders or document that operators must. The fallback chain
must not silently clamp the workflow budget to a shorter app budget without
saying so in a log line.

### `forj dev` restart behavior

This is the strongest DX story in the design and it costs nothing to obtain.

The `forj dev` watcher rebuilds and restarts the App binary on every `.go`
change. For queue jobs that is disruptive: in-flight work is lost or retried
from the beginning. For workflows it is nearly free, because workflow state
lives on the server. A restarted worker re-polls, replays history, and resumes
exactly where it stopped. The developer edits an activity, saves, and watches a
workflow that was paused mid-execution continue with the new code.

Two edges to document rather than solve:

- Editing a workflow function while one of its executions is in flight causes a
  non-determinism panic on replay. The in-memory dev server default makes this
  rare, and `WORKFLOWS_DEV_PERSIST=false` is the escape hatch.
- The sticky execution cache is lost on restart, so the first task after a
  restart pays a replay cost proportional to history length. This is normal and
  should not be mistaken for a bug during Lighthouse latency inspection.

## Determinism Boundary

This is where GoForj's conventions actively fight Temporal, and the place where
generated shape does the most good.

Every generated GoForj job takes dependencies in its constructor and uses them
freely in `HandleTask`. A workflow written that way is broken — it will replay
non-deterministically, and the failure surfaces later, in production, as
corrupted state rather than as a compile error.

The scaffolding must therefore make the split structural, not advisory.

### Generated shape

`forj make:workflow Payment` emits two files in one package.

```go
// internal/workflows/invoice/payment_workflow.go
// Workflow code is replayed. It must be deterministic and must not hold
// dependencies. Do all I/O through activities.
type PaymentWorkflow struct{}

func NewPaymentWorkflow() *PaymentWorkflow { return &PaymentWorkflow{} }

func (w *PaymentWorkflow) Execute(ctx workflow.Context, in PaymentInput) (PaymentResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 5},
	})

	var charged ChargeResult
	if err := workflow.ExecuteActivity(ctx, ChargeCardActivityName, in).Get(ctx, &charged); err != nil {
		return PaymentResult{}, err
	}
	return PaymentResult{ChargeID: charged.ID}, nil
}
```

```go
// internal/workflows/invoice/payment_activities.go
// Activity code runs once, may perform I/O, and receives App dependencies
// through Wire exactly like any other GoForj type.
type PaymentActivities struct {
	invoices *repositories.InvoiceRepository
	mailer   *mail.Mailer
}

func NewPaymentActivities(invoices *repositories.InvoiceRepository, mailer *mail.Mailer) *PaymentActivities {
	return &PaymentActivities{invoices: invoices, mailer: mailer}
}

func (a *PaymentActivities) ChargeCard(ctx context.Context, in PaymentInput) (ChargeResult, error) {
	return ChargeResult{}, nil
}
```

The workflow struct has an empty constructor and no fields. The activities
struct is the Wire-injected one. A developer who reaches for a repository inside
the workflow finds nothing to reach for, and the doc comment explains why. That
is the whole mechanism, and it is worth more than any amount of documentation.

`make:workflow` accepts `--activities=false` for orchestration-only workflows
that compose child workflows and timers.

### Testing model

`forj make:workflow` must also emit a passing test. The Go SDK's
`testsuite.WorkflowTestSuite` executes workflows with a mocked time-skipping
environment and no server at all, so a durable-timer workflow that waits three
days runs in milliseconds.

```go
func TestPaymentWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnActivity(ChargeCardActivityName, mock.Anything, mock.Anything).
		Return(ChargeResult{ID: "ch_1"}, nil)

	env.ExecuteWorkflow((&PaymentWorkflow{}).Execute, PaymentInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}
```

This is a genuine differentiator and fits the repo's test culture. It also gives
the render and smoke matrices a workflow assertion that needs no container.

Integration coverage uses the existing testcontainers path against the same
`temporalio/temporal` image the Compose profile pins, following
[rendering-and-smoke-workflow.md](../context/rendering-and-smoke-workflow.md).

## Task Queue Naming And Multi-App

Task queues default to `<app>-workflows`, so the default App and an `admin` App
never steal each other's tasks. `WORKFLOWS_TASK_QUEUE` overrides it, and
`<APP>_WORKFLOWS_TASK_QUEUE` overrides it per App using the existing prefixed
environment convention.

A workflows-only additional App is a natural deployment boundary:

```bash
forj make:app worker --components workflows
```

That App renders `cmd/worker/main.go`, boots straight into the worker through
`HasRuntime()`, and deploys independently of the API. The mixed-App rules from
Phase 4 of
[app-primitive-component-gating-plan.md](completed/app-primitive-component-gating-plan.md)
apply unchanged: shared framework files follow `ProjectComponents.Workflows`,
App accessors and framework injectors follow that App's `Components.Workflows`,
and an App without Workflows must not advertise, wire, or execute the package
even when it exists for another App.

## Relationship To Jobs And Scheduler

Temporal overlaps both. The framework should take a position rather than leave
developers to guess, and the generated documentation should carry this table.

| Use | Reach for | Why |
| --- | --- | --- |
| Send an email, resize an image, sync one record | Background Jobs | Single step, high volume, cheap retry. Temporal's per-execution overhead is not justified. |
| Order fulfillment, onboarding with a three-day timer, saga with compensation | Workflows | State must survive deploys and restarts; steps must be individually retryable. |
| Recur inside this App: cache warm, health poll, local cleanup | Scheduler | In-process, no durability requirement. |
| Recur as a business obligation: monthly billing, SLA escalation | Temporal Schedules | Must survive deploys and must not double-fire across replicas. |

They compose rather than compete. An activity may dispatch a GoForj job for
fan-out. A GoForj schedule may start a workflow. Neither direction requires new
framework surface.

The last row is the one that will generate the most questions, because the
in-process scheduler will happily run a monthly billing job and will silently
double-fire it the moment a second replica exists. Say so in the generated
`internal/workflows/README.md`.

## Observability

### Metrics

Add a `workflows` subsystem toggle to the metrics manager, matching
`QueueEnabled()` and `SchedulerEnabled()` in
`templates/internal/metrics/manager.go.tmpl`, plus `METRICS_WORKFLOWS_PORT`
following `METRICS_JOBS_PORT=10002` and `METRICS_SCHEDULER_PORT=10001`. `10003`
is the next free port.

The Temporal SDK emits its own metrics through a handler on `client.Options`,
covering task latency, poll success, and workflow completion. Bridging that into
the GoForj registry needs a Phase 0 spike: the SDK ships `contrib/tally` and
`contrib/opentelemetry` bridges, and the question is whether a direct adapter
into `internal/metrics` is cleaner than pulling a second metrics stack into
every Workflows-enabled App. Prefer the direct adapter if the handler interface
is reachable from a public package; do not ship two registries.

Framework-owned instrumentation to add on top of the SDK's own:

- workflow executions started, by workflow type
- activity executions and failures, by activity type
- worker poller saturation, since a saturated poller is the most common
  production symptom and is invisible in application logs

### Lighthouse

`internal/workflows/lighthouse.go` registers a `workflows` capability and
commands, following `internal/jobs/lighthouse.go`. The panel should answer the
questions an operator actually has:

- which task queues this process polls, and whether pollers are healthy
- which workflow and activity types are registered in this binary
- recent executions with status, duration, and failure reason
- a deep link to the local Temporal Web UI on `8233`

The deep link matters more than reimplementing Temporal's UI. Temporal already
ships a good execution browser with history and stack traces. Lighthouse should
answer "is this binary wired correctly and polling" and hand off for the rest.
That is also the split
[repo-boundaries-and-ownership.md](../context/repo-boundaries-and-ownership.md)
implies: do not rebuild a mature upstream operator surface.

## Environment Contract

A `WORKFLOWS_*` section in `templates/.env.tmpl`, gated on
`.ProjectComponents.Workflows` and placed after the `QUEUE_*` section:

```
# Workflows
WORKFLOWS_DRIVER={{ .Resources.WorkflowsDriver }}
WORKFLOWS_SUPPORTED_DRIVERS={{ .Resources.WorkflowsSupportedDrivers }}
WORKFLOWS_ADDRESS=127.0.0.1:7233
WORKFLOWS_NAMESPACE=default
WORKFLOWS_TASK_QUEUE={{ .DefaultTaskQueue }}
WORKFLOWS_SHUTDOWN_TIMEOUT=60s
WORKFLOWS_MAX_CONCURRENT_ACTIVITIES=100
WORKFLOWS_MAX_CONCURRENT_WORKFLOW_TASKS=100
```

`WORKFLOWS_API_KEY`, `WORKFLOWS_TLS_KEY_PATH`, and `WORKFLOWS_TLS_CERT_PATH` are
secret-like and must be blanked in `.env.example` and `.env.testing` by the
existing contract synchronization described in
[generated-app-extension-points.md](../context/generated-app-extension-points.md).

The dev-server Compose ports `TEMPORAL_GRPC_PORT` and `TEMPORAL_UI_PORT` stay
distinct from the `WORKFLOWS_*` application keys. They configure the container;
`WORKFLOWS_ADDRESS` configures the client. Conflating them breaks the moment
the App points at Temporal Cloud.

## Truthful Disabled Contract

A fresh Workflows-disabled render must contain no:

- `internal/workflows` package or generated client
- App `Workflows()` accessor or Wire provider
- `make:workflow`, `make:activity`, or `workflow:work` command
- `WORKFLOWS_*` environment key
- workflow metric toggle, port, or dashboard panel
- workflow service consumer or `temporal` Compose profile activation
- workflow runtime advertisement in `about`, discovery, or Lighthouse
- `go.temporal.io/sdk` module requirement retained by generated code

The last one is the one most likely to leak. `go.temporal.io/sdk` pulls a
substantial dependency tree including gRPC and protobuf, and a disabled render
that still carries it in `go.mod` fails the contract even if no code imports it.
Module synchronization must be gated on the project envelope containing
Workflows, exactly as queue driver modules are gated on Jobs.

Transition preflight follows `validateJobsRenderTransition`
(`internal/forj/project_component_transitions.go:224`): disabling Workflows must
fail before any write when user-authored files exist under `internal/workflows`
or when an App-owned `inject_workflows_app.go` contains registrations. Workflow
source is expensive to lose and, unlike a queue job, may correspond to
executions still running on a live server.

That last point deserves its own guard. Removing the component does not cancel
in-flight executions on the Temporal cluster; it only removes the workers that
would have completed them. The preflight message must say so, because the
resulting failure mode — executions stuck with no poller — is invisible from the
App side.

## Package Boundary Decision

[repo-boundaries-and-ownership.md](../context/repo-boundaries-and-ownership.md)
states that reusable primitives belong in sibling repos while `goforj` owns the
component flag, environment policy, generated wiring, and Lighthouse
integration. Two readings apply here.

**Option A — everything in templates.** Generated code imports
`go.temporal.io/sdk` directly; no new repo. Fastest path to a working prototype.
Costs: violates the stated boundary; client, worker, and registry logic become
template text that can only be exercised through a full render; no unit test
surface outside a rendered App.

**Option B — a thin `github.com/goforj/workflow` sibling.** Owns client
configuration and dialing, worker lifecycle and drain, the registry, driver
selection, and the inspection contract, mirroring `goforj/queue`. `goforj` owns
the component, environment policy, generated wiring, generators, and Lighthouse.

**Recommend Option B.** The SDK does the heavy lifting, so the repo stays
genuinely thin — configuration resolution, lifecycle, and an introspection
interface. The decisive argument is testability: a manager exercisable only
through `forj render` is painful to maintain, and every lifecycle property in
this document is a unit test in Option B and an integration test in Option A.
It also produces the seam that the compile-time extension model will need, which
makes Workflows a good validation case for those seams later.

The cost is real and should be acknowledged: a new sibling repo means a release
cadence, a version in `go.mod`, and the coordination described in
[releasing-sibling-repos.md](../context/releasing-sibling-repos.md). Phase 0
should confirm the repo has enough substance to justify that. If the spike shows
it is a hundred lines of configuration wrapping, collapse to Option A and
revisit.

## Implementation Strategy

The work proceeds vertically, following the pattern established by the primitive
gating plan. A phase is complete only when its enabled and disabled renders
compile and its absence assertions pass.

### Phase 0: Spikes and baseline

- Spike the metrics bridge. Determine whether the SDK's metrics handler
  interface is reachable from a public package, or whether `contrib/tally` is
  required. This decides whether Workflows-enabled Apps carry a second metrics
  stack.
- Spike the `goforj/workflow` boundary. Write the client, worker, and registry
  against the SDK and measure what is left after the SDK's own abstractions.
  Confirm or reject Option B.
- Spike worker drain under `worker.Options.WorkerStopTimeout` and confirm that
  stop-polling-then-drain behaves as documented against the dev server.
- Record the framework-owned files, render-once files, environment sections,
  commands, and modules the component will own.
- Capture a default-render baseline before any component gate changes output.

Exit criteria: the boundary decision is settled with code behind it; the metrics
approach is chosen; the current default render remains green.

### Phase 1: Component and resource model

- Add `ComponentWorkflows` to the catalog, the `Components` struct, the
  `Enabled`/`SetEnabled` switches, YAML ordering, CLI parsing, and the App
  component allowlist.
- Add `ResourceWorkflows` and `ServiceWorkflowsTemporal` with the `temporal` and
  `null` drivers.
- Add `Workflows` to `HasRuntime()`.
- Wire `Providers` and `DefaultFor` onto the existing `KeyTemporal` dev service.
- Keep the wizard row hidden and render no templates yet.

Exit criteria: an existing project round-trips its configuration unchanged; a
config with `workflows` selected resolves a service plan that activates the
`temporal` profile; no generated output changes.

### Phase 2: Runtime vertical slice

- Add `internal/generate/workflows.go` and its catalog parity test.
- Render `internal/workflows` with client, worker, registry, runtime, and
  `workflow:work`.
- Add `inject_workflows.go` and render-once `inject_workflows_app.go`.
- Add the fourth runtime block to `run_cmd.go.tmpl`.
- Extend `runtime.Timeouts` with `WorkflowShutdownTimeout()`.
- Add the `WORKFLOWS_*` environment section and contract synchronization rules.
- Add the render step to `project_renderer.go` beside the Jobs step.

Exit criteria: a Workflows-enabled render compiles, generates Wire output, boots
`./bin/app` into a host containing the workflows runtime, connects to the dev
server, and shuts down cleanly within its drain budget. A Workflows-disabled
render satisfies the full absence contract including the `go.mod` requirement.

### Phase 3: Generators and the determinism boundary

- Add `make:workflow` and `make:activity` with their raw templates, following
  `make:job` for both creation and removal.
- Ensure both update `inject_workflows_app.go` registration and the Wire
  provider together.
- Emit the `testsuite` test alongside every generated workflow.
- Render the example greeting workflow, its activities, and its passing test.
- Write `internal/workflows/README.md` covering the determinism rule, the
  Jobs/Scheduler decision table, versioning with `workflow.GetVersion`, and
  heartbeating.

Exit criteria: `forj make:workflow Payment` produces a compiling workflow, an
injected activities struct, a passing test, and complete registration; removal
reverses all of it; the generated test runs with no server.

### Phase 4: Observability and multi-App closure

- Add the `workflows` metrics subsystem, port, and framework instrumentation.
- Add the Lighthouse capability, commands, and panel with the Temporal UI link.
- Add workflow runtime advertisement to `about` and runtime discovery.
- Close mixed-App seams: prove default-disabled-with-named-enabled and
  default-enabled-with-named-disabled in both directions.
- Add the transition preflight, including the in-flight-executions warning.
- Add a scenario spec. `internal/scenarios/specs/account-transfer-transaction.yaml`
  already exists and is the canonical Temporal sample; align with it.

Exit criteria: both mixed-App directions render, compile, and advertise only
their own participation; disabling with residue fails before any write;
rerender and generation are idempotent.

### Phase 5: Documentation and wizard exposure

- Expose the wizard row, unselected by default.
- Add `docs/context/workflows.md` and a topic-map entry in
  `docs/context/index.md`.
- Document the `forj dev` restart-durability behavior and its two edges.
- Update `docs/graceful-shutdown.md` with the worker drain sequence.

## File-By-File Task List

| File | Change |
| --- | --- |
| `project/components_catalog.go` | `ComponentWorkflows` constant and catalog entry, `DefaultSelected: false` |
| `project/config.go` | `Workflows` field, `Enabled`/`SetEnabled` cases, `HasRuntime()` at line 389, `ValidateRenderContract` |
| `project/components_yaml.go` | canonical key ordering |
| `project/app_components.go` | `appComponentKeys`, `NormalizeAppComponents` metrics gating |
| `project/resource_catalog.go` | `ResourceWorkflows`, `ServiceWorkflowsTemporal`, `temporal` and `null` drivers, env placeholders |
| `project/service_plan.go` | service key ordering |
| `internal/devservices/catalog.go:164` | `Providers` and `DefaultFor` on the existing `KeyTemporal` entry |
| `internal/generate/workflows.go` | new generator, modeled on `queues.go` |
| `internal/generate/resource_catalog_parity_test.go` | parity entry |
| `internal/generate/cmd.go` | `Workflows` selection flag, path list, disabled-request error |
| `internal/forj/project_renderer.go` | "Workflow Components Rendering" step beside the Jobs step at ~1057 |
| `internal/forj/project_component_transitions.go` | `validateWorkflowsRenderTransition` and residue paths |
| `internal/forj/new_project_cmd.go` | wizard row |
| `templates/internal/workflows/*.tmpl` | client, worker, registry, runtime, `worker_cmd`, lighthouse, example, README |
| `templates/internal/makecmd/make_workflow_cmd.go.tmpl` | generator command and test |
| `templates/internal/makecmd/make_activity_cmd.go.tmpl` | generator command and test |
| `templates/internal/makecmd/workflow.tmpl`, `activity.tmpl`, `workflow_test.tmpl` | raw scaffolds |
| `templates/wire/inject_workflows.go.tmpl` | framework Wire set |
| `templates/wire/inject_workflows_app.go.tmpl` | render-once app registration |
| `templates/wire/app.go.tmpl` | `Workflows()` accessor and constructor parameter |
| `templates/app/root_cmd.go.tmpl` | `workflow:work`, `make:workflow`, `make:activity` |
| `templates/internal/cmd/run_cmd.go.tmpl` | fourth runtime block |
| `templates/internal/runtime/timeouts.go.tmpl` | `WorkflowShutdownTimeout()` |
| `templates/internal/runtime/about.go.tmpl`, `discovery.go.tmpl` | advertisement and discovery |
| `templates/internal/metrics/manager.go.tmpl` | `WorkflowsEnabled()` toggle |
| `templates/.env.tmpl` | `WORKFLOWS_*` section, `METRICS_WORKFLOWS_PORT=10003` |
| `templates/containers/developer-services/temporal.yml.tmpl` | optional persistence volume |
| `internal/envcontract/contract.go` | secret-like key classification |
| `internal/scenarios/specs/` | workflow scenario aligned with the existing transaction spec |
| `docs/context/index.md`, `docs/context/workflows.md` | topic map and context doc |
| `docs/graceful-shutdown.md` | worker drain sequence |

## Acceptance Criteria

- A fresh Workflows-disabled render satisfies the complete absence contract,
  including no `go.temporal.io/sdk` requirement in `go.mod`.
- A Workflows-enabled render with Jobs, Events, and Storage disabled compiles
  every App, generates Wire output, and runs its generated tests.
- `forj dev` on a Workflows-enabled project starts the Temporal dev server,
  reports it in `forj dev status`, and surfaces the Web UI as a discoverable
  link.
- `./bin/app` with no arguments enters the run host and starts the workflows
  runtime.
- `forj make:workflow Payment` produces a compiling workflow with no
  dependencies, an injected activities struct, a passing `testsuite` test, and
  complete registration in `inject_workflows_app.go`. Removal reverses all of it.
- The generated workflow test passes with no Temporal server running.
- `SIGTERM` stops polling before draining, completes in-flight activities within
  `WORKFLOWS_SHUTDOWN_TIMEOUT`, closes the client, and exits zero.
- A worker that cannot reach Temporal fails fast with a named runtime error. A
  client-only App boots normally with Temporal unreachable and fails only on
  first use.
- Restarting the App under `forj dev` mid-execution resumes the workflow.
- Both mixed-App directions render, compile, and advertise only their own
  participation.
- Disabling Workflows with user-authored source or App-owned registrations fails
  before any write, and the message notes that in-flight cluster executions are
  not cancelled.
- Rerender and generation are idempotent.

## Open Questions

1. **`APP_SHUTDOWN_TIMEOUT` interaction.** A `60s` workflow budget exceeds the
   `30s` app default. Raise the app default for Workflows-enabled renders, or
   document the requirement and log loudly when the workflow budget is clamped?
2. **Named workflow resources.** Is a second namespace a named resource, a
   second task queue a named resource, or both? Jobs treats a named queue as one
   resource; Temporal has two orthogonal axes and the mapping is not obvious.
3. **Temporal Schedules.** Should the Scheduler component gain a
   `DurableSchedule` option that registers with Temporal instead of running
   in-process, or should that stay explicit workflow code? The first is better
   DX and blurs a boundary this design otherwise keeps clean.
4. **Dev server persistence default.** In-memory avoids non-determinism panics
   during development but loses history on every restart, which undercuts the
   restart-durability demo that is one of the design's best properties.
5. **Interceptors.** The SDK's interceptor chain is the correct place for
   logging, metrics, and trace propagation. Framework-owned by default, or an
   App-owned extension point?

## Future Work

- Temporal Cloud as a first-class configuration target with mTLS and API-key
  paths proven end to end.
- A `forj workflow:list` inventory command mirroring the queue inventory work in
  [job-queue-production-dx-plan.md](job-queue-production-dx-plan.md).
- Codec server support so encrypted payloads remain readable in the Temporal UI.
- A `temporal` CLI passthrough command for local operations.
- Reconsidering the abstraction only if a second engine arrives with a real
  implementation to test it against.
