# Environment Secrets Library Design

## Status

- Design status: proposed
- Planning date: 2026-09-06
- Target repositories: a new `github.com/goforj/envsecrets` sibling repository, an optional bridge in the Secrets repository, and `goforj`
- Primary library scope: bounded manifest-declared secrets supplied through a process environment, immutable snapshots, explicit refresh, redacting values, observability, and test support
- Cross-repository source of truth: this design is normative until the new repository contains an accepted design or implementation plan that references it

## Summary

GoForj should add a small, domain-neutral `github.com/goforj/envsecrets` library for applications that intentionally receive secrets through process environment variables.

The library should not be another environment configuration package and should not be a managed secret-store client. Its job is narrower:

1. accept an explicit, bounded manifest of logical secret names and their exact environment-variable mappings;
2. read only those mappings through an injected lookup dependency;
3. preserve the distinction between a missing variable and an explicitly empty variable;
4. publish one immutable snapshot after the complete manifest has been read and validated;
5. return opaque values whose formatting and serialization redact by default;
6. make refresh an explicit, transactional operation; and
7. expose value-free operational events and deterministic test tools.

The root module must import neither `github.com/goforj/env/v2` nor the proposed general `github.com/goforj/secrets` core. An optional bridge module may import both `envsecrets` and `secrets`, but that bridge is a separate integration surface. Managed-store fallback must never happen inside the root library or implicitly inside the bridge.

## Decision

Create `github.com/goforj/envsecrets` as a standalone Go module with these rules:

1. A caller constructs a `Store` with a validated `Manifest` and an injected `Lookup`.
2. The process environment is available only through an explicitly selected `ProcessLookup`; package construction never reaches `os.LookupEnv` behind the caller's back.
3. Every accessible logical name must appear in the manifest. Arbitrary reads, wildcard bindings, prefix scans, environment enumeration, and a public list API are excluded.
4. A binding maps one logical name to one exact environment-variable name. A manifest prefix is optional and is applied mechanically to binding suffixes.
5. Logical names and environment names use a conservative ASCII grammar and fixed size and count limits.
6. Capture distinguishes missing, present-empty, and present-nonempty states. Each binding declares whether it is optional, must be present, or must be nonempty.
7. Construction captures the initial complete snapshot. A failed construction returns no usable store.
8. `Snapshot` is immutable. Reads do not consult the source again.
9. `Refresh` is explicit, serialized, and all-or-nothing. A failed refresh preserves the previously published snapshot.
10. A successful refresh publishes one new generation atomically. Existing snapshot handles continue to observe their old generation.
11. Secret values redact under `fmt`, structured logging, text marshaling, JSON marshaling, and Go-syntax formatting. Plaintext access requires an explicitly named reveal method.
12. Returned byte slices are copies. Input strings are copied into library-owned bytes during capture. The library documents that Go cannot guarantee zeroization or prevent caller-created copies.
13. Errors may identify logical names, environment names, limits, and states, but never include secret values.
14. Observer events contain operation metadata and aggregate counts only. They never contain a secret, environment-variable name, logical secret name, hash, length, or value-derived label.
15. The root library does not load dotenv files, mutate the process environment, spawn commands, sanitize child environments, read managed stores, parse application configuration, or infer secret names.
16. Tests use an in-memory lookup and a capture observer without mutating the process environment.
17. GoForj wiring loads dotenv configuration, if enabled, before constructing `envsecrets`. The generated application then passes `envsecrets.ProcessLookup{}` explicitly.
18. Missing environment secrets fail according to the manifest. They never trigger an implicit managed-store lookup.

## Why This Is A Separate Library

### It is not a replacement for `github.com/goforj/env/v2`

The current `env/v2` behavior was traced before this design was written:

- typed getters read the ambient process environment directly through `os.Getenv`;
- permissive getters generally treat missing and empty values alike and apply a fallback;
- `MustGet` rejects both missing and empty values by panicking;
- `Scope.ChildNames` discovers keys by enumerating `os.Environ`;
- `Load` and `Reload` discover dotenv files, parse layers, and mutate the process environment transactionally;
- the loader separately preserves unset versus explicitly empty process state; and
- `Dump` deliberately prints values without redaction.

Those behaviors are appropriate for general application configuration, but they are not the desired secret capability boundary. `envsecrets` does not provide typed configuration getters, application-environment helpers, runtime detection, dotenv loading, mutable scopes, fallbacks, or debug dumping.

An application should continue to use `env/v2` for ordinary configuration such as ports, timeouts, feature choices, hostnames, and development dotenv layering. It should use `envsecrets` when a value is intentionally classified as a secret and needs allowlisted access, redacting presentation, explicit availability policy, and snapshot semantics.

Using `envsecrets` does not make process environment delivery equivalent to a managed secret store. It makes a constrained and common delivery mechanism harder to misuse.

### It is not the general `github.com/goforj/secrets` core

The general `secrets` core should define provider-neutral secret retrieval and any managed-store concepts such as references, versions, leases, provider errors, or rotation metadata. `envsecrets` has none of those concepts. A process environment variable has no authenticated provider identity, remote version, lease, revocation protocol, or per-read authorization decision.

Keeping the modules independent provides useful dependency and trust boundaries:

- programs that only use process environment delivery do not pull in provider abstractions or SDKs;
- the general core does not need to standardize operating-system environment behavior;
- root-package users cannot accidentally enable a network fallback; and
- security review can treat process inheritance and managed-store access as different risks.

### Managed-store fallback must never be implicit

If `DATABASE_PASSWORD` is missing, silently reading a managed store changes more than availability. It introduces network access, credentials, provider authorization, latency, rate limits, billing, audit effects, retry behavior, and a second precedence rule. It can also hide a broken deployment, select an old value, or let a compromised environment redirect the intended source.

Therefore:

- `envsecrets` never imports or calls a managed secret provider;
- the optional bridge exposes explicit adapters, not a fallback chain;
- a caller that wants precedence composes sources in application code under a named policy;
- missing, empty, lookup-failure, and managed-provider failure remain distinguishable; and
- an environment value, including an explicitly empty accepted value, never causes an unrequested provider lookup.

## Goals

1. Make a small declared set of process-environment secrets safe to wire and straightforward to test.
2. Eliminate unavoidable package-global environment reads from the core behavior.
3. Preserve missing versus present-empty semantics from lookup through validation and access.
4. Prevent unreviewed secret-name discovery and unconstrained arbitrary reads.
5. Make accidental formatting, JSON encoding, and structured logging redact values by default.
6. Provide stable reads through immutable snapshots and atomic generation publication.
7. Make refresh deliberate, observable, and failure-safe.
8. Bound attacker-controlled names, per-value bytes, aggregate snapshot bytes, and manifest size before publication.
9. Keep the root module useful without GoForj and independent of other GoForj libraries.
10. Document the limits of environment-based secret delivery honestly.

## Non-goals

