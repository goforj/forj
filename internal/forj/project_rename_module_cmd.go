package forj

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/goforj/console"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

// ProjectRenameModuleCmd keeps project configuration and source references on the same Go module path.
type ProjectRenameModuleCmd struct {
	DryRun bool `help:"List files that would change without writing them"`

	root string
	ui   *console.Console
}

// Signature registers module renaming as a project-wide authoring command.
func (*ProjectRenameModuleCmd) Signature() string {
	return `name:"project:rename-module" help:"Rename the project's Go module and source references"`
}

// Help explains the rewrite boundary so a preview can be reviewed before applying it.
func (*ProjectRenameModuleCmd) Help() string {
	return "Run from the project root. Updates .goforj.yml, the go.mod module directive, Go imports and module-qualified string literals, and templ imports.\n" +
		"Nested modules retain their own identities and files. Dependencies, vendor, node_modules, hidden directories, and symlinks are not traversed.\n\n" +
		"Run forj project:rename-module with no arguments. It shows the current module, prompts for a new path, and asks for confirmation before writing.\n" +
		"Use --dry-run to preview the changed files instead.\n\n" +
		"Run forj build afterward to regenerate code and rebuild App binaries."
}

// Run prompts for the replacement and requires explicit confirmation after the complete rename has been planned.
func (c *ProjectRenameModuleCmd) Run() error {
	root := c.root
	if root == "" {
		root = "."
	}
	ui := c.ui
	if ui == nil {
		ui = console.Default()
	}
	project, err := loadModuleRenameProject(root)
	if err != nil {
		return err
	}
	currentModule := project.mod.Module.Mod.Path
	ui.Infof("Rename project module")
	ui.Infof("Current module: %s", currentModule)
	var newModule string
	for {
		newModule, err = ui.Ask("New module name (leave blank to cancel)")
		if err != nil {
			return err
		}
		if newModule == "" {
			ui.Infof("Module rename canceled")
			return nil
		}
		if err := validateNewProjectModulePath(newModule); err != nil {
			ui.Errorf("%v", err)
			continue
		}
		break
	}
	plan, err := planProjectModuleRename(root, newModule)
	if err != nil {
		return err
	}
	if plan.oldModule != currentModule {
		return fmt.Errorf("project module changed while prompting; run the command again")
	}
	for _, path := range plan.nested {
		ui.Infof("Leaving nested module unchanged: %s", path)
	}
	if len(plan.files) == 0 {
		ui.Infof("Go module is already %s", plan.newModule)
		return nil
	}
	ui.Infof("New module: %s", plan.newModule)
	ui.Infof("%d files across the project will change", len(plan.files))
	if c.DryRun {
		for _, file := range plan.files {
			ui.Infof("  %s", file.path)
		}
		ui.Infof("Dry run complete; no files changed")
		return nil
	}
	confirmed, err := ui.Confirm("Rename the entire project module and all of its imports?", false)
	if err != nil {
		return err
	}
	if !confirmed {
		ui.Infof("Module rename canceled")
		return nil
	}
	if err := plan.apply(os.Rename); err != nil {
		return err
	}
	ui.Successf("Renamed Go module %s to %s (%d files)", plan.oldModule, plan.newModule, len(plan.files))
	ui.Infof("Run forj build to regenerate code and rebuild App binaries")
	return nil
}

// moduleRenamePlan retains original bytes so a failed write can restore already changed files.
type moduleRenamePlan struct {
	root, oldModule, newModule string
	files                      []moduleRenameFile
	nested                     []string
}

// moduleRenameFile records a validated regular file without changing its permissions.
type moduleRenameFile struct {
	path          string
	before, after []byte
	mode          fs.FileMode
}

// moduleRenameProject retains parsed metadata so the prompt and planner apply the same identity checks.
type moduleRenameProject struct {
	config, manifest moduleRenameFile
	document         yaml.Node
	moduleNode       *yaml.Node
	mod              *modfile.File
}

