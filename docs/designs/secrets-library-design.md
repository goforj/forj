# Secrets Library Design

## Status

- Design status: implementation-ready
- Planning date: 2026-09-06
- Last reviewed: 2026-09-08
- Target repositories: `github.com/goforj/secrets` and independent provider driver modules
- Primary scope: one application-facing read operation over configured secret sources

## Summary

The library should provide one way for application code to read a value from a configured secret source:

```go
password, err := secretReader.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	return err
}
```

`Get` returns the value as a Go string. A Go string preserves arbitrary bytes and is immutable, so the core API does not need separate `Secret`, `Value`, `Text`, or `Bytes` types. Applications that need a byte slice can convert explicitly:

```go
privateKey, err := secretReader.Get(ctx, "TLS_PRIVATE_KEY")
if err != nil {
	return err
}
privateKeyBytes := []byte(privateKey)
```

Provider selection, provider locators, retries, and other operational policy belong to trusted construction. They are not request-time arguments.

Environment variables remain ordinary configuration owned by `github.com/goforj/env/v2`:

```go
password := env.MustGet("DATABASE_PASSWORD")
```

There is no `envsecrets` package, environment driver, secret-value wrapper, automatic fallback, or shared environment and provider abstraction.

## Decision

Create a small `github.com/goforj/secrets` module and independent provider driver modules with these rules:

1. The complete application-facing contract is `Get(context.Context, string) (string, error)`.
2. Names use uppercase environment-style configuration vocabulary such as `DATABASE_PASSWORD`.
3. Names are exact opaque identifiers. Underscores do not create a hierarchy and names never become provider paths.
4. A trusted binding maps each name to one configured provider read.
5. Callers cannot provide provider locators, endpoints, accounts, projects, vaults, mounts, or filesystem paths to `Get`.
6. `Get` returns the fetched value directly. There is no disclosure accessor.
7. Go strings preserve exact bytes, including empty, NUL, newline, and invalid UTF-8 sequences.
8. Empty is a valid managed secret. A missing binding or provider value returns an error.
9. The library never logs, formats, serializes, hashes, or otherwise presents a fetched value.
10. Returned errors contain no value, locator, provider response, credential, token, tenant identifier, or provider SDK error.
11. V1 reads the provider's current or latest value. Exact-version and alias APIs wait for a demonstrated application need.
12. V1 is read-only. Creation, mutation, deletion, policy management, and rotation orchestration remain provider or operator responsibilities.
13. First-party Readers are safe for concurrent use when their injected client satisfies the documented concurrency contract.
14. Every provider driver has contract tests and real-service integration coverage.
15. Environment variables continue to use the existing env package and are not a Secrets driver.

## Why The API Is This Small

Most consumers need a credential string to construct a database pool, HTTP client, mail client, signer, or provider SDK. Returning a wrapper and requiring a second method call makes every normal read harder without preventing the application from disclosing the value a moment later.

The reusable boundary still matters. It hides provider SDKs and locators, centralizes safe errors, and lets deployment choose a provider without changing domain code. Those benefits do not require a large application API.

Features should not enter the public Reader merely because a provider exposes them. Versions, aliases, provider metadata, batching, invalidation, and health details wait until an application use case proves that they belong in the domain-facing contract.

## Goals

1. Make the normal read one call.
2. Keep provider details out of application code.
3. Preserve returned bytes exactly.
4. Keep library-owned errors and diagnostics free of secret material.
5. Support deterministic tests with a tiny interface.
6. Remain framework-agnostic and usable through ordinary dependency injection.
7. Verify every production driver against the real provider contract.

## Non-goals

1. A redacting value wrapper.
2. Runtime selection of versions, aliases, fields, providers, or locators.
3. Secret creation, mutation, deletion, or policy management.
4. Automatic environment-variable fallback.
5. Environment or dotenv loading.
6. A universal configuration system.
7. Atomic multi-secret transactions.
8. Exactly-once rotation or change notification.
9. Guaranteed memory zeroization in Go.
10. Returning provider SDK errors or response bodies.
11. Adding cache, retry, bulk-read, or metadata APIs before demonstrated use cases require them.

