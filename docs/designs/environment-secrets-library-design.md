# Environment Secrets Package Design

## Status

- Design status: proposed
- Planning date: 2026-09-07
- Target repository: the existing `github.com/goforj/env` repository
- Import path: `github.com/goforj/env/v2/envsecrets`
- Package name: `envsecrets`
- Primary scope: small, redacting access to secrets delivered through environment variables

## Summary

The existing `env/v2` module should gain an `envsecrets` subpackage with a deliberately small primary API:

```go
password, err := envsecrets.Require("DATABASE_PASSWORD")
if err != nil {
	return err
}

mailToken, present, err := envsecrets.Lookup("MAIL_API_TOKEN")
if err != nil {
	return err
}
```

`Require` reads one present, nonempty environment value. `RequirePresent` permits an explicitly empty value. `Lookup` distinguishes missing from present-empty. Successful calls return an opaque Value whose ordinary formatting and serialization redact.

The package has no Binding declarations, Manifest, Spec, Store, Snapshot, Capture, Refresh, YAML configuration, GoForj component, general Secrets driver, or cross-library bridge.

## Decision

Add `envsecrets` to the existing `env/v2` module with these rules:

1. Package-level functions read the process environment directly for the common production case.
2. A small Reader provides the same API over an injected Lookup for tests and specialized launchers.
3. Every call performs exactly one lookup of one exact validated environment name.
4. Require means present and nonempty.
5. RequirePresent means present, while explicitly empty is accepted.
6. Lookup returns presence separately and accepts explicitly empty values.
7. Successful reads return an opaque immutable Value.
8. Value formatting and serialization always redact.
9. Bytes and Text are explicit disclosure methods. Bytes returns a copy.
10. The package never enumerates environment variables or infers names.
11. The package does not load dotenv files, mutate the environment, cache values, or retain a global store.
12. The existing root env package remains the owner of dotenv loading and general configuration.
13. The package is not a driver for `github.com/goforj/secrets`.
14. Missing values never trigger a managed-store fallback.
15. GoForj does not persist or generate environment-secret declarations.

## Why This Smaller API

An environment variable is already a named binding. Reading it once already produces a stable value object. Requiring applications to declare another key, construct a manifest, capture a Store, and retrieve the same binding adds ceremony without creating a real source transaction.

The operating system does not provide an atomic multi-variable environment snapshot. A Capture abstraction could publish an internally consistent map, but its input values could still come from different mutation moments. Most production environments establish variables before process start and rotate them through process replacement.

The useful primitive is therefore one safe read:

- make missing and empty behavior explicit;
- validate the exact name;
- copy the returned bytes;
- redact accidental presentation; and
- allow Lookup injection when tests need isolation.

Applications that need several secrets read them during their existing startup composition and retain the returned Values or construct the dependent resources immediately.

## Why It Lives In The Env Module

Environment-secret access shares its delivery mechanism and ordering with ordinary environment configuration. If dotenv loading is enabled, it must finish before secret reads. Child-process inheritance and process replacement also belong to environment lifecycle guidance.

A separate repository would duplicate environment vocabulary and create an unnecessary release lifecycle. A subpackage still makes the safety distinction visible:

```go
port := env.GetInt("PORT", "8080")
password, err := envsecrets.Require("DATABASE_PASSWORD")
```

The root env package supports ordinary configuration behaviors such as defaults, discovery, dumping, and dotenv mutation. The envsecrets subpackage intentionally does not.

The root env package does not import envsecrets. The subpackage need not import root env internals and can rely on the standard library plus its narrow Lookup contract.

## Relationship To The General Secrets Library

`github.com/goforj/secrets` models runtime reads from managed and mounted providers. Those providers have locators, versions, aliases, authorization failures, retries, cache freshness, and readiness behavior.

Environment access has none of those portable semantics. It is not a provider driver and does not adapt to `secrets.Reader`.

An application that supports either delivery mechanism uses an application-owned interface:

```go
type DatabasePasswordSource interface {
	DatabasePassword() ([]byte, error)
}
```