// loadModuleRenameProject rejects ambiguous metadata before showing a module identity or preparing changes.
func loadModuleRenameProject(root string) (*moduleRenameProject, error) {
	config, err := readModuleRenameFile(root, ".goforj.yml")
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(config.before))
	if err := decoder.Decode(&document); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse .goforj.yml: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf(".goforj.yml must contain a single YAML document")
	}
	var identity struct {
		Module string `yaml:"module_name"`
	}
	if err := document.Decode(&identity); err != nil {
		return nil, fmt.Errorf("parse .goforj.yml: %w", err)
	}
	var moduleNode *yaml.Node
	if len(document.Content) == 1 && document.Content[0].Kind == yaml.MappingNode {
		mapping := document.Content[0].Content
		for i := 0; i < len(mapping); i += 2 {
			if mapping[i].Value == "module_name" {
				moduleNode = mapping[i+1]
			}
		}
	}
	if moduleNode == nil || moduleNode.Kind != yaml.ScalarNode || moduleNode.Tag != "!!str" || moduleNode.Anchor != "" || identity.Module == "" {
		return nil, fmt.Errorf(".goforj.yml must contain an explicit, nonempty module_name string without an anchor")
	}
	manifest, err := readModuleRenameFile(root, "go.mod")
	if err != nil {
		return nil, err
	}
	mod, err := modfile.Parse("go.mod", manifest.before, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}
	if mod.Module == nil || mod.Module.Mod.Path != identity.Module {
		return nil, fmt.Errorf("go.mod and .goforj.yml must declare the same module path before renaming")
	}
	return &moduleRenameProject{config: config, manifest: manifest, document: document, moduleNode: moduleNode, mod: mod}, nil
}

// planProjectModuleRename excludes independent module namespaces before rewriting project-owned references.
func planProjectModuleRename(root, newModule string) (*moduleRenamePlan, error) {
	if newModule != strings.TrimSpace(newModule) {
		return nil, fmt.Errorf("Go module path must not have surrounding whitespace")
	}
	if err := validateNewProjectModulePath(newModule); err != nil {
		return nil, err
	}
	project, err := loadModuleRenameProject(root)
	if err != nil {
		return nil, err
	}
	config, manifest := project.config, project.manifest
	document, moduleNode, mod := project.document, project.moduleNode, project.mod
	plan := &moduleRenamePlan{root: root, oldModule: mod.Module.Mod.Path, newModule: newModule}
	if plan.oldModule == newModule {
		return plan, nil
	}
	var dependencies []string
	for _, requirement := range mod.Require {
		dependencies = append(dependencies, requirement.Mod.Path)
	}
	for _, replacement := range mod.Replace {
		dependencies = append(dependencies, replacement.Old.Path)
	}
	var sources []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			nestedPath := filepath.Join(relative, "go.mod")
			if _, err := os.Lstat(filepath.Join(root, nestedPath)); err == nil {
				file, err := readModuleRenameFile(root, nestedPath)
				if err != nil {
					return err
				}
				nested, err := modfile.Parse(nestedPath, file.before, nil)
				if err != nil {
					return err
				}
				if nested.Module == nil {
					return fmt.Errorf("%s has no module directive", nestedPath)
				}
				dependencies = append(dependencies, nested.Module.Mod.Path)
				plan.nested = append(plan.nested, relative)
				return filepath.SkipDir
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), ".templ") {
			sources = append(sources, relative)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	for _, dependency := range dependencies {
		if dependency == newModule {
			return nil, fmt.Errorf("new module path %q already belongs to a dependency or nested module", newModule)
		}
	}
	moduleNode.Value = newModule
	var configBytes bytes.Buffer
	encoder := yaml.NewEncoder(&configBytes)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return nil, fmt.Errorf("encode .goforj.yml: %w", err)
	}
	config.after = configBytes.Bytes()
	if err := mod.AddModuleStmt(newModule); err != nil {
		return nil, err
	}
	manifest.after, err = mod.Format()
	if err != nil {
		return nil, err
	}
	plan.files = []moduleRenameFile{config, manifest}
	for _, path := range sources {
		file, err := readModuleRenameFile(root, path)
		if err != nil {
			return nil, err
		}
		file.after, err = renameModuleReferences(path, file.before, plan.oldModule, newModule, dependencies)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(file.before, file.after) {
			plan.files = append(plan.files, file)
		}
	}
	return plan, nil
}