## Public API

The root application contract and function adapter are:

```go
package secrets

type Reader interface {
	Get(ctx context.Context, name string) (string, error)
}

type Func func(ctx context.Context, name string) (string, error)

func (f Func) Get(ctx context.Context, name string) (string, error)
```

That interface is intentionally sufficient for domain code and application services. `Func` makes one-off adapters and focused tests possible without a mock framework. Get first panics with the fixed message `secrets: nil Func` when the Func itself is nil, treating nil as bad wiring rather than a request failure. A nonnil Func then rejects a nil context and validates the name before invoking the function.

The companion `secretstest` package provides the common map-backed test case:

```go
reader := secretstest.New(t, map[string]string{
	"DATABASE_PASSWORD": "public-test-password",
})
```

Its complete constructor is `func New(t testing.TB, values map[string]string) secrets.Reader`. It marks itself as a test helper, validates and copies its input, fails the test for an invalid fixture, and returns an immutable Reader. A missing name returns ErrNotFound and an explicitly empty fixture returns an empty string successfully. Tests that need failures or changing values use `secrets.Func` and keep that behavior explicit in the test.

Func forwards the function's value and error unchanged after request validation. The function owner is responsible for concurrency safety and for preventing sensitive values or provider details from entering its errors. The stronger sanitization and concurrency guarantees in this design apply to built-in driver Readers, not arbitrary third-party implementations of Reader.

## Names

Names follow the same vocabulary applications already use for environment configuration:

```text
DATABASE_PASSWORD
MAIL_API_TOKEN
PAYMENTS_SIGNING_KEY
TLS_PRIVATE_KEY
```

A name must begin with an uppercase ASCII letter and continue with uppercase ASCII letters, digits, or underscores. It has a fixed maximum length of 255 bytes. The limit bounds untrusted lookup and error input while remaining well above ordinary environment-style names; it does not mirror or constrain a provider locator. The library does not trim, uppercase, prefix, split, or otherwise normalize input. Requiring a leading letter rejects empty and underscore-only names while retaining familiar environment-style vocabulary.

The name is application vocabulary, not a provider locator. `DATABASE_PASSWORD` may map to an AWS secret ID, Google resource, Azure vault entry, Vault field, or mounted file without changing application code.

Invalid input is rejected before lookup. Unknown input does not reach a provider.

## Value Semantics

`Get` returns a Go string containing the exact bytes produced by the bound provider read. Go strings are byte sequences and do not require valid UTF-8.

The library does not trim whitespace, remove newlines, parse JSON, decode base64, parse PEM, or reject NUL bytes. A text-only provider must return its exact UTF-8 bytes. A byte-capable provider preserves arbitrary bytes.

The empty string is a valid fetched value. Applications decide whether a particular credential may be empty.

Returning a plain string means the library cannot redact it after return. This is deliberate. Callers must not log, dump, serialize, persist, or include the returned value in errors. The library protects its own errors and provider integrations, but does not pretend to control ordinary application memory or caller-authored test output.

## Errors

V1 exposes only stable classifications that application code can reasonably act on:

```go
var (
	ErrInvalid     = errors.New("secrets: invalid request")
	ErrNotFound    = errors.New("secrets: not found")
	ErrPermission  = errors.New("secrets: access denied")
	ErrUnavailable = errors.New("secrets: source unavailable")
)
```

Errors support `errors.Is`. Context cancellation and deadlines remain detectable through `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)`.

Every operation on a nonnil built-in Reader applies this order:

1. a nil context returns ErrInvalid;
2. invalid name syntax returns ErrInvalid without echoing the input;
3. a valid name absent from the configured bindings returns ErrNotFound without provider I/O;
4. after provider I/O fails, a canceled or expired caller context returns the matching context error; and
5. every other provider result follows the normative mapping below.

| Condition | Public classification |
| --- | --- |
| unknown binding, missing remote object, provider explicitly reports no readable current or latest version, missing Vault field | ErrNotFound |
| unauthenticated identity, denied access, or decryption denied by provider policy | ErrPermission |
| throttling, transport failure, provider timeout, server failure, malformed success response, integrity failure, unsupported remote value type, or oversize value | ErrUnavailable |
| invalid context, name, or constructor configuration | ErrInvalid |

