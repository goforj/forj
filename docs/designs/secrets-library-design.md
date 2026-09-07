# Secrets Library And GoForj Integration Design

## Status

- Design status: proposed
- Planning date: 2026-09-06
- Target repositories: a new `github.com/goforj/secrets` sibling repository, independent provider driver modules, and `goforj`
- Primary sibling-library scope: logical secret reads, redacting values, version selection, safe caching, lifecycle, observations, and test support
- Primary GoForj scope: an optional Secrets component, generated source and binding configuration, App and tenant scoping, readiness, Testkit integration, and render coverage
- Cross-repository source of truth: this design is normative until the Secrets repository contains an accepted design or implementation plan that references it

## Summary

GoForj should add a small, domain-neutral Secrets library and an optional generated Secrets component. Application code should request a logical key and receive an opaque value without knowing an AWS ARN, Vault mount, Kubernetes volume path, or provider SDK type:

```go
secret, err := app.Secrets().Get(ctx, secrets.Key("database.primary.password"))
if err != nil {
	return err
}

password, err := secret.Value().Text()
if err != nil {
	return err
}
```

The root library should be byte-preserving, read-focused, safe to format accidentally, explicit about current, alias, and exact-version selection, and honest about cache staleness. Provider addresses and tenant mapping belong to trusted construction-time bindings rather than request input.

The initial supported sources should be memory/fake, a mounted-file source compatible with ordinary files, Docker secrets, and Kubernetes Secret volumes, AWS Secrets Manager, Google Cloud Secret Manager, Azure Key Vault Secrets, and HashiCorp Vault KV v2. Every production driver should have a conformance suite and real-service integration coverage. An emulator or SDK mock may supplement that coverage, but does not establish provider compatibility by itself.

Environment-delivered secrets are a separate concern owned by `github.com/goforj/env/v2/envsecrets`. They are captured during application configuration and are not a Secrets driver. Applications choose environment capture or runtime provider retrieval in composition through narrow domain interfaces; neither package imports or adapts the other.

## Decision

Create a root `github.com/goforj/secrets` module with independently released provider driver modules, then integrate it into GoForj as the optional `Secrets` App component.

Adopt these decisions:

1. The application-facing operation is a read by logical key.
2. Logical keys are stable application vocabulary, not provider paths, resource names, environment variable names, or URLs.
3. A trusted immutable catalog maps each logical key to one named source and one driver-specific locator.
4. Callers cannot supply a provider locator, endpoint, project, account, vault, mount, filesystem path, application scope, or tenant scope to `Get`.
5. The canonical root payload is bytes. Byte-capable drivers preserve arbitrary bytes. Text-only providers preserve exact UTF-8 text encoded as bytes and declare that limitation explicitly.
6. Secret values have unexported storage and redact through `String`, `GoString`, and serialization interfaces.
7. Explicit `Bytes` and `Text` accessors are disclosure boundaries, not security sandboxes. Callers can still copy or leak returned data.
8. `Bytes` returns a copy so a caller cannot mutate a cached value. Drivers and caches also copy at ownership boundaries.
9. The library does not claim reliable zeroization in Go. Garbage collection, compiler copies, strings, provider SDKs, and operating-system buffers prevent that promise.
10. Empty bytes are a valid secret value and are distinct from a missing key.
11. Core reads default to the source's current value. Exact version and alias selection use distinct validated selector types.
12. A driver rejects unsupported selector kinds. It never silently treats an exact version or alias as current.
13. Returned metadata identifies the resolved revision with a provider-opaque identifier. Applications must not parse it.
14. A logical binding can restrict callers to current-only even when its provider supports historical versions.
15. Provider-specific field extraction is declared in the trusted driver binding. The core does not assume every secret is JSON or a key/value object.
16. Root v1 is read-only. Secret creation, mutation, deletion, alias movement, policy management, and rotation orchestration remain operator or provider responsibilities.
17. Reading current after rotation is eventually observable according to provider propagation, mounted-volume propagation, and configured cache policy.
18. The library does not promise exactly-once rotation, exactly-once change notification, instantaneous propagation, or coordinated consumer reconfiguration.
19. Caching is opt-in and bounded per source or binding. No-cache is the safe default in the reusable library.
20. Current and alias reads have finite freshness TTLs. Exact-version reads may use a longer TTL, but never become immortal because versions can be disabled, deleted, destroyed, or lose authorization.
21. Concurrent cache misses for the same effective request are coalesced without coupling the provider call to the first caller's cancellation.
22. A bounded stale-on-error window is optional. It applies only to retryable availability failures, never to permission, authentication, not-found, disabled, destroyed, invalid, or integrity failures.
23. A stale result is visibly marked in returned safe metadata. Staleness is never hidden solely in logs or metrics.
24. Cache keys include source identity, logical binding identity, selector kind, and selector value. They never include payload bytes.
25. Negative results are not cached in v1.
26. Manual invalidation removes every selector entry for one logical binding in the current scoped client. It does not prove that every consumer discarded prior copies.
27. Bulk reads are an optional optimization, not an atomic transaction. Results are per key and may span revisions or succeed partially.
28. A bounded helper uses bounded individual reads by default. Native provider batching requires explicit trusted configuration because it may require additional authorization; it is used only when selectors and error semantics are also preserved.
29. Errors expose stable safe kinds and logical keys. Ordinary error strings do not contain values, provider locators, request or response bodies, credentials, tokens, tenant IDs, or provider SDK errors.
30. Context cancellation and deadlines remain detectable with `errors.Is` while error text remains safe.
31. Drivers classify retryability. The root does not retry reads unless the caller explicitly installs a bounded retry wrapper.
32. Readers and sources are safe for concurrent use after successful construction.
33. Construction validates the entire catalog before publication. There is no partially usable client with silently skipped bindings.
34. Client close is idempotent, prevents new reads, cancels client-owned refresh work, and drains admitted reads. Provider transports are borrowed by bound sources and closed exactly once by their construction owner.
35. The root does not start polling goroutines by default. Refresh-ahead and revision polling are explicit wrappers with explicit lifecycle ownership.
36. Liveness never depends on an external secret provider.
37. Readiness can require successful startup resolution of designated bindings and can enforce maximum accepted staleness. It never reports payloads.
38. A lightweight provider ping that does not prove authorization to a required secret cannot by itself establish readiness.
39. Observations contain bounded operation, configured source name, driver kind, safe logical-key label, selector kind, cache outcome, stale state, duration, and error kind only.
40. Raw logical keys are excluded from metrics unless they come from a generated bounded allowlist. Unknown keys use `other` or a process-keyed digest in diagnostic events.
41. Traces, metrics, logs, Lighthouse, Audit, panic output, and readiness details never contain secret values or provider locators.
42. Provider SDK default workload identity chains are the deployment default. Long-lived static cloud credentials are not generated into application configuration.
43. Bootstrap credentials cannot be fetched through the same Secrets client whose construction needs them.
44. Vault v1 accepts only externally managed credentials, normally Vault Agent sink material or an injected renewable token provider. The KV driver does not own login, renewal, or reauthentication.
45. Application and tenant scope are fixed when a scoped reader is constructed. Request-controlled tenant identifiers never directly choose a secret namespace.
46. Each generated App receives its own reader and catalog. Sharing a provider transport is allowed; sharing bindings or caches across scopes is not implicit, and one App close cannot close another App's transport.
47. Tenant readers come from a trusted resolver that authorizes and canonicalizes the tenant before applying a reversible collision-free token encoding to a predeclared namespace template or catalog.
48. The mounted-file driver accepts only configured relative paths below one configured root, follows projected-volume symlinks only through race-safe handle-relative confinement, bounds file size, and preserves exact bytes.
49. The file driver does not trim trailing newlines. Deployment tooling must create the intended bytes.
50. The memory source and fake apply production key and selector validation and return defensive copies.
51. Fakes capture only keys, selectors, timing, and outcome classes by default. Tests must opt in explicitly to inspect revealed values.
52. Component-off GoForj applications contain no Secrets imports, configuration, driver SDKs, generated accessors, or runtime initialization.
53. Generated configuration never contains payloads, cloud private keys, Vault tokens, or AppRole SecretIDs. Provider locators are separately classified sensitive operational metadata and follow the explicit inline-versus-deployment-reference policy below.
54. Runtime configuration can select only drivers compiled into the application. It cannot load arbitrary plugins or accept arbitrary provider endpoints from request data.
55. Provider driver releases require capability-parameterized root conformance tests plus live or real-server integration coverage described in this design.
56. Environment-delivered secrets remain outside this driver model. The library does not provide an environment driver, precedence chain, or fallback into process environment variables.