1. General environment configuration, typed parsing, defaults, or application-environment selection.
2. Dotenv parsing, discovery, precedence, writing, synchronization, or file watching.
3. Managed secret-store access, provider selection, remote fallback, caching, leasing, rotation, or revocation.
4. Environment-variable discovery, wildcard access, prefix enumeration, or secret-name inference.
5. Detecting whether arbitrary environment variables look secret.
6. Encrypting process memory or guaranteeing zeroization under the Go runtime.
7. Protecting a secret after caller code reveals, copies, logs, transmits, or persists it.
8. Preventing operating-system administrators, same-user debuggers, crash tooling, or compromised process code from reading memory or environment state.
9. Automatically removing secrets from child-process environments.
10. Parsing secret values as URLs, JSON, certificates, or application-specific credentials.
11. Global singleton registration or package-level `Get` helpers.
12. Dynamic addition or removal of bindings after construction.

## Terminology

### Logical name

A stable, domain-neutral name used by application code, such as `database.password` or `mail.api_token`. It is not automatically derived from an environment name.

### Environment name

The exact process environment-variable name read for a binding, such as `APP_DATABASE_PASSWORD`.

### Binding

A manifest entry that maps one logical name to one environment suffix or exact environment name and declares its requirement.

### Manifest

The immutable declaration from which the allowed lookup set is compiled. A manifest is configuration supplied at construction, not a live registry.

### Snapshot

One immutable generation containing the captured state of every manifest binding. It contains no method that refreshes itself.

### Store

The holder of the current snapshot, compiled manifest, lookup dependency, refresh serialization, and observer.

### Reveal

An operation that deliberately creates a caller-visible plaintext copy from an opaque `Value`.

## Public API

The proposed v1 root API is:

```go
package envsecrets

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	MaxBindings             = 256
	MaxLogicalNameBytes     = 128
	MaxEnvironmentNameBytes = 253
	DefaultMaxValueBytes    = 1 << 20
	MaxValueBytesLimit      = 16 << 20
	DefaultMaxSnapshotBytes = 16 << 20
	MaxSnapshotBytesLimit   = 64 << 20
)

var (
	ErrInvalid         = errors.New("envsecrets: invalid handle")
	ErrInvalidManifest = errors.New("envsecrets: invalid manifest")
	ErrUnknownName     = errors.New("envsecrets: unknown logical name")
	ErrMissing         = errors.New("envsecrets: secret is missing")
	ErrEmpty           = errors.New("envsecrets: secret is empty")
	ErrValueTooLarge   = errors.New("envsecrets: secret exceeds value limit")
	ErrSnapshotTooLarge = errors.New("envsecrets: snapshot exceeds aggregate limit")
	ErrLookup          = errors.New("envsecrets: lookup failed")
	ErrEntropy         = errors.New("envsecrets: identity generation failed")
	ErrGenerationExhausted = errors.New("envsecrets: generation exhausted")
	ErrInternal        = errors.New("envsecrets: internal failure")
)

type Requirement uint8

const (
	Optional Requirement = iota
	RequirePresent
	RequireNonEmpty
)

type Binding struct {
	Name        string
	Environment string
	Requirement Requirement
}

type Manifest struct {
	Prefix           string
	Bindings         []Binding
	MaxValueBytes    int
	MaxSnapshotBytes int
}

type Lookup interface {
	LookupEnv(ctx context.Context, name string) (value string, present bool, err error)
}

type LookupFunc func(ctx context.Context, name string) (value string, present bool, err error)

func (f LookupFunc) LookupEnv(ctx context.Context, name string) (string, bool, error)

type ProcessLookup struct{}

func (ProcessLookup) LookupEnv(ctx context.Context, name string) (string, bool, error)

type Observer interface {
	ObserveEnvSecrets(ctx context.Context, event Event)
}

type ObserverFunc func(ctx context.Context, event Event)

func (f ObserverFunc) ObserveEnvSecrets(ctx context.Context, event Event)

type Option func(*options) error

func WithObserver(observer Observer) Option
func WithClock(clock func() time.Time) Option

type Store struct {
	// unexported
}

func New(ctx context.Context, manifest Manifest, lookup Lookup, opts ...Option) (Store, error)
func (s Store) Identity() Identity
func (s Store) Snapshot() Snapshot
func (s Store) Refresh(ctx context.Context) error
func (s Store) String() string
func (s Store) GoString() string
func (s Store) Format(fmt.State, rune)

type Snapshot struct {
	// unexported
}

func (s Snapshot) Generation() uint64
func (s Snapshot) CapturedAt() time.Time
func (s Snapshot) Get(name string) (Value, error)
func (s Snapshot) Lookup(name string) (Value, bool, error)
func (s Snapshot) String() string
func (s Snapshot) GoString() string
func (s Snapshot) Format(fmt.State, rune)

type Identity struct {
	// unexported random per-Store identity
}

func (i Identity) Equal(other Identity) bool
func (i Identity) RevisionToken() ([]byte, error)
func (i Identity) String() string
func (i Identity) GoString() string
func (i Identity) Format(fmt.State, rune)

type Value struct {
	// unexported
}

func (v Value) Empty() bool
func (v Value) RevealBytes() ([]byte, error)
func (v Value) RevealString() (string, error)
func (v Value) String() string
func (v Value) GoString() string
func (v Value) MarshalText() ([]byte, error)
func (v Value) MarshalJSON() ([]byte, error)
func (v Value) Format(fmt.State, rune)

type Operation string

const (
	OperationOpen    Operation = "open"
	OperationRefresh Operation = "refresh"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

type ErrorKind string

const (
	ErrorNone                ErrorKind = ""
	ErrorInvalidManifest     ErrorKind = "invalid_manifest"
	ErrorMissing             ErrorKind = "missing"
	ErrorEmpty               ErrorKind = "empty"
	ErrorValueTooLarge       ErrorKind = "value_too_large"
	ErrorSnapshotTooLarge    ErrorKind = "snapshot_too_large"
	ErrorLookup              ErrorKind = "lookup"
	ErrorCanceled            ErrorKind = "canceled"
	ErrorDeadlineExceeded    ErrorKind = "deadline_exceeded"
	ErrorEntropy             ErrorKind = "entropy"
	ErrorGenerationExhausted ErrorKind = "generation_exhausted"
	ErrorInternal            ErrorKind = "internal"
)

type Event struct {
	Operation  Operation
	Outcome    Outcome
	Generation uint64
	Present    int
	Empty      int
	Missing    int
	Duration   time.Duration
	ErrorKind  ErrorKind
}
```

The API intentionally has no package-level default store, `Get`, `MustGet`, `Names`, `Range`, `All`, raw map, `Set`, or `Delete` function.

### Construction and nil dependencies

`New` validates and compiles the manifest before reading any values, checks cancellation before capture and before every binding lookup, captures every binding, applies requirements, checks cancellation again immediately before publication, and publishes generation 1 only after the entire capture succeeds. Cancellation at any check returns the context error and no Store, and no later binding is queried. The supplied `Lookup`, observer, and clock are constructor dependencies, not optional state checked on every read. A lookup supplied to `New`, or an observer or clock supplied through an option, must be non-nil. Nil collaborators are programmer wiring errors and have no supported degraded behavior. Production wiring must not rely on nil collaborators being silently ignored.

