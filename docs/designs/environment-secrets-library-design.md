# Environment Secrets Package Design

## Status

- Design status: proposed
- Planning date: 2026-09-07
- Target repository: the existing `github.com/goforj/env` repository
- Import path: `github.com/goforj/env/v2/envsecrets`
- Package name: `envsecrets`
- Primary scope: explicitly declared secret environment variables, immutable capture, redacting values, controlled refresh, and deterministic tests

## Summary

The existing `env/v2` module should gain an `envsecrets` subpackage for applications that intentionally receive secrets through process environment variables.

The package is not a second environment configuration system and is not a driver for `github.com/goforj/secrets`. It provides a narrow safety boundary around one delivery mechanism:

```go
var (
	DatabasePassword = envsecrets.RequireNonEmpty("DATABASE_PASSWORD")
	MailAPIToken      = envsecrets.Optional("MAIL_API_TOKEN")
)

secretEnv, err := envsecrets.Capture(
	ctx,
	envsecrets.ProcessLookup{},
	DatabasePassword,
	MailAPIToken,
)
if err != nil {
	return fmt.Errorf("capture environment secrets: %w", err)
}

password, err := secretEnv.Get(DatabasePassword)
if err != nil {
	return err
}
```

Declarations live in application Go code next to composition. There is no public manifest workflow, YAML mapping, generated `env_secrets` component, logical-name alias, environment driver, or bridge into the general Secrets library.

## Decision

Add `envsecrets` to the existing `env/v2` module with these rules:

1. Applications declare secret variables as opaque `Binding` values in Go code.
2. Each binding contains one exact environment-variable name and one requirement.
3. `Capture` validates the complete binding set, reads only those exact names through an injected `Lookup`, and publishes one immutable initial snapshot.
4. The package never enumerates the process environment or derives names.
5. Missing, present-empty, and present-nonempty values remain distinct.
6. A value must be explicitly revealed. All ordinary formatting and serialization redact.
7. Returned bytes are copies and snapshot storage is immutable.
8. `Refresh` is explicit, serialized, and all-or-nothing.
9. The package has fixed conservative bounds in v1 rather than configuration options for weakening them.
10. The package does not load dotenv files or mutate the environment. The existing root `env` package retains that responsibility.
11. The package does not import or call `github.com/goforj/secrets`.
12. The general Secrets library does not treat environment variables as a driver.
13. Applications choose environment capture or managed retrieval in composition through narrow domain interfaces.
14. Missing environment values never trigger managed-store fallback.
15. GoForj does not persist environment-secret declarations in `render`, named-App configuration, or another YAML section.

## Why It Lives In The Env Module

Environment secret capture shares its delivery mechanism and lifecycle with ordinary environment configuration:

- dotenv loading, when used, must finish before capture;
- process replacement is the normal rotation mechanism;
- missing versus explicitly empty values follow environment semantics;
- child processes may inherit ambient values; and
- tests need controlled environment lookup behavior.

These concerns already belong to `env/v2`. A separate repository would create another release lifecycle, duplicate environment vocabulary, and obscure which package owns dotenv behavior.

A subpackage still provides an important API boundary. The root `env` package supports ordinary configuration behaviors such as defaults, discovery, dumping, and dotenv mutation. `envsecrets` deliberately excludes those operations. The import identifier makes that distinction visible at every call site:

```go
port := env.GetInt("PORT", "8080")
password, err := secretEnv.Get(DatabasePassword)
```

The root `env` package must not import `envsecrets`. The subpackage can remain independent of root implementation details and operate through its narrow `Lookup` interface.

## Relationship To The General Secrets Library

`github.com/goforj/secrets` models runtime reads from managed or mounted providers. Those sources have concepts such as provider locators, versions, aliases, authorization errors, retries, cache freshness, and readiness.

Environment capture has none of those portable semantics. Treating it as a driver would require exceptions for caching, coalescing, retries, versions, readiness, and lifecycle. It would also encourage implicit substitution between a startup configuration input and a runtime provider.