## Why A Separate Library

Secret retrieval has a reusable contract independent of code generation: byte preservation, redaction, logical naming, version selection, safe errors, caching, concurrency, and provider normalization. Putting these rules in generated code would duplicate security-sensitive behavior across applications. Putting them in a cloud-specific package would make application services depend on deployment choices.

The library should not become a universal configuration system. Ordinary non-sensitive settings need useful display, serialization, defaults, and diagnostics, all of which conflict with a secret value's disclosure boundary. It should also not become a deployment control plane. Provider policies, replication, creation, and rotation are operated outside an application read path.

## Goals

1. Give services one small, stable read API using logical keys.
2. Preserve payloads exactly within each driver's declared byte or UTF-8 capability.
3. Make accidental formatting and serialization redact by default.
4. Keep provider SDK types, resource names, and credentials out of domain code.
5. Normalize safe version and failure semantics without erasing provider differences.
6. Make freshness, staleness, readiness, and lifecycle behavior explicit.
7. Support least-privilege workload identity and tenant isolation.
8. Provide deterministic local tests and provider conformance suites.
9. Make every initial production driver demonstrably compatible with the real service contract.
10. Integrate into GoForj without affecting component-off applications.

## Non-goals

1. Secret creation, editing, deletion, replication, or policy administration.
2. A rotation scheduler or credential broker.
3. Exactly-once rotation or exactly-once change delivery.
4. Guaranteed zeroization or memory locking in Go.
5. General application configuration.
6. Environment or dotenv lookup in the root module.
7. Automatic struct population through reflection.
8. Automatic JSON, YAML, PEM, certificate, database DSN, or key parsing.
9. Arbitrary provider addresses supplied by request handlers.
10. Cross-provider transactions or atomic bulk snapshots.
11. A universal provider alias model.
12. Transparent fallback from one security boundary or provider to another.
13. Automatic retries with unbounded latency.
14. Writing secret material to disk for caching.
15. Returning provider SDK errors or response bodies to callers.

## Terminology

### Logical key

A stable application-owned identifier such as `database.primary.password`. It is safe vocabulary, but may still reveal architecture, so observations bound or pseudonymize it.

### Binding

An immutable trusted mapping from one logical key to a named source, a driver-specific locator, allowed selectors, and cache/readiness policy.

### Source

A configured provider instance such as one AWS region and account boundary, one Google project boundary, one Azure vault, one Vault address and mount, or one mounted directory.

### Locator

Driver-owned configuration that addresses a provider value. It is never supplied at read time and is treated as sensitive operational metadata even when it is not a credential.

### Selector

The requested current value, exact immutable-looking provider version identifier, or movable provider alias. Support depends on the binding and driver.

### Revision

Safe, provider-opaque metadata identifying the concrete value returned. It may be useful for equality and diagnostics but is not globally ordered.

### Secret

An immutable result containing an opaque redacting value and safe retrieval metadata.

### Freshness

Whether a result was fetched within its configured TTL. Freshness does not prove that a provider has globally propagated a rotation.

### Stale result

A previously successful cached result returned after its freshness TTL because a retryable provider failure occurred within an explicitly allowed stale window.

## Repository And Module Ownership

### Root module

`github.com/goforj/secrets` owns:

- keys, selectors, values, results, safe metadata, and errors;
- immutable catalogs and routing;
- cache, coalescing, retry, refresh, and lifecycle wrappers;
- bounded multi-read helpers;
- observation contracts and redaction rules;
- memory source, fake, and conformance suites;
- package documentation and executable examples; and
- compatibility tests that driver modules can import.

The root module must not depend on GoForj, cloud SDKs, Vault SDKs, Kubernetes clients, Docker clients, or `env/v2/envsecrets`.

### Independent driver modules

Use nested modules where provider dependencies justify independent updates:

```text
github.com/goforj/secrets
github.com/goforj/secrets/driver/file
github.com/goforj/secrets/driver/awssecretsmanager
github.com/goforj/secrets/driver/gcpsecretmanager
github.com/goforj/secrets/driver/azurekeyvault
github.com/goforj/secrets/driver/vaultkv2
```

The memory implementation remains in the root because it has no external dependency and defines reference behavior. The file module can remain independent to permit operating-system-specific containment implementations without expanding the root surface.

### GoForj

The framework owns:

- the optional `Secrets` component and component catalog entry;
- project and named-App driver selections;
- generated source, binding, cache, required-key, and tenant-scope configuration;
- compiled driver constructors and App accessors;
- startup preflight, readiness, metrics, Lighthouse summaries, and shutdown ordering;
- Testkit fake installation;
- generated docs and secure local-development guidance;
- render tests for every component and driver combination; and
- dependency pins in root, render-warm, fixture, and generated module surfaces.

### Generated application

The application owns:

- logical key names and which services may request them;
- provider resources, access policies, aliases, and rotation;
- application and tenant authorization before scoped-reader creation;
- required-at-startup and maximum-staleness policy;
- converting bytes or text into destination-specific credentials;
- reinitializing database pools, clients, or signers after rotation; and
- preventing revealed values from entering errors, logs, commands, templates, or persistence.

## Proposed Core API

The exact names may change during implementation, but the capability surface should remain this small:

```go
package secrets

type Key string

type Reader interface {
	Get(ctx context.Context, key Key, options ...GetOption) (Secret, error)
}

type Client interface {
	Reader
	Invalidate(ctx context.Context, key Key) error
	Close(ctx context.Context) error
}

func Current() GetOption
func Exact(version string) GetOption
func Alias(alias string) GetOption

type Secret struct {
	// unexported
}

func (s Secret) Value() Value
func (s Secret) Metadata() Metadata
func (s Secret) String() string
func (s Secret) GoString() string
func (s Secret) Format(fmt.State, rune)
func (s Secret) MarshalText() ([]byte, error)
func (s Secret) MarshalJSON() ([]byte, error)

type Value struct {
	// unexported
}

func (v Value) Bytes() ([]byte, error)
func (v Value) Text() (string, error)
func (v Value) Len() int
func (v Value) String() string
func (v Value) GoString() string
func (v Value) Format(fmt.State, rune)
func (v Value) MarshalText() ([]byte, error)
func (v Value) MarshalJSON() ([]byte, error)

type Metadata struct {
	Key       Key
	Revision  Revision
	FetchedAt time.Time
	Stale     bool
	StaleFor  time.Duration
}

type Revision struct {
	// unexported
}

func (r Revision) Equal(other Revision) bool
func (r Revision) String() string
func (r Revision) GoString() string
func (r Revision) Format(fmt.State, rune)
func (r Revision) MarshalText() ([]byte, error)
func (r Revision) MarshalJSON() ([]byte, error)
```

`Get(ctx, key)` means current. Options are mutually exclusive; more than one selector returns `ErrInvalid`. Constructors reject empty exact versions, invalid aliases, excessive lengths, and control characters before routing. The public reader has no locator-bearing overload.

`Value.String`, `Value.GoString`, JSON marshaling, text marshaling, and structured logging integration emit a constant such as `[REDACTED]`. `Secret` marshals only that same marker, not even its otherwise-safe metadata, so serialization cannot become a second inventory surface. Every concrete object reachable through a public API that retains a payload, locator, credential, or arbitrary injected failure implements safe `String`, `GoString`, `fmt.Formatter`, text marshaling, and JSON marshaling. This includes Secret, Value, clients, snapshots, bound sources returned behind interfaces, driver configuration values, memory sources, fakes, and mutation controllers. The rule covers values, pointers, every formatting verb, and nesting inside another formatted struct. Generated App container formatting must not traverse secret-retaining internals. Invalid zero `Secret` and `Value` values still redact under every formatting and marshaling path; disclosure accessors return `ErrInvalid`. Explicit accessor fields on trusted construction configuration remain readable to the code that owns them, but generic formatting and serialization redact locators. `Value.Bytes` returns a fresh copy. `Value.Text` returns a new string only for valid UTF-8 and otherwise returns `ErrNotText`. Neither accessor removes whitespace, trailing newlines, or NUL bytes. Destination-specific code decides whether those bytes are acceptable.