The zero value of `Store`, `Snapshot`, `Identity`, and `Value` is invalid. Formatting still redacts, while methods that can return errors use `ErrInvalid` rather than impersonating an intentionally present-empty value. Public documentation directs callers through `New` and `Snapshot.Get` or `Snapshot.Lookup`.

All four public handles are value-semantic wrappers over private immutable or synchronized state. `New` returns a Store value and `Store.Snapshot` returns a Snapshot value; callers do not need pointers to public handles. Non-error zero behavior is frozen: a zero Store returns a zero Snapshot and zero Identity; a zero Snapshot reports generation 0 and the zero time; a zero Value reports `Empty()==false`, which never proves validity; a zero Identity compares unequal to every identity including another zero and formats as `[INVALID]`. `Get`, `Lookup`, `Refresh`, `RevealBytes`, and `RevealString` return `ErrInvalid` for zero handles. Pointers to Store, Snapshot, Value, or Identity are outside the supported API and receive no nil-pointer guarantee because Go must dereference a nil pointer before invoking their value-receiver methods. This choice preserves safe formatting for both valid and zero values without claiming impossible nil behavior.

`WithClock` exists for deterministic metadata tests, not for expiry. The clock is read once per capture after all lookups and validation succeed. `CapturedAt` is informational and is not evidence that every environment value changed at that instant.

Construction creates one cryptographically random opaque `Identity`; entropy failure fails construction before lookup. `Identity.RevisionToken` returns a fresh copy of the fixed-length random identity bytes and is the only machine-readable adapter protocol; it returns `ErrInvalid` for a zero Identity. The token is not derived from a secret, environment name, manifest, or pointer address, but callers still must not log or persist it. `Identity.Equal` compares that same identity, while formatting returns only a process-keyed safe digest and never the token. Multiple adapters over one Store combine the token with Snapshot generation through an unambiguous length-delimited encoding to derive the same driver-private provider-revision component without a global registry. The Secrets root still adds source instance, binding, App, and tenant scope, so public Revision values remain unequal across any of those domains.

## Manifest Semantics

### Grammar and bounds

Logical names must match:

```text
[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*
```

They are case-sensitive, are not normalized, and may occupy at most `MaxLogicalNameBytes` bytes. Every segment follows the same rule: a leading lower-case letter followed by lower-case letters, digits, or underscores. This grammar supports readable names such as `database.primary_password` and rejects whitespace, path syntax, control characters, leading punctuation, and lookalike case variants.

`Prefix` and each binding's `Environment` must contain only uppercase ASCII letters, digits, and underscores, must start with an uppercase ASCII letter or underscore, and must not contain `=` or NUL. An empty prefix is valid. A nonempty prefix must end with `_`. The exact lookup name is `Prefix + Binding.Environment`. Canonical uppercase names avoid case-folding aliases on Windows and preserve one portable manifest meaning.

Requiring the separator in the prefix avoids hidden normalization. For example:

```go
Manifest{
	Prefix: "APP_",
	Bindings: []Binding{
		{Name: "database.password", Environment: "DATABASE_PASSWORD", Requirement: RequireNonEmpty},
		{Name: "mail.api_token", Environment: "MAIL_API_TOKEN", Requirement: Optional},
	},
}
```

maps exactly to `APP_DATABASE_PASSWORD` and `APP_MAIL_API_TOKEN`.

A manifest may contain at most `MaxBindings` entries. The final environment name may occupy at most `MaxEnvironmentNameBytes` bytes. Logical names and final canonical environment names must each be unique. Duplicate mappings are rejected because aliases make access and requirement reporting ambiguous. Bindings retain declaration order for deterministic lookup and failure behavior, but no public API exposes that order.

`Requirement` must be one of the three declared constants. Unknown numeric values are invalid. `MaxValueBytes` uses `DefaultMaxValueBytes` when zero, must be positive when set, and may not exceed `MaxValueBytesLimit`. `MaxSnapshotBytes` similarly defaults to `DefaultMaxSnapshotBytes` and cannot exceed `MaxSnapshotBytesLimit`. Capture checks the per-value bound and increments a checked aggregate before copying each present value. Exceeding either bound fails the whole candidate. Caller-retained prior snapshots remain outside the store's enforceable current-generation bound and are documented as such.

An empty binding list is valid and produces an empty snapshot. This supports component-disabled compositions without inventing dummy names, although GoForj should normally omit the component instead.

### No discovery

The manifest is the complete allowlist. Capture calls `LookupEnv` once for each compiled exact environment name. It never invokes `os.Environ`, scans a prefix, expands wildcards, or probes names inferred from logical names.

`Snapshot` does not expose names or entries. Callers already possess the manifest at composition time. Omitting listing makes it harder for generic diagnostics, template renderers, or reflection helpers to turn a secret capability into inventory disclosure. A future list API would require a separate security design and must return logical metadata only, never values or environment names.

## Missing And Explicitly Empty Values

The lookup tuple is interpreted exactly:

| Lookup result | Captured state | `Optional` | `RequirePresent` | `RequireNonEmpty` |
| --- | --- | --- | --- | --- |
| `present=false` | missing | accepted | `ErrMissing` | `ErrMissing` |
| `present=true, value=""` | present-empty | accepted | accepted | `ErrEmpty` |
| `present=true, value!=""` | present-nonempty | accepted | accepted | accepted |

The returned `value` must be ignored when `present` is false. A lookup implementation that returns a nonempty value with `present=false` is treated as missing, and the nonempty string is not retained.

`Snapshot.Get(name)` behaves as follows:

- an undeclared logical name returns an error matching `ErrUnknownName`;
- a declared but missing optional name returns an error matching `ErrMissing`;
- a present-empty name returns a valid `Value` for which `Empty()` is true; and
- a present-nonempty name returns a valid `Value` for which `Empty()` is false.

`Snapshot.Lookup(name)` separates optional absence without making unknown names look absent:

- an undeclared name returns `(Value{}, false, ErrUnknownName)`;
- a declared missing name returns `(Value{}, false, nil)`; and
- a declared present name, including present-empty, returns `(value, true, nil)`.

`RequirePresent` exists for protocols in which an explicit empty value has meaning. `RequireNonEmpty` is the normal policy for credentials. The library does not trim whitespace. A value containing spaces is nonempty because changing its bytes would corrupt valid secrets and would introduce application-specific policy.

There are no fallbacks. A caller may choose optional absence in domain wiring, but the library does not replace missing or empty secrets with literals.

## Snapshot And Refresh Model

### Why snapshot by default

Repeated ambient reads create inconsistent request behavior if another goroutine calls `os.Setenv`, if test code mutates globals, or if a launcher updates its own environment under mistaken assumptions. They also make it impossible to state which collection of values an application currently uses.

Construction therefore captures one immutable generation. Normal reads are map lookups against that generation and do not call the injected source. An application can pass a snapshot, or a narrower interface backed by it, to the components that need secrets.

