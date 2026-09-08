# Logging Sink Routing and Redaction Design

## Status

- Design status: implemented
- Planning date: 2026-07-31
- Implemented: 2026-08-01
- Scope: Framework-managed `internal/logger`, console/JSON output, additional sinks, and Lighthouse log transport

## Summary

The App logger should create one canonical structured entry, redact it once, apply filtering and dedupe, then route the safe entry to normal output and registered sinks. Lighthouse is a sink in this pipeline and must never receive an unredacted copy.

Synchronous delivery remains the compatibility default. Explicit asynchronous sinks use bounded queues and an explicit backpressure policy; no logging mode may create an unbounded goroutine or buffer per entry.

## Current Behavior

`AppLogger` writes console or JSON output and supports `AddSink(LogSink)`. Sinks are copied under a lock and invoked synchronously in registration order. The fluent typed path shares an output dedupe decision with sinks, while legacy `GetWriter` traffic is reparsed and has a separate structured sink path. Lighthouse registers `NewLogSink` at process startup and performs another wire-level dedupe before websocket emission.

`LogSink` is currently a function with no error result, so the logger cannot distinguish delivery success from failure. There is also no central redaction boundary.

## Canonical Pipeline

Every structured entry, including entries recovered from legacy zerolog writer access, follows this order:

```text
build or parse entry
  -> normalize reserved fields
  -> redact message and fields
  -> dedupe/filter
  -> write primary console or JSON output
  -> deliver sinks in registration order
```

Redaction happens before signatures, dedupe state, formatting, callbacks, queues, Lighthouse conversion, or diagnostic capture. Sinks receive independent snapshots so one sink cannot mutate data observed by another.

The safe path remains copy-on-write. Entries that contain no sensitive values retain their existing field storage and dedupe signature, while framework-owned typed paths redact the small set of values that can carry user input before marking the entry safe. Sink field maps are materialized only when a sink is registered, and multiple sinks receive independent copies.

Primary output is attempted before optional sinks. A slow or failed remote sink therefore cannot erase the local record. Sink registration order defines delivery order for synchronous sinks; ordering is per logger emission, not a global order across concurrently logging goroutines.

## Redaction Policy

The framework owns a conservative default key policy for case-insensitive names such as passwords, secrets, tokens, authorization/cookie headers, private keys, and common credential fields. Applications may add keys and value matchers, but may not disable framework-required redaction for Lighthouse or other framework sinks.

Redaction replaces values with a stable marker and preserves field names and value types only where doing so cannot reveal content. Nested maps and slices are traversed with depth, collection-size, and string-length limits. Message redaction covers registered high-confidence patterns; callers should still put sensitive values in structured fields rather than interpolate them into messages.

The same policy applies to console, JSON, legacy writer entries that parse successfully, additional sinks, dedupe summaries, and Lighthouse. Unparseable raw legacy writes cannot be safely field-redacted; they retain existing raw-output compatibility but are not forwarded to structured sinks. Documentation should mark raw writer access as unsuitable for secrets.

## Sink Modes and Backpressure

The default sink mode is synchronous because existing `AddSink` callbacks run inline and callers may rely on immediate observation in tests or shutdown paths.

An opt-in async sink has:

- one bounded queue;
- one long-lived worker, not one goroutine per log entry;
- FIFO delivery for entries accepted by that sink;
- a configured queue capacity;
- an explicit `block`, `drop-newest`, or `drop-oldest` overflow policy; and
- counters or rate-limited fallback diagnostics for dropped and failed deliveries.

Remote and development UI transports should normally use bounded async delivery with `drop-oldest`, keeping recent operational context. Security/audit destinations that require durable acceptance should use synchronous delivery or a purpose-built durable writer rather than pretending a lossy log queue is an audit log.

Async sinks flush within a caller-provided shutdown context. They stop accepting entries when closing and report the number not delivered when the budget expires. Flush time is subordinate to the App shutdown budget.

## Sink Failure

A sink failure never stops delivery to later sinks and never recursively logs through the same pipeline. Failures go to a minimal rate-limited fallback diagnostic on `stderr` and to framework metrics when metrics are enabled. Panics from application sinks are recovered at the routing boundary and treated as failures.

New sink implementations should support delivery and close/flush errors. The existing `LogSink func(LogEntry)` and `AddSink` remain as a compatibility adapter whose callback is treated as synchronous and infallible. A separate options-based registration API can introduce named, error-returning, closable sinks without changing existing call sites.

## Lighthouse

Lighthouse registers through the same router and receives only redacted entries. Its transport dedupe may remain as protection against reconnection or source-specific chatter, but it must operate on redacted data and must not recreate fields from pre-redaction state. Disconnection, queue overflow, and serialization failure are sink failures; they do not delay primary App output.

## Compatibility

- Existing logger methods, console shape, JSON field names, `GetWriter`, `LogSink`, and `AddSink` continue to compile.
- Synchronous callback behavior remains the default.
- Moving optional sink delivery after primary output is an intentional ordering clarification; callback-versus-`stderr` interleaving was not a documented API guarantee.
- Redaction changes visible values only for fields or patterns classified as sensitive.
- Existing dedupe configuration remains supported, with dedupe moved after redaction so secret values never enter dedupe state.

## Validation Plan

Implementation should test redaction parity across console, JSON, legacy parsed entries, all sinks, summaries, and Lighthouse; deterministic synchronous ordering; concurrent isolation; each async overflow policy; panic/error continuation; bounded shutdown flush; and unchanged behavior for existing `AddSink` callers.