One implementation reveals an envsecrets Value read during composition. Another reads the general Secrets client. The application chooses explicitly. Neither library provides fallback, precedence, revision translation, or shared configuration.

General Secrets tests use its memory source and fake, not environment variables.

## Goals

1. Make the safe path shorter than direct `os.Getenv` plus ad hoc validation.
2. Preserve missing versus explicitly empty semantics.
3. Redact values across ordinary formatting and serialization.
4. Make plaintext access explicit.
5. Support deterministic tests without requiring process-environment mutation.
6. Reject invalid or unreasonably large names and values.
7. Remain consistent with the existing env package's convenience-oriented API.
8. Document environment-delivery limitations honestly.

## Non-goals

1. A registry, manifest, binding set, snapshot, or refresh lifecycle.
2. Atomic multi-variable reads.
3. General environment parsing, defaults, or typed conversion.
4. Dotenv loading, discovery, precedence, writing, or watching.
5. Environment enumeration, prefix scans, wildcard access, or name inference.
6. Managed-provider access, fallback, caching, versions, leases, or revocation.
7. A general Secrets driver or adapter.
8. GoForj render or named-App configuration.
9. Preventing other same-process code from calling `os.Getenv`.
10. Encrypting process memory or guaranteeing zeroization.
11. Automatically unsetting variables or filtering child environments.
12. Package-global mutable hooks or replaceable default readers.

## Proposed API

```go
package envsecrets

const (
	MaxEnvironmentNameBytes = 253
	MaxValueBytes           = 1 << 20
)

var (
	ErrInvalidName   = errors.New("envsecrets: invalid environment name")
	ErrMissing       = errors.New("envsecrets: secret is missing")
	ErrEmpty         = errors.New("envsecrets: secret is empty")
	ErrValueTooLarge = errors.New("envsecrets: secret exceeds value limit")
	ErrNotText       = errors.New("envsecrets: value is not valid UTF-8 text")
	ErrLookup        = errors.New("envsecrets: lookup failed")
	ErrInvalid       = errors.New("envsecrets: invalid value or reader")
)

func Require(name string) (Value, error)
func RequirePresent(name string) (Value, error)
func Lookup(name string) (Value, bool, error)

type LookupSource interface {
	LookupEnv(name string) (value string, present bool, err error)
}

type LookupFunc func(name string) (value string, present bool, err error)

func (f LookupFunc) LookupEnv(name string) (string, bool, error)

type ProcessLookup struct{}

func (ProcessLookup) LookupEnv(name string) (string, bool, error)

type Reader struct {
	// unexported
}

func FromLookup(source LookupSource) (Reader, error)
func (r Reader) Require(name string) (Value, error)
func (r Reader) RequirePresent(name string) (Value, error)
func (r Reader) Lookup(name string) (Value, bool, error)
func (r Reader) String() string
func (r Reader) GoString() string
func (r Reader) Format(fmt.State, rune)

type Value struct {
	// unexported
}

func (v Value) Empty() bool
func (v Value) Bytes() ([]byte, error)
func (v Value) Text() (string, error)
func (v Value) String() string
func (v Value) GoString() string
func (v Value) Format(fmt.State, rune)
func (v Value) MarshalText() ([]byte, error)
func (v Value) MarshalJSON() ([]byte, error)
```

Package-level functions behave exactly like a Reader backed by ProcessLookup. They are ordinary functions, not methods on mutable package state. `FromLookup` rejects a nil source and returns an immutable Reader.

The API intentionally has no `DefaultReader` variable, global Lookup replacement, `MustRequire`, raw string return, list operation, cache, Store, or refresh method.

## Name Validation

Names must contain only uppercase ASCII letters, digits, and underscores, must begin with an uppercase letter or underscore, and must not contain `=`, NUL, whitespace, or control characters. A name may occupy at most `MaxEnvironmentNameBytes` bytes.

Validation occurs before lookup. Invalid caller input returns ErrInvalidName and is never passed to the injected source. Errors do not echo an invalid raw name. Uppercase canonical names avoid case-folding aliases on Windows and establish one portable API meaning.

