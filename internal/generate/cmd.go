package generate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goforj/goforj/internal/commanddiag"
	"github.com/goforj/goforj/internal/envcontract"
	"github.com/goforj/goforj/internal/envfile"
	"github.com/goforj/goforj/internal/projectlayout"
	"github.com/goforj/goforj/internal/resourceenv"
	"github.com/goforj/goforj/project"
)

// moduleTidyRunner names the dependency publication boundary used after generated imports change.
type moduleTidyRunner func(projectDir string) error

// GenerationSelection names the project-owned surfaces that should be regenerated.
type GenerationSelection struct {
	// Environment overlays explicit build inputs without changing the active dotenv file.
	Environment   map[string]string
	Storage       bool
	Cache         bool
	Mail          bool
	Queue         bool
	Events        bool
	Database      bool
	Observability bool
}

// GenerationResult names file counts so callers cannot transpose positional integer returns.
type GenerationResult struct {
	// TotalFiles reports the current artifacts owned by the selected generators.
	TotalFiles int
	// ChangedFiles reports current artifacts written and legacy artifacts removed during this run.
	ChangedFiles int
}

// GenerationSelectionFromComponents translates durable component intent into generator participation.
func GenerationSelectionFromComponents(components project.Components) GenerationSelection {
	return GenerationSelection{
		Storage:       components.Storage,
		Cache:         components.Cache,
		Mail:          components.Mail,
		Queue:         components.Jobs,
		Events:        components.Events,
		Database:      components.HasDatabase(),
		Observability: components.Observability,
	}
}

// any reports whether the caller selected at least one generated surface explicitly.
func (s GenerationSelection) any() bool {
	return s.Storage || s.Cache || s.Mail || s.Queue || s.Events || s.Database || s.Observability
}

// generationTask keeps ordering, file accounting, and dependency behavior in one inventory.
type generationTask struct {
	selected             bool
	generatedFiles       int
	updatesDependencies  bool
	disabledRequestError string
	generate             func(generationInput) (int, error)
}

// generationRun records the distinctions needed by project rendering and the interactive command's dependency policy.
type generationRun struct {
	totalFiles             int
	changedFiles           int
	dependencyTaskRan      bool
	dependencyFilesChanged bool
}

// Cmd selects generated resource packages and derived project files.
type Cmd struct {
	Storage       bool `help:"Generate storage code"`
	Cache         bool `help:"Generate cache code"`
	Mail          bool `help:"Generate mail code"`
	Queue         bool `help:"Generate queue code"`
	Events        bool `help:"Generate events code"`
	DB            bool `help:"Generate DB connection accessors"`
	Observability bool `help:"Generate observability-derived files"`
}

// NewCmd returns a generate command with the conventional all-resources default.
func NewCmd() *Cmd {
	return &Cmd{}
}

// Signature declares the generated command name and help text.
func (*Cmd) Signature() string {
	return `name:"generate" help:"Generate application code and derived files"`
}

// Run regenerates the selected project resources from the project-owned environment snapshot.
func (c *Cmd) Run() error {
	return c.run(runGoModTidy)
}

// run keeps module mutation explicit so tests cannot replace process-global generator behavior.
func (c *Cmd) run(tidy moduleTidyRunner) error {
	requested := c.generationSelection()
	available, err := commandGenerationSelection(".")
	if err != nil {
		return err
	}
	selection := requested
	if requested.any() {
		if err := validateRequestedGenerationSelection(requested, available); err != nil {
			return err
		}
	} else {
		selection = available
	}

	input, err := loadProjectGenerationInput(".")
	if err != nil {
		return err
	}
	run, err := runGenerationLifecycle(input, selection)
	if err != nil {
		return err
	}
	if run.dependencyTaskRan {
		if err := tidy("."); err != nil {
			return err
		}
	}
	return nil
}

// GenerateProjectFiles regenerates selected resources beneath projectDir and returns named file accounting.
func GenerateProjectFiles(projectDir string, selection GenerationSelection) (GenerationResult, error) {
	return generateProjectFiles(projectDir, selection, runGoModTidy)
}