### Explicit refresh

`Store.Refresh(ctx)` is included for controlled environments that deliberately mutate the current process environment or use a test lookup. It must not be presented as a complete rotation system. Most orchestrators replace a process to rotate environment-delivered credentials, which remains the preferred deployment model.

Refresh semantics are:

1. acquire serialized refresh admission through a context-aware gate so a canceled queued caller can return without waiting for the active refresh;
2. read every binding in manifest order through the same lookup supplied at construction;
3. stop at the first lookup, size, context, or requirement error;
4. discard the candidate generation on any error;
5. preserve the current snapshot exactly on failure;
6. assign the next generation only after successful complete capture; and
7. publish the new snapshot through an atomic pointer swap.

The atomic swap is the Refresh commit point. Cancellation observed before it returns the context error and preserves the prior snapshot. Once the swap occurs, Refresh returns nil regardless of cancellation that arrives during or after observation; the generation is committed and is never reported as a failed refresh. The observer still receives the operation context and should stop promptly when canceled, but its completion, panic recovery, or cancellation cannot change the committed result. This avoids error-driven retries that would publish an unintended extra generation and makes successful Refresh a reliable bridge visibility barrier.

`Snapshot()` is lock-free after successful construction. It returns a value handle containing the immutable state pointer current at the instant of the atomic load. A concurrent refresh may complete immediately before or after that load; either complete generation is valid. There is no mixed generation.

Generation numbers begin at 1 and increase only on successful refresh, including a successful refresh whose bytes equal the prior generation. This makes every deliberate capture observable without comparing secret values. Overflow is practically unreachable; if generation is `math.MaxUint64`, refresh returns a non-value-bearing internal error and preserves the current snapshot.

The refresh context controls source calls and observer correlation. `ProcessLookup` checks `ctx.Err()` before reading. Refresh rechecks cancellation before capture, before every binding lookup, and immediately before publication. Cancellation before commit stops before the next lookup or swap, discards the candidate, and preserves the current generation. Observer execution occurs after publication or failure reporting and never holds refresh admission. Releasing admission before the callback means observer calls from the same Store may overlap and may complete out of generation order; observer implementations must be concurrency-safe and use the event generation rather than callback order. The observer receives the operation context and is contractually non-blocking; a compliant observer returns when that context is canceled. A misbehaving injected observer can delay its caller, as with any synchronous callback, but cannot block admission for later refreshes, roll back a published generation, or change a committed nil result.

Existing snapshot handles remain valid after refresh. The library does not mutate or wipe them because doing so would violate immutability and race with readers. Applications that require immediate revocation cannot achieve it by environment snapshot refresh and should use a provider with revocation semantics plus application-level connection replacement.

## Lookup Contract

`Lookup` is narrow so tests and specialized launchers can inject deterministic behavior. Implementations must:

- honor context cancellation when their work can block;
- return the exact unmodified value;
- set `present=true` for an explicitly empty variable;
- avoid logging values; and
- be safe for the library's sequential calls and any calls made by other stores.

`ProcessLookup.LookupEnv` is the only root-package implementation that imports `os`. It is stateless, calls `os.LookupEnv`, and cannot receive a lookup error because the standard-library call has no error result. It never calls `os.Environ` and never caches.

Making `ProcessLookup{}` explicit at the call site preserves testability and shows the ambient capability in dependency wiring:

```go
secretStore, err := envsecrets.New(
	ctx,
	secretManifest,
	envsecrets.ProcessLookup{},
)
if err != nil {
	return fmt.Errorf("open environment secrets: %w", err)
}
```

The package does not expose a `DefaultLookup` variable. Tests must not replace function globals.

## Opaque Values, Copies, And Lifetime

### Redacting presentation

`Value` stores unexported bytes. Public `Store` and `Snapshot` handles contain only a private pointer to internal state, so their value-receiver formatting methods are safe and do not copy synchronization primitives. `Value`, `Snapshot`, and `Store`, including pointers and values nested inside other structs, implement safe `String`, `GoString`, and `fmt.Formatter` behavior. `Value.MarshalText` and `Value.MarshalJSON` return or encode the literal `[REDACTED]`. They do so for both empty and nonempty values so formatting does not disclose either content or length. No formatting verb reveals content. Generated App containers implement safe formatting without recursively exposing secret-retaining internals.

Redaction is defense in depth, not a guarantee. Reflection, unsafe code, a debugger, memory capture, or a caller that invokes a reveal method can expose plaintext. A structured logger may inspect objects outside normal formatting contracts, so application logging policy must still reject secret-bearing values.

JSON and text marshaling return the redaction marker instead of an error. This keeps diagnostic serialization from failing open through a caller's fallback encoder. The documentation must warn that a redacted `Value` is not a useful persistence or transport representation.

### Reveal methods and copying

`Value` carries a private validity marker distinct from its byte length. `RevealBytes` returns a newly allocated copy on every valid call. Mutating that slice cannot change the snapshot. `RevealString` creates a new plaintext string from valid snapshot bytes. Both return an invalid-handle error for a zero `Value`, so caller mistakes cannot convert missing data into an accepted empty secret. Go strings are immutable but are not erasable, so callers should prefer bytes when an accepting API supports them.

Lookup returns strings because `os.LookupEnv` does. Capture converts each accepted string to a new byte slice and retains only that owned slice in the candidate snapshot. It does not retain a caller-owned mutable byte slice. Implementations must avoid intermediate conversions where practical, but the design does not claim that the runtime, operating system, lookup implementation, compiler, or garbage collector removes all prior copies.

Each `Value` refers to immutable snapshot-owned storage internally. Returning `Value` by value does not expose that storage. A snapshot and its secret bytes remain live as long as the store, a snapshot handle, or any returned `Value` retains them. Refresh does not shorten the lifetime of previously returned generations.

V1 has no `Destroy` or `Close` method. Best-effort wiping would give a misleading guarantee while aliases, strings, old snapshots, stack copies, runtime copies, and concurrent readers may remain. Documentation should recommend minimizing snapshot and revealed-copy lifetimes, avoiding core dumps where appropriate, and replacing the process for strong lifecycle transitions.

## Errors

Errors use stable sentinel classification with typed context:

```go
type ManifestError struct {
	Field string
	Index int
	Name  string
	Cause error
}

func (e *ManifestError) Error() string
func (e *ManifestError) Unwrap() error

type AccessError struct {
	Name        string
	Environment string
	kind        error
}

func (e *AccessError) Error() string
func (e *AccessError) Unwrap() error
func (e *AccessError) GoString() string
func (e *AccessError) Format(fmt.State, rune)
```

Manifest errors match `ErrInvalidManifest`. Access errors wrap one of `ErrUnknownName`, `ErrMissing`, `ErrEmpty`, `ErrValueTooLarge`, `ErrSnapshotTooLarge`, `ErrLookup`, or a context error as appropriate. Identity creation, generation exhaustion, invalid Store use, and unexpected internal failures match their dedicated stable sentinels without retaining source data. Input length is rejected before grammar parsing or lookup. Errors may include a logical and exact environment name only after the logical name matches a declared binding because operators need to repair wiring. Invalid or unknown caller input uses a fixed label and leaves both exported name fields empty. They must not include a value, value prefix, value length for ordinary failures, hash, provider response body, or a formatted `Value` supplied by lookup.