The packages therefore have no adapter or bridge. Domain code that supports either delivery mechanism depends on an application-owned interface:

```go
type DatabasePasswordSource interface {
	DatabasePassword() ([]byte, error)
}
```

One composition adapter can reveal a captured `envsecrets.Value`. Another can call a managed `secrets.Reader`. Selection is explicit application wiring, not library fallback or a shared driver configuration.

Tests of the general Secrets library use its memory source and fake. They do not use process environment variables as a provider substitute.

## Goals

1. Make intentional environment-secret access explicit and reviewable in Go code.
2. Avoid a second YAML or generated representation of environment-variable names.
3. Preserve missing and empty semantics.
4. Prevent unconstrained arbitrary reads and environment enumeration.
5. Make accidental formatting and serialization redact values.
6. Provide stable reads through immutable snapshots.
7. Make refresh explicit and failure-safe.
8. Bound names, binding counts, individual values, and aggregate snapshot size.
9. Support deterministic tests without mutating the real process environment.
10. Document the limitations of environment delivery honestly.

## Non-goals

1. General environment parsing, defaults, typed conversion, or application-environment selection.
2. Dotenv parsing, discovery, precedence, writing, synchronization, or watching.
3. Environment-variable discovery, wildcard access, prefix scans, or name inference.
4. Managed-provider access, remote fallback, caching, leases, versions, or revocation.
5. A `github.com/goforj/secrets` driver or common reader adapter.
6. GoForj render configuration or generated secret declarations.
7. Detecting whether undeclared environment variables look sensitive.
8. Encrypting process memory or guaranteeing zeroization in Go.
9. Automatically unsetting captured variables.
10. Automatically filtering child-process environments.
11. Dynamic addition or removal of bindings after capture.
12. Package-global stores or package-level arbitrary-name getters.

## Terminology

### Binding

An opaque declaration of one exact environment-variable name and its presence requirement. A Binding is safe to copy and use as an access key. Formatting it does not reveal its environment name.

### Capture

One complete attempt to read and validate every declared binding.

### Snapshot

One immutable successful capture generation.

### Store

The holder of the current snapshot, original binding set, lookup dependency, and refresh gate.

### Value

An opaque handle to immutable captured bytes. Formatting redacts; reveal methods return explicit copies.

## Proposed API