// generateProjectFiles keeps dependency publication injectable without exposing it through the public generator API.
func generateProjectFiles(projectDir string, selection GenerationSelection, tidy moduleTidyRunner) (GenerationResult, error) {
	input, err := loadProjectGenerationInput(projectDir)
	if err != nil {
		return GenerationResult{}, err
	}
	for key, value := range selection.Environment {
		input.environment.values[key] = value
	}
	run, err := runGenerationLifecycle(input, selection)
	result := GenerationResult{TotalFiles: run.totalFiles, ChangedFiles: run.changedFiles}
	if err != nil {
		return result, err
	}
	if run.dependencyFilesChanged {
		if err := tidy(projectDir); err != nil {
			return result, err
		}
	}
	return result, nil
}

// runGenerationLifecycle keeps environment contract synchronization at the generation boundary shared by direct, build, and dev workflows.
func runGenerationLifecycle(input generationInput, selection GenerationSelection) (generationRun, error) {
	run, err := runGenerationTasks(input, selection)
	if err != nil {
		return run, err
	}
	example, err := generationEnvironmentExample(input.projectDir)
	if err != nil {
		return run, err
	}
	if _, err := envcontract.SyncWithExample(input.projectDir, example); err != nil {
		return run, fmt.Errorf("sync environment contracts: %w", err)
	}
	return run, nil
}

// generationEnvironmentExample removes obsolete framework-owned keys before the contract pair is atomically derived by generation.
func generationEnvironmentExample(projectDir string) ([]byte, error) {
	path := filepath.Join(projectDir, ".env.example")
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read environment example: expected a regular file")
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect environment example: %w", err)
	}
	example, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read environment example: %w", err)
	}
	config, err := project.LoadProjectConfigAt(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return example, nil
		}
		return nil, fmt.Errorf("load environment generation config: %w", err)
	}
	runtimeApps := projectlayout.RuntimeApps(projectDir, config)
	example, _ = resourceenv.RemoveGeneratedAssignments(example, project.ProjectComponents(config), runtimeApps)
	lines := strings.Split(string(example), "\n")
	remove := make(map[string]bool)
	for name, app := range config.Apps {
		if project.NormalizeConfiguredAppComponents(config, app.Components).Cache {
			continue
		}
		prefix := project.AppEnvironmentPrefix(name)
		if prefix != "" {
			remove[prefix+"_CACHE_DRIVER"] = true
		}
	}
	filtered := lines[:0]
	for _, line := range lines {
		key, ok := envfile.ScanKey(line)
		if ok && remove[key] {
			continue
		}
		filtered = append(filtered, line)
	}
	return []byte(strings.Join(filtered, "\n")), nil
}

// generationSelection returns the command-line flags as one named selection value.
func (c Cmd) generationSelection() GenerationSelection {
	return GenerationSelection{
		Storage:       c.Storage,
		Cache:         c.Cache,
		Mail:          c.Mail,
		Queue:         c.Queue,
		Events:        c.Events,
		Database:      c.DB,
		Observability: c.Observability,
	}
}

// commandGenerationSelection uses durable project intent and limits directory inference to pre-contract compatibility surfaces.
func commandGenerationSelection(projectDir string) (GenerationSelection, error) {
	config, err := project.LoadProjectConfigAt(projectDir)
	if err == nil {
		return GenerationSelectionFromComponents(project.ProjectComponents(config)), nil
	}
	if !os.IsNotExist(err) {
		return GenerationSelection{}, fmt.Errorf("load project generation selection: %w", err)
	}
	return legacyCommandGenerationSelection(projectDir)
}

// legacyCommandGenerationSelection preserves only the package markers used before durable component configuration existed.
func legacyCommandGenerationSelection(projectDir string) (GenerationSelection, error) {
	selection := GenerationSelection{}
	legacyDirectories := []struct {
		selected *bool
		paths    []string
	}{
		{selected: &selection.Cache, paths: []string{filepath.Join("internal", "caches")}},
		{selected: &selection.Mail, paths: []string{filepath.Join("internal", "mail")}},
		{selected: &selection.Queue, paths: []string{filepath.Join("internal", "jobs"), filepath.Join("internal", "queues")}},
		{selected: &selection.Database, paths: []string{filepath.Join("internal", "database")}},
		{selected: &selection.Observability, paths: []string{filepath.Join("containers", "observability", "vmagent")}},
	}
	for _, directory := range legacyDirectories {
		exists, err := anyGenerationDirectoryExists(projectDir, directory.paths)
		if err != nil {
			return GenerationSelection{}, err
		}
		*directory.selected = exists
	}
	return selection, nil
}