A size error may include the configured limit but not the observed length. Lookup failures are classified as `ErrLookup`, but the arbitrary source error is never stored in, wrapped by, observed by, or returned from the library because it may contain a value. `AccessError.Unwrap` returns only the stable library sentinel or context sentinel held in `kind`. `AccessError.Error`, `GoString`, and `Format` use only bounded library fields. Library examples log only library classification and declared names.

No root API panics for missing or empty values. Invalid construction and refresh return errors. Programmer errors in unsupported zero values are documented rather than converted into secret-bearing panics.

## Concurrency

After `New` succeeds:

- `Store.Snapshot`, all `Snapshot` methods, all `Value` methods, and formatting are safe for concurrent use;
- `Store.Refresh` is safe for concurrent calls and serializes complete attempts;
- readers never block on lookup or observer work;
- only complete immutable snapshots are published;
- the manifest is deep-copied during construction, so later caller mutation has no effect; and
- observer implementations are responsible for their own concurrency because observations from both the same Store and different stores may overlap and complete out of generation order.

The library controls its own state but cannot make arbitrary concurrent process-environment mutation semantically atomic. Go's environment functions are concurrency-safe, but reading several names across separate calls cannot obtain an operating-system transaction. A caller that changes multiple source names during capture can produce a generation containing values from different mutation moments. Deployment should prefer process replacement. Tests that mutate a fake across capture must coordinate through the fake.

## Process Environment Security Limitations

Environment delivery has risks the library cannot remove:

- secrets are normally present from process start and may be readable by privileged operators, same-user tooling, crash handlers, platform diagnostics, or compromised dependencies;
- environment limits and encoding rules vary by operating system and launcher;
- the environment does not provide provider authenticity, freshness, version, lease, revocation, or rotation acknowledgement;
- environment names can reveal system topology even when values are redacted;
- a secret exposed through a reveal method becomes ordinary application memory; and
- a process snapshot is not a hardware-backed or locked-memory secret container.

The library improves access discipline inside cooperative application code. It is not a sandbox and must not be marketed as making environment variables intrinsically secure.

### Child-process inheritance

Children commonly inherit the parent's entire environment when `exec.Cmd.Env` is nil. Merely reading a secret through `envsecrets` does not remove it from the ambient environment, so an unrelated subprocess may receive every launcher-supplied credential.

GoForj and library documentation must require explicit child environments at trust boundaries:

```go
cmd := exec.CommandContext(ctx, executable, args...)
cmd.Env = []string{
	"PATH=" + safePath,
	"LANG=C.UTF-8",
}
```

Applications should construct an allowlist appropriate to the child. Filtering by secret-looking name is insufficient because classification is incomplete. `envsecrets` does not modify `exec.Cmd`, expose an environment sanitizer, or unset captured variables. Unsetting after capture is not a reliable default because other application code may require the variables, concurrent reads can race, operating-system copies may remain, and old child construction may already have captured an environment.

## Dotenv Non-ownership

`envsecrets` does not read `.env`, search ancestor directories, parse dotenv syntax, select `APP_ENV`, watch files, or mutate `os.Environ`. A deployment or application may populate the process environment before construction. That population is outside this library.

In GoForj development, the existing `env/v2` loader remains the dotenv owner. Startup order is explicit:

1. load ordinary environment files through the generated environment lifecycle;
2. finish any process-environment mutation;
3. construct `envsecrets` with `ProcessLookup{}`; and
4. wire snapshots or narrow secret accessors into consumers.

Calling `env.Reload` after constructing `envsecrets` does not alter an existing secret snapshot. The application must deliberately call `secretStore.Refresh` after a successful environment reload if it accepts the non-atomic cross-library transition. GoForj should not automate this coupling in v1.

Dotenv files containing real secrets remain subject to the existing repository contract: local `.env` is ignored, committed examples redact secrets, and testing values must be conspicuously public. This library does not make a committed dotenv secret safe.

## Observability

Observability must help diagnose availability without creating a second disclosure path.

The observer receives exactly one event for each construction capture after options have established an observer and for each Refresh attempt on a valid Store. Successful open reports generation 1. Failed open reports generation 0. Failed refresh reports the still-current generation. Calling Refresh on a zero invalid Store returns `ErrInvalid` without an event because no observer-bearing state exists. Counts reflect captured states only when safely known; if lookup stops early, aggregate counts are zero to avoid presenting a partial candidate as meaningful inventory.

On a successful capture, `Present` counts all bindings for which Lookup returned `present=true`, including present-empty values; `Empty` is the subset of Present whose value length is zero; and `Missing` counts `present=false`. Therefore `Present + Missing` equals the manifest binding count and `Empty <= Present`. Failed partial captures report all three as zero as specified above. These inclusion rules are v1 compatibility and receive direct empty, nonempty, missing, and mixed tests.

Events must not contain:

- logical names or environment names;
- secret values, lengths, hashes, fingerprints, encodings, or prefixes;
- manifest contents;
- per-binding labels; or
- arbitrary lookup source errors or their messages.

`Event.ErrorKind` is a closed bounded classification and is not an `error`. Caller-facing `AccessError` values, their logical or environment names, and their unwrap chains never enter an event. Metrics adapters should use bounded labels such as operation and outcome only. Logs may include generation, operation, duration, aggregate counts, and error classification. Traces must not attach manifest names or values.

Successful events use `ErrorNone`. Manifest validation, required missing or empty values, per-value and aggregate bounds, injected lookup failure, cancellation, deadline, identity entropy failure, generation exhaustion, and an unexpected internal failure map to the corresponding declared constant. Unknown lookup failures always use `ErrorLookup`; other unexpected failures match `ErrInternal` and use `ErrorInternal`. Observer panic recovery does not rewrite a completed operation event into failure.

Observer panics are recovered so telemetry cannot corrupt publication or turn a successful refresh into an application error. The panic is not re-panicked. A future internal diagnostic hook may count observer panics, but v1 should keep the observer path simple. Observer calls are synchronous after completion so tests are deterministic; production observers must return promptly.

The library emits nothing by default. It has no logger, metrics SDK, OpenTelemetry dependency, or global callback.

## Fake And Test Tools

The root module should include `envsecrets/fake`, which imports only the root module and the standard library.

Proposed API:

```go
package fake

type Lookup struct {
	// unexported, concurrency-safe
}

func NewLookup(values map[string]string) *Lookup
func (l *Lookup) LookupEnv(ctx context.Context, name string) (string, bool, error)
func (l *Lookup) Set(name, value string)
func (l *Lookup) SetEmpty(name string)
func (l *Lookup) Unset(name string)
func (l *Lookup) Fail(name string, err error)
func (l *Lookup) ClearFailure(name string)
func (l *Lookup) Calls() []string
func (l *Lookup) ResetCalls()
func (l *Lookup) String() string
func (l *Lookup) GoString() string
func (l *Lookup) Format(fmt.State, rune)
func (l *Lookup) MarshalText() ([]byte, error)
func (l *Lookup) MarshalJSON() ([]byte, error)

type Observer struct {
	// unexported, concurrency-safe
}

func (o *Observer) ObserveEnvSecrets(ctx context.Context, event envsecrets.Event)
func (o *Observer) Events() []envsecrets.Event
func (o *Observer) Reset()
```