```go
package envsecrets

const (
	MaxBindings             = 256
	MaxEnvironmentNameBytes = 253
	MaxValueBytes           = 1 << 20
	MaxSnapshotBytes        = 16 << 20
)

var (
	ErrInvalid           = errors.New("envsecrets: invalid handle")
	ErrInvalidBinding    = errors.New("envsecrets: invalid binding")
	ErrDuplicateBinding  = errors.New("envsecrets: duplicate binding")
	ErrUnknownBinding    = errors.New("envsecrets: binding was not captured")
	ErrMissing           = errors.New("envsecrets: secret is missing")
	ErrEmpty             = errors.New("envsecrets: secret is empty")
	ErrValueTooLarge     = errors.New("envsecrets: secret exceeds value limit")
	ErrSnapshotTooLarge  = errors.New("envsecrets: snapshot exceeds aggregate limit")
	ErrLookup            = errors.New("envsecrets: lookup failed")
	ErrGenerationExhausted = errors.New("envsecrets: generation exhausted")
)

type Requirement uint8

const (
	RequirementOptional Requirement = iota
	RequirementPresent
	RequirementNonEmpty
)

type Binding struct {
	// unexported
}

func Optional(environment string) Binding
func RequirePresent(environment string) Binding
func RequireNonEmpty(environment string) Binding
func (b Binding) String() string
func (b Binding) GoString() string
func (b Binding) Format(fmt.State, rune)
func (b Binding) MarshalText() ([]byte, error)
func (b Binding) MarshalJSON() ([]byte, error)

type BindingError struct {
	Index       int
	Environment string
	Requirement Requirement
	// unexported stable cause
}

func (e *BindingError) Error() string
func (e *BindingError) Unwrap() error
func (e *BindingError) GoString() string
func (e *BindingError) Format(fmt.State, rune)

type Lookup interface {
	LookupEnv(ctx context.Context, name string) (value string, present bool, err error)
}

type LookupFunc func(ctx context.Context, name string) (value string, present bool, err error)

func (f LookupFunc) LookupEnv(ctx context.Context, name string) (string, bool, error)

type ProcessLookup struct{}

func (ProcessLookup) LookupEnv(ctx context.Context, name string) (string, bool, error)

type Store struct {
	// unexported
}

func Capture(ctx context.Context, lookup Lookup, bindings ...Binding) (Store, error)
func (s Store) Snapshot() Snapshot
func (s Store) Get(binding Binding) (Value, error)
func (s Store) Lookup(binding Binding) (Value, bool, error)
func (s Store) Refresh(ctx context.Context) error
func (s Store) String() string
func (s Store) GoString() string
func (s Store) Format(fmt.State, rune)

type Snapshot struct {
	// unexported
}

func (s Snapshot) Generation() uint64
func (s Snapshot) CapturedAt() time.Time
func (s Snapshot) Get(binding Binding) (Value, error)
func (s Snapshot) Lookup(binding Binding) (Value, bool, error)
func (s Snapshot) String() string
func (s Snapshot) GoString() string
func (s Snapshot) Format(fmt.State, rune)

type Value struct {
	// unexported
}

func (v Value) Empty() bool
func (v Value) RevealBytes() ([]byte, error)
func (v Value) RevealString() (string, error)
func (v Value) String() string
func (v Value) GoString() string
func (v Value) Format(fmt.State, rune)
func (v Value) MarshalText() ([]byte, error)
func (v Value) MarshalJSON() ([]byte, error)
```

The constructors intentionally return Binding values without errors so package-level declarations remain ergonomic. They retain the supplied string privately. `Capture` performs authoritative validation before any lookup. A zero Binding is invalid.

The API contains no public Manifest or Spec, logical-name mapping, prefix, options map, package-global store, arbitrary `Get(string)`, `MustGet`, `Names`, `Range`, `All`, `Set`, or `Delete` operation.

V1 uses fixed hard bounds. If real deployments demonstrate a need for different limits, a later design may add options that can only reduce or deliberately raise bounded ceilings. The initial API should not add configuration ceremony without evidence.

## Binding Semantics

Environment names must contain only uppercase ASCII letters, digits, and underscores, must begin with an uppercase ASCII letter or underscore, and must not contain `=`, NUL, whitespace, or control characters. They may occupy at most `MaxEnvironmentNameBytes` bytes.

`Capture` validates the complete binding slice before calling Lookup. It rejects:

- an empty binding set;
- a zero Binding;
- an invalid or overlong environment name;
- an unknown Requirement value;
- more than `MaxBindings` declarations; and
- duplicate environment names, including duplicates with different requirements.

Declaration order determines lookup and first-failure order. The package copies the binding slice and names during construction. Later mutation of caller-owned input cannot alter the Store.

Rejecting an empty binding set catches incomplete wiring. Conditional application composition should omit Capture rather than publish an empty secret Store.

Bindings are access tokens, not metadata views. They expose no environment-name or requirement accessor in v1. Application code already names the declaration and can keep the exact variable name beside it. Generic tooling must not use reflection or formatting to inventory bindings.

Store and Snapshot accept only a Binding whose exact validated name appeared in the original capture set. A separately constructed Binding for the same exact name is accepted because declarations are value-semantic. A binding for an uncaptured name returns `ErrUnknownBinding` without consulting Lookup.

## Missing And Empty Values

Lookup results have these meanings:

| Lookup result | Optional | RequirePresent | RequireNonEmpty |
| --- | --- | --- | --- |
| `present=false` | capture succeeds as missing | `ErrMissing` | `ErrMissing` |
| `present=true, value=""` | capture succeeds as empty | capture succeeds as empty | `ErrEmpty` |
| `present=true, value!=""` | capture succeeds | capture succeeds | capture succeeds |