The package performs no prefixing or normalization. The string passed by the caller is the exact name sent to LookupSource after validation.

## Read Semantics

Each method calls LookupEnv exactly once after validation. It copies the returned string into private Value bytes only after presence, requirement, and size checks succeed.

| Lookup result | Require | RequirePresent | Lookup |
| --- | --- | --- | --- |
| `present=false` | ErrMissing | ErrMissing | `(Value{}, false, nil)` |
| `present=true, value=""` | ErrEmpty | valid empty Value | `(value, true, nil)` |
| `present=true, value!=""` | valid Value | valid Value | `(value, true, nil)` |

When present is false, the returned string is ignored even if a faulty source returns nonempty data.

Require does not trim whitespace. A value containing spaces or a trailing newline is nonempty and is preserved exactly. Application-specific validation happens after explicit disclosure.

A value larger than MaxValueBytes returns ErrValueTooLarge. The error may include the configured limit but not the observed length.

There is no cross-call consistency promise. Two calls can observe different process-environment states if another goroutine mutates the environment between them. Applications should finish environment setup before reading secrets and retain successful Values or constructed resources.

## Process And Injected Readers

ProcessLookup is stateless and delegates to `os.LookupEnv`. It never calls `os.Environ`, loads dotenv files, mutates state, caches values, or logs names or values.

Injected LookupSource implementations must:

- preserve missing versus present-empty;
- return exact string bytes without trimming or normalization;
- avoid blocking indefinitely because the interface has no context;
- avoid logging values; and
- be safe for concurrent use if shared.

The interface intentionally has no context. Process environment lookup is immediate, and adding context to every production call would add ceremony without providing cancellation. A specialized source that needs network or blocking I/O does not belong behind this package.

Reader is safe for concurrent use when its source is safe. It holds no cache or mutable lookup policy.

## Values And Redaction

Value stores private immutable bytes plus a validity marker. Empty returns true only for a valid explicitly empty Value. A zero Value returns false and cannot impersonate present-empty.

Bytes returns a new allocation on every successful call. Mutating it cannot affect the original Value. Text returns a new string only when the bytes are valid UTF-8; otherwise it returns ErrNotText. Neither method trims or normalizes.

Value formatting emits `[REDACTED]` for empty, nonempty, and zero values under `%s`, `%v`, `%+v`, `%#v`, pointers, and nesting. JSON and text marshaling emit the same marker. There is no ordinary plaintext serialization path.

Reader formatting emits a constant marker and never traverses its injected source. This matters because a fake source may retain values or arbitrary errors.

Redaction is defense in depth. Callers that use Bytes or Text can leak plaintext. Reflection, unsafe code, debuggers, core dumps, runtime copies, and the operating system remain outside the guarantee. Go cannot promise reliable zeroization, so the package provides no misleading Destroy method.

## Errors

Errors support stable `errors.Is` classification. An error may expose the operation, exact validated environment name, and stable kind in its message. Including a validated name helps operators repair deployment wiring. It never contains a value, value length, hash, prefix, source response, or arbitrary source error.

Invalid raw names are omitted from error fields and formatting. Validated names may appear only after validation completes.

An injected source error may contain secret material. The package consumes it without formatting or wrapping and returns a fixed error matching ErrLookup. The original error is not retained in the public unwrap graph.

Every formatting verb, GoString, and nested wrapper must remain value-free. Package examples log stable classification, not arbitrary source errors.

## Concurrency And Zero Values

Package-level functions are safe for concurrent use because ProcessLookup is stateless and `os.LookupEnv` is concurrency-safe. Reader and Value are safe for concurrent use under the source contract.

The zero Reader is invalid. Its methods return ErrInvalid without lookup, and formatting remains safe. The zero Value is invalid; Bytes and Text return ErrInvalid, Empty returns false, and formatting still redacts.

Reader and Value are value-semantic handles. Pointers are not required by the API. Non-nil pointers inherit formatting methods, while direct calls through nil pointers receive no special guarantee.