// readModuleRenameFile refuses special files before opening them, including symlinked source files.
func readModuleRenameFile(root, path string) (moduleRenameFile, error) {
	info, err := os.Lstat(filepath.Join(root, path))
	if err != nil {
		return moduleRenameFile{}, err
	}
	if !info.Mode().IsRegular() {
		return moduleRenameFile{}, fmt.Errorf("%s must be a regular file, not a symlink or special file", path)
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	return moduleRenameFile{path: path, before: data, mode: info.Mode().Perm()}, err
}

// renameModuleReferences edits literal spans without reformatting source or matching similarly prefixed modules.
func renameModuleReferences(path string, source []byte, oldModule, newModule string, dependencies []string) ([]byte, error) {
	files := token.NewFileSet()
	mode := parser.Mode(0)
	if strings.HasSuffix(path, ".templ") {
		mode = parser.ImportsOnly
	}
	file, err := parser.ParseFile(files, path, source, mode)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	type edit struct {
		start, end int
		value      string
	}
	var edits []edit
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil || !modulePathContains(oldModule, value) {
			return true
		}
		for _, dependency := range dependencies {
			if dependency != oldModule && modulePathContains(oldModule, dependency) && modulePathContains(dependency, value) {
				return true
			}
		}
		updated := newModule + strings.TrimPrefix(value, oldModule)
		quoted := strconv.Quote(updated)
		if strings.HasPrefix(literal.Value, "`") {
			quoted = "`" + updated + "`"
		}
		edits = append(edits, edit{files.Position(literal.Pos()).Offset, files.Position(literal.End()).Offset, quoted})
		return true
	})
	result := slices.Clone(source)
	for _, edit := range slices.Backward(edits) {
		result = append(result[:edit.start], append([]byte(edit.value), result[edit.end:]...)...)
	}
	return result, nil
}

// modulePathContains requires a path-component boundary to keep unrelated module names intact.
func modulePathContains(module, path string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}

// apply stages every update before replacing files and restores previous writes if a replacement fails.
func (p *moduleRenamePlan) apply(rename func(string, string) error) error {
	staged := make([]string, len(p.files))
	defer func() {
		for _, path := range staged {
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	for i, file := range p.files {
		current, err := readModuleRenameFile(p.root, file.path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current.before, file.before) || current.mode != file.mode {
			return fmt.Errorf("%s changed while planning the rename; run the command again", file.path)
		}
		if file.mode&0o222 == 0 {
			return fmt.Errorf("%s is read-only", file.path)
		}
		staged[i], err = stageModuleRenameFile(p.root, file, file.after)
		if err != nil {
			return err
		}
	}
	for i, file := range p.files {
		if err := rename(staged[i], filepath.Join(p.root, file.path)); err != nil {
			failure := fmt.Errorf("replace %s: %w", file.path, err)
			for j := i - 1; j >= 0; j-- {
				original := p.files[j]
				backup, restoreErr := stageModuleRenameFile(p.root, original, original.before)
				if restoreErr == nil {
					restoreErr = os.Rename(backup, filepath.Join(p.root, original.path))
					_ = os.Remove(backup)
				}
				if restoreErr != nil {
					failure = errors.Join(failure, fmt.Errorf("restore %s: %w", original.path, restoreErr))
				}
			}
			return failure
		}
	}
	return nil
}

// stageModuleRenameFile uses the destination directory so each replacement stays on one filesystem.
func stageModuleRenameFile(root string, file moduleRenameFile, data []byte) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(filepath.Join(root, file.path)), ".forj-module-*")
	if err != nil {
		return "", err
	}
	path := temporary.Name()
	if _, err = temporary.Write(data); err == nil {
		err = temporary.Chmod(file.mode)
	}
	err = errors.Join(err, temporary.Close())
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