`NewLookup` copies its input. Returned call and event slices are copies. `SetEmpty` and `Unset` are distinct. `Fail` accepts a non-nil error and affects only the exact name; callers should still use value-free errors, but the fake's formatting and marshaling never traverse the injected error. Calls preserve actual lookup order. The fake does not infer prefixes or validate names because the root manifest owns that behavior. Every public fake object that retains values, names, events, or injected failures satisfies the same safe formatting and serialization contract as the root handles, including pointers, nesting, and every formatting verb.

Examples should never place realistic credentials in source. Use conspicuous values such as `public-test-database-password` and avoid printing reveals. Tests may compare revealed bytes directly and must report only mismatch location, not got or want plaintext.

No helper should call `t.Setenv` behind the caller's back. Integration tests for `ProcessLookup` may use `t.Setenv` on unique names and must not run those mutations in parallel.

## Optional Bridge Module

If the general `github.com/goforj/secrets` contract benefits from environment-backed adapters, add a separate nested module in that provider ecosystem:

```text
github.com/goforj/secrets/driver/envsecrets
```

Only that module may import both `github.com/goforj/envsecrets` and `github.com/goforj/secrets`. The root module's `go.mod` must never acquire the general core dependency.

The bridge accepts a narrow provider of `Identity()` and `Snapshot()` implemented by `envsecrets.Store`. It reads the current snapshot at the start of each request but never triggers `Refresh` itself. Its immutable mapping is from each `secrets.Key` to one declared `envsecrets` logical name; environment-variable names remain solely inside the original manifest. The bridge must not construct `ProcessLookup`, load dotenv files, own refresh policy, own provider credentials, or compose precedence. The adapter maps:

- declared present values to provider-neutral values through copies;
- declared missing values to the core's not-found classification;
- root errors to stable core classifications without copying secret content into messages.

Adapter construction validates every mapped environment logical name against the Store's immutable manifest by using the manifest-aware Snapshot lookup contract, which distinguishes declared-missing from unknown. Its constructor consumes lower errors without formatting or wrapping and returns a fixed error containing at most the already-validated general Secrets key and stable construction class. The environment logical name, exact variable name, manifest, and lower AccessError never appear in formatting, fields, unwrap chains, observations, or panic paths. An unknown mapping fails binding or catalog construction before a Secrets client is published. A bound source therefore holds one already-validated environment logical name and receives only a selector at read time. If private state corruption somehow produces `ErrUnknownName` later, the adapter returns non-retryable `SourceInvalidResponse`; it never reclassifies a configuration defect as provider not-found.

The returned record declares byte payload capability because a Go environment string may contain bytes that are not valid UTF-8. It supplies no freshness timestamp; the Secrets root stamps `FetchedAt` and its internal monotonic validation instant when the adapter read completes. Snapshot `CapturedAt` remains environment-library metadata and never enters the general Secrets freshness clock.

Any mismatch in semantics must remain explicit. The bridge supports current selection only. It creates a provider-opaque revision from a random per-Store identity plus the snapshot generation, allowing equality and refresh detection inside the process without hashing secret bytes or claiming an external source version. Revision formatting uses the Secrets core's process-keyed safe representation. The same environment bytes recaptured into a new generation intentionally produce a different revision because the revision represents capture generation, not content identity.

The bridge declares `CacheAllowed=false`, `CoalescingAllowed=false`, and only lazy startup support through the Secrets driver capability contract because the environment snapshot already supplies immutable capture and publication but the bridge owns no freshness or refresh lifecycle. Secrets catalog construction and GoForj generation reject an outer cache, coalescing policy, `startup: required`, or `startup: readiness` binding. Required environment values are validated by `envsecrets.New` before the lazy bridge is published. After `Store.Refresh` returns successfully, a new bridge read obtains the newly current snapshot even if a read against the old generation is still blocked; that old read completes against the generation it already acquired. Applications that need stronger consumer replacement coordinate that above both libraries.

The bridge must not provide `EnvironmentThenManaged`, `ManagedThenEnvironment`, or similarly implicit chains. An application that wants multiple sources constructs them under an application-owned name such as `CredentialPolicy` and specifies exact precedence, failure behavior, and observability. That composition should have tests for missing, empty, source failure, and conflicting availability.

The bridge is optional and should not be released until the general core's accepted API exists. Its versioning and release tag are independent because it is a nested module in the Secrets repository.

## GoForj Wiring

### Component shape

GoForj should treat environment secret access as a small composition capability, not an infrastructure driver. It provisions no container, network service, CLI login, or health probe.

Generated environment-secret ownership is per App and has no inheritance. Component metadata uses the same explicit `apps` shape as the general Secrets component:

```yaml
render:
  components: [web_api, env_secrets]
  env_secrets:
    mode: direct
    prefix: ORDERS_
    bindings:
      database.password:
        environment: DATABASE_PASSWORD
        requirement: require_non_empty
apps:
  worker:
    components: [jobs, env_secrets]
    env_secrets:
      mode: direct
      prefix: WORKER_
      bindings:
        queue.token:
          environment: QUEUE_TOKEN
          requirement: require_present
```

`env_secrets` is the component identifier. The default App uses `render.components` plus `render.env_secrets`, matching the repository's existing default-App persistence. Each additional named App uses `apps.<name>.components` plus `apps.<name>.env_secrets`. Both locations carry the same typed component shape after App normalization, and configuration does not inherit, merge, or override another App. `mode` is required and is either `direct` or `secrets_driver`. Every binding requires an explicit `environment` and `requirement`; generation never relies on the root API's zero-value `Optional` behavior. The accepted YAML requirement values map exactly to `Optional`, `RequirePresent`, and `RequireNonEmpty`. An App without the component receives no environment-secrets capability. Duplicate names, missing requirement fields, a block without component selection, and a selected component without its required block fail generation. Each App gets its own Store, Identity, and manifest, even when two Apps intentionally map the same environment name. Direct tests prove that the default App cannot request a named App's logical binding and that every generated requirement mode has the documented missing and empty behavior.

`render.env_secrets` and `apps.<name>.env_secrets` are the only persisted configuration locations for the default and additional Apps respectively. A top-level `env_secrets` key and nested `render.env_secrets.apps` are rejected by strict decoding. Render tests accept both canonical locations and reject the alternatives so direct and bridge generation cannot diverge.