`Revision.Equal` compares a length-delimited internal identity domain containing driver kind, source instance, logical binding, App and tenant scope, and the resolved concrete provider revision. Once a movable selector resolves, the request form is not part of equality: current, alias, and exact requests that resolve to the same concrete provider revision compare equal within the same binding and scope. Identical provider version strings such as `1` across keys, sources, Apps, or tenants are still unequal. Every Revision formatting and marshaling path exposes only a process-keyed safe digest of that domain, including when nested in Metadata. Revisions are equality tokens, not sortable sequence numbers, and equality is guaranteed only within the process lifetime unless a future persisted-revision contract says otherwise.

`Metadata.FetchedAt` is stamped by the root when a successful source read completes. It is an informational wall-clock projection of that root acquisition instant, not a provider creation time and not an input to freshness arithmetic. Driver-facing Record constructors accept no timestamp. Internally each cache or readiness result stores a root-clock monotonic `validatedAt`; TTL, stale windows, readiness maximum age, and age buckets use monotonic elapsed duration from that instant. The production clock is `time.Now`, preserving Go's monotonic reading in process. A test clock must implement the root's monotonic clock interface explicitly. Wall-clock rollback or a provider's future timestamp cannot extend eligibility.

The underlying driver seam is narrower than each SDK:

```go
type BoundSource interface {
	Read(ctx context.Context, selector Selector) (Record, error)
	Capabilities() SourceCapabilities
}

type SourceFailureKind uint8

const (
	SourceNotFound SourceFailureKind = iota + 1
	SourceDeleted
	SourceDisabled
	SourceDestroyed
	SourceUnauthenticated
	SourcePermission
	SourceUnavailable
	SourceRateLimited
	SourceIntegrity
	SourceTooLarge
	SourceUnsupported
	SourceInvalidResponse
)

type RetryDisposition uint8

const (
	DoNotRetry RetryDisposition = iota
	MayRetry
)

func NewSourceError(kind SourceFailureKind, retry RetryDisposition, retryAfter time.Duration) error
```

Each driver exposes typed locator configuration and a `Bind` constructor that validates it and returns a `BoundSource` holding driver-private immutable locator state. A root binding associates that already-bound source with a logical key, selector policy, and cache policy. `SourceCapabilities` declares supported selectors, `PayloadBytes` or `PayloadUTF8`, `CacheAllowed`, `CoalescingAllowed`, and the supported startup policies; catalog construction rejects incompatible policy before publication. Disabling coalescing is reserved for sources whose own atomic publication boundary must be observed by every newly admitted read. A source that does not own or guarantee freshness may support only lazy startup. `Selector` and `Record` have bounded constructors usable by external driver modules; Record construction accepts payload and concrete provider revision but no source-controlled freshness timestamp. Ordinary `Reader.Get` callers never receive a locator or binding constructor. Trusted application wiring can implement a custom driver, but cannot make untrusted request input choose a locator through the read API. This compilable seam avoids privileged cross-package field access, `map[string]any`, reflection, public provider SDK objects, and application type assertions.

`NewSourceError` is the only driver-facing failure constructor. It accepts no message, cause, key, source name, locator, or provider object. It validates the closed kind, permits `MayRetry` only for unavailable and rate-limited failures, bounds nonzero retry-after, and returns an immutable safely formatted error recognized by the root. Context cancellation and deadline errors caused by the supplied context are returned directly. A driver maps and consumes its SDK error internally, then returns a SourceError without wrapping that SDK value; an unknown provider failure becomes non-retryable `SourceInvalidResponse`. The root adds only its already-validated logical key and safe source name while preserving sentinel and closed-kind behavior. External-driver conformance tests compile outside the root package and verify every constructor branch, retry decision, formatting verb, unwrap chain, and provider-error non-retention.

Memory, mounted file, AWS, and Google declare `PayloadBytes`. Azure and Vault KV v2 string fields declare `PayloadUTF8`. AWS is byte-capable for every binding because different versions can use `SecretString` or `SecretBinary`; string responses are returned as their exact bytes and no representation promise is inferred from an earlier version. Text-only provider drivers reject provider data that cannot be represented as valid UTF-8 and preserve valid text exactly without trimming or normalization. V1 defines no implicit base64 convention. Applications needing binary material in a string-only provider must own an explicit encoding and decode only after the disclosure boundary.

## Keys, Paths, Fields, And Scope

Logical keys use lower-case ASCII segments separated by dots. Each segment starts with a letter and continues with letters, digits, or underscores. Keys have bounded segment and total lengths. They are case-sensitive after validation; the library does not lowercase input silently.

Bindings are exact. V1 does not derive arbitrary provider paths by replacing dots with slashes. For fixed scopes, generation expands and validates every effective binding before runtime. Tenant templates are the only deferred case: construction validates their structure and all fixed locator fields, while authorized tenant resolution validates each final locator before a bound source is created.

Examples of trusted mappings:

| Logical key | Driver locator |
| --- | --- |
| `database.primary.password` | AWS secret ID `production/orders/database-password` |
| `payments.signing_key` | Google resource `projects/orders-prod/secrets/payments-signing-key` |
| `mail.api_token` | Azure vault plus secret name `mail-api-token` |
| `oauth.github.client_secret` | Vault mount `apps`, path `orders/oauth/github`, field `client_secret` |
| `tls.private_key` | Mounted relative file `tls/private-key.pem` |

AWS, Google, Azure, and file values are byte blobs. A JSON document stored in those services remains a blob unless the application explicitly uses a separate reviewed decoding helper after disclosure. Vault KV v2 is natively a JSON object, so its driver binding requires a top-level field name and accepts string values in v1. It rejects objects, arrays, numbers, booleans, and null rather than applying surprising stringification. A later explicit encoded-bytes convention may add base64 decoding.

Application scope and tenant scope are construction inputs:

```go
reader, err := registry.ForApp("billing")
tenantReader, err := tenantSecrets.ForAuthorizedTenant(tenantRef)
```

The resolver receives an authorized canonical tenant reference, not an HTTP header or raw route variable. It encodes the canonical identifier with a versioned, length-delimited, reversible base32 encoding and never truncates it. An encoded token that exceeds a provider locator bound is rejected rather than hashed or shortened. Namespace templates substitute only that token and cannot substitute slashes, URLs, Vault mounts, cloud project IDs, or source names. A trusted `ScopedBindingFactory`, created and structurally validated with the App catalog, combines the token with immutable template state, validates the final locator, and asks the driver to create a bound source that borrows the manager-owned transport. The factory API is available only to trusted scoped-reader construction and never to ordinary `Get` callers.

Tenant-scoped readers are lightweight views over one App client. They own no provider transport or refresh goroutine and do not require independent close. Cache keys include the full scope identity and the cache has global entry and byte bounds across tenants, so creating many scoped views cannot create an unbounded reader registry. Tests cover distinct canonical identities, normalization collisions, maximum encodable length, concurrent construction, cache eviction, and continued operation of one tenant after another view is discarded.

Tenant-templated bindings must use `lazy` startup policy in v1. GoForj rejects `required` or `readiness` because the manager has no authority to enumerate tenants or construct authorized tenant locators during application startup. An application that needs bounded preflight for selected tenants must build an explicit application-owned workflow after authorization; it is not inferred from the Secrets component.

## Versions And Aliases

The abstraction preserves three intents:

| Selector | Meaning | Cache implication |
| --- | --- | --- |
| current | Provider's current/default value at read time | Movable, finite TTL |
| exact | One provider version identifier | Stable-looking but still revocable or deletable |
| alias | One movable provider label | Movable, finite TTL |

Drivers declare selector capabilities during catalog construction:

- AWS supports current, exact `VersionId`, and alias-like `VersionStage`. Current means `AWSCURRENT`.
- Google supports current through `latest`, numeric exact versions, and version aliases.
- Azure supports current through an omitted version and exact version strings. V1 does not invent aliases for Azure.
- Vault KV v2 supports current and numeric exact versions. V1 does not invent aliases.
- mounted files support current only because the filesystem does not provide portable retained-version reads.
- memory supports all selectors so tests can model alias movement and historical reads.

Every binding must declare a nonempty selector allowlist supported by its driver. Required and readiness startup always resolve current, so catalog construction rejects either policy unless current appears in that allowlist. Lazy bindings may be exact-only or alias-only, but a default `Get` on them returns `ErrUnsupported`; callers must use the declared selector explicitly. Tests cover empty, current, exact-only, and alias-only allowlists against all three startup policies, including driver capability mismatches.

When both an AWS version ID and stage would be useful for an internal verification, the driver ensures they identify the same version as required by AWS. The public request still contains only one selector intent.

