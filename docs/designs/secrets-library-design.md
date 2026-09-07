# Secrets Library Design

## Status

- Design status: proposed
- Planning date: 2026-09-06
- Last simplified: 2026-09-07
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
13. Readers are safe for concurrent use.
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
4. Keep errors and diagnostics free of secret material.
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

That interface is intentionally sufficient for domain code and application services. `Func` makes one-off adapters and focused tests possible without a mock framework. Its Get method validates the name before invoking the function. A nil Func returns ErrInvalid rather than panicking.

The companion `secretstest` package provides the common map-backed test case:

```go
reader := secretstest.New(t, map[string]string{
	"DATABASE_PASSWORD": "public-test-password",
})
```

`secretstest.New` marks itself as a test helper, validates and copies its input, fails the test for an invalid fixture, and returns an immutable Reader. Tests that need failures or changing values use `secrets.Func` and keep that behavior explicit in the test.

## Names

Names follow the same vocabulary applications already use for environment configuration:

```text
DATABASE_PASSWORD
MAIL_API_TOKEN
PAYMENTS_SIGNING_KEY
TLS_PRIVATE_KEY
```

A name must begin with an uppercase ASCII letter or underscore and continue with uppercase ASCII letters, digits, or underscores. It has a fixed maximum length of 253 bytes. The library does not trim, uppercase, prefix, split, or otherwise normalize input.

The name is application vocabulary, not a provider locator. `DATABASE_PASSWORD` may map to an AWS secret ID, Google resource, Azure vault entry, Vault field, or mounted file without changing application code.

Invalid input is rejected before lookup. Unknown input does not reach a provider.

## Value Semantics

`Get` returns a Go string containing the exact bytes produced by the bound provider read. Go strings are byte sequences and do not require valid UTF-8.

The library does not trim whitespace, remove newlines, parse JSON, decode base64, parse PEM, or reject NUL bytes. A text-only provider must return its exact UTF-8 bytes. A byte-capable provider preserves arbitrary bytes.

The empty string is a valid fetched value. Applications decide whether a particular credential may be empty.

Returning a plain string means the library cannot redact it after return. This is deliberate. Callers must not log, dump, serialize, persist, or include the returned value in errors. The library protects its own errors, test history, and provider integrations, but does not pretend to control ordinary application memory.

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

Provider-specific authentication, throttling, disabled-version, destroyed-version, transport, decoding, and server failures map to the closest stable classification. The mapping is documented and tested per driver. V1 does not expose a large public error taxonomy before applications demonstrate a need to branch on those distinctions.

An error may include an exact name only after it has matched the trusted catalog. Invalid and unknown caller input is not echoed. Provider SDK errors are consumed by the driver and never retained in the returned unwrap graph.

## Provider Drivers

Initial drivers should cover:

- mounted files, including Docker secrets and Kubernetes Secret volumes;
- AWS Secrets Manager;
- Google Cloud Secret Manager;
- Azure Key Vault Secrets; and
- HashiCorp Vault KV v2.

Each driver accepts an injected provider client plus typed bindings and returns a complete Reader. Configuration fixes provider locators and read strategies before the Reader is published:

```go
reader, err := awssecrets.New(client, map[string]string{
	"DATABASE_PASSWORD": "production/orders/database-password",
})
if err != nil {
	return err
}
```

AWS, Google, Azure, and mounted-file constructors accept a name-to-locator map because each configured client already fixes its account, project, vault, or root. Vault uses a small driver-specific binding value because a KV read genuinely requires both a path and field. Constructors copy and validate the complete map, reject nil injected clients, invalid names, and empty locators, then publish an immutable concurrent Reader.

An application normally chooses one provider Reader. Applications that genuinely use multiple secret providers inject the relevant Readers into the services that own them. V1 does not add a universal multiplexer before a concrete cross-provider use case requires one.

Examples of trusted bindings:

| Application name | Provider binding |
| --- | --- |
| `DATABASE_PASSWORD` | AWS secret ID `production/orders/database-password` |
| `PAYMENTS_SIGNING_KEY` | Google secret name in the client's configured project |
| `MAIL_API_TOKEN` | Azure vault URL and secret name |
| `OAUTH_GITHUB_CLIENT_SECRET` | Vault mount, path, and field |
| `TLS_PRIVATE_KEY` | Relative file below one configured root |

