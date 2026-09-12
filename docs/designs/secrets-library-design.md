# Secrets Library Design

## Status

- Design status: implementation-ready with provider error and release verification requirements
- Planning date: 2026-09-06
- Last reviewed: 2026-09-12
- Target repositories: `github.com/goforj/secrets` and independent provider driver modules
- Primary scope: simple application-facing read, write, and delete operations over configured secret sources

## Summary

The library should provide direct operations for application code to read, write, and delete a value at a configured secret source:

```go
password, err := secretReader.Get(ctx, "DATABASE_PASSWORD")
if err != nil {
	return err
}

err = secretStore.Set(ctx, "DATABASE_PASSWORD", replacement)
if err != nil {
	return err
}

err = secretStore.Delete(ctx, "DATABASE_PASSWORD")
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

Provider selection, provider locators, retries, and other operational policy belong to trusted construction. They are not request-time arguments. Managed-provider drivers implement the full Store contract. Naturally read-only sources, such as Docker and Kubernetes mounted secrets, implement Reader without pretending that writes are possible.

Environment variables remain ordinary configuration owned by `github.com/goforj/env/v2`:

```go
password := env.MustGet("DATABASE_PASSWORD")
```

There is no `envsecrets` package, environment driver, secret-value wrapper, automatic fallback, or shared environment and provider abstraction.

## Decision

Create a small `github.com/goforj/secrets` module and independent provider driver modules with these rules:

1. The application-facing operations are `Get`, `Set`, and `Delete` with ordinary strings.
2. Reader, Writer, and Deleter remain separate capability interfaces; Store composes all three.
3. Names use uppercase environment-style configuration vocabulary such as `DATABASE_PASSWORD`.
4. Names are exact opaque identifiers. Underscores do not create a hierarchy and names never become provider paths.
5. A trusted mutable binding maps each name one-to-one to an entire provider secret resource; aliases are rejected.
6. Callers cannot provide provider locators, endpoints, accounts, projects, vaults, mounts, or filesystem paths at operation time.
7. `Get` returns the fetched value directly and `Set` accepts the replacement directly. There is no value wrapper or disclosure accessor.
8. Go strings preserve exact bytes, including empty, NUL, newline, and invalid UTF-8 sequences, within each provider's capabilities.
9. Delete requests deletion of the entire bound secret resource and all of its versions using the provider's standard recovery behavior. Provider consistency may allow a brief read after success; Delete does not promise immediate physical erasure or purge.
10. The library never places a secret value or derived fingerprint in logs, errors, or diagnostics. It only encodes values and computes provider-required integrity checks as part of the requested provider operation.
11. Returned errors contain no value, locator, provider response, credential, token, tenant identifier, or provider SDK error.
12. V1 reads and writes the provider's current or latest value. Exact-version and alias APIs wait for a demonstrated application need.
13. Policy management, access grants, and rotation orchestration remain provider or operator responsibilities.
14. First-party implementations are safe for concurrent use when their injected client satisfies the documented concurrency contract.
15. Set and Delete are serialized per binding within one Store instance. The library does not claim cross-binding transactions or coordination across Store instances and provider-native writers.
16. Every provider driver has contract tests and real-service integration coverage for every capability it exposes.
17. Environment variables continue to use the existing env package and are not a Secrets driver.

## Why The API Is This Small

Most consumers need a credential string to construct a database pool, HTTP client, mail client, signer, or provider SDK. Returning a wrapper and requiring a second method call makes every normal read harder without preventing the application from disclosing the value a moment later.

The reusable boundary still matters. It hides provider SDKs and locators, centralizes safe errors, and lets deployment choose a provider without changing domain code. Those benefits do not require a large application API.

Features should not enter the public Reader merely because a provider exposes them. Versions, aliases, provider metadata, batching, invalidation, and health details wait until an application use case proves that they belong in the domain-facing contract.

## Goals

1. Make normal reads, writes, and deletes one call each.
2. Keep provider details out of application code.
3. Preserve returned bytes exactly.
4. Keep library-owned errors and diagnostics free of secret material.
5. Support deterministic tests with a tiny interface.
6. Remain framework-agnostic and usable through ordinary dependency injection.
7. Verify every production driver against the real provider contract.

## Non-goals

1. A redacting value wrapper.
2. Runtime selection of versions, aliases, fields, providers, or locators.
3. Remote secret-resource creation or provider administration, including policy, grants, replication changes, purge, recovery, and forced destruction.
4. Automatic environment-variable fallback.
5. Environment or dotenv loading.
6. A universal configuration system.
7. Atomic multi-secret transactions.
8. Exactly-once rotation or change notification.
9. Guaranteed memory zeroization in Go.
10. Returning provider SDK errors or response bodies.
11. Adding cache, retry, bulk-read, or metadata APIs before demonstrated use cases require them.
12. Provisioning access policies, credentials, projects, vaults, mounts, or other provider infrastructure.

## Public API

The root application contracts are:

```go
package secrets

type Reader interface {
	Get(ctx context.Context, name string) (string, error)
}

type Writer interface {
	Set(ctx context.Context, name string, value string) error
}

type Deleter interface {
	Delete(ctx context.Context, name string) error
}

type Store interface {
	Reader
	Writer
	Deleter
}

type Func func(ctx context.Context, name string) (string, error)