In `direct` mode, generated wiring may inject that App's Store or Snapshot as described below, and a Secrets environment source may not reference it. In `secrets_driver` mode, the Store is private manager state supplied only to that same App's `github.com/goforj/secrets/driver/envsecrets` adapter; direct Store, Snapshot, and arbitrary-name accessors are omitted. The same App's Secrets binding supplies an explicit `env_secret` logical name and generation verifies it against the App-local manifest. Every App reference creates a distinct adapter over its own Store and Identity. Missing same-App component selection or configuration, cross-App mapping, or mixed direct and bridge exposure fails generation.

Bridge-mode construction adds a mandatory error translation boundary around `envsecrets.New`. It consumes the lower-level AccessError without formatting or wrapping it and returns a fixed value-free generated construction error carrying only the App-safe component name and stable error class. It may include a declared general `secrets.Key` only when generation has established one unambiguous reverse mapping; otherwise it reports only bounded aggregate failure counts. Environment-name exclusion is a provenance rule: no environment logical-name field, exact variable-name field, manifest, or lower error is copied or formatted into the result. Identical bytes originating independently from a permitted general key or fixed class do not violate the rule. The lower-level error is never an unwrap target, log field, trace attribute, Lighthouse detail, or panic value. Direct mode retains the root envsecrets operator contract that may identify an exact declared environment name. Tests force missing, empty, oversize, lookup, and cancellation failures with distinct canary names and search every returned error, unwrap chain, log, observation, and generated diagnostic. Collision fixtures then use environment names equal to permitted keys and classes and assert structured fields and unwrap provenance rather than impossible substring absence.

Generated wiring should, for each configured App, perform steps 1 through 4 and 7 in either mode; bridge mode applies the name-free translation boundary to step 4, and steps 5 and 6 apply only to `direct` mode:

1. render a project-owned manifest from explicit component metadata;
2. complete the existing environment load before secret construction;
3. call `envsecrets.New(ctx, manifest, envsecrets.ProcessLookup{}, options...)`;
4. fail application construction on required missing, empty, oversized, or lookup errors;
5. inject `envsecrets.Store` only into application composition that owns refresh policy;
6. inject `envsecrets.Snapshot` or narrower domain accessors into ordinary consumers; and
7. connect a value-free observer adapter when Metrics or Lighthouse integration is enabled.

When `envsecrets` is used directly, the generated App should not expose a global arbitrary-name getter. A generated domain package can define a narrow interface:

```go
type SigningKeySource interface {
	SigningKey() ([]byte, error)
}
```

Its adapter retrieves the declared logical name from a snapshot and returns a copy. This keeps domain code unaware of environment-variable naming and prevents arbitrary name access from spreading through the application.

When the optional bridge is selected as a driver of the general Secrets component, the generated application follows the Secrets design's `App.Secrets()` reader contract. The bridge still permits only keys in its immutable mapping. These are distinct composition modes, not two APIs for the same generated component.

### Manifest source of truth

GoForj component metadata should distinguish ordinary configuration from secret bindings. The renderer writes exact logical and environment mappings into generated Go code. It does not scan `.env`, classify names by suffix, or generate a manifest from the ambient environment.

The existing `.env.example` and `.env.testing` synchronization remains authoritative for development file inventory and safe public test values. The manifest is an application access contract, not a replacement for those files. Generation should cross-check that rendered secret environment names appear in the environment contract, while preserving existing redaction rules. As a narrow test-only exception, environment-driver integration fixtures may place conspicuously public marker values in `.env.testing`; production-like credentials and realistic secret material remain forbidden there.

Generated secret manifests must be deterministic, deduplicated, and bounded at generation time, then validated again by the library at runtime. Named App components should use explicit mapping prefixes from generation metadata rather than runtime string discovery.

### Lifecycle

The default GoForj application captures secrets once during startup. It does not install a signal handler, poll the environment, or refresh after a timer. In `direct` mode, startup failure identifies the declared environment logical name and exact environment-variable name but never the value. In `secrets_driver` mode, the mandatory bridge boundary reports neither name, as specified above.

If an application explicitly enables refresh, application composition owns when it occurs and how dependent resources are replaced. Publishing a new snapshot does not retroactively update a database connection, HTTP client, signer, or worker that already copied old bytes. Refresh success therefore means only that a new snapshot is available, not that the whole application rotated credentials.

Shutdown performs no secret-store close because the root store owns no goroutines or external resources.

### Lighthouse and metrics

Generated integration may record operation, outcome, duration, generation, and aggregate state counts. It must not record names, mappings, values, sizes, hashes, or manifest entries. Lighthouse views must not offer reveal actions. Secret access itself should not emit an event per `Get`, which would create high-cardinality access patterns and presence side channels.

## Compatibility

### Root library v1

The following are public compatibility commitments:

- logical-name and environment-name grammar;
- manifest bounds and prefix concatenation;
- requirement state table;
- error sentinel meanings and `errors.Is` behavior;
- immutable generation and refresh publication semantics;
- redaction marker and marshaling behavior;
- returned-copy ownership;
- observer field meanings; and
- absence of source discovery or fallback.

Changing a grammar to reject a previously accepted manifest is a source/runtime compatibility risk. Lowering bounds is incompatible. Changing empty treatment or implicit trimming is a runtime behavior break. Exposing values through new formatting or marshaling is a security break even if source-compatible.

New requirement modes or optional observer fields may be added compatibly when zero-value behavior remains stable. New listing, mutable access, automatic refresh, or fallback behavior requires a new design and must not appear as a convenience addition.

The module should use semantic import versioning for any future v2. It should choose the lowest supported Go version consistent with required standard-library features and must not raise that minimum for convenience.

### Relationship to `env/v2`

No existing `env/v2` API changes. There is no automatic migration from `env.MustGet("TOKEN")` because changing from ambient per-call reads and panics to snapshots and returned errors is an application design change.

Migration is additive:

1. keep ordinary configuration in `env/v2`;
2. declare secret mappings in an `envsecrets.Manifest`;
3. construct the store after any dotenv load;
4. inject snapshots or narrow adapters; and
5. remove direct secret reads only after consumer tests cover missing, empty, and reveal lifetimes.

Applications may use both modules indefinitely. Neither module should re-export the other.

## Testing Plan

### Unit tests

The root module must directly test:

- every logical-name, prefix, suffix, final-name, duplicate, count, requirement, and value-size validation branch;
- zero, default, maximum, and over-maximum bounds;
- missing, present-empty, and present-nonempty under every requirement;
- ignored nonempty lookup output when `present=false`;
- exact manifest-order lookup and no undeclared calls;
- initial capture success and failure;
- unknown versus missing behavior for `Get` and `Lookup`;
- copied manifests, lookup strings, returned bytes, fake inputs, fake outputs, and event slices;
- redaction under `%s`, `%v`, `%+v`, `%#v`, JSON, text marshaling, and common structured-logging paths that honor standard interfaces for root handles, fake lookups, fake observers, pointers, and nested objects;
- reveal correctness for empty, UTF-8, arbitrary non-NUL environment content, and maximum-sized values;
- every method on zero Value, Snapshot, Store, and Identity handles, proving supported invalid handles never panic or impersonate present-empty values;
- direct tests documenting that nil pointers to all four value-semantic handle types are outside the supported API rather than promising impossible receiver behavior;
- refresh success, unchanged-value refresh, failure rollback, cancellation before commit, cancellation after publication returning nil, generation increments, and overflow handling;
- construction cancellation before capture, between every pair of lookups, and immediately before publication with no returned Store or later lookup;
- observer success and failure events, aggregate counts, closed error kinds, observer panic recovery, and absence of names, caller errors, unwrap targets, or values;
- aggregate snapshot bytes at, below, and above the configured limit, including coexistence with caller-retained old generations;
- uppercase environment-name enforcement and Windows case-folding collision fixtures;
- typed error fields and every required `errors.Is` and `errors.As` classification, proving arbitrary lookup errors never appear anywhere in the returned unwrap graph; and
- invalid and unknown names containing control characters or credential-like fixture text without raw error or observation disclosure; and
- `ProcessLookup` preservation of missing versus explicit empty.