Provider SDK clients are injected into driver construction so tests can control responses and applications can use standard workload identity configuration. Static cloud credentials and secret payloads do not belong in source configuration.

The code that constructs an SDK client or file root owns and closes it. Reader has no Close method, and a driver Reader does not acquire independent ownership of an injected client. Application lifecycle code closes each owned dependency exactly once after reads have stopped.

The mounted-file driver confines configured relative paths below its root, follows projected-volume symlinks only through race-safe containment, reads one bounded file descriptor, and preserves exact bytes.

Vault authentication remains externally managed in v1. The driver reads KV v2 data with a configured mount, path, and field but does not own login, token renewal, or reauthentication.

## Retries, Caching, And Rotation

V1 should begin without a public retry, cache, invalidation, refresh, revision, or metadata API.

Provider SDK retry behavior must be explicitly bounded during driver construction. A later reusable retry or cache implementation may wrap a Reader without changing its interface. It requires its own evidence, limits, cancellation rules, stale-value policy, and concurrency tests before adoption.

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
- returned values absent from errors and test histories.

Each provider driver has a shared contract suite covering complete binding validation, exact dispatch, arbitrary byte strings where supported, safe error normalization, cancellation, and concurrency. Release validation includes real AWS, Google Cloud, Azure, and Vault services. Mounted-file integration tests exercise ordinary files, Docker-style mounts, Kubernetes projected-volume rotation layouts, size limits, and path-escape races.

Mocks and emulators improve fast feedback but do not replace live compatibility tests. Every integration suite creates isolated resources, uses conspicuously public fixtures, and cleans them up.

## Compatibility

Before v1, freeze only:

- `Reader.Get(context.Context, string) (string, error)`;
- name grammar and maximum length;
- exact value preservation;
- stable error classifications; and
- current-value semantics documented by each driver.

Adding a driver does not change the root application API. Changing name grammar, empty-value handling, byte preservation, error classification, or current-value behavior is a runtime compatibility change.

The root module does not depend on GoForj or provider SDKs. Each driver module pins its own SDK and is released independently.

## Implementation Plan

### Phase 1: Minimal root

Implement Reader, Func, name validation, four safe error classifications, the immutable map-backed test Reader, and the shared driver conformance suite.

### Phase 2: Local driver

Implement the mounted-file driver and its confinement, exact-byte, Docker, and Kubernetes volume tests.

### Phase 3: Managed providers

Implement AWS, Google Cloud, Azure, and Vault driver modules. Require shared contract tests and real-service integration coverage for every release.

### Phase 4: Evidence-driven additions

Add caching, retries, richer errors, metadata, dynamic version selection, or rotation coordination only after concrete applications demonstrate the need and the behavior can remain behind the minimal Reader where possible.

## Acceptance Criteria

1. Normal application use is one Get call returning a string.
2. Reader has no key wrapper, result wrapper, disclosure accessor, options, metadata, or lifecycle methods.
3. Names use uppercase configuration-style vocabulary and never imply paths or hierarchy.
4. Callers cannot choose a provider or locator at read time.
5. Values round-trip exactly, including arbitrary byte strings and empty values.
6. Errors and diagnostics never disclose returned values, locators, credentials, or provider SDK errors.
7. Every production driver passes shared contract tests and real-service integration coverage.
8. Environment variables continue to use env.MustGet and require no new package or API.
9. GoForj requires no component, render configuration, generated accessor, template, or special integration.
10. The design makes no zeroization, automatic rotation, atomic bulk-read, or exactly-once claim.

## References

- [AWS Secrets Manager GetSecretValue](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html)
- [Google Cloud Secret Manager access version](https://cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access)
- [Azure Key Vault Get Secret](https://learn.microsoft.com/en-us/rest/api/keyvault/secrets/get-secret)
- [HashiCorp Vault KV v2](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2)
- [Kubernetes mounted Secret updates](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Go string specification](https://go.dev/ref/spec#String_types)