// validateRequestedGenerationSelection prevents explicit flags from bypassing the project's component contract.
func validateRequestedGenerationSelection(requested, available GenerationSelection) error {
	availableTasks := generationTasksFor(available)
	for index, task := range generationTasksFor(requested) {
		if task.selected && !availableTasks[index].selected {
			return errors.New(task.disabledRequestError)
		}
	}
	return nil
}

// generationTasksFor preserves the stable generation order and keeps per-resource behavior out of orchestration code.
func generationTasksFor(selection GenerationSelection) []generationTask {
	return []generationTask{
		{selected: selection.Storage, generatedFiles: 2, updatesDependencies: true, disabledRequestError: "cannot generate Storage: the Storage component is disabled in .goforj.yml", generate: generateStorageFiles},
		{selected: selection.Cache, generatedFiles: 2, updatesDependencies: true, disabledRequestError: "cannot generate Cache: the Cache component is disabled in .goforj.yml", generate: generateCacheFiles},
		{selected: selection.Mail, generatedFiles: 2, updatesDependencies: true, disabledRequestError: "cannot generate Mail: the Mail component is disabled in .goforj.yml", generate: generateMailFiles},
		{selected: selection.Queue, generatedFiles: 2, updatesDependencies: true, disabledRequestError: "cannot generate Queue: the Background Jobs component is disabled; enable it in .goforj.yml", generate: generateQueueFiles},
		{selected: selection.Events, generatedFiles: 2, updatesDependencies: true, disabledRequestError: "cannot generate Events: the Events component is disabled in .goforj.yml", generate: generateEventFiles},
		{selected: selection.Database, generatedFiles: 1, updatesDependencies: true, disabledRequestError: "cannot generate DB: no Database component is enabled in .goforj.yml", generate: generateDBFiles},
		{selected: selection.Observability, generatedFiles: 1, disabledRequestError: "cannot generate Observability: the Observability component is disabled in .goforj.yml", generate: generateObservabilityFiles},
	}
}

// runGenerationTasks executes the canonical task inventory and preserves file-count and dependency metadata for its caller.
func runGenerationTasks(input generationInput, selection GenerationSelection) (generationRun, error) {
	run := generationRun{}
	for _, task := range generationTasksFor(selection) {
		if !task.selected {
			continue
		}
		written, err := task.generate(input)
		if err != nil {
			return run, err
		}
		run.totalFiles += task.generatedFiles
		run.changedFiles += written
		if task.updatesDependencies {
			run.dependencyTaskRan = true
			run.dependencyFilesChanged = run.dependencyFilesChanged || written > 0
		}
	}
	return run, nil
}

// anyGenerationDirectoryExists reports whether any legacy or prerequisite package marker is an existing directory.
func anyGenerationDirectoryExists(projectDir string, paths []string) (bool, error) {
	for _, path := range paths {
		info, err := os.Stat(filepath.Join(projectDir, path))
		if err == nil {
			if info.IsDir() {
				return true, nil
			}
			continue
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

// runGoModTidy refreshes dependencies without exposing project resource credentials to Go or invoked VCS processes.
func runGoModTidy(projectDir string) error {
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = projectDir
	cmd.Env = generationSubprocessEnvironment(projectDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return commanddiag.Wrap("go mod tidy", err, string(output))
	}
	return nil
}

// generationSubprocessEnvironment uses project App identities so even invalid resource inputs cannot leak into child processes.
func generationSubprocessEnvironment(projectDir string) []string {
	assignments := os.Environ()
	snapshot := generationEnvironmentFromAssignments(assignments)
	filter := newGenerationEnvironmentFilter(projectDir, snapshot)
	environment := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		key, _, _ := strings.Cut(assignment, "=")
		if filter.keeps(key) {
			continue
		}
		environment = append(environment, assignment)
	}
	return environment
}