Tests must never include plaintext values in failure formatting. Helper assertions compare bytes but report binding index or logical test-case name only.

### Race and concurrency tests

Run `go test -race ./...` with tests covering:

- many readers on one snapshot;
- readers concurrent with repeated successful and failed refreshes;
- serialized concurrent refresh attempts;
- a canceled refresher waiting for admission, cancellation between lookups, cancellation immediately before publication, and cancellation after the atomic swap;
- retained old generations during publication;
- a context-aware blocked observer proving publication and later refresh admission are not held by the callback;
- fake mutation under its documented synchronization; and
- overlapping same-Store observer callbacks, out-of-order completion identified by generation, and concurrent observer access.

Bridge tests construct multiple adapters over one Store and prove copied RevisionToken bytes plus generation produce the same driver-private component without using Identity formatting or a global registry, while public Secrets revisions remain unequal across sources, bindings, Apps, and tenants. They prove a zero Identity token fails safely and mutation of a returned token cannot change Store identity. They prove an undeclared mapped environment logical name fails adapter construction through a fixed error whose formatting and unwrap chain exclude the offending environment logical name and lower AccessError, while a declared optional-missing name binds successfully and later maps to not-found. A blocked read against generation N, a successful refresh to generation N+1, and a second read must prove that the second read observes N+1 without joining the old operation. Tests also verify invalid UTF-8 byte preservation, root-stamped `FetchedAt` independence from Snapshot `CapturedAt`, and rejection of both required and readiness startup configuration.

Tests assert that each observed set belongs to one complete generation. They do not mutate the real process environment concurrently.

### Fuzzing

Fuzz manifest validation with arbitrary strings and sizes to ensure no panic, bypass of final-name limits, duplicate normalization, or accidental lookup before validation. Fuzz formatting and marshaling to ensure the original value never appears. Fuzz state transitions across fake lookup mutations and refresh failure points.

Secret fuzz inputs must remain in memory and must not be written to corpus files when generated dynamically. Seed corpus values must be conspicuously public fixtures.

### Integration and example tests

Executable examples should show explicit `ProcessLookup` injection or use the fake. Expected output must be placed immediately after the producing call and must display only redaction or metadata. A subprocess integration test should demonstrate that an unfiltered child inherits a marker and that an explicitly constructed allowlist does not, using public fixture text only.

If the bridge is released, it needs contract tests against the exact supported `secrets` version, including classification mapping and proof that it performs no fallback. GoForj render tests must validate the largest supported generated composition and compile it with every relevant nested module under `GOWORK=off` when published dependencies are being verified.

## Release Plan

### Phase 1: root module

1. Create the `github.com/goforj/envsecrets` repository with license, security policy, Go module, package docs, manifest validation, capture, snapshot, value, observer, and fake packages.
2. Pin the minimum Go version to the oldest version required by the implementation, not the newest locally installed version.
3. Add unit, race, fuzz-smoke, vet, static analysis, dependency review, vulnerability scanning, and secret scanning workflows.
4. Publish an initial prerelease such as `v0.1.0` for API evaluation.
5. Exercise it in a small standalone program and a GoForj integration branch.
6. Resolve naming and semantic feedback before `v1.0.0`.

The v1 release gate requires complete tests for every validation and failure branch, race-clean refresh publication, executable examples, stable redaction tests, no root dependency on `env/v2` or `secrets`, and documentation that states the security limitations prominently.

### Phase 2: GoForj integration

1. Add pinned root-module dependency surfaces to every relevant GoForj module, template module, render-warm module, and fixture.
2. Add explicit manifest metadata and generated composition.
3. Preserve existing dotenv generation and loading ownership.
4. Add startup-failure, disabled-component, named-component, Metrics, Lighthouse, and largest-composition render tests.
5. Render only in a temporary directory, run relevant module tests independently, and validate published resolution with `GOWORK=off`.
6. Document migration from direct `env.MustGet` reads without presenting `envsecrets` as an `env/v2` replacement.

GoForj integration should begin against a tagged root release, not an unpublished local replacement. Intentional sibling-module `replace` directives used by repository tests may remain where established, but release validation must prove the selected public module version resolves independently.

### Phase 3: optional bridge

After the general `secrets` core has an accepted and tagged v1 contract:

1. implement the bridge as its own nested module;
2. test exact error and copy semantics against both dependencies;
3. verify that the bridge has no source-chain or fallback API;
4. tag the nested module using its full module tag convention; and
5. document explicit application-owned composition separately.

The root module release must not wait for the bridge. A root tag does not prove that the nested bridge module is published, so tags and availability must be checked independently.

## Security Review Checklist

Before v1, reviewers should verify:

- only manifest-derived exact names reach lookup;
- validation completes before the first lookup;
- no API enumerates the environment or snapshot;
- missing and empty remain distinct at every boundary;
- every failure path excludes secret bytes;
- all standard formatting and marshaling paths redact for values, snapshots, stores, pointers, nested structs, and generated containers;
- copies cannot mutate snapshot storage;
- failed refresh cannot publish partial state;
- old snapshots are documented as remaining live;
- observer data is bounded and value-free;
- child inheritance is documented with an allowlist example;
- dotenv and managed-store ownership remain outside the root module;
- dependency graphs confirm that root imports neither `env/v2` nor `secrets`; and
- examples, tests, CI output, and fuzz corpora contain no realistic credentials.

## Open Questions

1. Whether `MaxEnvironmentNameBytes` should be lower than 253 for a more conservative cross-platform contract. The release candidate should test supported operating systems before freezing the v1 bound.
2. Whether JSON and text marshaling should return `[REDACTED]` or a typed refusal error. This design selects deterministic redaction because diagnostic encoders often fall back after errors, but structured-logging integration tests should validate that choice.
3. Whether the observer should receive aggregate empty and missing counts on successful capture. The design includes them because they are bounded and useful, but a security review may choose total binding count plus outcome only if presence aggregation is considered sensitive.
4. Whether GoForj should generate narrow domain adapters or leave all adapters project-owned. The root library API is unaffected either way.

None of these questions permits implicit fallback, environment discovery, dotenv ownership, or plaintext observability.