Returned metadata records the concrete provider version where available. For files it uses an internal content revision token. The raw content hash is not exposed because short secrets may be brute-forced from an unsalted digest.

## Caching, Rotation, And Staleness

Every source has a separate execution policy containing:

- a mandatory bounded per-source read timeout;
- a context-aware maximum in-flight operation count; and
- an optional bounded retry policy with total attempt and backoff limits.

The client may also impose a lower global concurrency ceiling and a bounded pending-admission count. One fair context-aware admission system covers foreground fills, refresh-ahead, explicit retries, native batches, startup preflight, and readiness resolution. A coalescing leader creates the client-owned total operation context before the first admission attempt, so queue time is bounded even when the caller supplied no deadline. Exceeding the pending bound returns safe `ErrOverloaded`. Cancellation while queued returns without provider work. Saturation, canceled admission, close, and starvation behavior are race-tested.

The root cache is in-memory only and stores immutable byte copies. Its separate cache policy contains:

- fresh TTL;
- optional refresh-ahead interval and jitter;
- optional maximum stale-on-error duration;
- maximum entries and maximum total payload bytes; and
- per-read maximum payload bytes.

Cache policy is validated before catalog publication. Any cache feature requires a positive TTL within root hard bounds. Refresh-ahead must be positive and less than TTL; its maximum jitter must be nonnegative and small enough that every scheduled refresh remains strictly before TTL. `MaxStale` requires a positive TTL and stale-on-error enablement. Duration addition uses checked arithmetic, and `TTL + MaxStale` overflow is rejected. Cache entry and byte limits must be positive when caching is enabled and must fit root hard bounds. The per-read payload limit is mandatory and bounded even when cache fields are all zero. Zero cache fields mean no cache, not an unbounded read. Tests cover every zero, boundary, relational, and overflow rejection.

No-cache is the root default. Every source still requires a positive execution timeout within root hard bounds. GoForj emits a conservative driver-specific timeout explicitly rather than allowing an infinite value. `CacheAllowed=false` rejects only nonzero cache fields; it does not disable execution timeout or admission policy. File sources should default to a short TTL or no cache because the operating system already caches reads and projected-volume rotation should become visible promptly.

For one effective request, the first expired caller starts a provider read and concurrent callers join it when the source permits coalescing. The shared provider operation uses the client-owned total context created by the leader. Its configured timeout is one deadline covering the first admission, all provider attempts, and retry backoff, not a fresh duration per attempt. Every attempt acquires per-source and client-wide capacity with that same context and releases it before retry backoff, so a failing request cannot hold scarce execution slots while sleeping. Each waiting caller can stop waiting when its own context ends without canceling work still needed by others. Each fill receives a monotonically ordered publication ticket within its binding and selector partition. When the final waiter leaves, the client atomically marks the fill nonjoinable, revokes its publication ticket, removes it from the coalescing lookup, and only then cancels its context. A replacement fill receives a newer ticket and only the current non-revoked ticket may publish. The orphaned provider call remains separately registered in lifecycle drain tracking until the source exits. A later caller therefore starts a new fill even if the canceled orphan is slow to return, while concurrency admission still counts both provider operations. Tests pause an old source call through cancellation, let its replacement publish a newer revision, then return the old value and prove it neither serves the new caller nor regresses the cache.

Wrapper order is fixed: validate and route, check cache, coalesce the miss when allowed, create the total operation context, acquire execution admission with that context, perform bounded retries inside that same deadline, then consider stale-on-error only after eligible retries are exhausted. The context is created before the first admission attempt, never after it. A stale result therefore cannot suppress a retry that could refresh it. Native batch operations follow the same timeout and retry envelope.

Provider SDK automatic retries are disabled for every driver. The root execution wrapper is the sole retry owner, including when its configured attempt count is one, so total deadlines, admission release during backoff, observation counts, and cancellation remain enforceable. Driver tests count transport attempts and prove cancellation during root backoff prevents another SDK call.

Each binding cache partition has a monotonic invalidation epoch. `Invalidate` advances the epoch, removes published entries, and marks any readiness state for the binding unresolved before returning. Every operation that can publish into the cache or readiness state, including a foreground fill, refresh-ahead operation, required startup, and readiness resolution, captures its starting epoch and may publish only if that epoch is still current. An older fill may return its fetched value only to callers that joined it before invalidation, but it cannot repopulate the cache, publish ready status, or serve callers admitted after invalidation. The next scheduled readiness resolve uses the new epoch and is the only path back to ready. Close advances a terminal epoch and prevents every later publication. Tests linearize invalidation against fills, stale fallback, alias movement, refresh-ahead, readiness status and cache publication, health probes, and close.

`Invalidate(ctx, key)` checks context before mutation, requires a declared key, and affects current, exact, and alias cache entries plus readiness state for that binding inside only the client's fixed App and tenant scope. After the epoch change it emits its synchronous observation with the caller context; cancellation can stop a compliant observer but does not roll back completed invalidation. Once the epoch change commits, the method returns nil after observation even if the context became canceled, because reporting a cancellation would falsely imply that invalidation did not occur. A tenant view cannot invalidate another scope. V1 exposes no source-wide invalidation API; manager maintenance that needs one iterates its bounded generated binding catalog explicitly.

Stale-on-error is disabled by default. When enabled, it may return a prior value only when the failure is explicitly retryable and its class is `Unavailable`, `RateLimited`, or a provider timeout, and only while monotonic elapsed time since the root-owned `validatedAt` remains within `TTL + MaxStale`. Both conditions are mandatory: non-retryable `SourceUnavailable` and `KindInvalidResponse` never use stale data even though they match `ErrUnavailable`. The result sets `Stale` and emits a safe observation. `StaleFor` means monotonic elapsed duration beyond the fresh TTL, computed as `elapsed - TTL`; it is zero for a fresh result and does not mean total age. Tests freeze the clock at the TTL boundary, immediately after it, and at `TTL + MaxStale`. Authentication failure, authorization failure, missing or disabled versions, integrity failure, invalid requests, and explicit cancellation never use stale data.

Refresh-ahead is optional, jittered, and bounded. A failed early refresh never replaces or evicts the existing entry while its original TTL remains fresh, regardless of stale-on-error configuration. After TTL, serving that retained entry is governed exclusively by the stale policy and failure allowlist. Tests fail refresh-ahead before TTL with stale disabled, prove ordinary reads still receive the fresh entry through the TTL boundary, then verify post-TTL behavior separately. Polling detects revisions, not rotation events. A revision may be observed late, intermediate revisions may be skipped, and multiple workers may observe changes at different times.

V1 does not invoke application callbacks with newly rotated values. Consumers that can reload credentials should own a controlled loop that reads, compares `Revision`, constructs a replacement client or pool, publishes it atomically, and drains the old resource. GoForj may later provide a lifecycle coordinator, but its notifications would be at-least-once and coalescible, never exactly-once.

## Bulk Reads And Snapshots

The root may ship this helper outside the core `Reader` interface:

```go
results := secrets.ReadMany(ctx, reader, keys,
	secrets.WithConcurrency(4),
)
```

It returns one result per requested key in input order and a summary error when any failed. It validates duplicate keys and a maximum batch size before reads. Successful values remain available when other keys fail. It does not promise a shared instant, revision, transaction, rollback, or all-or-nothing result.

A driver may implement an internal batch capability, but the router never selects it merely because it exists. Bounded individual reads are the default and preserve the same per-secret authorization as `Get`. Trusted source configuration must explicitly enable native batching and acknowledge any additional provider permission before the router may use it for requests to the same source whose selector and error semantics it preserves. AWS `BatchGetSecretValue` requires the additional `secretsmanager:BatchGetSecretValue` permission, so enabling it is an operational IAM change that generated plans and documentation call out. A missing batch permission is returned as a permission failure and is never silently retried as individual reads because fallback would create policy-dependent behavior. Provider-native list APIs are never used to turn tags or prefixes into an unbounded application read.

## Error Contract

Stable sentinels support `errors.Is`:

```go
var (
	ErrInvalid       = errors.New("invalid secret request")
	ErrNotFound      = errors.New("secret not found")
	ErrNotText       = errors.New("secret is not valid UTF-8 text")
	ErrUnauthenticated = errors.New("secret source authentication failed")
	ErrPermission    = errors.New("secret access denied")
	ErrUnavailable   = errors.New("secret source unavailable")
	ErrRateLimited   = errors.New("secret source rate limited")
	ErrDisabled      = errors.New("secret version disabled or destroyed")
	ErrIntegrity     = errors.New("secret payload failed integrity validation")
	ErrTooLarge      = errors.New("secret payload exceeds configured limit")
	ErrUnsupported   = errors.New("secret selector unsupported")
	ErrClosed        = errors.New("secret reader closed")
	ErrOverloaded    = errors.New("secret reader overloaded")
)
```

