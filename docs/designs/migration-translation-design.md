# Migration Translation Design

## Purpose

GoForj should make multi-dialect database support practical without pretending that databases are identical.

Developers commonly author migrations by hand or by copying SQL from a database browser. The best GoForj workflow should preserve that natural SQL-first behavior while reducing the repetitive work of maintaining SQLite, MySQL, and Postgres variants.

This design proposes a migration translation system:

- developers write SQL in the dialect they know
- GoForj parses supported schema DDL into an internal operation model
- GoForj renders missing dialect variants
- generated SQL is committed, reviewed, and executed as SQL

The migration runner should remain raw SQL-first. Translation is a tooling layer, not a hidden runtime abstraction.

## Goals

- support raw SQL as the primary migration artifact
- reduce the cost of maintaining multiple dialect migration files
- support SQLite, MySQL, and Postgres as first-class dialects
- infer target dialects from the same environment-driven database driver selection used by the App
- allow a developer to author in one dialect and generate the others
- keep generated SQL visible and reviewable
- protect manually edited migration files from accidental overwrite
- emit clear warnings for lossy or unsupported translations
- support strict mode for CI and framework templates
- integrate cleanly with `forj dev`, `forj build`, and migration testing
- avoid a Blueprint-style migration DSL as the primary authoring model
- keep the runtime migration path deterministic and free of implicit translation

## Non-Goals

- do not build a universal SQL transpiler
- do not translate arbitrary queries or DML in v1
- do not hide final SQL behind a permanent DSL
- do not promise that all schema features are portable
- do not silently downgrade database-specific behavior
- do not make the migration runner depend on the translator
- do not require applications to use the translator
- do not translate missing dialects inside a production binary by default
- do not introduce a CLI wizard as the primary migration authoring experience

## Design Principle

GoForj should translate common schema intent, not arbitrary database behavior.

The user-facing golden path should be singular:

```text
SQL files are the migration contract.
```

Translation, validation, embedding, and test orchestration are tooling around that contract. They are not separate migration authoring models.

The operating model:

```text
author native SQL -> parse supported DDL -> normalize operations -> render target SQL -> review and commit SQL -> run SQL
```

Not:

```text
author arbitrary dialect SQL -> magically execute it everywhere
```

This keeps the system aligned with GoForj's broader philosophy:

- explicit artifacts
- inspectable behavior
- strong tooling
- minimal runtime magic
- escape hatches when infrastructure-specific behavior matters

## File Model

Use grouped SQL migrations as the future golden path.

Each migration is one directory. Each dialect/direction is one file.

```text
migrations/
  2026_05_26_120000_create_users/
    mysql.up.sql
    mysql.down.sql
    postgres.up.sql
    postgres.down.sql
    sqlite.up.sql
    sqlite.down.sql
```

Named connections continue to use nested connection directories:

```text
migrations/
  reporting/
    2026_05_26_120000_create_reports/
      postgres.up.sql
      postgres.down.sql
```

This keeps one logical migration unit per directory and avoids the scale problem of hundreds of loose flat files.

Separate `up` and `down` files are intentional. A combined file with section markers is easier to mis-edit and requires an extra section parser.

### Legacy Flat Files

GoForj currently uses a flat-file migration model. Existing projects should remain supported.

Today `make:migration` writes files under `migrations/` and resolves supported database drivers from `DB_SUPPORTED_DRIVERS`, falling back to `DB_DRIVER`, then `sqlite`.

When one database driver is supported, GoForj keeps legacy naming:

```text
migrations/
  2026_05_25_120000_create_users.up.sql
  2026_05_25_120000_create_users.down.sql
```

When multiple database drivers are supported, GoForj writes dialect-qualified flat files:

```text
migrations/
  2026_05_25_120000_create_users.mysql.up.sql
  2026_05_25_120000_create_users.mysql.down.sql
  2026_05_25_120000_create_users.postgres.up.sql
  2026_05_25_120000_create_users.postgres.down.sql
  2026_05_25_120000_create_users.sqlite.up.sql
  2026_05_25_120000_create_users.sqlite.down.sql
```

Named connections continue to use nested connection directories:

```text
migrations/
  reporting/
    2026_05_25_120000_create_reports.mysql.up.sql
    2026_05_25_120000_create_reports.mysql.down.sql
```