The returned value is ignored when `present=false`, even if a custom Lookup returns a nonempty string.

For a captured optional-missing binding:

- `Get` returns `ErrMissing`;
- `Lookup` returns `(Value{}, false, nil)`.

For an undeclared binding, both methods return `ErrUnknownBinding`; Lookup does not turn an access mistake into optional absence.

Whitespace is not trimmed. Empty means exactly zero bytes. A value containing spaces or a trailing newline is nonempty and is preserved exactly.

## Capture And Snapshot Model

Capture proceeds in this order:

1. validate the complete binding set and Lookup dependency;
2. check context cancellation;
3. before every binding, check cancellation again;
4. call Lookup once for that exact environment name;
5. enforce requirement and per-value bounds;
6. enforce the aggregate bound with checked arithmetic;
7. check cancellation immediately before publication; and
8. publish generation 1 only after the complete candidate succeeds.

Any failure returns a zero Store and retains no usable partial snapshot. No lookup occurs before all declaration validation succeeds.

Normal reads use immutable snapshot storage and never access the process environment. `Store.Get` and `Store.Lookup` load the current snapshot once and delegate to it. Callers that need several values from one generation obtain a Snapshot explicitly.

`CapturedAt` is root-owned informational metadata recorded once after a complete successful capture. It is not proof that the launcher changed every value at that instant and is not a remote freshness claim.

## Refresh

`Store.Refresh(ctx)` deliberately recaptures the original immutable binding set through the original Lookup. It is intended for controlled tests and applications that knowingly mutate their environment source. Process replacement remains the recommended production rotation model.

Refresh attempts are serialized through a context-aware gate. A canceled queued caller returns without lookup. The active attempt checks cancellation before each lookup and immediately before publication. Any pre-commit error discards the candidate and preserves the current snapshot exactly.

The atomic snapshot swap is the commit point. Generation begins at 1 and increments only at commit, including when bytes are unchanged. Once the swap occurs, Refresh returns nil even if the context is canceled immediately afterward. Reporting an error after commit would invite a retry that creates an unintended extra generation.

Snapshot loads remain lock-free. A concurrent reader sees either the complete prior generation or the complete new generation, never a mixture. Previously returned Snapshot and Value handles remain valid and keep their generation storage alive.

If generation reaches `math.MaxUint64`, Refresh returns `ErrGenerationExhausted` and preserves the current snapshot.

## Lookup Contract

Lookup is injected so core tests and specialized launchers do not mutate process-global state. Implementations must:

- distinguish missing from present-empty;
- return exact string bytes without trimming or normalization;
- avoid logging the name and value inside the lookup path;
- honor context cancellation before blocking work and return promptly when possible; and
- be safe for concurrent use if shared by several Stores.

`ProcessLookup` is stateless and calls `os.LookupEnv`. It checks `ctx.Err()` immediately before that call. It never calls `os.Environ`, caches a value, loads dotenv files, or mutates the environment.

## Redaction And Disclosure

Binding, Store, Snapshot, and Value implement safe presentation for values and pointers under `%s`, `%v`, `%+v`, `%#v`, and nested formatting. Binding emits `[ENV_SECRET_BINDING]`; Value emits `[REDACTED]`; Store and Snapshot emit bounded metadata without names or values.

Value JSON and text marshaling emit `[REDACTED]` for empty and nonempty values. Binding marshaling emits only its constant marker. There is no ordinary encoding path for plaintext.

`RevealBytes` returns a new allocation on every successful call. Mutation cannot alter snapshot storage. `RevealString` returns a new string. Both return `ErrInvalid` for a zero Value. Value carries a private validity bit so a zero handle cannot impersonate an explicitly present-empty secret.