An exported `Error` exposes a closed `ErrorKind`, operation class, a declared logical key, safe source name, retryable flag, and bounded retry-after duration. Source failure kinds map one-to-one to corresponding root kinds except where sentinel groups are intentional. `KindDisabled` and `KindDestroyed` both match `ErrDisabled` through `errors.Is`, while remaining distinguishable for lifecycle decisions. `KindDeleted` and `KindNotFound` both match `ErrNotFound`; Vault's read response can distinguish soft-deleted data and uses `KindDeleted`, while an absent secret, version, or Vault field uses `KindNotFound`. Azure ordinary retrieval cannot distinguish a deleted secret from other absence without a separate broader operation, so v1 deliberately normalizes its 404 to `KindNotFound`. `KindInvalidResponse` matches `ErrUnavailable` but is non-retryable unless a later explicit provider mapping proves otherwise. Provider-specific states that cannot support finer distinctions normalize to the closest documented kind. Conformance tests assert the source kind, root kind, sentinel, retryability, and retry-after behavior for every supported provider state. Input length is bounded before grammar parsing or lookup. Invalid or unknown caller input uses a fixed `unknown` label or process-keyed diagnostic digest, never the raw attempted key. A key is included only after exact catalog membership is established. The error must not expose the provider error as an unwrap target. Drivers retain provider details only for a separately configured safe diagnostic classifier. Error formatting uses fixed templates and never includes locators or SDK messages.

If `ctx.Err()` caused the result, `errors.Is(err, context.Canceled)` or `context.DeadlineExceeded` remains true through an explicit safe wrapper. A provider timeout not caused by caller context maps to `ErrUnavailable`. Unknown provider failures default to non-retryable until classified deliberately.

## Concurrency And Lifecycle

Catalogs, bindings, and policies are immutable after construction. Readers, sources, and caches are safe for concurrent calls. Drivers use provider SDK clients according to their documented concurrency contracts and do not mutate client configuration during reads.

Construction order is:

1. validate non-secret configuration and source names;
2. construct bootstrap credentials and provider clients;
3. compile and validate every binding;
4. build cache and observation wrappers;
5. preflight designated required keys; and
6. publish the reader into the App only after successful startup policy.

The root `Client` owns only its routing, cache, coalescing, and optional refresh wrappers. Bound sources borrow provider transports. The generated manager owns each distinct provider transport and records it once even when several Apps or bindings share it. Construction rollback first closes and drains every constructed client, cache refresh worker, and readiness wrapper, including an unpublished client after late preflight failure. Only then does it close successfully created owned transports in reverse order.

Client close uses one linearized gate. It rejects new reads with `ErrClosed`, cancels client-owned background work and every orphaned coalesced fill, and waits for all admitted public calls through their final return or for the close context to expire. Tracking includes provider operations whose callers canceled, cache hits, stale-result assembly, and synchronous observation. A timed-out close leaves the client closed but not yet drained. A later idempotent `Close` call may wait again and returns nil once draining eventually completes. Timeout does not authorize the manager to close borrowed transports still in use. The manager closes each distinct provider transport only after every borrowing client drains; process shutdown reports a safe timeout if that cannot complete. Dropping cache references is best effort memory hygiene, not zeroization. Closing one App client never invalidates another App sharing a transport.

## Readiness And Startup

Bindings declare one of three startup policies:

- `required`: startup must retrieve the value successfully and within its maximum freshness;
- `readiness`: startup may complete, but readiness fails until the value resolves;
- `lazy`: the first application use performs the read.

Every binding declares a nonempty selector allowlist. `required` and `readiness` always resolve current at startup and therefore require `current` in that allowlist; exact-only and alias-only bindings must be lazy. V1 does not add a separate startup selector because pinning an exact credential at startup is an application deployment decision better expressed through ordinary explicit reads. Catalog construction and GoForj generation reject empty selector sets plus every required or readiness binding without current. Tests cover empty, current-only, exact-only, and alias-only sets against all three startup policies.

Generated production defaults should use `required` for credentials necessary to construct foundational resources and `lazy` for optional features. Readiness evaluates only declared keys, reports counts and safe error classes, and never lists provider locators, values, versions, or tenant IDs.

Generated readiness configuration is typed and bounded rather than inferred from cache settings:

```yaml
startup: readiness
readiness:
  max_age: 5m
  refresh_interval: 1m
  allow_stale: false
```

`max_age` and `refresh_interval` must be positive and within root hard bounds, and `refresh_interval` must be less than `max_age`. These fields are accepted only for `readiness` bindings. `allow_stale` defaults to false and may be true only when stale-on-error is enabled; even then, readiness accepts a stale result only while monotonic elapsed time from the root-owned `validatedAt` is no greater than `max_age` and its cache stale window has not expired. Required startup always performs a fresh resolution and does not accept stale data.

The generated manager owns a bounded readiness resolver only when `readiness` bindings exist. After provider construction it starts one coalesced resolution state machine per bounded binding set, with explicit request timeouts, exponential backoff, jitter, a maximum concurrent-read limit, and the configured continuing refresh interval independent of whether value caching is enabled. Scheduled readiness resolution performs a fresh provider resolve that bypasses a cache hit, captures the binding epoch before the read, and publishes successful cache and readiness state only if that epoch remains current. Health probes read the last immutable state and never initiate provider I/O. Successful resolution records only freshness and safe status. The readiness schedule re-resolves before maximum accepted freshness expires, so no-cache bindings can become unavailable and recover without application traffic. App shutdown first atomically closes the shared reader admission gate, then cancels readiness and other background work, invokes client close to drain already admitted operations, joins the resolver, and only afterward releases borrowed transports. No new Get, bulk read, invalidate, preflight, or readiness resolve can enter after shutdown begins. Tests cover unavailable-to-ready recovery, typed policy validation, no-cache refresh, cache bypass and update, invalidation during readiness resolution with health probes remaining unresolved, fresh-to-stale transitions, accepted-stale policy, provider recovery, coalescing across probes, read admission racing shutdown, and shutdown during backoff.

Liveness reports whether the process and internal refresh loop are functioning. It does not contact providers. A transient provider outage should affect readiness according to configured stale limits, not cause an orchestrator to restart every healthy process continuously.

## Observability Without Leakage

The root observer receives immutable safe events after validation and classification. Allowed fields include:

- operation: `get`, `read_many`, `refresh`, `invalidate`, `preflight`, `readiness`, or `close`;
- source: bounded configured name;
- driver: bounded implementation kind;
- logical-key label: generated allowlist value, `other`, or process-keyed digest;
- selector kind, never arbitrary selector text;
- outcome and stable error kind;
- cache hit, miss, refresh, bypass, or stale;
- stale boolean and bounded age bucket;
- duration; and
- retry attempt when an explicit retry wrapper is installed.

Forbidden fields include payload bytes or text, raw keys from dynamic input, locators, cloud resource names, file paths, Vault mounts and fields, exact versions, aliases, credentials, tokens, headers, SDK request or response objects, tenant IDs, and provider error strings.

Metrics use bounded labels. Traces use the same allowlist and set no payload events. Logs should record safe error kinds and operator-facing source names only. Lighthouse and Audit can report configuration presence, readiness state, cache policy, rotation age buckets, and failures, but never material or addresses.

Observer calls are synchronous and receive the operation context. Get and bulk events use the caller context, refresh and readiness events use their client-owned operation context, invalidate uses its caller context, and close uses the close context. Callbacks must return promptly and honor cancellation. A callback panic is recovered and cannot change a completed operation result; in-flight accounting is released in a defer after the callback returns or panics. Because observation is part of the public call's tracked completion, a blocked observer may cause `Close` to reach its context deadline, but later idempotent close calls can wait for final drainage. Tests cover every operation classification, panicking observers, blocked observers, cancellation, close timeout and retry, and proof that bookkeeping is released on every callback exit.

## Bootstrap Authentication

Provider authentication precedes secret retrieval and therefore needs a separate root of trust:

- AWS uses the SDK credential provider chain, favoring IAM roles, web identity, ECS credentials, or instance roles.
- Google uses Application Default Credentials, favoring attached workload identity.
- Azure uses `DefaultAzureCredential` or an explicitly injected `azcore.TokenCredential`, favoring managed identity or workload identity in deployments.
- Vault v1 uses Vault Agent with a protected token sink or another explicitly injected external token provider. Direct Kubernetes login, AppRole login, token renewal, and reauthentication are outside the KV driver's v1 lifecycle.
- mounted-file authentication and authorization are the deployment's mount and filesystem permissions.

Generated config may contain regions, project identifiers, vault URLs, source names, and auth mode selection because those are needed to construct clients. It must not generate static access keys, client secrets, service-account JSON, Vault tokens, or AppRole SecretIDs into committed files.

The client rejects credential configuration that recursively names one of its own logical keys. Cross-client bootstrap chains are also discouraged because they create fragile startup cycles. Operators should inject the first credential through platform identity, a protected mounted file, an external agent, or a separately owned credential broker.

## Driver Contracts

### Memory And Fake

The memory source is the reference implementation. Its builder accepts byte copies, exact revisions, alias mappings, disabled versions, and injected classified failures. Publication produces an immutable concurrent snapshot; mutation for rotation tests occurs through an explicit test controller, not through production `Reader` methods.

The fake records safe requests and can block on caller-controlled channels, simulate delays, move aliases, rotate current, return stale-triggering failures, and assert concurrency. Its default failure output and call history contain no payload. The memory source, fake, bound sources, and mutation controllers satisfy the full public-object redaction contract even when they retain test values, locators, or injected errors. An explicit `RevealForTest(key)` helper lives in a package clearly named `secretstest` and is unsuitable for generated production wiring.

### Mounted File

One configured root maps exact logical bindings to relative filenames. The driver rejects absolute paths, empty components, `.` and `..`, NUL bytes, platform volume prefixes, and unconfigured keys. It opens the file read-only, enforces a byte limit while reading, and returns exact contents.

Kubernetes projected Secret volumes use symlinks for atomic publication, so a blanket no-symlink rule is incompatible. The driver permits symlinks only when resolution is bound to the opened object and every target remains below the configured root. Linux uses handle-relative facilities such as constrained `openat2`; other supported platforms require an equivalent race-safe open plus final-handle verification. A lexical or pre-open containment check is never sufficient. A platform without an equivalent guarantee fails construction as unsupported. The driver reads from one verified descriptor so a publication swap cannot splice two generations into one result. Tests continuously replace every symlink component while concurrent reads attempt escape.

The final verified handle must identify a regular file. The driver uses nonblocking open semantics where needed to obtain and inspect the handle before reading, then rejects directories, FIFOs, sockets, devices, and other special files. This prevents a configured or replaced path from blocking in FIFO open or acquiring device semantics outside the bounded blob contract. Integration tests cover contained regular files plus FIFO, Unix socket, and available device-node rejection.

The source treats missing files as `ErrNotFound`, permission errors as `ErrPermission`, and oversize files as `ErrTooLarge`. A containment failure, escaping symlink, non-regular final object, or verified-handle mismatch is a non-retryable `ErrIntegrity` condition and can never trigger stale fallback. Only bounded ordinary I/O failures with deliberate mappings use retryable availability. It does not watch recursively in v1. Polling or cache expiry observes replacement. Kubernetes `subPath` mounts do not receive automated Secret updates, so generated documentation must reject `subPath` for bindings expected to rotate. Docker Swarm rotation commonly changes the secret object and service attachment, potentially restarting tasks; the driver does not claim in-place updates.

### AWS Secrets Manager

The binding contains a configured secret ID and optional allowed stages. Current reads request `AWSCURRENT` or omit the selector consistently. Exact reads use `VersionId`; alias reads use `VersionStage`. The driver always declares byte capability, returns `SecretBinary` as decoded bytes, and returns `SecretString` as its exact Go string bytes. It rejects an impossible response containing neither payload or both payload fields. A version may change representation without changing the binding contract.

The driver records the returned VersionId internally as the revision. It maps decryption, permission, not-found, invalid-request, throttling, and service failures deliberately. It does not log AWS request structures because they contain the secret ID, and it does not expose CloudTrail-sensitive request details through errors. Native batch reads are disabled by default, require explicit source configuration plus the documented batch IAM action, are restricted to the semantics supported by `BatchGetSecretValue`, and retain per-secret failures.

### Google Cloud Secret Manager

The binding contains a fully qualified secret resource prefix or a validated project/location plus secret ID. Current reads access `versions/latest`; exact reads use a numeric version; alias reads use a validated version alias. The driver returns payload bytes exactly and uses the concrete version name from the response as its internal revision.

The driver verifies CRC32C when the response supplies a checksum and maps mismatch to `ErrIntegrity`. Payloads are bounded below both the provider limit and the application's configured limit. With the least-privilege `secretmanager.versions.access` permission, failed AccessSecretVersion calls cannot reliably distinguish disabled from destroyed without a separate metadata request requiring `secretmanager.versions.get`. V1 performs no such probe and maps both states to `KindDisabled` matching `ErrDisabled`; it never parses provider message text to guess. Missing, permission, quota, and availability responses retain their documented safe classes. An access-only live identity proves successful reads and the grouped disabled/destroyed behavior. A future opt-in metadata classification capability would need explicit additional IAM configuration, timeout and retry accounting, race semantics, and a separate compatibility review. The implementation uses the official Go client and injected credentials without exporting protobuf types.

### Azure Key Vault Secrets

The binding fixes one vault URL and secret name. Current reads call Get Secret without a version. Exact reads pass the configured version. V1 rejects alias selectors because Key Vault Secrets does not expose the portable movable-alias contract used by AWS and Google.

Azure secret values are strings. The driver returns their UTF-8 bytes exactly and uses the returned secret identifier's version component internally. It maps disabled, not-found, forbidden, throttled, and unavailable states explicitly. V1 does not call Get Deleted Secret after an ordinary 404 because that requires a separate permission and introduces a recovery race; deleted and never-present secrets therefore both use `KindNotFound`. V1 also defines no client-side not-before or expiry enforcement because the provider may return values outside those metadata windows and the portable binding has no temporal policy. Operators enforce lifecycle in Key Vault and application code may inspect separately retrieved business metadata outside this secret-read contract. A later temporal policy requires its own typed configuration, clock semantics, errors, and compatibility review.

### HashiCorp Vault KV V2

The source fixes the Vault address, namespace if used, and KV v2 mount. Each binding fixes the secret path and top-level field. The driver calls the v2 data endpoint, not KV v1, and keeps mount versus data path separation explicit. Current omits `version` or uses provider-defined zero; exact requires a positive decimal version. Aliases are unsupported.

The driver accepts only a string field in v1 and returns its UTF-8 bytes. It uses response metadata version as the revision and treats soft-deleted, destroyed, missing-field, permission, sealed, standby/availability, and authentication failures distinctly. It does not request the metadata endpoint merely to read a value, because that may require broader policy. Vault custom metadata, lease data, token data, and response objects never enter ordinary observations.

The driver never owns token renewal or reauthentication in v1. It obtains token material for each bounded operation through the injected external provider, does not retain it in observations, and maps expired or rejected credentials safely. Vault Agent or the application-owned credential provider owns readiness and rotation of that material. Adding driver-owned login or renewal later requires a separate lifecycle handle with readiness, cancellation, join, rollback, and close semantics.

## Generated GoForj Configuration

The design should extend the existing component catalog, YAML sequence format, App normalization, resource plan, templates, and render tests rather than introduce a parallel component system. A representative source-of-truth project shape is:

```yaml
render:
  components: [web_api, secrets]
  secrets:
    sources:
      primary:
        driver: aws_secrets_manager
        region: us-east-1
        read_timeout: 5s
        cache:
          ttl: 1m
          max_stale: 0s
    bindings:
      database.primary.password:
        source: primary
        secret_id: production/orders/database-password
        selectors: [current]
        startup: required
apps:
  worker:
    components: [jobs, secrets]
    secrets:
      sources:
        primary:
          driver: aws_secrets_manager
          region: us-east-1
          read_timeout: 5s
      bindings:
        queue.consumer.token:
          source: primary
          secret_id: production/orders/queue-consumer-token
          selectors: [current]
          startup: lazy
```