Flat files should be treated as a compatibility format once grouped migrations land. New projects and public docs should use grouped migrations.

Resolution rule:

- prefer a dialect-specific file when present
- otherwise use a shared file when present
- otherwise report a missing migration for that dialect

For grouped migrations, dialect-specific files are `mysql.up.sql`, `postgres.up.sql`, and `sqlite.up.sql`.

For flat migrations, dialect-specific files are `<timestamp>_<name>.mysql.up.sql`, `<timestamp>_<name>.postgres.up.sql`, and `<timestamp>_<name>.sqlite.up.sql`.

## Dialect Source

Supported dialects should come from the same environment-driven database configuration used by the App and by `make:migration`, not from `.goforj.yml`.

Source of truth:

```dotenv
DB_DRIVER=mysql
DB_SUPPORTED_DRIVERS=sqlite,mysql,postgres
```

Resolution:

- use `DB_SUPPORTED_DRIVERS` when set
- otherwise use `DB_DRIVER`
- otherwise fall back to `sqlite`

This matters because supported database drivers can change between environments. A developer may run translation with one `.env`, `.env.local`, CI environment, or generated App configuration and get a different target set.

If the project supports `sqlite`, `mysql`, and `postgres`, then:

```bash
forj migrate translate --from mysql
```

means:

```text
read mysql migration files
generate missing or stale sqlite and postgres files
```

The translator should print the resolved source for transparency:

```text
Resolved target dialects from DB_SUPPORTED_DRIVERS=sqlite,mysql,postgres
```

## Command UX

Migration creation should not force users to maintain six files immediately.

In grouped mode, `make:migration` should create one source dialect by default:

```text
migrations/
  2026_05_26_120000_create_users/
    mysql.up.sql
    mysql.down.sql
```

The source dialect should come from `DB_DRIVER`, or from an explicit flag if needed:

```bash
forj make:migration create_users
forj make:migration create_users --from postgres
```

Additional dialect files are created by translation when needed.

Primary command:

```bash
forj migrate translate --from mysql
```

Expected behavior:

- scan `migrations/` and named connection migration directories
- find migrations with MySQL source files
- infer target dialects from `DB_SUPPORTED_DRIVERS` / `DB_DRIVER`
- generate missing target files
- populate untouched scaffold files created by `make:migration`
- skip protected manual files
- report warnings and unsupported operations
- support both grouped migrations and legacy flat files

Example output:

```text
Source dialect: mysql
Target dialects: sqlite, postgres

2026_05_25_120000_create_users
  generated sqlite.up.sql
  generated sqlite.down.sql
  generated postgres.up.sql
  generated postgres.down.sql

2026_05_25_121500_add_user_indexes
  skipped postgres.up.sql: manual file exists
  warning sqlite: partial index expression requires review

Done. Review generated SQL before committing.
```

Useful options:

```bash
forj migrate translate --from mysql
forj migrate translate --from postgres --only 2026_05_25_120000_create_users
forj migrate translate --from sqlite --update
forj migrate translate --from mysql --strict
forj migrate translate --from mysql --force-generated
forj migrate translate --from mysql --dry-run
```

Option semantics:

- `--from` selects the source dialect.
- `--only` limits translation to one migration.
- `--update` regenerates stale generated files, but still protects manual files.
- `--force-generated` overwrites files marked as generated by GoForj.
- `--strict` fails on lossy translation, unsupported operations, or warnings.
- `--dry-run` prints the plan without writing files.

`--to` may exist as an escape hatch, but should not be required in normal use.

```bash
forj migrate translate --from mysql --to sqlite
```

## Runtime Translation Policy

Runtime migration execution should not translate missing dialect files by default.

Reason:

- migrations mutate durable state
- production binaries should execute known embedded SQL
- app owners should review generated SQL before shipping
- runtime failures should come from database execution, not parser/render ambiguity
- production environments may not have writable filesystems

The lifecycle should be:

```text
edit migration -> translate missing dialects -> validate -> test -> build/embed -> runtime executes embedded SQL
```

Runtime behavior:

- if the active `DB_DRIVER` has an embedded migration file, run it
- if no embedded file exists for the active driver, fail with a clear error
- do not silently translate and apply SQL inside the running application