## Dotenv And Root Env Ordering

envsecrets does not read or discover dotenv files. If the application uses root env loading, startup order is:

1. load ordinary environment layers through `env/v2`;
2. finish process-environment mutation;
3. call envsecrets Require, RequirePresent, or Lookup; and
4. construct and publish dependent application resources.

Calling env.Reload later does not change any previously returned Value. The application must read again and deliberately replace dependent resources if it supports in-process reload. GoForj does not automate that transition.

Local `.env` files containing real secrets remain ignored. Committed `.env.example` values remain blank or redacted. `.env.testing` contains only conspicuously public fixtures.

## Process Environment Limitations

The package cannot improve the underlying isolation of environment delivery:

- values commonly exist from process start;
- privileged operators, same-user tooling, crash handlers, and compromised dependencies may read them;
- environment behavior varies by operating system and launcher;
- environment values have no provider identity, remote version, lease, revocation, or rotation acknowledgement; and
- revealed values become ordinary application memory.

Children commonly inherit the complete environment when `exec.Cmd.Env` is nil. Applications should construct explicit child environments at trust boundaries:

```go
cmd := exec.CommandContext(ctx, executable, args...)
cmd.Env = []string{
	"PATH=" + safePath,
	"LANG=C.UTF-8",
}
```

Filtering only secret-looking names is insufficient. The package does not alter commands or unset variables because doing so can race application code and cannot erase prior copies.

## Application And GoForj Integration

There is no envsecrets GoForj component and no render, named-App, or top-level configuration section.

Application composition reads required values directly:

```go
password, err := envsecrets.Require("DATABASE_PASSWORD")
if err != nil {
	return fmt.Errorf("read database password: %w", err)
}

database, err := openDatabase(password)
if err != nil {
	return fmt.Errorf("open database: %w", err)
}
```

GoForj may document dotenv ordering and safe usage but does not generate declarations, scan source, infer secret variables, or maintain a second inventory. Existing `.env.example` and `.env.testing` tooling remains authoritative for environment-file documentation.

For application-level tests that should not mutate the process environment, composition accepts a Reader or a narrow application interface:

```go
secretReader, err := envsecrets.FromLookup(fakeLookup)
if err != nil {
	return err
}

password, err := secretReader.Require("DATABASE_PASSWORD")
```

## Fake Package

The same module should include `github.com/goforj/env/v2/envsecrets/fake`:

```go
package fake

type Lookup struct {
	// unexported, concurrency-safe
}

func New(values map[string]string) *Lookup
func (l *Lookup) LookupEnv(name string) (string, bool, error)
func (l *Lookup) Set(name, value string)
func (l *Lookup) SetEmpty(name string)
func (l *Lookup) Unset(name string)
func (l *Lookup) Fail(name string, err error)
func (l *Lookup) ClearFailure(name string)
func (l *Lookup) Calls() []string
func (l *Lookup) ResetCalls()
```

The fake copies input and returned state, distinguishes empty from missing, records call order, and is safe for concurrent use. Formatting never traverses retained values, names, or injected errors. Failure output is fixed and value-free.

No helper mutates the process environment behind the caller's back. A small ProcessLookup integration test may use `t.Setenv` with unique public fixtures and must not run in parallel.

## Observability

The package has no logger, observer, global callback, metrics dependency, trace dependency, or per-access event stream. Per-read events could expose secret access patterns and add work to a deliberately tiny function.

Applications may measure their explicit startup operations. Safe telemetry includes operation, outcome, duration, and stable error class. It excludes names, presence, values, lengths, hashes, and injected errors.

## Test Plan

Direct tests cover:

- every name grammar boundary and overlong input;
- invalid input rejected before lookup;
- exactly one lookup per valid call;
- Require, RequirePresent, and Lookup across missing, empty, and nonempty values;
- ignoring source output when present is false;
- values at, below, and above MaxValueBytes;
- whitespace, newline, NUL, valid UTF-8, and invalid UTF-8 Go strings;
- Bytes defensive copying and Text UTF-8 validation;
- zero Value and Reader behavior;
- nil source rejection;
- source errors absent from formatting and unwrap graphs;
- redaction under every formatting verb, pointer, nesting, JSON, text, and logging adapter;
- Reader formatting that does not traverse its source;
- fake copy, mutation, failure, call history, and concurrency behavior;
- package-level and Reader semantic parity; and
- ProcessLookup missing versus explicitly empty behavior.