This deliberately small taxonomy does not erase the mapping contract. Each driver section defines its provider states exactly and its conformance tests freeze them.

An error may include an exact name only after it has matched the trusted catalog. Invalid and unknown caller input is not echoed. Provider SDK errors are consumed by the driver and never retained in the returned unwrap graph. Drivers classify provider errors by typed SDK errors, gRPC codes, or HTTP status codes, never by matching error-message text.

The public package owns the four sentinel variables shown above. Root code and first-party drivers share syntax validation through `github.com/goforj/secrets/internal/names`, whose complete cross-module API is `func Valid(name string) bool`. Nested driver modules build safe public errors through `github.com/goforj/secrets/internal/driverkit`, which imports the public sentinels and exposes this fixed support API:

```go
func ValidateName(name string) error
func Invalid() error
func NotFound(name string) error
func Permission(name string) error
func Unavailable(name string) error
```

ValidateName delegates to the shared names package and returns a cause-free error matching ErrInvalid without echoing rejected input. Invalid has the same safe behavior for other configuration failures. The remaining constructors return cause-free errors matching their named sentinel and include only the already validated application name. Root code imports names but never driverkit, so the layout has no import cycle. It keeps the application package small and prevents independently versioned first-party drivers from duplicating frozen validation or error formatting. Go's internal import rule permits the nested first-party module paths to share both packages. External Reader implementations cannot import them and own their contracts explicitly.

Although internal prevents application imports, these packages are stable first-party cross-module contracts. Their listed signatures, name grammar, errors.Is identity, cause-free behavior, and disclosure constraints remain compatible throughout the root module's major version. Exact Error text is not a compatibility contract. Root CI compiles and runs the contract suite for every released driver against the newest compatible root before a root release.

## Provider Drivers

Initial drivers should cover:

- mounted files, including Docker secrets and Kubernetes Secret volumes;
- AWS Secrets Manager;
- Google Cloud Secret Manager;
- Azure Key Vault Secrets; and
- HashiCorp Vault KV v2.

Each network driver accepts a local one-method client interface plus bindings and returns a concrete Reader that implements secrets.Reader. The SDK client remains easy to fake without reproducing its full API. Configuration fixes provider locators before the Reader is published:

```go
reader, err := awssecrets.New(client, map[string]string{
	"DATABASE_PASSWORD": "production/orders/database-password",
})
if err != nil {
	return err
}
```

AWS accepts a name-to-locator map because its complete locator is one string and may address another account. Azure and mounted-file clients already fix their vault or root. Google also accepts the resource parent because its client may address multiple projects or locations. Vault uses a small driver-specific binding value because a KV read genuinely requires both a path and field.

Every constructor applies the same rules. It rejects a nil or typed-nil client, an empty binding map, invalid application names, locators that fail the local checks specified by that driver, and invalid driver-specific configuration with a cause-free error matching ErrInvalid. Provider syntax that the driver's local contract deliberately leaves to the service is a read-time ErrUnavailable. The constructor validates the entire input before returning, copies maps and binding values, and never retains caller-mutable configuration. The resulting Reader performs no configuration mutation. The injected client must support concurrent calls; the standard provider SDK clients do. This is a constructor contract for test doubles and custom clients as well.

An application normally chooses one provider Reader. Applications that genuinely use multiple secret providers inject the relevant Readers into the services that own them. V1 does not add a universal multiplexer before a concrete cross-provider use case requires one.

Examples of trusted bindings:

| Application name | Provider binding |
| --- | --- |
| `DATABASE_PASSWORD` | AWS secret ID `production/orders/database-password` |
| `PAYMENTS_SIGNING_KEY` | Google secret name below a configured global or regional parent |
| `MAIL_API_TOKEN` | Azure secret name in the client's configured vault |
| `OAUTH_GITHUB_CLIENT_SECRET` | Vault mount, path, and field |
| `TLS_PRIVATE_KEY` | Relative file below one configured root |

Provider SDK clients are injected into driver construction so tests can control responses and applications can use standard workload identity configuration. Static cloud credentials and secret payloads do not belong in source configuration.