func (f Func) Get(ctx context.Context, name string) (string, error)
```

Consumers accept the narrowest capability they need. Most application services depend only on Reader. Administrative commands, bootstrap code, and rotation workflows can depend on Writer, Deleter, or Store. This narrows compile-time use and discourages accidental mutation; it is not an authority boundary because a concrete Store can be recovered by type assertion. Provider credentials and process isolation enforce actual write authority.

`Func` remains a focused Reader adapter rather than growing callbacks for every operation. Get first panics with the fixed message `secrets: nil Func` when the Func itself is nil, treating nil as bad wiring rather than a request failure. A nonnil Func then rejects a nil context and validates the name before invoking the function.

The companion `secretstest` package provides the common map-backed test case:

```go
reader := secretstest.New(t, map[string]string{
	"DATABASE_PASSWORD": "public-test-password",
})
```

Its complete constructor is `func New(t testing.TB, values map[string]string) secrets.Store`. It marks itself as a test helper, validates and copies its input, fails the test for an invalid fixture, and returns a concurrency-safe in-memory Store whose configured-name catalog remains fixed. Get on an unknown or deleted name returns ErrNotFound. Set replaces an existing configured value but returns ErrNotFound for an unknown or deleted name. Delete is idempotent for a configured name and removes its current value without removing its binding. An explicitly empty fixture or Set value remains valid. Tests that need read failures or changing computed values use `secrets.Func`; tests that need mutation failures define the narrow interface directly so failure behavior remains explicit.

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

The name is application vocabulary, not a provider locator. `DATABASE_PASSWORD` may map to an AWS ARN, Google resource, Azure vault entry, Vault path, or mounted file without changing application code.

Invalid input is rejected before lookup. Unknown input does not reach a provider.

## Value Semantics

On success, `Get` returns a Go string containing the exact bytes produced by the bound provider read. Go strings are byte sequences and do not require valid UTF-8. Each driver rejects a response beyond its explicit read cap with ErrUnavailable before converting it to the returned value.

The library does not trim whitespace, remove newlines, parse JSON, decode base64, parse PEM, or reject NUL bytes. A text-only provider must return its exact UTF-8 bytes. A byte-capable provider preserves arbitrary bytes.

The empty string is a valid fetched value. Applications decide whether a particular credential may be empty.

Returning a plain string means the library cannot redact it after return. This is deliberate. Callers must not log, dump, serialize, persist, or include the returned value in errors. The library protects its own errors and provider integrations, but does not pretend to control ordinary application memory or caller-authored test output.

## Mutation Semantics

Set publishes a new current value at a trusted, already provisioned binding. It does not intentionally create the remote secret resource. A resource observed missing, deleted, or destroyed is ErrNotFound. Resource provisioning remains explicit because creating a secret can require KMS, replication, residency, expiry, tags, ownership, and policy decisions that have no safe cross-provider default. Providers without conditional writes can still race an external delete-and-recreate operation; each affected driver documents that limit.

Set does not change resource-level policy, labels, tags, replication, expiry, recovery, or access grants. Provider-specific version metadata is either preserved by the provider or supplied as trusted binding configuration, as documented by each driver.

A successful Set means the provider accepted the exact value for publication as current. Visibility follows the provider's documented consistency model. The library adds no local read-your-write cache.

One Store instance serializes Set and Delete calls for the same binding from preflight through final reconciliation. Acquisition is context-aware: if the caller context ends while queued, the operation returns promptly with the matching context error, does not match ErrIndeterminate, and makes no provider call. Calls for different bindings and all Get calls may proceed concurrently. This prevents the Store's own Delete from invalidating an in-flight Set reconciliation. It does not coordinate separate Store instances or external provider writers, so the exclusive lifecycle ownership rule still applies. Concurrent Set calls complete in their serialized order; provider-native rotation may publish another value afterward.

Delete is idempotent for a valid bound name while that binding continues to identify the same resource generation. It always targets the entire provider secret resource, never one field or one version. Success means the provider accepted its standard resource deletion operation or the resource was already absent. Some providers complete deletion asynchronously, so a concurrent or immediately subsequent Get may briefly observe the prior value. Recovery windows, retained recoverable data, and reuse delays remain provider-specific and are documented by each driver. Delete does not promise immediate physical erasure or purge. Deleting, recreating, and then retrying is a new lifecycle operation rather than an idempotent retry.

Set and Delete operate only on configured bindings. They never accept locators or provider options. A driver that cannot safely implement these semantics exposes Reader rather than returning ErrUnsupported at runtime.

A mutable binding requires exclusive lifecycle ownership by that Store. Other actors must not delete, purge, recreate, repoint, or repurpose the bound resource concurrently. Provider-native rotation that only publishes a new version remains allowed. AWS's immutable full ARN and Google's Delete ETag narrow individual race windows, but neither changes the general ownership rule for a mutation-capable binding. Applications that cannot provide that ownership use the driver only through Reader and perform destructive administration through provider-specific tooling.

## Errors

V1 exposes only stable classifications that application code can reasonably act on:

```go
var (
	ErrInvalid     = errors.New("secrets: invalid request")
	ErrNotFound    = errors.New("secrets: not found")
	ErrPermission  = errors.New("secrets: access denied")
	ErrUnavailable = errors.New("secrets: source unavailable")
	ErrIndeterminate = errors.New("secrets: mutation outcome unknown")
)
```

Errors support `errors.Is`. Context cancellation and deadlines remain detectable through `errors.Is(err, context.Canceled)` and `errors.Is(err, context.DeadlineExceeded)`.

Every operation on a nonnil built-in Reader applies this order:

1. a nil context returns ErrInvalid;
2. invalid name syntax returns ErrInvalid without echoing the input;
3. a valid name absent from the configured bindings returns ErrNotFound without provider I/O;
4. after provider I/O fails, a canceled or expired caller context returns the matching context error; and
5. every other provider result follows the normative mapping below.

Set and Delete apply the same context, name, and binding precedence. Set additionally validates the value against the selected driver's documented size and representation limits before provider I/O. Delete converts a provider not-found result to success after the binding has matched, preserving its idempotent contract.

A mutation can commit remotely before its response is lost or before a provider returns another error. If a driver cannot prove from the provider contract or an explicit read-back reconciliation that Set or Delete did not commit, it returns an error matching ErrIndeterminate instead of classifying only the final SDK error. This includes permission, throttle, conflict, timeout, internal, service-unavailable, and transport errors returned after a mutating client method may have dispatched. A local rejection or read-only preflight failure retains its definite classification. Provider-specific not-found and precondition responses remain definite only where the operation cannot have committed, while Delete not-found is success when it proves the desired same-generation state. When the caller context also ended, an indeterminate error matches both ErrIndeterminate and the corresponding context error. Get never returns ErrIndeterminate.

For a mutation, a nil SDK error is the provider client's authoritative acknowledgement. The driver does not expose or depend on mutation response metadata, so it ignores the response body, including a nil pointer or zero-value response paired with a nil error. Read and preflight responses remain subject to the field validation specified by each driver. This contract keeps injected clients simple and avoids inventing portable meaning for provider-specific mutation metadata.

| Condition | Public classification |
| --- | --- |
| unknown binding, missing remote object, or provider explicitly reports no readable current or latest value | ErrNotFound |
| unauthenticated identity, denied access, or decryption denied by provider policy before mutation dispatch | ErrPermission |
| throttling, Get transport failure, definite read or preflight timeout or server rejection, malformed read success response, integrity failure, unsupported remote value type, or oversize provider response | ErrUnavailable |
| invalid context, name, value, or constructor configuration | ErrInvalid |
| mutation outcome cannot be proven from the provider contract | ErrIndeterminate |

This deliberately small taxonomy does not erase the mapping contract. Each driver section defines its provider states exactly and its conformance tests freeze them.

An error may include an exact name only after it has matched the trusted catalog. Invalid and unknown caller input is not echoed. Provider SDK errors are consumed by the driver and never retained in the returned unwrap graph. Drivers classify provider errors by typed SDK errors, gRPC codes, HTTP status codes, or explicitly documented bounded structured error fields, never by matching rendered error-message text.

The root and each independently versioned driver own their small private validation and safe-error helpers. Drivers do not import root internal packages. Direct tests freeze identical name grammar, errors.Is behavior, cause-free errors, and disclosure constraints in every module without creating a hidden cross-module API.

ErrIndeterminate is never a generic blind-retry signal. Retrying Set with the same value is value-idempotent but may create another provider version; a caller that must avoid version churn reads and compares before retrying. Delete retry safety depends on proving that the same resource generation is still targeted. AWS's immutable ARN provides that identity. Google, Azure, and Vault callers rely on the binding's exclusive lifecycle ownership before retrying; Google's ETag protects the race inside one Delete call but a new call obtains new metadata. A Vault CAS conflict after an indeterminate Put requires read-back reconciliation because the first Put may have committed.

## Provider Drivers

Initial drivers should cover:

- mounted files, including Docker secrets and Kubernetes Secret volumes;
- AWS Secrets Manager;
- Google Cloud Secret Manager;
- Azure Key Vault Secrets; and
- HashiCorp Vault KV v2.

Each network driver accepts its provider client plus bindings and returns a concrete Store. Every driver uses a narrow local interface matching only the SDK methods it calls. The Vault client is a configured KV v2 client for one mount, not a general API client. Tests use interface fakes for contract coverage and provider-supported test infrastructure for integration coverage. Configuration fixes provider locators before the Store is published:

```go
secretStore, err := awssecrets.New(client, map[string]awssecrets.Binding{
	"DATABASE_PASSWORD": {
		ARN:      "arn:aws:secretsmanager:us-east-1:123456789012:secret:production/orders/database-password-AbCdEf",
		Encoding: awssecrets.Text,
	},
})
if err != nil {
	return err
}
```

AWS bindings use full ARNs. Azure and mounted-file clients already fix their vault or root. Google also accepts the resource parent because its client may address multiple projects or locations. Vault bindings use dedicated KV v2 paths.

Every constructor applies the same rules. It rejects a nil or typed-nil client, an empty binding map, invalid application names, locators that fail the local checks specified by that driver, and invalid driver-specific configuration with a cause-free error matching ErrInvalid. Provider syntax that the driver's local contract deliberately leaves to the service is an operation-time ErrUnavailable. The constructor validates the entire input before returning, copies maps and binding values, and never retains caller-mutable configuration. The resulting implementation performs no configuration mutation. The injected client must support concurrent calls; the standard provider SDK clients do. This is a constructor contract for test doubles and custom clients as well.

An application normally chooses one provider Store and passes it to mutation workflows while injecting only its Reader capability into consumers. Applications that genuinely use multiple secret providers inject the relevant capabilities into the services that own them. V1 does not add a universal multiplexer before a concrete cross-provider use case requires one.

Examples of trusted bindings:

| Application name | Provider binding |
| --- | --- |
| `DATABASE_PASSWORD` | Canonical AWS secret ARN and text/binary encoding |
| `PAYMENTS_SIGNING_KEY` | Google secret name below a configured global or regional parent |
| `MAIL_API_TOKEN` | Azure secret name in the client's configured vault |
| `OAUTH_GITHUB_CLIENT_SECRET` | Dedicated Vault KV v2 path |
| `TLS_PRIVATE_KEY` | Relative file below one configured root |

Provider SDK clients are injected into driver construction so tests can control responses and applications can use standard workload identity configuration. Static cloud credentials and secret payloads do not belong in source configuration.

The code that constructs an SDK client or file root retains ownership. The capability interfaces have no Close method, and a driver never closes an injected dependency. Application lifecycle code closes owned dependencies after operations stop only when they expose a close operation. The Google client and os.Root are closable; the selected AWS, Azure, and Vault client APIs are not.

### AWS Secrets Manager

The module path is `github.com/goforj/secrets/driver/awssecrets`. Its local SDK seams and constructors are:

```go
type Client interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	PutSecretValue(context.Context, *secretsmanager.PutSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
	DescribeSecret(context.Context, *secretsmanager.DescribeSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DescribeSecretOutput, error)
	DeleteSecret(context.Context, *secretsmanager.DeleteSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error)
}

