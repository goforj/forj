# Rename A Project's Go Module

Run `project:rename-module` from the directory containing `.goforj.yml` and `go.mod`:

```bash
forj project:rename-module
```

The command shows the current module, asks for the new path, and asks for confirmation before updating the project and its imports:

```text
Current module: example.com/starter
New module name (leave blank to cancel): github.com/acme/orders
New module: github.com/acme/orders
42 files across the project will change
Rename the entire project module and all of its imports? [y/N]: y
```

The file count depends on the project. Leave the new name blank, answer no, or press Enter at confirmation to cancel without changing any files. Invalid module names prompt again. The command requires an interactive terminal and has no positional module argument or automatic confirmation flag.

To list the affected files without writing, run `forj project:rename-module --dry-run` and enter the new module at the prompt. After a confirmed rename, run `forj build` to regenerate code and rebuild the App binaries.

The rename updates the project configuration, the `go.mod` module directive, and references in every App's Go source, including tests and generated files. Go string literals that equal the old module or start with its slash-separated path are updated too, including the generated runtime's module information. For `.templ` sources, the command updates imports so subsequent templ generation retains the new path.

For example, renaming `example.com/starter` to `github.com/acme/orders` changes:

```yaml
# .goforj.yml before
project_name: Orders
module_name: example.com/starter
```

```yaml
# .goforj.yml after
project_name: Orders
module_name: github.com/acme/orders
```

```go
// Before
import "example.com/starter/internal/billing"
```

```go
// After
import "github.com/acme/orders/internal/billing"
```

The new path uses the same local validation as `forj new`. No repository lookup or dependency download is required for the rename. The configuration and manifest must agree on the old module before any files are changed. Entering the current module is a no-op.

The command preserves the project name, App names, configuration settings and comments, dependency versions, relative `replace` directives, Go version, and source file permissions. YAML and `go.mod` formatting may be normalized; Go source formatting and import aliases are preserved. Source references use a path-component boundary, so `example.com/starter-extra` does not change when renaming `example.com/starter`.

Independent nested modules retain their own module paths and files and are reported in the output. References to dependency and nested-module paths remain intact, even when their names start with the old project module. Update any nested module that consumes the renamed project separately, including its imports and dependency declarations. A target path already used by a dependency or nested module is rejected.

The rewrite does not traverse hidden directories, `vendor`, `node_modules`, or directory symlinks. A selected source file, manifest, or project config that is itself a symlink or special file is rejected. Documentation, ordinary comments, unrelated string contents, environment files, assets, Git remotes, and built binaries are left alone.

All source parsing and change planning completes before writing. The command stages updated files and attempts to restore already replaced files if a later replacement fails, reporting any restoration failure. Review the resulting diff, then run `forj build` to regenerate code and rebuild the App binaries. Updating `.goforj.yml` ensures future renders use the new module path.