The code that constructs an SDK client or file root retains ownership. Reader has no Close method, and a driver Reader never closes an injected dependency. Application lifecycle code closes owned dependencies after reads stop only when they expose a close operation. The Google client and os.Root are closable; the selected AWS, Azure, and Vault client APIs are not.

### AWS Secrets Manager

The module path is `github.com/goforj/secrets/driver/awssecrets`. Its constructor and local client seam are:

```go
type Client interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

func New(client Client, bindings map[string]string) (*Reader, error)
```

Each locator must contain between 1 and 2,048 bytes, the complete local validation required by the AWS driver. AWS remains authoritative for whether that string is an accepted full ARN or secret name. The driver sends only SecretId, leaving VersionId and VersionStage unset so AWS selects AWSCURRENT. Cross-account use requires an ARN.

Exactly one of SecretString or SecretBinary must be populated. SecretString returns its exact Go string; SecretBinary converts its bytes directly to a string. Neither field, both fields, a nil response, or an empty payload is ErrUnavailable because AWS documents a minimum payload length of one.

AWS API error codes have this complete mapping:

| Classification | AWS error codes |
| --- | --- |
| ErrNotFound | ResourceNotFoundException |
| ErrPermission | AccessDeniedException, IncompleteSignature, InvalidClientTokenId, NotAuthorized, OptInRequired, UnrecognizedClientException |
| ErrUnavailable | DecryptionFailure, EncryptionFailure, InternalFailure, InternalServiceError, InvalidAction, InvalidNextTokenException, InvalidParameterException, InvalidRequestException, LimitExceededException, MalformedPolicyDocumentException, PreconditionNotMetException, PublicPolicyException, RequestExpired, ResourceExistsException, ServiceUnavailable, ThrottlingException, ValidationError, ValidationException, and every unrecognized code |

The driver extracts only structured Smithy error codes. It does not inspect message text to guess whether InvalidRequestException means scheduled deletion, and it does not return the AWS error in its unwrap graph. The conformance suite covers every listed code.

### Google Cloud Secret Manager

The module path is `github.com/goforj/secrets/driver/gcpsecrets`. Its constructor and local client seam are:

```go
type Client interface {
	AccessSecretVersion(context.Context, *secretmanagerpb.AccessSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
}

func New(client Client, parent string, bindings map[string]string) (*Reader, error)
```

Parent must be exactly `projects/{project}` for a global secret or `projects/{project}/locations/{location}` for a regional secret, with nonempty project and location segments. Each locator contains 1 to 255 ASCII letters, digits, hyphens, or underscores. The driver requests `{parent}/secrets/{id}/versions/latest`, so the injected client does not falsely imply project scope. The caller must construct the client with the regional endpoint when using a regional parent.

The driver requires a nonnil response, nonnil payload, nonnil DataCrc32C, and a matching CRC32C Castagnoli checksum before returning payload bytes as a string. Missing or mismatched integrity data and malformed responses are ErrUnavailable. NotFound and FailedPrecondition for an unavailable latest version are ErrNotFound. Unauthenticated and PermissionDenied are ErrPermission. ResourceExhausted, Aborted, Internal, Unavailable, provider deadlines, transport failures, and all unclassified statuses are ErrUnavailable.

### Azure Key Vault Secrets

The module path is `github.com/goforj/secrets/driver/azuresecrets`. Its constructor and local client seam are:

```go
type Client interface {
	GetSecret(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
}

func New(client Client, bindings map[string]string) (*Reader, error)
```

The injected Azure client already fixes one vault URL. Each locator contains 1 to 127 ASCII letters, digits, or hyphens. The driver passes an empty version string to request the latest version and nil options. Azure values are text-only. A nil Value is ErrUnavailable; a returned empty string is valid. HTTP 404, including ordinary reads of deleted secrets, is ErrNotFound. HTTP 401 and 403 are ErrPermission. HTTP 408, 429, 5xx, transport errors, and all other statuses are ErrUnavailable. The driver does not make the broader GetDeletedSecret request.

### HashiCorp Vault KV v2