Potential explicit escape hatch for development only:

```bash
./bin/app migrate --allow-runtime-translation
```

This should not be the default and should be clearly documented as unsafe for production.

## Generated File Ownership

Generated files should include a small header.

```sql
-- Code generated by forj migrate translate from mysql; DO NOT EDIT.
-- Source: mysql.up.sql
-- Source checksum: sha256:...
```

Manual files should not include this header.

Default overwrite behavior:

- create missing files
- populate untouched scaffold files created by `make:migration`
- overwrite generated files only when source checksum changed and `--update` or automatic build policy allows it
- never overwrite manual files unless an explicit force flag is provided

This protects users who accept generated SQL initially and later customize one dialect by hand.

An untouched scaffold file is a file that still only contains the initial `make:migration` comment, such as:

```sql
-- Up migration (postgres)
```

Those files are safe to replace because the user has not authored dialect-specific SQL yet.

## Internal Architecture

The translator should have four layers.

```text
parser -> operation model -> renderer -> writer
```

### Parser

Dialect-specific parsers consume supported DDL.

Initial parser scope:

- `CREATE TABLE`
- `DROP TABLE`
- `ALTER TABLE ADD COLUMN`
- `ALTER TABLE DROP COLUMN` where supported
- `CREATE INDEX`
- `CREATE UNIQUE INDEX`
- `DROP INDEX`
- primary keys
- unique constraints
- foreign keys
- nullable columns
- default values
- common scalar types

The parser should produce structured diagnostics with source locations when possible.

### Operation Model

The internal model should describe portable schema operations.

Example concepts:

```text
CreateTable
DropTable
AddColumn
DropColumn
CreateIndex
DropIndex
AddForeignKey
DropForeignKey
```

Column model:

```text
name
type family
length/precision
nullable
default
primary key
unique
references
auto increment / identity
```

The operation model is not a public replacement for SQL in v1. It is an internal representation used by the translator.

### Renderer

Dialect renderers produce SQL for each target.

Renderer responsibilities:

- choose dialect-appropriate types
- render constraints and indexes
- account for transactional DDL capabilities
- emit warnings for semantic differences
- preserve deterministic formatting

### Writer

The writer owns:

- file naming
- generated headers
- checksum metadata
- overwrite protection
- dry-run reporting

## Type Mapping

The first release should only support boring, common type mappings.

Examples:

| Intent | SQLite | MySQL | Postgres |
|---|---|---|---|
| string | `TEXT` | `VARCHAR(n)` | `VARCHAR(n)` |
| text | `TEXT` | `TEXT` | `TEXT` |
| bool | `INTEGER` | `BOOLEAN` | `BOOLEAN` |
| int | `INTEGER` | `INT` | `INTEGER` |
| bigint | `INTEGER` | `BIGINT` | `BIGINT` |
| decimal | `NUMERIC(p,s)` | `DECIMAL(p,s)` | `NUMERIC(p,s)` |
| timestamp | `TEXT` or `DATETIME` | `DATETIME` | `TIMESTAMP` |
| uuid | `TEXT` | `CHAR(36)` | `UUID` |
| json | `TEXT` | `JSON` | `JSONB` |

Mappings that are semantically different should produce warnings.

Examples:

- case-insensitive text
- JSON query behavior
- timestamp timezone semantics
- generated columns
- partial indexes
- expression indexes
- check constraints
- enum types

## Unsupported Features

Unsupported or lossy features should never be silently translated.

Examples that should warn or fail:

- Postgres extensions
- Postgres `CITEXT`
- MySQL engine options
- MySQL charset/collation behavior
- SQLite virtual tables
- full-text search
- triggers
- stored procedures
- generated columns
- complex `ALTER TABLE` rewrites
- dialect-specific index expressions

Suggested behavior:

- default mode: generate what is safe, write warnings, mark files for review
- strict mode: fail the command

## Review Markers

Generated files with warnings may include review comments.

```sql
-- FORJ REVIEW: Postgres JSONB was translated to SQLite TEXT.
-- Verify application code does not depend on JSON operators.
```

This keeps warnings close to the SQL artifact that developers review.

## Translation Correctness

Translation correctness has two different meanings:

- syntactic correctness: the generated SQL parses for the target dialect
- behavioral correctness: the generated migration produces the intended schema on a real database

GoForj can provide strong offline checks for syntax and structure, but it cannot fully prove behavioral correctness for MySQL or Postgres without executing against those engines.

The design should be honest about that boundary.

Correctness should be enforced before the binary ships. Runtime execution should verify embedded migration metadata, but it should not be responsible for discovering or translating missing dialects.

### Offline Guarantees

Offline translation should provide deterministic best-effort guarantees without requiring database servers.

The translator can verify:

- source SQL parses into supported operations
- every operation has a target renderer
- generated SQL is parseable by the target dialect parser when a parser is available
- generated identifiers are quoted consistently
- unsupported features produce diagnostics
- lossy mappings produce warnings
- generated files include source checksums
- generated output is stable across repeated runs

This should be the default local experience.

```bash
forj migrate translate --from mysql
```

Default translation should not require Postgres or MySQL to be running.

### Parser Round Trip

Each rendered target file should be parsed back through the target dialect parser when possible.

Pipeline:

```text
source SQL -> source parser -> operation model -> target renderer -> target SQL -> target parser -> operation model
```

The final operation model does not need to be byte-for-byte identical to the source model, but it should be semantically comparable for supported operations.

Examples:

- table names match
- column names match
- nullable flags match
- primary keys match
- indexes match
- foreign keys match where supported
- type families match, even if concrete type names differ

If the rendered SQL cannot be parsed back into supported operations, translation should fail.

### Golden Fixtures

The translator library should maintain a fixture suite for each dialect pair.

Example fixture shape:

```text
testdata/translate/
  create_users/
    mysql.input.sql
    sqlite.golden.sql
    postgres.golden.sql
    warnings.golden
```

Fixtures should cover:

- simple tables
- nullable and non-null columns
- defaults
- primary keys
- unique constraints
- indexes
- foreign keys
- timestamps
- decimal fields
- JSON fields
- unsupported or lossy cases

Golden fixtures catch renderer regressions without requiring live database servers.

### Dialect Syntax Validation

Where practical, use mature SQL parsers or dialect grammars for offline validation.

Possible approaches:

- own a small parser for the supported DDL subset
- reuse existing Go parsers where quality is acceptable
- use target-dialect parsers only for the supported subset
- fail closed when a construct cannot be parsed confidently

The supported subset should be intentionally smaller than each database's full SQL grammar. If a statement cannot be parsed into supported operations, it should remain raw/manual for that dialect.

### Live Validation

Behavioral correctness requires live database engines.

GoForj should provide an explicit command for that:

```bash
forj migrate test --dialects sqlite,postgres,mysql
forj migrate test --all
```

Live validation should:

- create or reset a test database
- apply migrations for each dialect
- inspect the resulting schema where possible
- run rollback checks where supported
- report target-specific errors clearly

This should be recommended for CI when projects claim multi-dialect support.

### No-Server Local Workflow

A developer without Postgres or MySQL running should still be productive.

Expected local flow:

```bash
forj make:migration create_users
forj migrate translate --from mysql
forj migrate validate --offline
```

`validate --offline` should check parsing, generated file metadata, target renderability, and stale outputs. It should not claim that the migration has been proven against a real database.

Suggested output:

```text
Offline validation passed.

Checked:
  source mysql syntax for supported DDL
  generated sqlite syntax for supported DDL
  generated postgres syntax for supported DDL
  source checksums

Not checked:
  live postgres execution
  live mysql execution

Run `forj migrate test --all` for live dialect validation.
```

### CI Recommendation

Projects that advertise multiple database dialects should run live migration tests in CI.

Minimum CI matrix:

```text
sqlite
mysql
postgres
```

If CI does not have database services available, it should at least run:

```bash
forj migrate validate --offline --strict
```

But the docs should be clear that offline validation is not equivalent to live database validation.

## Migration Testing

Translation should pair with dialect testing.

Suggested commands:

```bash
forj migrate test --dialects sqlite,postgres,mysql
forj migrate test --all
```

The test command should:

- create clean databases
- apply all migrations
- roll back where possible
- verify migration table state
- report dialect-specific failures

`forj migrate test --all` should be zero-config by default.

Dialect behavior:

- SQLite uses a temporary SQLite database.
- MySQL uses a disposable MySQL/MariaDB container when Docker/Podman is available.
- Postgres uses a disposable Postgres container when Docker/Podman is available.

For MySQL and Postgres, connection resolution should be:

1. validate that a supported container runtime is available
2. start a disposable container when available
3. otherwise fall back to the live configured connection for that driver when it is safe to do so
4. otherwise fail with a clear message

This preserves the zero-config happy path while still allowing teams without Docker/Podman to validate against live local or CI database services.

The command should not require users to create separate test environment variables.

Container behavior:

- allocate host ports dynamically
- use the same database name from `DB_DATABASE` when it is a simple database name, otherwise use `app`
- use container-local credentials controlled by the test runner
- never use the app's real `DB_HOST`, `DB_PORT`, or credentials for disposable containers
- clean up containers automatically
- support `--keep` for debugging
- support image override flags later if needed

Live connection fallback behavior:

- use the existing driver configuration for the dialect being tested
- never apply migration tests directly to the configured application database by default
- create an isolated temporary database/schema on the live server when the driver supports it
- run migrations only against that isolated database/schema
- drop the temporary database/schema after the test unless `--keep` is passed
- print that a live configured connection is being used
- never silently run destructive setup against a production-looking database

The configured live connection should be used as a control-plane connection only. It gives GoForj enough access to create a temporary validation database, not permission to mutate the configured application database.

Suggested safety checks:

- reject hostnames that look production-specific unless `--allow-live` is passed
- require permission to create and drop a temporary validation database/schema
- generate a database/schema name such as `goforj_migrate_test_<timestamp>_<random>`
- refuse to run if GoForj cannot switch to the generated validation database/schema
- refuse to run if the target database/schema name equals the configured application database/schema
- require `--yes` in non-interactive mode when falling back to live connections

Live fallback should fail closed. If GoForj cannot create an isolated validation target, it should not run migration tests against that live node.

Example live fallback flow:

```text
connect to configured postgres server
create database goforj_migrate_test_20260526_ab12
connect to goforj_migrate_test_20260526_ab12
apply migrations
inspect schema
drop database goforj_migrate_test_20260526_ab12
```

For MySQL, use a temporary database. For Postgres, prefer a temporary database when permissions allow it; a temporary schema may be acceptable only if the migration runner can guarantee search path isolation and cleanup.

Useful options:

```bash
forj migrate test --all
forj migrate test --all --containers
forj migrate test --all --use-live
forj migrate test --all --allow-live --yes
```

Option semantics:

- `--containers` requires Docker/Podman-backed disposable databases and fails if unavailable.
- `--use-live` skips container orchestration and uses configured live connections.
- `--allow-live` permits live fallback when containers are unavailable.
- `--yes` allows non-interactive confirmation for live fallback after safety checks.

Example output:

```text
Resolved dialects: sqlite, mysql, postgres

sqlite
  database: /tmp/goforj-migrate-test/app.sqlite
  result: passed

mysql
  image: mariadb:11
  database: app
  port: 49321
  result: passed

postgres
  image: postgres:16
  database: app
  port: 49322
  result: passed
```

Fallback output:

```text
mysql
  container runtime: unavailable
  fallback: configured live connection
  database: app_test
  result: passed
```

This makes multi-dialect migration testing realistic for normal app teams. Users can still opt into existing services later, but that should not be required for the default path.

## Development Loop Integration

`forj dev` should not silently rewrite manual SQL files.

Reasonable development behavior:

- detect migration source changes
- print that translations are stale
- optionally run translation for generated files only
- never overwrite manual target files

Potential future environment flag:

```dotenv
DB_MIGRATIONS_TRANSLATE_ON_DEV=true
```

Default should be conservative until the ownership model is proven.

## Build Integration

`forj build` should validate migration readiness before migrations are embedded into the binary.

Suggested behavior:

- resolve supported drivers from `DB_SUPPORTED_DRIVERS` / `DB_DRIVER`
- detect stale generated target files
- detect missing dialect files for supported drivers
- optionally translate missing generated files before embedding when the source dialect is unambiguous and translation is clean
- fail if translation has warnings in strict projects
- fail if multiple source dialects exist and `--from` is required
- fail if generated files are stale
- embed only final SQL artifacts and migration metadata