Redaction is defense in depth. Reflection, unsafe code, debuggers, memory dumps, and callers that invoke reveal methods can expose data. Go does not provide reliable zeroization because strings, runtime copies, stacks, SDKs, and the garbage collector may retain copies. V1 provides no misleading Destroy method.

## Errors

Errors expose stable sentinel classification through `errors.Is`. A typed BindingError may contain declaration index, exact validated environment name, Requirement, and a stable sentinel. It never contains a value, value length, prefix, hash, Lookup response, or arbitrary formatted source error.

An exact environment name is included only after it has passed binding validation and exact membership is known. Invalid input uses an index and fixed label rather than echoing attacker-controlled text.

An arbitrary Lookup error may itself contain secret material. The package therefore does not retain it in the public error graph. Lookup failure returns an error matching `ErrLookup` with fixed safe formatting and no provider cause. Context cancellation and deadline errors remain detectable through `errors.Is` when they caused the result.

Formatting every public error under every verb must remain value-free. Examples log only stable classification and declaration identity.

## Observability

The package has no logger, global callback, metrics dependency, trace dependency, or per-access event stream. Per-Get events would reveal secret access patterns and create unnecessary hot-path work.

Applications may measure Capture and Refresh around their explicit calls. Safe telemetry includes operation, outcome, duration, generation, and stable error class. It excludes environment names, Binding values, secret presence, value lengths, hashes, and Lookup errors.

A future observer would need a separate compatibility and disclosure review. V1 does not add callback lifecycle and panic semantics without a demonstrated integration need.

## Concurrency And Zero Values

Store, Snapshot, Binding, Value, and ProcessLookup are safe for concurrent use after successful construction. Store and Snapshot are value-semantic wrappers around private state. Copying them does not copy synchronization primitives.

The zero value of Binding, Store, Snapshot, and Value is invalid. Formatting remains safe. Error-returning methods return `ErrInvalid`; Snapshot generation and capture time return zero metadata; Value.Empty returns false because zero does not prove present-empty.

Pointers to these value-semantic handles are not the supported API surface. Non-nil pointers inherit safe formatting, but direct method calls through nil pointers receive no special guarantee.

## Process Environment Limitations

The package cannot change the security properties of environment delivery:

- values commonly exist from process start;
- privileged operators, same-user tooling, crash handlers, or compromised dependencies may read them;
- environment limits and encoding behavior vary across operating systems and launchers;
- the environment has no authenticated provider identity, version, lease, revocation, or rotation acknowledgement; and
- revealed values become ordinary application memory.

Children commonly inherit the entire environment when `exec.Cmd.Env` is nil. Applications must construct explicit child environments at trust boundaries:

```go
cmd := exec.CommandContext(ctx, executable, args...)
cmd.Env = []string{
	"PATH=" + safePath,
	"LANG=C.UTF-8",
}
```

Filtering only secret-looking names is insufficient. `envsecrets` does not alter commands or unset variables because doing so would race other application code and cannot remove existing operating-system or runtime copies.

## Relationship To Dotenv And Root Env

`envsecrets` does not parse or discover `.env` files. If an application uses `env/v2` loading, startup order is explicit:

1. load ordinary environment layers through the root env package;
2. finish process-environment mutation;
3. capture declared secret bindings with `envsecrets.ProcessLookup{}`; and
4. inject the Store, a Snapshot, or narrow domain accessors.

Calling `env.Reload` does not update an existing Store. An application that deliberately supports in-process reload calls Store.Refresh after a successful environment reload and accepts that the two package operations are not one transaction. GoForj does not automate this coupling in v1.

Local `.env` files containing real secrets remain ignored. Committed `.env.example` values remain blank or redacted. `.env.testing` may contain only conspicuously public fixtures. This package does not make committed dotenv credentials safe.

## Application And GoForj Integration

There is no `env_secrets` GoForj component and no `render.env_secrets`, `apps.<name>.env_secrets`, or top-level configuration section.

Applications declare bindings in ordinary Go code, normally near the composition that consumes them:

```go
package appsecrets

var DatabasePassword = envsecrets.RequireNonEmpty("DATABASE_PASSWORD")

func Capture(ctx context.Context) (envsecrets.Store, error) {
	return envsecrets.Capture(
		ctx,
		envsecrets.ProcessLookup{},
		DatabasePassword,
	)
}
```

GoForj may document the startup ordering and provide generic application composition seams, but it does not generate bindings, scan source code, infer secrets from suffixes, or synchronize a second declaration format. Existing `.env.example` and `.env.testing` tooling remains the environment inventory.

Generated App containers should receive narrow application-owned accessors where practical rather than exposing a global Store. A domain adapter can close over a Binding and Snapshot:

```go
type databasePassword struct {
	snapshot envsecrets.Snapshot
}

func (s databasePassword) Password() ([]byte, error) {
	value, err := s.snapshot.Get(DatabasePassword)
	if err != nil {
		return nil, err
	}
	return value.RevealBytes()
}
```

This is application composition, not a driver or compatibility layer for the general Secrets package.

## Test Package

The same module should include `github.com/goforj/env/v2/envsecrets/fake`:

```go
package fake

type Lookup struct {
	// unexported, concurrency-safe
}

func New(values map[string]string) *Lookup
func (l *Lookup) LookupEnv(ctx context.Context, name string) (string, bool, error)
func (l *Lookup) Set(name, value string)
func (l *Lookup) SetEmpty(name string)
func (l *Lookup) Unset(name string)
func (l *Lookup) Fail(name string, err error)
func (l *Lookup) ClearFailure(name string)
func (l *Lookup) Calls() []string
func (l *Lookup) ResetCalls()
```

The fake copies input and output state, distinguishes empty from missing, records exact lookup order, and is safe for concurrent use. Its formatting never traverses retained values, names, or injected errors. Failure output is fixed and value-free.

Tests do not mutate the real process environment except a small nonparallel ProcessLookup integration test using unique conspicuously public values. No helper calls `t.Setenv` behind the caller's back.

## Test Plan

### Unit and contract tests

Direct tests cover:

- all environment-name grammar boundaries and overlong input;
- zero, maximum, and over-maximum binding counts;
- zero and duplicate bindings, including conflicting requirements;
- complete validation before the first lookup;
- exact declaration-order lookup with no undeclared calls;
- missing, present-empty, and present-nonempty under every requirement;
- ignored nonempty lookup output when `present=false`;
- value and aggregate size boundaries with checked arithmetic;
- initial capture success and every failure branch;
- Get and Lookup behavior for captured, optional-missing, uncaptured, and zero bindings;
- copied input strings, returned bytes, fake state, and call history;
- byte preservation for empty, whitespace, newline, NUL, valid UTF-8, and invalid UTF-8 Go strings;
- redaction under every formatting verb, JSON, text, pointers, nesting, and common logging adapters;
- arbitrary secret-bearing Lookup errors absent from all public formatting and unwrap chains;
- every zero-handle method;
- fixed bounds that cannot be bypassed by integer overflow; and
- ProcessLookup missing versus explicitly empty behavior.

### Refresh and race tests

Run `go test -race ./...` with:

- many readers of one Snapshot;
- Store reads concurrent with successful and failed refresh;
- serialized concurrent refresh attempts;
- cancellation while queued for refresh admission;
- cancellation before capture, between every pair of lookups, and immediately before commit;
- cancellation after commit returning nil;
- failure rollback preserving generation and bytes;
- retained old snapshots during repeated publication;
- generation overflow;
- synchronized fake mutation; and
- no mixed-generation result.

### Fuzzing

Fuzz Binding construction and capture validation with arbitrary strings, duplicate sets, counts, and sizes. Fuzz formatting and marshaling to ensure original values never appear. Fuzz refresh state transitions with a synchronized fake.

Dynamically generated secret fuzz values remain in memory and are not written to corpus files. Seed corpus values are conspicuously public.

### Examples