The module path is `github.com/goforj/secrets/driver/vaultsecrets`. The caller fixes the mount by supplying a KVv2 client:

```go
type Client interface {
	Get(context.Context, string) (*api.KVSecret, error)
}

type Binding struct {
	Path  string
	Field string
}

func New(client Client, bindings map[string]Binding) (*Reader, error)
```

Path and Field must each contain between 1 and 1,024 bytes. Path must be relative, must not begin or end with a slash, and must not contain an empty, dot, or dot-dot segment. Field is an exact map key and is not split or normalized. Get requests the latest version. A nil secret, nil Data from a soft-deleted or destroyed latest version, or missing field is ErrNotFound. V1 accepts only a Go string field, including an empty string; null, numbers, booleans, arrays, objects, and other types are ErrUnavailable rather than being stringified. Vault 401 and 403 are ErrPermission, Vault 404 is ErrNotFound, and throttling, transport, 5xx, malformed responses, and every other failure are ErrUnavailable. The driver never falls back to an older version.

### Mounted Files

The module path is `github.com/goforj/secrets/driver/filesecrets`. Its constructor borrows an already opened root:

```go
const MaxFileBytes = 1 << 20

func New(root *os.Root, bindings map[string]string) (*Reader, error)
```

The file driver requires Go 1.25.12 or newer because it depends on the patched os.Root confinement behavior. The root remains caller-owned. Locators contain at most 4,096 bytes and must be nonempty relative slash-separated paths without NUL, backslash, empty, dot, or dot-dot components and without a trailing slash. Contained symlinks remain allowed for Kubernetes projected-volume layouts; os.Root rejects escapes. Bind mounts created by a privileged actor inside the root are outside the threat model.

For each read on Unix, a platform helper calls root.OpenFile once with read-only and nonblocking flags. It then stats that same descriptor, rejects anything that is not a regular file, reads through a MaxFileBytes plus one limit, and closes the descriptor. Nonblocking open prevents an unconnected FIFO from hanging before the type check. The Windows helper performs the same one-descriptor sequence with its ordinary read-only flag because Windows filesystem opens do not have Unix FIFO semantics. The helper never checks one path and opens another.

Directories, FIFOs, sockets, devices, symlink loops, files that grow beyond the limit, and every oversize result are ErrUnavailable. Missing files are ErrNotFound and filesystem permission failures are ErrPermission. It does not trim bytes. An unconnected FIFO test has a strict timeout and must return ErrUnavailable without a writer.

V1 supports Linux, macOS, and Windows. New operating systems require the same confinement suite before support is enabled; New returns ErrInvalid on an unsupported target. This deliberately excludes js, where os.Root documents incomplete escape protection, and platforms whose rename semantics have not been validated. Linux integration coverage includes Docker and Kubernetes layouts.

## Provider Value Capabilities

| Driver | Selected value | Returned representation | Empty value | Validation |
| --- | --- | --- | --- | --- |
| AWS | AWSCURRENT | SecretString text or SecretBinary bytes | Provider rejects empty | Exactly one nonempty union field |
| Google | `latest` alias | Arbitrary payload bytes | Accepted if service returns it | CRC32C required and verified |
| Azure | Latest version | UTF-8 text from Value | Empty accepted if returned | Value must be nonnil |
| Vault KV v2 | Latest version only | Selected string field | Empty string accepted | Data and field present; field type must be string |
| Mounted file | Contents at read time | Arbitrary file bytes | Empty file accepted | Regular file, confined path, fixed size limit |

The cross-provider fidelity guarantee is capability-aware: every driver preserves exactly the values accepted by its documented driver contract. The library does not claim that every provider or driver can accept every value shape or size.

Vault authentication remains externally managed in v1. The driver reads KV v2 data with a configured mount, path, and field but does not own login, token renewal, or reauthentication.

## Security Boundary

First-party drivers never log and never return raw provider errors. Their public errors contain only a stable classification and, optionally, an application name that already matched the trusted bindings. Secret payloads, provider locators, request objects, tenant data, and credentials are never included.

