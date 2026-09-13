# Stacks

A Stack selects the providers behind an App's resources. `forj stack` opens an interactive wizard from the project root, with no required arguments.

The wizard supports two workflows:

- Change the current `.env` directly, without naming or saving a stack.
- Create reusable dotenv definitions, then switch between them or build their defaults into a binary.

## Current configuration and saved definitions

```text
forj stack

What would you like to do?
1. Change current configuration
2. Switch to a saved stack
3. Save current configuration as a stack
4. Create a stack
5. Build a saved stack
6. Restore previous configuration
7. Cancel
```

The wizard lists existing stacks and current resource drivers. Creating a stack starts from portable defaults, the current configuration, or an existing stack. The editor uses the resource catalog for driver choices, includes named resources and configured App overrides, and lets owners enter additional connection settings through hidden input. Compose profiles can be edited explicitly, including an empty selection.

Saving and activating are separate confirmations. Saving alone leaves `.env` unchanged. The dev environment watcher ignores saved Stack definitions and private metadata, so saving an inactive Stack does not request an environment rebuild. Reusing a name asks before replacing its definition and private settings.

```text
.env                         Active private configuration
.env.stack.services          Shareable provider choices
.env.stack.services.local    Private connection settings or working copy
.env.stack.portable          Shareable portable provider choices
.env.stack.portable.local    Private portable settings
.env.stack-state.local       Active name and previous managed configuration
```

Definitions contain driver selections, supported driver lists, Compose profiles, and SQLite file paths when SQLite is selected. Other resource settings, including connection endpoints and credentials, are private by default. The wizard updates `.gitignore` to make definitions shareable while keeping private files ignored. It refuses to write a private file already tracked by Git.

Stack definitions do not contain `APP_KEY`, `APP_ENV`, or unrelated application settings. A Stack is independent of the existing `APP_ENV` environment layers. Before activation, the wizard identifies standard dotenv overlays and inherited process variables containing resource settings, without displaying their values. Their existing runtime precedence still applies.

## Switching to portable providers

For enabled resources, the portable preset suggests:

| Resource | Provider |
|---|---|
| Database | SQLite |
| Cache | Memory |
| Queue | Workerpool |
| Events | Inproc |
| Storage | Local |
| Mail | Log |
| Compose profiles | Empty |

Example before:

```dotenv
DB_DRIVER=mysql
DB_SUPPORTED_DRIVERS=mysql
CACHE_DRIVER=redis
CACHE_SUPPORTED_DRIVERS=memory,redis
COMPOSE_PROFILES=mysql,redis
```

After choosing portable defaults and confirming activation:

```dotenv
DB_DRIVER=sqlite
DB_SUPPORTED_DRIVERS=mysql,sqlite
DB_DATABASE=./_data/stacks/portable/db.db
DB_SQLITE_DATABASE=./_data/stacks/portable/db.db
DB_DSN=
CACHE_DRIVER=memory
CACHE_SUPPORTED_DRIVERS=memory,redis
COMPOSE_PROFILES=
```

Portable database settings clear stale DSNs and assign distinct paths to named databases and App overrides. Existing supported drivers remain available in the generated build contract. Run `forj build` after activation to regenerate and compile driver support.

Switching back to a saved services stack restores its connection settings. Activation preserves unrelated `.env` text, comments, and multiline values. Before every switch, GoForj saves the previous managed settings privately. Restore previous configuration works even when the previous configuration had no stack name. This stores one previous configuration, not an unlimited history.

If the active stack has manual edits, the wizard asks whether to retain them in a private working copy, discard them when leaving, or cancel. Keeping them does not rewrite the shareable definition. A private working copy preserves absent keys as well as explicitly empty values. To deliberately update a shareable definition, save the current configuration under the same name.

The final preview masks connection values and requires confirmation. A failed file replacement rolls back earlier writes. Concurrent changes detected while the wizard was open require starting again. Stack operations use `.env.stack-lock.local`; after an interrupted process, remove a stale lock only after verifying no Stack operation is still running.

## Building a Stack

```bash
forj build --stack portable
forj customer-portal build --stack portable
```

The build reads the shareable definition directly. It does not activate that stack, read its `.local` file, or copy credentials from the current `.env` into the binary. Normal build generation still updates generated source and dependency support.

The selected App's defaults are folded into its binary's base configuration keys. Values use a structured payload, preserving commas and explicitly empty settings. Existing runtime environment precedence remains in place:

1. Existing forced `--env-overrides`, when explicitly supplied.
2. Runtime process and dotenv configuration, with the existing App overlay rules.
3. Explicit `--env-defaults`.
4. Baked Stack defaults.
5. Resource runtime defaults.

An existing services `.env` therefore still selects services when running a portable-default binary from that project directory. Run the binary with the intended deployment configuration. An explicit provider failure does not trigger an automatic switch to SQLite or memory. Runtime driver selections must be among the binary's compiled providers.

Existing projects need `forj render` once to receive the structured Stack runtime helper. Existing `forj build` behavior and explicit project configuration remain unchanged when `--stack` is omitted. `stack` is now a framework command and cannot be used as a new App name.

## Database transitions

Activating a Stack edits `.env`. The wizard does not directly start or stop containers, restart Apps, translate migrations, create schemas, or transfer data. A running `forj dev` session can react to that edit, rebuild, and run its configured startup tasks. Stop the dev session before preparing a database transition or building a different deployment Stack. An empty Compose profile selection prevents automatic dependency startup; use `forj down` to stop existing containers.

SQLite, MySQL, and Postgres can require different SQL. Memory caches, workerpool queues, and inproc events also have different persistence and process-sharing behavior. Portable configuration cannot prove that custom application SQL or external integrations are portable.

The intended database transition workflow is:

```text
Select target Stack
Review the driver and connection changes
Translate and review target-dialect migrations
Validate migrations on the target database
Transfer data explicitly and verify the result
Activate the target Stack
Build and restart the App
```

The [migration translation design](../designs/migration-translation-design.md) owns SQL translation. Translation and data transfer remain explicit operations with separately reviewed targets. This Stack implementation does not add a SQL translator or invoke one implicitly. Returning to another Stack selects that Stack's data; it does not synchronize writes made since leaving it.