This mirrors GoForj's generated-component philosophy while keeping runtime deterministic.

Build output should make migration ownership clear:

```text
Migrations
  supported drivers: sqlite, mysql, postgres
  translated missing postgres files from mysql
  validated sqlite/mysql/postgres files
  embedded 42 migration files
```

Runtime binaries should verify the embedded manifest:

- active driver is present
- migration checksums match embedded metadata
- no required migration file is missing for the active driver
- no runtime translation is needed

## Example Workflow

A developer supports SQLite locally and MySQL/Postgres in production.

Environment:

```dotenv
DB_DRIVER=mysql
DB_SUPPORTED_DRIVERS=sqlite,mysql,postgres
```

Create a grouped migration:

```bash
forj make:migration create_users
```

This creates:

```text
migrations/2026_05_25_120000_create_users/
  mysql.up.sql
  mysql.down.sql
```

Author the source dialect the developer knows:

```text
migrations/2026_05_25_120000_create_users/mysql.up.sql
migrations/2026_05_25_120000_create_users/mysql.down.sql
```

Translate:

```bash
forj migrate translate --from mysql
```

Review generated files:

```text
migrations/2026_05_25_120000_create_users/sqlite.up.sql
migrations/2026_05_25_120000_create_users/sqlite.down.sql
migrations/2026_05_25_120000_create_users/postgres.up.sql
migrations/2026_05_25_120000_create_users/postgres.down.sql
```

Test:

```bash
forj migrate test --all
```

Build:

```bash
forj build
```

Commit all SQL files.

## Package Boundary

This should likely become a reusable library plus framework integration.

Potential package:

```text
github.com/goforj/migrate
```

Library responsibilities:

- migration file discovery
- dialect resolution
- migration table management
- SQL execution
- translation parser/render pipeline
- checksums
- embedded migration manifest support
- zero-config migration test orchestration

GoForj framework responsibilities:

- generated App environment conventions
- CLI integration
- `forj dev` and `forj build` behavior
- docs and golden paths
- test environment conventions
- binary embedding policy

If translation grows independently, the parser/render pipeline may deserve its own package boundary inside the library.

## Open Questions

- Should generated target files be updated automatically by default, or only with `--update`?
- Should `forj build` write clean translated migrations automatically, or require an explicit pre-build `forj migrate translate`?
- Should migration translation metadata live only in file headers, or also in a sidecar manifest?
- How should rollback translation be handled when a safe inverse cannot be inferred?
- Should the translator support shared portable `.up.sql` files as a source dialect?
- How much SQL parsing should be owned directly versus delegated to existing parsers?
- Should MySQL tests use MySQL, MariaDB, or both by default?
- What container runtime abstraction should be used for Docker and Podman?

## Recommended v1

Start narrow.

v1 should provide:

- raw SQL migration runner
- grouped SQL migration files as the golden path
- legacy flat-file migration support
- dialect-specific file resolution
- `forj migrate translate --from <dialect>`
- parser/render support for common DDL only
- generated file headers and overwrite protection
- warnings and strict mode
- migration status and checksums
- `forj migrate validate --offline`
- zero-config `forj migrate test --all` with temp SQLite and disposable MySQL/Postgres containers
- build-time migration validation before embedding
- runtime manifest verification without default runtime translation

Do not support arbitrary SQL transpilation in v1.

Do not introduce Blueprint as the primary migration authoring path.

The product promise should be:

```text
Write SQL naturally. GoForj translates the portable schema parts, flags the database-specific parts, and keeps the executed SQL visible.
```

## Stack Transitions

[Stacks](../context/stacks.md) provide the configuration boundary for transitions such as MySQL to SQLite to Postgres. A saved Stack identifies the target providers; it does not imply permission to translate SQL, execute migrations, or transfer data.

A future transition workflow should prepare target-dialect migrations, report unsupported or lossy operations, validate the target schema, transfer and verify data explicitly, and only then activate the target configuration. Keep the source Stack and its connection settings available until the transition has been verified. Reverse configuration switching must not imply reverse data synchronization.

The Stack wizard currently previews database driver changes and explains this boundary. The translator remains a separate tooling implementation under this design.