That guarantee does not cover arbitrary Reader or Func implementations, logging configured on an injected SDK client, provider-side audit logs, process memory after Get returns, or application handling of the returned string. Construction documentation must warn that provider request metadata can contain configured locators and that SDK logging must be reviewed before enabling it. Tests install recording log sinks where SDKs permit them and assert that first-party driver code emits nothing.

The mounted-file boundary protects against path traversal and symlink escape by an unprivileged writer beneath the supplied root. It does not protect against a privileged actor changing mounts within that root, reading application memory, or replacing the root object supplied during trusted construction.

## Retries, Caching, And Rotation

V1 should begin without a public retry, cache, invalidation, refresh, revision, or metadata API.

Each network driver invokes its injected client method exactly once per Get and adds no retry loop. The client owner configures SDK retries, backoff, and per-attempt limits before injection; the driver never mutates that policy or passes per-operation retry overrides. The caller context bounds the complete Get call. Driver tests assert one client-method invocation and exact context propagation, but cannot promise a universal network-attempt count because SDK retry configuration is external.

Production construction should use bounded SDK retry settings and a caller deadline. These are operational recommendations, not hidden constructor behavior. A later reusable retry or cache implementation may wrap a Reader without changing its interface. It requires its own evidence, limits, cancellation rules, stale-value policy, and concurrency tests before adoption.

Applications normally consume a secret while constructing another resource. Rotation becomes useful only when the application can replace and drain that resource safely. The Secrets library does not claim that observing a new provider value rotates a database pool, signer, token source, or HTTP client.

Applications may read required secrets before reporting ready. Liveness never depends on a secret provider. Detailed provider health stays internal to construction and operations rather than expanding Reader.

## Framework Relationship

GoForj does not need a Secrets component, render configuration, generated accessor, template, or dependency pin. A GoForj application may construct and inject a `secrets.Reader` through ordinary application wiring exactly as it would use any framework-agnostic Go library.

Required startup reads are application lifecycle decisions. Applications may read required secrets before reporting ready, while lazy consumers may read them when constructing or invoking the dependent resource. The library does not encode those policies in framework configuration.

## Environment Variables

Environment variables remain part of the existing env package:

```go
password := env.MustGet("DATABASE_PASSWORD")
```

Some environment values are sensitive, but their loading, precedence, scoping, reload behavior, and access semantics do not change because of that classification. The env package already warns callers not to pass secrets to `env.Dump`.

No new environment-secret API or package is required. Applications explicitly choose whether `DATABASE_PASSWORD` is supplied through env or a managed-provider Reader. Neither library performs fallback or precedence between the two.

## Testing

Root tests cover:

- valid, invalid, maximum-length, and unknown names;
- Func name validation, nil behavior, cancellation, and concurrency;
- secretstest fixture validation, copying, missing names, and empty values; and
- returned values absent from library-generated errors and failure messages.

Each provider driver has a shared contract suite covering complete binding validation, exact dispatch, current-value selection, its complete error-mapping table, arbitrary byte strings where supported, malformed success responses, safe error normalization, one injected-client invocation, context propagation, cancellation, and concurrency. Tests include every constructor rejection branch and verify that a failed constructor retains no partially valid configuration.

Each network driver release runs its own real-provider integration suite. A root or unrelated driver release does not require credentials for every provider. Mounted-file integration tests exercise ordinary files, Docker-style mounts, Kubernetes projected-volume rotation layouts, size limits, and path-escape races. Race-enabled tests cover every Reader and its standard SDK client.

Mocks and emulators improve fast feedback but do not replace live compatibility tests. Every live suite creates uniquely named isolated resources, uses conspicuously public fixture values, scopes credentials to those resources, and registers cleanup before the first operation that can fail. CI retains no fetched values, provider response bodies, or verbose SDK logs.

## Modules And Releases

The repository contains these independently testable Go modules and tag prefixes:

| Module | Release tag |
| --- | --- |
| `github.com/goforj/secrets` | `vX.Y.Z` |
| `github.com/goforj/secrets/secretstest` | `secretstest/vX.Y.Z` |
| `github.com/goforj/secrets/driver/awssecrets` | `driver/awssecrets/vX.Y.Z` |
| `github.com/goforj/secrets/driver/gcpsecrets` | `driver/gcpsecrets/vX.Y.Z` |
| `github.com/goforj/secrets/driver/azuresecrets` | `driver/azuresecrets/vX.Y.Z` |
| `github.com/goforj/secrets/driver/vaultsecrets` | `driver/vaultsecrets/vX.Y.Z` |
| `github.com/goforj/secrets/driver/filesecrets` | `driver/filesecrets/vX.Y.Z` |

An untagged `github.com/goforj/secrets/integration` module orchestrates repository-wide tests but is not an application dependency. During repository development, sibling modules retain relative replace directives so unpublished changes are tested together. Release automation publishes the root first, then secretstest and affected drivers against that published root version. It verifies every tag independently and runs each affected module with GOWORK disabled so no release is accidentally resolved through local replacements.

The root, secretstest, and network-driver modules initially use Go 1.24.4, matching the established GoForj driver baseline, unless an SDK's minimum version is higher when implementation begins. The file driver alone starts at Go 1.25.12 for the patched os.Root behavior. A driver dependency cannot raise the root module's Go version. Any later minimum-version increase is scoped and documented per module.

## Compatibility

Before v1, freeze only:

- `Reader.Get(context.Context, string) (string, error)`;
- name grammar and maximum length;
- exact value preservation;
- stable error classifications; and
- current-value semantics documented by each driver.

The internal names and driverkit cross-module contracts are also frozen for first-party driver compatibility, even though applications cannot import them. A root release must preserve source compatibility with every released driver in the same root major version and pass those drivers' contract tests.

Adding a driver does not change the root application API. Changing name grammar, empty-value handling, byte preservation, error classification, or current-value behavior is a runtime compatibility change.

The root module does not depend on GoForj or provider SDKs. Secretstest and each driver depend on a released root version. Each network driver pins only its own SDK and is released independently.

## Implementation Plan

### Phase 1: Minimal root

Implement Reader, Func, name validation, four safe error classifications, internal driver helpers, the immutable map-backed test Reader, and the shared driver conformance suite.

### Phase 2: Local driver

Implement the mounted-file driver and its confinement, exact-byte, Docker, and Kubernetes volume tests.

### Phase 3: Managed providers

Implement AWS, Google Cloud, Azure, and Vault driver modules. Require shared contract tests and each driver's own real-service integration coverage for that driver release.

### Phase 4: Evidence-driven additions

Add caching, retries, richer errors, metadata, dynamic version selection, or rotation coordination only after concrete applications demonstrate the need and the behavior can remain behind the minimal Reader where possible.

## Acceptance Criteria

1. Normal application use is one Get call returning a string.
2. Reader has no key wrapper, result wrapper, disclosure accessor, options, metadata, or lifecycle methods.
3. Names use uppercase configuration-style vocabulary and never imply paths or hierarchy.
4. Callers cannot choose a provider or locator at read time.
5. Values round-trip exactly within each provider's documented capabilities, including arbitrary bytes and empty values where supported.
6. First-party library errors and diagnostics never disclose returned values, locators, credentials, or provider SDK errors.
7. Every production driver passes shared contract tests and real-service integration coverage.
8. Environment variables continue to use env.MustGet and require no new package or API.
9. GoForj requires no component, render configuration, generated accessor, template, or special integration.
10. The design makes no zeroization, automatic rotation, atomic bulk-read, or exactly-once claim.
11. Each constructor, provider request, success shape, current-value rule, error mapping, retry owner, lifecycle owner, and module release path is specified without requiring implementation-time API invention.

## References

- [AWS Secrets Manager GetSecretValue](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html)
- [Google Cloud Secret Manager access version](https://cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access)
- [Azure Key Vault Get Secret](https://learn.microsoft.com/en-us/rest/api/keyvault/secrets/get-secret)
- [HashiCorp Vault KV v2](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2)
- [Kubernetes mounted Secret updates](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Go string specification](https://go.dev/ref/spec#String_types)
- [Go os.Root](https://pkg.go.dev/os#Root)
- [GO-2026-4970 os.Root path escape](https://pkg.go.dev/vuln/GO-2026-4970)
