# Practical Workflows

This document is the day-to-day operating guide for changing GoForj and validating the result.

## Changing `goforj`

Typical loop:

1. change template, generator, or framework code
2. run focused tests
3. rerender a smoke app
4. build, run, or smoke the rendered app

Common checks:

```bash
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./internal/forj -count=1
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./internal/generate -count=1
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./internal/apiindex -count=1
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./internal/build -count=1
```

## Renaming A Project Module

From a generated project's root:

```bash
forj project:rename-module
forj build
```

The command shows the current module, prompts for the new path, and asks for confirmation before making changes. Use `--dry-run` to preview the affected files. It updates `.goforj.yml`, the root `go.mod` declaration, Go imports and module-qualified string literals, and templ imports across all Apps. See [Rename A Project's Go Module](../project-module-rename.md) for rewrite boundaries and nested-module handling.

## Changing `console`

Typical loop:

1. change the reusable behavior in the `console` repository
2. keep package-level and `*Console` forms aligned
3. add source-comment examples with expected output
4. regenerate the README and run package, docs, and example tests
5. tag and push the package before bumping GoForj

Do not move GoForj command semantics, its private build-progress protocol, or
Bubble Tea state into the sibling package. Generated apps also retain their own
`internal/console` template until that dependency contract is migrated
separately.

See [Console Output](console.md) and
[Releasing Sibling Repos](releasing-sibling-repos.md) for the current boundary
and validation commands.

## Changing `web`

Typical loop:

1. change `web`
2. run tests in the `web` repo
3. point the rendered app at local `web` using `render.module_replaces`
4. rerender and smoke the app

Common check:

```bash
cd ../web
GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache go test ./...
```

## Changing `queue`

Typical loop:

1. change `queue`
2. run focused driver tests
3. tag and push modules if GoForj needs published versions
4. bump GoForj dependency
5. rerender, build, and test the app

Recent regression note:

- If `queue:hello-test` fails with `job queue is required`, do not "fix" it by adding `appJobSet` to `jobSet`.
- `wire.go.tmpl` already includes both `jobSet` and `appJobSet`; duplicating `appJobSet` inside `jobSet` causes Wire duplicate-provider failures.
- The remaining issue is understood as a runtime/default-queue construction problem, not a command-registration or missing-example-job-provider problem.

## Working With The Rendered App

Use the rendered app to validate that:

- templates actually generate correctly
- runtime wiring still holds together
- sibling repo changes really work in a real app

Edit the rendered app directly only when:

- testing a local hypothesis quickly
- fixing a temporary local-only path/config issue
- patching the smoke target intentionally

Do not treat those edits as the durable fix unless they are intentionally local-only.

Useful generated files worth inspecting when behavior diverges:

- generated `wire` files
- `internal/storages/manager_gen.go`
- `internal/runtime/discovery.go`
- generated `internal/jobs/lighthouse.go`
- generated `internal/schedules/lighthouse.go`
- generated `app/schedules.go`

## Working With Environment Contracts

Rendered projects use a synchronized three-file contract:

- `.env` is ignored, private local state; `forj build`, `forj generate`, `forj dev`, and `forj run` create it from `.env.example` when missing
- `.env.example` is the committed safe inventory
- `.env.testing` is the committed deterministic profile selected when tests load `goforj/env`

Use `forj env:set KEY` to enter a local secret without putting its value in shell history. The next `forj generate`, `forj build`, or `forj dev` run adds a blank committed entry for a newly discovered application key; put a safe public default in `.env.example` explicitly when the test contract should inherit one. CI can run `forj env:check` on a clean checkout to verify the committed pair without creating `.env`; process variables remain the override path for live-service credentials. An unmanaged legacy `.env.testing` must be reviewed and removed before GoForj changes ignore rules or creates the committed profile, preventing previously private test values from becoming stageable.

## Integration Test Reality

Some of the most valuable GoForj failures are in `internal/forj` integration tests, not the small unit tests.

Practical pattern:

```bash
PATH="/tmp:$PATH" GOCACHE=/tmp/gocache GOMODCACHE=/tmp/gomodcache \
go test -tags=integration ./internal/forj -count=1
```

Important reminders:

- `wire` must be available on `PATH` for temp-app renders
- a generator/template regression can cascade into many `missing app binary` failures; fix the first render or wire failure first
- test harness env drift can look like a runtime auth bug
- raw testcontainers logging is too noisy for normal output; prefer concise harness lifecycle lines

## `forj` App Delegation / `forj run` / `forj dev`

### App Commands

Mental model:

- `forj <app-command>` is the normal generated App development surface
- `forj run <app-command>` is the explicit App-command path

Examples:

- `forj app`
- `forj route:list`
- `forj queue:work`

### `forj dev`

`forj dev` is watcher/orchestration UX.

It should not own process naming policy or app log prefix semantics beyond watcher concerns.

The child app/process topology belongs lower in `run` and runtime launch logic.

Structured projects keep all setup in the existing `dev.pre` list. GoForj schedules recognized framework bootstrap work such as Compose startup, database readiness, frontend dependency installation, and generators around the SPA build, App build, and auto-migration boundaries. If every task is recognized, frontend assets settle before the App's single cold-start compile. An arbitrary custom pre-task retains the historical binary-ready ordering, which can require the compatibility rebuild. Schema-dependent generators run after auto-migration and cause a final App rebuild.

For shutdown, `forj dev` owns watcher process orchestration:

- Ctrl+C, restart, and render-triggered restarts should signal all watcher subprocesses in parallel.
- Shutdown waits for the group after signaling, so one slow target does not delay other targets from receiving an interrupt.
- Keep output collapsed into concise watcher lifecycle lines instead of per-process shutdown spam.
- Do not make Docker Compose helper-container delays look like app runtime shutdown. If a one-shot helper is safe to interrupt, prefer a short service-level Compose grace period over shortening shutdown for the whole stack.

## Frequent Pitfalls

- do not use `~` in `module_replaces`
- do not assume relative replace paths will work
- do not fix persistent generated-app issues only in the rendered app
- do not put driver/backend-specific fixes in `goforj` if they belong in `queue`
- do not put reusable web concerns in GoForj just because the template currently holds them
- do not reimplement reusable semantic output, terminal layout, prompts, loaders, or progress in GoForj when they belong in `console`
- do not reintroduce duplicated env parsing in leaf components
- do not use non-semantic commit subjects when committing GoForj changes
- do not paper over missing DI wiring with defensive nil checks in commands or services