`secrets` is the component identifier. The default App uses `render.components` plus `render.secrets`, matching the repository's existing default-App persistence. Each additional named App uses `apps.<name>.components` plus `apps.<name>.secrets`. Both locations carry the same typed component shape after App normalization, and configuration does not inherit or merge across Apps. The same logical key may appear in several App catalogs with independent locators, but duplicate keys within one App are rejected. Generation builds one immutable source and binding catalog plus cache namespace per App. The manager may deduplicate an ordinary provider transport only when the complete typed transport configuration and ownership policy are equal; catalogs, bindings, caches, and lifecycle gates remain separate. A Secrets block without component selection, a selected component without its required block, references to undeclared same-App sources, `render.secrets.apps`, and top-level `secrets` fail strict validation. Render tests cover both canonical locations and every rejected alternative. Runtime tests prove that the default App cannot request `queue.consumer.token`, `worker` cannot request `database.primary.password`, and closing either App does not affect the other.

Driver-specific settings should be typed YAML structures with strict unknown-field rejection. Do not use a free-form options map. Generation emits typed constructors and an App-specific logical-key package so a key assigned only to one App is not advertised to another:

```go
package secretkeys

const DatabasePrimaryPassword secrets.Key = "database.primary.password"
```

Locator metadata is not a secret payload or credential, but it can reveal account structure, project names, vault layout, and application architecture. Each locator field therefore supports exactly one trusted source: an inline value for operators who intentionally permit it in their repository, or a named deployment-configuration reference resolved by generated GoForj wiring at startup. Public templates and `.env.example` contain only blank locator placeholders. Generated plans, logs, readiness, Lighthouse, metrics, and errors show binding counts and safe source names, never locator values. Repositories that commit inline locators must apply their normal access policy and secret scanning; the word "non-secret" is not used to imply unrestricted diagnostic safety.

Each configured App receives `App.Secrets() secrets.Reader`, commonly narrowed further by application-owned interfaces. The explicit App entry is the complete binding set, with no implicit inheritance. Component-off and unassigned-App templates omit the method entirely. A project that compiles multiple drivers may select among only those compiled constructors. Changing App assignment, driver, locator, scope template, startup policy, or cache staleness is an explicit configuration change called out in generated diffs.

Generated `.env.example` and `.env.testing` continue to follow the existing environment contract. They may hold non-secret bootstrap selectors only when the provider SDK requires them, never retrieved secret payloads. The Secrets component does not copy provider values into process environment variables.

## Relationship To Environment Secrets

`github.com/goforj/env/v2/envsecrets` captures explicitly declared environment variables during application configuration. It is not a source driver for this library. Process environment delivery lacks provider versions, per-read authorization, retry semantics, remote freshness, and the lifecycle expected by the driver contract.

Applications that can use either delivery mechanism define a narrow domain interface and select one implementation in application composition. An environment-backed implementation reads a captured `envsecrets.Value`; a managed implementation reads this library's `Reader`. GoForj does not translate between them, share keys between them, or fall back from one to the other. General Secrets tests use the memory source and fake rather than process environment variables.

## Testkit And Fakes

The conformance suite should test every source for:

- exact payload preservation parameterized by capability: empty, binary, NUL, newline, and invalid UTF-8 for `PayloadBytes`, and empty plus all valid UTF-8 forms for `PayloadUTF8`;
- defensive copying on input, cache storage, result construction, and `Bytes` access;
- constant or safe-digest formatting through every formatting verb, pointers, nested values, Secret results, Revision, Metadata, concrete clients, sources, bound drivers, fakes, mutation controllers, driver configuration, generated containers, JSON, text marshaling, errors, and observations;
- current, exact, alias, unsupported selector, and alias movement semantics;
- missing, disabled, destroyed, permission, authentication, rate limit, timeout, cancellation, oversize, and integrity failures;
- mandatory timeout cancellation for a source and native batch that block until their context ends;
- per-source and client-wide admission saturation, queued cancellation, fairness, retry deadline, and shutdown behavior;
- invalid and unknown key inputs containing control characters or credential-like fixture text without raw error or observation disclosure;
- concurrent reads, miss coalescing, caller cancellation, invalidation epochs, stale and refresh-ahead publication, and close races under the race detector;
- stale-on-error requiring both an allowed class and retryable disposition, including non-retryable unavailable, invalid response, filesystem containment, and the hard maximum age;
- retry-before-stale ordering where a transient failure succeeds on a later attempt despite a stale candidate;
- revision equality across current, alias, and exact requests that resolve to the same concrete provider revision, plus inequality for identical version strings across bindings, sources, Apps, and tenants;
- partial and ordered bulk results;
- no locator, version, alias, tenant, provider message, or payload leakage; and
- idempotent shutdown with active reads.

Lifecycle integration tests construct two App clients over one borrowed transport, close them in both orders, inject failure after every construction stage, exercise close deadlines with active reads, cancel every waiter before close, and prove the manager joins orphaned fills and closes the shared transport exactly once only after all borrowers drain. Tenant tests cover reversible token encoding, overlength rejection without truncation, normalization collisions, global cache bounds, and concurrent scoped views.

GoForj Testkit installs an App-scoped fake before resource construction. It supports required-key assertions and deterministic rotations without real cloud credentials. Generated integration tests can reveal known public fixture values explicitly, but failure messages should compare digests or lengths rather than print actual bytes.

## Live Integration And Emulator Policy

Provider SDK mocks prove request construction, not service compatibility. Release gates should distinguish these layers:

| Driver | Hermetic coverage | Real contract coverage required before release |
| --- | --- | --- |
| memory/fake | root conformance and race tests | Hermetic implementation is the real source |
| mounted file | temporary directory, replacement, permissions where supported, containment, symlink, and concurrent publication tests | Linux container tests mounting a Docker secret-like file and a Kubernetes-style atomic-writer symlink tree; periodic Kubernetes end-to-end test for actual projected Secret update and `subPath` exclusion |
| AWS | injected Smithy client and optional local compatibility service | live AWS account creates a unique secret, reads string and binary values, moves a stage, reads exact/current/previous, proves default `ReadMany` with a `GetSecretValue`-only identity, separately enables batch with its additional IAM action and exercises partial failure, rotates, and deletes it |
| Google | injected official client seam; any community emulator is supplementary | live Google Cloud project creates a unique secret, uses an access-only identity to read bytes, verifies CRC32C, tests grouped disabled/destroyed classification, moves an alias, and deletes it |
| Azure | injected SDK transport; any unofficial emulator is supplementary | live Azure vault creates a unique secret, reads current and exact versions, disables/enables a version, records actual provider behavior for not-before and expiry metadata without client enforcement, exercises authorization classification where feasible, then deletes and purges or recovers according to fixture policy |
| Vault KV v2 | a pinned real Vault binary or container in dev/test mode with KV v2 and policies | the real Vault server test covers versions, soft delete, undelete, destroy, missing fields, policy denial, externally supplied token expiry, and mount/path separation; a separately scheduled deployed Vault test covers Vault Agent token replacement and TLS |

AWS Secrets Manager, Google Cloud Secret Manager, and Azure Key Vault Secrets do not have an official local emulator contract that should replace live tests. LocalStack, hand-written HTTP servers, recorded responses, or other substitutes may improve speed and fault coverage, but tests must label them compatibility or mock tests rather than live provider tests.

Live tests are opt-in for contributors, isolated by unique prefixes, least-privilege identities, bounded budgets, and cleanup in `defer` plus a scheduled janitor. They never print payloads. CI records only resource counts and safe error classes. A driver release is blocked if its required live suite has not passed against the minimum and current supported SDK/provider surface for that release.

Tests must exercise provider behavior, not merely successful `Get`: version movement, disabled/deleted states, binary fidelity, authorization mapping, retry classification, timeouts, and cleanup are part of compatibility. Destructive cases use resources created by that test run only.

## Security Model

The library protects primarily against accidental disclosure, confused-deputy addressing, path escape, unsafe diagnostics, cross-scope cache reuse, and unbounded resource use. It does not protect a secret from arbitrary code running in the same process after that code calls `Bytes` or `Text`.

Required controls include:

- least-privilege provider policies restricted to configured resources and operations;
- workload identity and short-lived credentials where supported;
- TLS verification with no generated insecure-skip option;
- explicit endpoint allowlists for custom endpoints used only in controlled tests;
- immutable catalogs and exact mapping instead of caller-built locators;
- per-payload, catalog, batch, concurrency, cache-entry, cache-byte, timeout, and retry bounds;
- strict file-root containment and read-only mounts;
- separate cache partitions per App and tenant scope;
- no disk cache, swap-protection claim, or core dump protection claim;
- redaction tests across all formatting and observation paths;
- dependency scanning, provider SDK pin review, and secret scanning in CI; and
- documented incident response for leaked application logs or credentials.