type Encoding uint8

const (
	Text Encoding = iota + 1
	Binary
)

type Binding struct {
	ARN      string
	Encoding Encoding
}

func New(client Client, bindings map[string]Binding) (*Store, error)
```

New requires a canonical full Secrets Manager ARN for every binding and rejects duplicate ARNs. Requiring the provider's immutable resource identifier prevents a name and ARN from aliasing one mutable resource. Cross-account operation still requires suitable resource policy. Get sends only SecretId, leaving VersionId and VersionStage unset so AWS selects AWSCURRENT.

Encoding is trusted construction policy, not a Get or Set option. A Text binding requires exactly SecretString and returns its exact UTF-8 bytes. A Binary binding requires exactly SecretBinary and converts its bytes directly to a string. A response using the other representation, neither field, both fields, a nil response, or an empty payload is ErrUnavailable because AWS documents a payload length of 1 to 65,536 bytes.

Set accepts 1 to 65,536 bytes. Text additionally requires valid UTF-8 and writes SecretString; Binary writes the exact bytes as SecretBinary. It calls PutSecretValue once with SecretId, the configured representation, and one cryptographically random UUID as ClientRequestToken. The token is generated once per public Set call and remains stable across any internal AWS SDK attempts, preventing duplicate versions from one call. Failure to generate it returns ErrUnavailable before provider I/O. ResourceNotFoundException remains ErrNotFound; Set never calls CreateSecret. Existing tags, description, KMS key, and rotation configuration remain unchanged.

Before PutSecretValue, Set calls DescribeSecret with the same ARN. ResourceNotFoundException or a nonnil DeletedDate returns ErrNotFound without mutation. Other preflight errors use the definite read/preflight classifications below. If the resource is active, Set proceeds to PutSecretValue.

If PutSecretValue returns InvalidRequestException, Set calls DescribeSecret once more. A VersionIdsToStages entry for the generated ClientRequestToken proves that the attempted value version committed and takes precedence over every other returned field. Any response without that token, including a new DeletedDate, and any DescribeSecret error leaves the outcome ErrIndeterminate because post-Put state does not prove non-commit. Provider-native rotation may remove staging labels or cause older versions to be omitted, so absence of the token is never treated as proof. The preflight gives an already scheduled secret the root contract's ErrNotFound classification without relying on AWS error message text.

Delete first calls DescribeSecret. ResourceNotFoundException or a nonnil DeletedDate succeeds without another call. An active secret is passed to DeleteSecret with neither ForceDeleteWithoutRecovery nor RecoveryWindowInDays, retaining AWS's standard 30-day recovery window. If a concurrent deleter causes InvalidRequestException, the driver describes once more and succeeds only when DeletedDate is now nonnil. This bounded sequence makes Delete idempotent without treating unrelated invalid states as success. A scheduled secret is unavailable to Get, and Set does not silently restore it. Store construction documentation therefore lists DescribeSecret and DeleteSecret alongside the read and write IAM permissions.

For GetSecretValue, structured AWS API error codes have this mapping:

| Classification | AWS error codes |
| --- | --- |
| ErrNotFound | ResourceNotFoundException, InvalidRequestException |
| ErrPermission | AccessDeniedException, IncompleteSignature, InvalidClientTokenId, NotAuthorized, OptInRequired, UnrecognizedClientException |
| ErrUnavailable | DecryptionFailure, EncryptionFailure, InternalFailure, InternalServiceError, InvalidAction, InvalidNextTokenException, InvalidParameterException, LimitExceededException, MalformedPolicyDocumentException, PreconditionNotMetException, PublicPolicyException, RequestExpired, ResourceExistsException, ServiceUnavailable, ThrottlingException, ValidationError, ValidationException, and every unrecognized code on Get |

The driver extracts only structured Smithy error codes. The table applies to GetSecretValue: its InvalidRequestException is ErrNotFound for an unavailable current value, including during this driver's recovery window. DescribeSecret preflight errors use the same classifications except that InvalidRequestException is ErrUnavailable, since that operation does not establish current-value readability. A successful DescribeSecret preflight with DeletedDate follows the operation-specific Set or Delete rule above.

After mutation dispatch, Set handles InvalidRequestException through the reconciliation above. ResourceNotFoundException during the Set preflight or Put remains ErrNotFound because PutSecretValue cannot create the absent resource; on Delete it is idempotent success. Every other error returned after PutSecretValue or DeleteSecret may have dispatched is ErrIndeterminate unless the driver's documented DescribeSecret reconciliation proves success. The Get table therefore does not imply a definite mutation classification. The driver does not inspect message text and does not return the AWS error in its unwrap graph.

Contract tests cover every listed Get code, InvalidRequestException separately for GetSecretValue, DescribeSecret preflight, PutSecretValue reconciliation, and DeleteSecret reconciliation, mutation errors ending in otherwise definite status codes, both encodings, representation mismatches, scheduled-deletion Set, token evidence paired with DeletedDate, token absence after rotation, and mutation commit-then-error cases.

### Google Cloud Secret Manager

The module path is `github.com/goforj/secrets/driver/gcpsecrets`. Its local SDK seam and constructor are:

```go
type Client interface {
	AccessSecretVersion(context.Context, *secretmanagerpb.AccessSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
	GetSecret(context.Context, *secretmanagerpb.GetSecretRequest, ...gax.CallOption) (*secretmanagerpb.Secret, error)
	AddSecretVersion(context.Context, *secretmanagerpb.AddSecretVersionRequest, ...gax.CallOption) (*secretmanagerpb.SecretVersion, error)
	DeleteSecret(context.Context, *secretmanagerpb.DeleteSecretRequest, ...gax.CallOption) error
}

const MaxValueBytes = 65_536

func New(client Client, parent string, bindings map[string]string) (*Store, error)
```

Parent must be exactly `projects/{project}` for a global secret or `projects/{project}/locations/{location}` for a regional secret, with nonempty project and location segments. Each locator contains 1 to 255 ASCII letters, digits, hyphens, or underscores. The driver addresses `{parent}/secrets/{id}` and reads `{parent}/secrets/{id}/versions/latest`. The caller must construct the client with the regional endpoint when using a regional parent.

Bindings are one-to-one within the parent. New rejects two application names mapped to the same secret ID.

Get requires a nonnil response, nonnil payload, nonnil DataCrc32C, no more than MaxValueBytes of data, and a matching CRC32C Castagnoli checksum before returning payload bytes as a string. Missing or mismatched integrity data, oversized data, and malformed responses are ErrUnavailable. NotFound and FailedPrecondition for an unavailable latest version are ErrNotFound. Unauthenticated and PermissionDenied are ErrPermission. ResourceExhausted, Aborted, Internal, Unavailable, provider deadlines, transport failures, and all unclassified statuses are ErrUnavailable for Get.

Set accepts at most 65,536 bytes and calls AddSecretVersion once with the exact bytes and their CRC32C. NotFound remains ErrNotFound because AddSecretVersion cannot create an absent secret; Set never calls CreateSecret. Existing labels, annotations, replication, expiry, and rotation configuration remain unchanged. Every other error returned after AddSecretVersion may have dispatched is ErrIndeterminate, including a final permission, throttle, conflict, deadline, internal, unavailable, transport, or cancellation error after hidden client retries.

Delete first calls GetSecret on the secret resource and requires a nonempty ETag. NotFound succeeds without another call; a malformed success response is ErrUnavailable. It then calls DeleteSecret with the exact name and ETag. NotFound succeeds because it proves the desired state under exclusive lifecycle ownership. FailedPrecondition is a definite ErrUnavailable conflict because a prior successful attempt with that ETag would leave the resource absent rather than changed. Every other post-dispatch error is ErrIndeterminate. The ETag prevents deletion when the resource changes between the preflight and mutation, but the exclusive lifecycle ownership rule is still required across separate public calls. Google deletion removes the secret and all versions irreversibly; the library does not add a separate destroy-version operation.

### Azure Key Vault Secrets

The module path is `github.com/goforj/secrets/driver/azuresecrets`. Its local SDK seam and constructor are:

```go
type Client interface {
	GetSecret(context.Context, string, string, *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
	SetSecret(context.Context, string, azsecrets.SetSecretParameters, *azsecrets.SetSecretOptions) (azsecrets.SetSecretResponse, error)
	DeleteSecret(context.Context, string, *azsecrets.DeleteSecretOptions) (azsecrets.DeleteSecretResponse, error)
}

const MaxValueBytes = 25_000

type Binding struct {
	Name        string
	ContentType *string
	Enabled     *bool
	NotBefore   *time.Time
	Expires     *time.Time
	Tags        map[string]*string
}

func New(client Client, bindings map[string]Binding) (*Store, error)
```

The injected Azure client fixes one vault URL. Each Name contains 1 to 127 ASCII letters, digits, or hyphens. Azure identifiers are case-insensitive, so New lowercases names for duplicate detection and rejects any aliases. It validates and deep-copies all optional metadata. Get passes the original Name and an empty version string to request the latest version with nil options. Azure values are text-only. A nil Value or a value longer than MaxValueBytes is ErrUnavailable; a returned empty string is valid.

Set requires valid UTF-8 containing at most 25,000 bytes. It first calls GetSecret for the latest version only to establish that the resource exists. HTTP 404 remains ErrNotFound. It then calls SetSecret with the replacement Value and the trusted ContentType, Tags, Enabled, NotBefore, and Expires binding fields. The binding is authoritative for every version, so concurrent metadata changes outside this Store may be overwritten rather than silently merged from a stale read.

Azure exposes no conditional SetSecret operation. Its Store contract therefore requires the binding's exclusive lifecycle ownership for the complete GetSecret and SetSecret sequence. If an external actor violates that precondition by deleting and purging the resource between those calls, Azure can recreate it. Deployments that cannot enforce the precondition use the Azure driver only through Reader. Adversarial tests freeze both the normal existing-resource sequence and this documented provider limitation.

Delete calls DeleteSecret with nil options, initiating the vault's configured soft-delete and retention behavior for the secret and all versions. DeleteSecret HTTP 404 and a structured HTTP 409 response whose nested Azure code is exactly ObjectIsBeingDeleted both succeed idempotently under exclusive lifecycle ownership. The latter proves that an earlier request already initiated the desired deletion; other 409 responses do not.

Azure exposes the ordinary top-level code as Conflict in azcore.ResponseError.ErrorCode and places ObjectIsBeingDeleted at `error.innererror.code`. For a 409 only, the driver uses errors.As to obtain *azcore.ResponseError, requires a nonnil RawResponse, reads the SDK-cached body through azcore/runtime.Payload, rejects a body larger than 64 KiB, and unmarshals only this exact JSON envelope:

```go
var body struct {
	Error struct {
		InnerError struct {
			Code string `json:"code"`
		} `json:"innererror"`
	} `json:"error"`
}
```

Only an exact, case-sensitive Code match is idempotent success. A missing response, payload read failure, malformed JSON, alternate nesting, top-level-only code, oversized body, or any other nested code remains ErrIndeterminate. The parsed bytes and SDK error are discarded immediately and never logged, returned, or retained.

A successful response means Azure accepted deletion, not that propagation has completed, so an immediate Get may briefly return the prior version. Get and the read-only Set preflight map HTTP 404 to ErrNotFound, HTTP 401 and 403 to ErrPermission, and HTTP 409 and 429 to ErrUnavailable. Once SetSecret may have dispatched, every error, including HTTP 404, is ErrIndeterminate. Once DeleteSecret may have dispatched, every error except the two idempotent states above is ErrIndeterminate. This includes a final permission, unrelated conflict, throttle, timeout, 5xx, transport, or cancellation error after hidden client retries. The driver never calls GetDeletedSecret, RecoverDeletedSecret, or PurgeDeletedSecret. Contract tests use the real nested response shape and cover every malformed variant; live tests cover an immediate repeat while deletion is still in progress.

### HashiCorp Vault KV v2

The module path is `github.com/goforj/secrets/driver/vaultsecrets`. Each logical secret owns one complete KV v2 path whose data document contains only the fixed `value` field:

```go
type Client interface {
	Get(context.Context, string) (*api.KVSecret, error)
	GetMetadata(context.Context, string) (*api.KVMetadata, error)
	Put(context.Context, string, map[string]interface{}, ...api.KVOption) (*api.KVSecret, error)
	DeleteMetadata(context.Context, string) error
}

const MaxValueBytes = 1 << 20

func New(client Client, bindings map[string]string) (*Store, error)
```

Each path contains between 1 and 1,024 bytes. It must be relative, must not begin or end with a slash, and must not contain an empty, dot, or dot-dot segment. New rejects duplicate paths. The caller fixes the mount by supplying the corresponding KVv2 client. Provisioning for a mutation-capable path sets the custom metadata entry `goforj-secret-format=scalar-v1`. This fixed marker is provider metadata, not an application name or request-time option. It declares that the entire path is owned by this scalar Store and remains available when every data version is deleted or destroyed.

Get requests the latest version. An error matching api.ErrSecretNotFound through errors.Is, a structured HTTP 404, a nil secret, or nil Data from a soft-deleted or destroyed latest version is ErrNotFound. The standard KVv2 SDK converts ordinary missing-resource reads into wrapped api.ErrSecretNotFound errors without retaining an HTTP response error, so status-code matching alone is insufficient. A valid owned document contains exactly one `value` entry whose Go type is string and whose length does not exceed MaxValueBytes, including an empty string. Missing value, extra fields, null, numbers, booleans, arrays, objects, other types, and oversized strings are ErrUnavailable rather than being converted. This validation prevents Store from silently taking ownership of a shared or differently shaped path. Get never falls back to an older version.

Set requires valid UTF-8 containing at most MaxValueBytes. It first performs the same Get validation, requires nonnil VersionMetadata with a positive current Version, and calls GetMetadata to require the exact ownership marker. It then calls Put with exactly `map[string]interface{}{"value": value}` and WithCheckAndSet(currentVersion). A missing, deleted, destroyed, unmarked, shared, or differently shaped path is not overwritten. CAS prevents a concurrent writer from being silently overwritten. A final CAS mismatch is conservatively ErrIndeterminate because an injected client may have committed the first attempt and retried it with the now-stale version.

Delete first calls GetMetadata. An error matching api.ErrSecretNotFound through errors.Is or a structured HTTP 404 succeeds without another call. A nil metadata response paired with nil error, or a missing or different ownership marker, is ErrUnavailable and DeleteMetadata is not called. Once the marker proves path ownership, Delete calls Get. An active latest version must also pass the owned-document validation; nil Data from a deleted or destroyed latest version is accepted because the durable metadata marker remains authoritative. An api.ErrSecretNotFound match or structured HTTP 404 from this subsequent Get also permits DeleteMetadata to proceed: it proves no readable data, not that the already observed owned metadata has been removed. Permission, transport, and malformed-response failures still stop before deletion. DeleteMetadata then removes every version and all metadata. The marker check catches static misbinding but is not a concurrency boundary; exclusive lifecycle ownership prevents another writer from changing the path before DeleteMetadata. DeleteMetadata 404 succeeds idempotently. This deletion is irreversible in Vault. Tests cover active and all-deleted owned paths, a soft-deleted unmarked shared path, a wrong marker, and a concurrent mutation attempt.

For public Get and the read-only Set preflight, an api.ErrSecretNotFound match or structured HTTP 404 is ErrNotFound; HTTP 401 and 403 are ErrPermission; throttling, transport, malformed metadata, and 5xx failures are ErrUnavailable. Delete applies the read-absence rules above. A structured DeleteMetadata HTTP 404 is successful idempotence under exclusive lifecycle ownership. Every other error returned after Put or DeleteMetadata may have dispatched is ErrIndeterminate, including api.ErrSecretNotFound and a final permission, throttle, CAS, timeout, 5xx, transport, or cancellation error after hidden client retries. In particular, KVv2.Put can wrap api.ErrSecretNotFound when a write response is missing; that sentinel does not prove non-commit. Drivers never retain or return the SDK error or its locator-bearing message.

Contract tests include wrapped api.ErrSecretNotFound without api.ResponseError from Get and GetMetadata, idempotent Delete of an absent binding resource, absent data with owned metadata that still requires DeleteMetadata, and the same sentinel after Put or DeleteMetadata that must remain ErrIndeterminate. An HTTP fixture exercises the actual SDK's 404 translation rather than only interface fakes. Live tests begin with a soft-deleted owned latest version and verify that Delete removes its metadata and all versions; a separate unmarked shared-document fixture verifies refusal without destructive I/O.

Size-limit tests distinguish a value over MaxValueBytes, which returns ErrInvalid without any client call, from a value within that cap rejected by a lower server limit after Put may have dispatched, which matches ErrIndeterminate and not ErrUnavailable. An oversized Get response remains ErrUnavailable. The post-Put case also covers a client that commits before returning a size rejection from a hidden retry; neither case retains the SDK error or matches its message text.

### Mounted Files

The module path is `github.com/goforj/secrets/driver/filesecrets`. Its constructor borrows an already opened root:

```go
const MaxFileBytes = 1 << 20

func New(root *os.Root, bindings map[string]string) (*Reader, error)
```

The file driver requires the os.Root confinement fix in GO-2026-4970. Its supported toolchains are Go 1.25.12 or later patches in the 1.25 branch, Go 1.26.5 or later patches in the 1.26 branch, and stable Go 1.27 or later releases. Go 1.26.0 through 1.26.4 are explicitly unsupported even though they satisfy a go.mod minimum of 1.25.12; prerelease toolchains are outside this support policy.

The root remains caller-owned. Locators contain at most 4,096 bytes and must be nonempty relative slash-separated paths without NUL, backslash, empty, dot, or dot-dot components and without a trailing slash. Contained symlinks remain allowed for Kubernetes projected-volume layouts; os.Root rejects escapes. Bind mounts created by a privileged actor inside the root are outside the threat model.

For each read on Unix, a platform helper calls root.OpenFile once with read-only and nonblocking flags. It then stats that same descriptor, rejects anything that is not a regular file, reads through a MaxFileBytes plus one limit, and closes the descriptor. Nonblocking open prevents an unconnected FIFO from hanging before the type check. The Windows helper performs the same one-descriptor sequence with its ordinary read-only flag because Windows filesystem opens do not have Unix FIFO semantics. The helper never checks one path and opens another.

Directories, FIFOs, sockets, devices, symlink loops, files that grow beyond the limit, and every oversize result are ErrUnavailable. Missing files are ErrNotFound and filesystem permission failures are ErrPermission. It does not trim bytes. An unconnected FIFO test has a strict timeout and must return ErrUnavailable without a writer.

A Unix confinement regression uses the valid locator `entry`, with an in-root symlink `entry -> escape/` and a second in-root symlink `escape` pointing to an outside directory. A slash introduced by symlink resolution bypasses locator-only validation on affected toolchains. The fixture requires the same root.OpenFile call used by the driver to reject the escape without returning a descriptor, then verifies that the Reader returns an error and no value. Checking only the Reader's final error would miss a vulnerable open that succeeds and is later rejected by the regular-file check. CI runs this regression on Go 1.25.12, Go 1.26.5, and the current supported stable release on Linux and macOS, alongside the contained-symlink success cases.

V1 supports Linux, macOS, and Windows. New operating systems require the same confinement suite before support is enabled; New returns ErrInvalid on an unsupported target. This deliberately excludes js, where os.Root documents incomplete escape protection, and platforms whose rename semantics have not been validated. Linux integration coverage includes Docker and Kubernetes layouts.

## Provider Value Capabilities

| Driver | Capability | Selected value | Representation and Set limit | Delete behavior |
| --- | --- | --- | --- | --- |
| AWS | Store for canonical ARNs | AWSCURRENT | Configured text or binary; 1 to 65,536-byte Get and Set range | Secret scheduled for standard recovery window |
| Google | Store | `latest` alias | Arbitrary bytes; 65,536-byte Get and Set cap | Secret and all versions deleted |
| Azure | Store | Latest version | Valid UTF-8; 25,000-byte Get and Set cap | Vault-configured soft delete |
| Vault KV v2 | Store | Latest version only | Valid UTF-8 in fixed `value` field; 1 MiB Get and Set library cap, subject to lower server limits | All versions and metadata irreversibly deleted after ownership proof |
| Mounted file | Reader | Contents at read time | Arbitrary bytes; 1 MiB Get cap | Not supported by interface |

The cross-provider fidelity guarantee is capability-aware: every successful driver operation preserves exactly the values accepted by its documented driver contract. The library does not claim that every provider or driver can accept every value shape or size. The Vault cap is a library-side resource bound, not a promise that every Vault storage backend accepts a request of that size. A Set value above the library cap is ErrInvalid before provider I/O; a lower server limit reported after Put may have dispatched is ErrIndeterminate under the same mutation rule as every other post-Put error. An oversized read response is ErrUnavailable. Callers that require portable writes validate against the intersection of their selected deployment drivers: nonempty valid UTF-8 of at most 25,000 bytes for the initial managed set.

Vault authentication remains externally managed in v1. The driver operates on owned KV v2 paths below the configured client mount but does not own login, token renewal, or reauthentication.

## Security Boundary

First-party drivers never log and never return raw provider errors. Their public errors contain only a stable classification and, optionally, an application name that already matched the trusted bindings. Secret payloads, provider locators, request objects, tenant data, and credentials are never included.

That guarantee does not cover arbitrary Reader or Func implementations, logging configured on an injected SDK client, provider-side audit logs, process memory after Get returns, or application handling of the returned string. Construction documentation must warn that provider request metadata can contain configured locators and that SDK logging must be reviewed before enabling it. Tests install recording log sinks where SDKs permit them and assert that first-party driver code emits nothing.

The mounted-file boundary protects against path traversal and symlink escape by an unprivileged writer beneath the supplied root. It does not protect against a privileged actor changing mounts within that root, reading application memory, or replacing the root object supplied during trusted construction.

## Retries, Caching, And Rotation

V1 should begin without a public retry, cache, invalidation, refresh, revision, or metadata API.

Each network driver invokes its injected client method only in the sequence documented for that operation and adds no general retry loop. AWS Set makes one DescribeSecret preflight, one PutSecretValue call, and, only after InvalidRequestException, one DescribeSecret reconciliation call. Google Set makes one AddSecretVersion call. Azure Set makes one existence GetSecret call followed by one SetSecret call. Vault Set makes one owned-document Get call, one ownership GetMetadata call, and one CAS Put call. Google Delete makes one GetSecret call followed by one DeleteSecret call; Azure Delete makes one DeleteSecret call; AWS uses its bounded describe-delete verification sequence; and Vault Delete makes one ownership GetMetadata call, one Get call, and at most one DeleteMetadata call. Tests assert these exact client-method sequences and context propagation.

An SDK may perform multiple network attempts inside one client-method call. Retries must be bounded by the caller context. Mutation safety is provider-specific:

- AWS Set may use SDK retries because one Set call carries a stable ClientRequestToken. AWS Delete may use them because the binding is an immutable full ARN and repeated requests retain the same deletion policy.
- Google Set disables automatic retries with a driver-supplied per-call option because AddSecretVersion has no request token. Google Delete may use SDK retries because every attempt carries the same ETag.
- Azure SDK retries can publish the same value as more than one version. This is permitted version churn; a final ambiguous error remains ErrIndeterminate. Delete retries are safe under the exclusive lifecycle ownership rule because 404 or the exact ObjectIsBeingDeleted state after an earlier accepted attempt is success.
- Vault SDK retries can turn a committed CAS Put into a final CAS failure. The driver therefore classifies a final CAS failure as ErrIndeterminate. DeleteMetadata retries are safe under the exclusive lifecycle ownership rule because 404 after an earlier accepted attempt is success.

The driver does not claim to inspect or control retry policy hidden behind an injected interface. Contract tests include clients that simulate commit-then-retry outcomes, and integration tests cover the pinned standard SDK defaults. A later reusable retry or cache implementation may wrap a Reader without changing its interface. It requires its own evidence, limits, cancellation rules, stale-value policy, and concurrency tests before adoption.

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
- secretstest fixture validation, copying, Get, Set, idempotent Delete, empty values, and concurrent operations; and
- returned values absent from library-generated errors and failure messages.

Each provider driver owns a capability contract suite derived from the normative matrix in this design; there is no cross-module test-harness API. Reader coverage includes complete binding validation, exact dispatch, current-value selection, the complete error-mapping table, arbitrary byte strings where supported, malformed read responses, safe error normalization, context propagation, cancellation, and concurrency. Store coverage additionally freezes existing-resource Set behavior, exact accepted-value preservation, ignored mutation response bodies after nil errors, version-metadata behavior, size and encoding rejection before I/O, one-to-one resource bindings, same-generation idempotent whole-resource Delete, provider retention behavior, mutation call sequences, SDK retry requirements, indeterminate commits, CAS conflicts where applicable, lifecycle ownership, same-binding Set/Delete serialization, and cross-binding concurrency. A lock test blocks one mutation, cancels a same-binding waiter promptly with no client call or ErrIndeterminate match, and proves another binding still proceeds. Tests include every constructor rejection branch and verify that a failed constructor retains no partially valid configuration.

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

An untagged `github.com/goforj/secrets/integration` module orchestrates repository-wide tests but is not an application dependency. During repository development, sibling modules retain relative replace directives so unpublished changes are tested together. `GOWORK=off` disables workspace mode but does not disable those go.mod replacements; running tests inside a driver directory is therefore not proof that its published dependencies work.

Before publishing each module tag, release automation packages the exact candidate commit using Go module archive rules and tests it from a separate temporary consumer with no replace directives and GOWORK disabled. The consumer downloads the candidate at its intended version from a private preview proxy; a separate preview cache prevents that unpublished artifact from satisfying later published-module checks. The root is validated and published first. Secretstest and driver candidates must then resolve their required root version from the published release, not the repository checkout. Archive consumers compile the exported API and run relevant consumer contract tests on the module's minimum Go version and the current supported Go release.

For every preview and published consumer, automation inspects `go list -m -json all` and rejects any nonnil Replace field or module Dir resolved into the repository checkout. It asserts the expected candidate or released module version and, for secretstest or a driver, the selected published root version. After publishing each tag, a new consumer and fresh module cache download that exact version through the normal module proxy with checksum verification enabled, repeat the consumer tests and `go mod verify`, and compare the downloaded module checksums with the candidate archive. Each tag must point to the tested commit, and each nested module must be verified independently. Repository-local replacements remain available for development; release evidence comes from consumers that do not use them.

The root, secretstest, and network-driver modules initially use Go 1.24.4, matching the established GoForj driver baseline, unless an SDK's minimum version is higher when implementation begins. The file driver's go.mod minimum remains Go 1.25.12, with the per-branch patched toolchain requirements specified under Mounted Files. A go directive cannot exclude affected patches of a higher Go release, so package documentation and release instructions must state that support policy explicitly and file-driver CI must exercise its confinement regression at each specified patched branch minimum. A driver dependency cannot raise the root module's Go version. Any later minimum-version increase is scoped and documented per module.

## Compatibility

Before v1, freeze only:

- the Reader, Writer, Deleter, and Store method sets;
- name grammar and maximum length;
- exact value preservation;
- stable error classifications; and
- current-value semantics documented by each driver;
- Set creation, value-limit, and concurrency semantics; and
- idempotent Delete and provider retention semantics.

Adding a driver does not change the root application API. Changing name grammar, empty-value handling, byte preservation, error classification, current-value behavior, existing-resource Set behavior, version-metadata behavior, or whole-resource Delete postconditions is a runtime compatibility change.

The root module does not depend on GoForj or provider SDKs. Secretstest and each driver depend on a released root version. Each network driver pins only its own SDK and is released independently.

## Implementation Plan

### Phase 1: Minimal root

Implement Reader, Writer, Deleter, Store, Func, name validation, five safe error classifications, the concurrency-safe map-backed test Store, and root tests. Each independently versioned module owns its private validation and error helpers.

### Phase 2: Local driver

Implement the mounted-file driver and its confinement, exact-byte, Docker, and Kubernetes volume tests.

### Phase 3: Managed providers

Implement AWS, Google Cloud, Azure, and Vault driver modules. Require each driver's capability contract tests and its own real-service integration coverage for that driver release.

### Phase 4: Evidence-driven additions

Add caching, retries, richer errors, metadata, dynamic version selection, purge, recovery, or rotation coordination only after concrete applications demonstrate the need and the behavior can remain behind the minimal capability interfaces where possible.

## Acceptance Criteria

1. Normal application use is one direct Get, Set, or Delete call.
2. The capability interfaces have no key wrapper, value wrapper, disclosure accessor, options, metadata, or resource lifecycle methods.
3. Names use uppercase configuration-style vocabulary and never imply paths or hierarchy.
4. Callers cannot choose a provider or locator at operation time.
5. Values round-trip exactly through Get and Set within each provider's documented capabilities, including arbitrary bytes and empty values where supported.
6. First-party library errors and diagnostics never disclose returned values, locators, credentials, or provider SDK errors.
7. Every production driver passes its capability contract tests and real-service integration coverage for every capability it exposes.
8. Environment variables continue to use env.MustGet and require no new package or API.
9. GoForj requires no component, render configuration, generated accessor, template, or special integration.
10. The design makes no zeroization, purge, recovery, automatic rotation, atomic mutation, or exactly-once claim.
11. Each constructor, provider request, success shape, current-value rule, Set sequence, Delete behavior, error mapping, retry owner, lifecycle owner, and module release path is specified without requiring implementation-time API invention.
12. Provider tests distinguish read/preflight error classifications from post-dispatch mutation uncertainty, including AWS InvalidRequestException, Vault api.ErrSecretNotFound, and local, read-response, and post-Put size-limit failures.
13. Every module passes candidate-archive consumer checks before tagging and published-module consumer checks afterward, with the expected versions, published checksum verification, no replacement modules, and no repository checkout resolution.
14. Mounted-file support documents patched toolchains per Go release branch, and its confinement regression verifies rejection during open even when a valid locator resolves through a symlink target ending in a slash.

## References

- [AWS Secrets Manager GetSecretValue](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html)
- [Google Cloud Secret Manager access version](https://cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access)
- [Azure Key Vault Get Secret](https://learn.microsoft.com/en-us/rest/api/keyvault/secrets/get-secret)
- [HashiCorp Vault KV v2](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2)
- [Vault API v1.23.0 KVv2 implementation](https://github.com/hashicorp/vault/blob/api/v1.23.0/api/kv_v2.go)
- [Go workspace mode](https://go.dev/ref/mod#workspaces)
- [Go module replacement directives](https://go.dev/ref/mod#go-mod-file-replace)
- [Kubernetes mounted Secret updates](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Go string specification](https://go.dev/ref/spec#String_types)
- [Go os.Root](https://pkg.go.dev/os#Root)
- [GO-2026-4970 os.Root path escape](https://pkg.go.dev/vuln/GO-2026-4970)