Executable examples use the fake unless they specifically demonstrate ProcessLookup. Expected output appears immediately after the call that produces it and contains only redaction or safe metadata.

## Compatibility

Before v1, freeze:

- environment-name grammar;
- fixed bounds;
- Binding constructor semantics;
- missing and empty behavior;
- Capture and Refresh commit points;
- generation behavior;
- Value disclosure and redaction behavior;
- stable error sentinels; and
- zero-value behavior.

Changing a requirement's interpretation, accepting an undeclared read, trimming values, exposing names through Binding formatting, weakening redaction, or reporting an error after refresh commit is a runtime compatibility break. Lowering fixed bounds is also incompatible for inputs previously accepted.

The package ships with the existing `env/v2` module version and release process. It does not create an independently tagged module. Adding the subpackage does not require increasing the module's Go version unless its implementation uses a feature unavailable at the current minimum; any increase must identify that exact constraint.

## Implementation Plan

### Phase 1: Core capture

Add opaque Binding declarations, validation, Lookup and ProcessLookup, immutable Snapshot, Store access, redacting Value, stable errors, fixed bounds, fake lookup, and executable examples.

### Phase 2: Refresh and hardening

Add transactional Refresh, generation metadata, race tests, cancellation tests, adversarial formatting, fuzzing, child-process documentation, and ProcessLookup integration coverage.

### Phase 3: Env repository integration

Update package documentation and the env repository's release surfaces. Document root env loading order and application-owned binding declarations. Do not add GoForj render metadata, a general Secrets driver, or cross-module release coordination.

## Acceptance Criteria

1. The package lives at `github.com/goforj/env/v2/envsecrets` in the existing env repository and module.
2. The primary DX is package-level or composition-local Binding declarations in Go.
3. No public Manifest, logical-name mapping, or YAML configuration is required.
4. Capture reads only validated declared bindings and never enumerates the environment.
5. Missing and present-empty remain distinct through capture and access.
6. Values and bindings redact across all ordinary formatting and serialization paths.
7. RevealBytes returns defensive copies and zero Value cannot impersonate empty.
8. Refresh publishes only complete generations and preserves the prior snapshot on pre-commit failure.
9. Cancellation after refresh commit returns nil.
10. Every size, count, and grammar bound has direct tests.
11. Race tests prove immutable snapshot and serialized refresh behavior.
12. The package neither loads dotenv files nor mutates process environment state.
13. GoForj has no environment-secret render or App configuration surface.
14. Neither library provides an envsecrets driver, bridge, revision mapping, or fallback into the other.
15. Documentation makes no zeroization, revocation, or managed-provider security claim.

## Risks And Mitigations

### Binding declarations duplicate `.env.example`

Mitigation: treat the Go declaration as the access policy and `.env.example` as deployment documentation. Do not generate either from the other until a concrete synchronization workflow proves useful and preserves explicit review.

### Developers may bypass the package with `os.Getenv`

Mitigation: make the safe path concise, document it in project guidance, and optionally add repository linting later. The runtime cannot prevent same-process code from reading the environment.

### Redaction may create false confidence

Mitigation: describe reveal methods as disclosure boundaries and document debugger, memory, child-process, and runtime-copy limitations prominently.

### In-process refresh may be mistaken for rotation

Mitigation: describe process replacement as the production default. Refresh only republishes captured bytes; it does not replace database pools, clients, signers, or other consumers that copied an older value.

### Fixed limits may reject an unusual legitimate value

Mitigation: choose conservative but practical bounds, collect real cases, and add a reviewed bounded option only when evidence justifies the API cost.

## References

- [Go `os.LookupEnv`](https://pkg.go.dev/os#LookupEnv)
- [Go `os.Environ`](https://pkg.go.dev/os#Environ)
- [Go `os/exec.Cmd.Env`](https://pkg.go.dev/os/exec#Cmd)
- [Kubernetes guidance for defining environment variables](https://kubernetes.io/docs/tasks/inject-data-application/define-environment-variable-container/)
- [Docker Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/)