Redacting wrappers reduce accidents but are not a data-loss-prevention boundary. A caller can reveal and log a value, convert it to an immutable string, retain it indefinitely, or pass it to a library that logs configuration. Documentation should favor the shortest disclosure lifetime and destination constructors that accept bytes when available.

The mounted-file source must account for Kubernetes atomic symlinks without accepting escape symlinks. Custom provider endpoints are test-only by default because accepting arbitrary endpoints can enable credential exfiltration or server-side request forgery. Any production custom endpoint requires an explicit constructor, HTTPS validation, and operator-owned allowlisting.

## Compatibility

The root public API should remain provider-neutral. Adding a driver or a driver-only selector capability is not a root API break. Changing key grammar, redaction output, selector interpretation, stale eligibility, error classification, byte preservation, or close behavior is a runtime compatibility change and needs explicit release notes and tests.

Compatibility risks must be classified separately:

- source/API: exported Go types, interfaces, constructors, and sentinel behavior;
- configuration: generated YAML keys, driver names, defaults, and validation;
- runtime: cache freshness, stale eligibility, retries, readiness, and provider mapping;
- security: newly observable metadata, locator acceptance, or credential sourcing;
- operational: required IAM roles, provider APIs, network paths, rotation, and rollout;
- generated code: App fields, imports, constructors, and component-off parity; and
- minimum Go version: root and every nested module independently.

V1 should begin at `v0` while driver contracts and provider error mappings settle. Before `v1`, freeze the key grammar, selector constructors, value disclosure methods, stable errors, observation schema, and conformance interface. Do not raise the Go version merely to align driver SDKs; identify the exact module that requires a change and its effect.

## Releases And Module Validation

Each nested module has an independent tag such as `driver/awssecretsmanager/v0.1.0`; a root `v0.1.0` tag does not publish it. Release automation must inventory and test every `go.mod`, use the repository's module tag convention, and preserve development-time relative `replace` directives where they intentionally test unpublished sibling changes.

A release sequence should:

1. pass root unit, race, fuzz, redaction, and conformance tests;
2. pass every driver unit and conformance suite;
3. pass required live or real-server integration suites;
4. verify examples and documentation links;
5. tag the root and each changed nested module independently;
6. verify every tag is available through the Go proxy;
7. update GoForj and every generated pin surface;
8. render the largest supported composition in `/tmp`, including all compiled drivers and multiple Apps;
9. run generated tests with `GOWORK=off` to prove published modules resolve without local replacements; and
10. rerun generation and require no diff.

Provider SDK upgrades receive their own live compatibility run. A passing compile is not enough because error shapes, retry behavior, endpoint resolution, and authentication chains can change without altering the root API.

## Implementation Phases

### Phase 1: Root contract

Implement keys, selectors, opaque values, errors, immutable catalogs, routing, memory source, fake, safe observations, conformance tests, and executable examples. Prove formatting and serialization redaction with adversarial tests.

### Phase 2: File and cache behavior

Implement mounted files, secure containment, cache/coalescing, stale policy, lifecycle, and Kubernetes atomic-writer fixtures. Run race and fuzz tests before provider work builds on these wrappers.

### Phase 3: Remote drivers

Implement AWS, Google, Azure, and Vault in independent modules. For each driver, add SDK seam tests, root conformance, real-service tests, provider-specific version and failure tests, and safe cleanup before declaring support.

### Phase 4: GoForj integration

Add the optional component, strict generated config, typed key constants, App and tenant-scoped readers, startup/readiness, metrics, Lighthouse, Testkit, shutdown, multi-App coverage, and component-off parity. Update every module pin and render the maximum composition outside the repository directory.

### Phase 5: Rotation consumers

Only after concrete database, HTTP client, signer, and token-consumer lifecycles are proven should GoForj consider a coordinator. Its design must define atomic replacement and draining per consumer and must not claim exactly-once observation.

## Acceptance Criteria

1. Domain code reads logical keys without importing a driver package or provider SDK.
2. Byte-capable drivers round-trip binary, empty, newline-terminated, and invalid UTF-8 values exactly; text-only drivers round-trip valid UTF-8 exactly and reject incompatible provider data.
3. Formatting, JSON, text marshaling, errors, metrics, traces, logs, readiness, Lighthouse, and fake history do not disclose values or locators.
4. Callers cannot use `Get` to choose provider addresses, paths, projects, vaults, mounts, Apps, or tenants.
5. Current, exact, alias, and unsupported selector behavior is directly tested for each applicable driver.
6. Stale results occur only under the explicit bounded policy and are visible to callers.
7. Concurrency, cancellation, cache coalescing, epoch-ordered invalidation, refresh, linearized close, and shared-transport ownership pass race and lifecycle tests.
8. Required startup and readiness policies distinguish external availability from liveness.
9. Multi-App and tenant tests prove catalog and cache isolation, collision-free tenant encoding, bounded scoped views, and independent close behavior.
10. Component-off renders contain no Secrets artifacts or provider dependencies.
11. Every production driver passes root conformance plus the real-service coverage defined above.
12. All nested modules pass tests independently and published resolution passes with `GOWORK=off`.
13. Maximum generated composition renders in `/tmp`, builds, tests, and regenerates without a diff.
14. The root module and `env/v2/envsecrets` remain mutually independent and provide no adapter or fallback between them.
15. Documentation makes no zeroization, instantaneous rotation, atomic bulk-read, or exactly-once claim.

## Risks And Mitigations

### Redacting values may create false confidence

Mitigation: describe accessors as disclosure boundaries, test accidental formatting, and state plainly that same-process code can leak revealed values and Go cannot guarantee zeroization.

### A common selector may hide provider differences

Mitigation: keep selector kinds distinct, declare capabilities per binding, reject unsupported kinds, and document concrete provider behavior.

### Stale caching may extend compromised credentials

Mitigation: disable it by default, bound it tightly, exclude security and identity errors, expose stale metadata, and let required bindings reject staleness entirely.

### Tenant mapping may become a confused deputy

Mitigation: accept only authorized canonical tenant references, constrain templates, partition caches, and prevent request-time source or locator selection.

### File compatibility may weaken path safety

Mitigation: support only contained symlinks, use secure directory-relative opens where available, read one descriptor, and test the Kubernetes atomic-writer layout and escape races.

### Emulator-only tests may drift from providers

Mitigation: require live cloud suites and a real Vault server suite for releases, label substitutes accurately, and test error and rotation behavior as well as success.

### Bootstrap credentials may create cycles

Mitigation: make bootstrap an explicit construction layer using workload identity, mounted files, Vault Agent, or an external broker, and reject self-referential bindings.

## References

Provider implementations and tests should track these authoritative contracts:

- [AWS Secrets Manager `GetSecretValue`](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html)
- [AWS Secrets Manager versions and staging labels](https://docs.aws.amazon.com/secretsmanager/latest/userguide/whats-in-a-secret.html)
- [AWS Secrets Manager `BatchGetSecretValue`](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_BatchGetSecretValue.html)
- [AWS standardized credential providers](https://docs.aws.amazon.com/sdkref/latest/guide/standardized-credentials.html)
- [Google Secret Manager `AccessSecretVersion`](https://cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access)
- [Google Secret Manager payload and CRC32C contract](https://cloud.google.com/secret-manager/docs/reference/rest/v1/SecretPayload)
- [Google Secret Manager version aliases](https://cloud.google.com/secret-manager/docs/assign-alias-to-secret-version)
- [Google Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials)
- [Azure Key Vault Get Secret REST contract](https://learn.microsoft.com/en-us/rest/api/keyvault/secrets/get-secret/get-secret)
- [Azure SDK for Go authentication and `DefaultAzureCredential`](https://learn.microsoft.com/en-us/azure/developer/go/sdk/authentication/authentication-overview)
- [HashiCorp Vault KV v2 HTTP API](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2)
- [HashiCorp Vault Kubernetes authentication](https://developer.hashicorp.com/vault/docs/auth/kubernetes)
- [HashiCorp Vault AppRole authentication](https://developer.hashicorp.com/vault/docs/auth/approle)
- [Kubernetes Secret volume behavior and update propagation](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Kubernetes Secret volumes and `subPath` limitation](https://kubernetes.io/docs/concepts/storage/volumes/#secret)
- [Docker Swarm secrets](https://docs.docker.com/engine/swarm/secrets/)