Run `go test -race ./...` with concurrent package reads, shared Reader calls, fake mutation under its documented synchronization, and concurrent Value disclosure copies.

Fuzz name validation, arbitrary source strings, size boundaries, formatting, and marshaling. Dynamically generated secret values remain in memory and are not written to corpus files.

Executable examples place expected output immediately after producing calls and display only redaction or stable errors. They never print revealed fixture values.

## Compatibility

Before v1, freeze:

- environment-name grammar;
- fixed bounds;
- Require, RequirePresent, and Lookup semantics;
- Value validity, copying, disclosure, and redaction;
- injected Reader behavior;
- stable error sentinels; and
- zero-value behavior.

Changing missing or empty interpretation, trimming values, adding implicit lookup fallback, weakening redaction, or retaining arbitrary source errors is a runtime or security compatibility break. Lowering a fixed bound is incompatible for previously accepted inputs.

The package ships with the existing env/v2 module and release process. It does not create an independent module or tag. The module's Go version increases only if a required implementation feature demands it, with the exact constraint documented.

## Implementation Plan

### Phase 1: Small core

Implement package-level functions, ProcessLookup, injected Reader, opaque Value, name validation, fixed size bounds, stable errors, defensive copying, and package documentation.

### Phase 2: Testing and hardening

Add the fake package, exhaustive semantic tests, adversarial redaction tests, race coverage, fuzzing, ProcessLookup integration, child-process guidance, and executable examples.

### Phase 3: Env repository integration

Update env/v2 documentation and release surfaces. Document ordering after dotenv loading. Do not add GoForj configuration, a general Secrets driver, or cross-library release coordination.

## Acceptance Criteria

1. The package lives at `github.com/goforj/env/v2/envsecrets` in the existing env module.
2. Common production use is one Require, RequirePresent, or Lookup call.
3. Tests can inject a LookupSource through Reader.
4. Missing and explicitly empty values remain distinct.
5. Invalid names never reach lookup.
6. Values redact under all ordinary formatting and serialization paths.
7. Bytes returns a defensive copy and zero Value cannot impersonate empty.
8. Source errors never enter returned error graphs or formatting.
9. The package never enumerates, caches, mutates, or implicitly reloads the environment.
10. The package has no Manifest, Binding, Store, Snapshot, Capture, or Refresh API.
11. GoForj has no environment-secret configuration or generated declarations.
12. Neither library provides an environment driver, bridge, revision mapping, or fallback.
13. Race, fuzz, and boundary tests cover every public behavior.
14. Documentation makes no zeroization, revocation, or managed-provider security claim.

## Risks And Mitigations

### Repeated calls may observe different values

Mitigation: document that applications read environment secrets during startup and retain Values or constructed resources. The package does not pretend the process environment offers multi-variable transactionality.

### Developers may bypass envsecrets

Mitigation: keep the safe API shorter than ad hoc `os.Getenv` validation and optionally add repository linting later. The runtime cannot prevent same-process access.

### Redaction may create false confidence

Mitigation: document reveal methods, debugger access, memory copies, child inheritance, and the lack of reliable Go zeroization.

### Fixed limits may reject an unusual value

Mitigation: choose practical conservative bounds, gather real cases, and add a reviewed bounded option only when evidence justifies the added API.

## References

- [Go `os.LookupEnv`](https://pkg.go.dev/os#LookupEnv)
- [Go `os.Environ`](https://pkg.go.dev/os#Environ)
- [Go `os/exec.Cmd.Env`](https://pkg.go.dev/os/exec#Cmd)
- [Kubernetes environment variable guidance](https://kubernetes.io/docs/tasks/inject-data-application/define-environment-variable-container/)
