package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skipModuleTidy isolates generator artifact assertions from dependency resolution already covered by dedicated tests.
func skipModuleTidy(string) error {
	return nil
}

// TestRunGoModTidyPreservesCommandDiagnostics keeps generator-triggered failures actionable during project creation.
func TestRunGoModTidyPreservesCommandDiagnostics(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("not a module file\n"), 0o644); err != nil {
		t.Fatalf("write malformed go.mod: %v", err)
	}
	err := runGoModTidy(root)
	if err == nil || !strings.Contains(err.Error(), "go mod tidy: exit status 1") || !strings.Contains(err.Error(), "\n  go: errors parsing go.mod") {
		t.Fatalf("runGoModTidy() error = %v, want formatted command diagnostics", err)
	}
}

// TestGenerateProjectFilesSynchronizesEnvironmentContracts verifies every build caller receives contract publication through generation itself.
func TestGenerateProjectFilesSynchronizesEnvironmentContracts(t *testing.T) {
	root := t.TempDir()
	local := "APP_ENV=local\nAPP_KEY=private-key\nDB_DATABASE=app\nDB_PASSWORD=private-password\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(local), 0o600); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	if _, err := generateProjectFiles(root, GenerationSelection{}, skipModuleTidy); err != nil {
		t.Fatalf("generate project files: %v", err)
	}
	for _, name := range []string{".env.example", ".env.testing"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(content), "private-key") || strings.Contains(string(content), "private-password") {
			t.Fatalf("%s exposed local credentials:\n%s", name, content)
		}
	}
	testingEnvironment, err := os.ReadFile(filepath.Join(root, ".env.testing"))
	if err != nil {
		t.Fatalf("read testing environment: %v", err)
	}
	if !strings.Contains(string(testingEnvironment), "APP_ENV=testing") || !strings.Contains(string(testingEnvironment), "DB_DATABASE=app_testing") {
		t.Fatalf("testing environment omitted deterministic defaults:\n%s", testingEnvironment)
	}
}

// TestGenerationEnvironmentExamplePrunesDisabledAppKeys keeps contract cleanup inside the generation-owned synchronization boundary.
func TestGenerationEnvironmentExamplePrunesDisabledAppKeys(t *testing.T) {
	root := t.TempDir()
	config := "project_name: Test\nmodule_name: example.test/app\nrender:\n  components: [cli, cache]\napps:\n  billing:\n    components: [cli]\n"
	if err := os.WriteFile(filepath.Join(root, ".goforj.yml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	example := "CACHE_DRIVER=memory\nBILLING_CACHE_DRIVER=memory\nOWNER_SETTING=keep\n"
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte(example), 0o644); err != nil {
		t.Fatalf("write environment example: %v", err)
	}
	got, err := generationEnvironmentExample(root)
	if err != nil {
		t.Fatalf("generationEnvironmentExample() error: %v", err)
	}
	if strings.Contains(string(got), "BILLING_CACHE_DRIVER=") {
		t.Fatalf("disabled App key survived generation cleanup:\n%s", got)
	}
	for _, want := range []string{"CACHE_DRIVER=memory", "OWNER_SETTING=keep"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("generation cleanup omitted %q:\n%s", want, got)
		}
	}
}

func TestGenerateProjectFilesUsesPluralServicePackageDirs(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(projectDir, "internal", "caches"),
		filepath.Join(projectDir, "internal", "queues"),
		filepath.Join(projectDir, "internal", "runtime"),
		filepath.Join(projectDir, "internal", "storages"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeQueueRuntimeFixture(t, projectDir)
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte(strings.Join([]string{
		"CACHE_DRIVER=memory",
		"CACHE_SUPPORTED_DRIVERS=memory",
		"MAIL_DRIVER=log",
		"MAIL_SUPPORTED_DRIVERS=log",
		"QUEUE_DRIVER=null",
		"QUEUE_SUPPORTED_DRIVERS=null",
		"STORAGE_DRIVER=local",
		"STORAGE_SUPPORTED_DRIVERS=local",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	result, err := generateProjectFiles(projectDir, GenerationSelection{
		Storage: true,
		Cache:   true,
		Mail:    true,
		Queue:   true,
	}, skipModuleTidy)
	if err != nil {
		t.Fatalf("GenerateProjectFiles returned error: %v", err)
	}
	if result.TotalFiles != 8 {
		t.Fatalf("total files = %d, want %d", result.TotalFiles, 8)
	}
	if result.ChangedFiles == 0 {
		t.Fatal("expected generated files to be written")
	}

	for _, path := range []string{
		filepath.Join(projectDir, "internal", "caches", "manager_gen.go"),
		filepath.Join(projectDir, "internal", "mail", "manager_gen.go"),
		filepath.Join(projectDir, "internal", "queues", "manager_gen.go"),
		filepath.Join(projectDir, "internal", "storages", "manager_gen.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
}

func TestGenerateProjectFilesRunsGoModTidyWhenDBGenerationRuns(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("DB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\n"), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	called := 0
	tidy := func(dir string) error {
		called++
		if dir != projectDir {
			t.Fatalf("tidy directory = %q, want %q", dir, projectDir)
		}
		return nil
	}

	result, err := generateProjectFiles(projectDir, GenerationSelection{Database: true}, tidy)
	if err != nil {
		t.Fatalf("GenerateProjectFiles returned error: %v", err)
	}
	if result.TotalFiles != 1 {
		t.Fatalf("total files = %d, want %d", result.TotalFiles, 1)
	}
	if result.ChangedFiles == 0 {
		t.Fatal("expected generated db file to be written")
	}
	if called != 1 {
		t.Fatalf("tidy calls = %d, want 1", called)
	}
}

func TestGenerateProjectFilesSkipsGoModTidyWhenNothingChanged(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	t.Setenv("DB_DRIVER", "mysql")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("DB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\n"), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	if _, err := GenerateDBFiles(projectDir); err != nil {
		t.Fatalf("seed generated db file: %v", err)
	}

	called := 0
	tidy := func(string) error {
		called++
		return nil
	}

	result, err := generateProjectFiles(projectDir, GenerationSelection{Database: true}, tidy)
	if err != nil {
		t.Fatalf("GenerateProjectFiles returned error: %v", err)
	}
	if result.TotalFiles != 1 {
		t.Fatalf("total files = %d, want %d", result.TotalFiles, 1)
	}
	if result.ChangedFiles != 0 {
		t.Fatalf("changed files = %d, want 0", result.ChangedFiles)
	}
	if called != 0 {
		t.Fatalf("tidy calls = %d, want 0", called)
	}
}

func TestCmdRunRunsGoModTidyWhenDBGenerationRuns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "database"), 0o755); err != nil {
		t.Fatalf("mkdir database dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\n"), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	called := 0
	tidy := func(dir string) error {
		called++
		if dir != "." {
			t.Fatalf("tidy directory = %q, want %q", dir, ".")
		}
		return nil
	}

	cmd := &Cmd{DB: true}
	if err := cmd.run(tidy); err != nil {
		t.Fatalf("Cmd.Run returned error: %v", err)
	}
	if called != 1 {
		t.Fatalf("tidy calls = %d, want 1", called)
	}
}

// primitiveGenerationResource describes the test boundary shared by generated optional primitives.
type primitiveGenerationResource struct {
	name                    string
	componentKey            string
	generatedPackage        string
	stalePackages           []string
	environment             string
	disabledError           string
	legacyDirectoryFallback bool
	selectExplicitly        func(*Cmd)
}

// TestCmdRunFollowsPrimitiveComponentIntent exercises one durable generation contract across every optional primitive.
func TestCmdRunFollowsPrimitiveComponentIntent(t *testing.T) {
	disabled := false
	enabled := true
	resources := []primitiveGenerationResource{
		{
			name: "Cache", componentKey: "cache", generatedPackage: filepath.Join("internal", "caches"),
			stalePackages: []string{filepath.Join("internal", "caches")},
			environment:   "CACHE_DRIVER=memory\nCACHE_SUPPORTED_DRIVERS=memory\n",
			disabledError: "Cache component is disabled", legacyDirectoryFallback: true,
			selectExplicitly: func(cmd *Cmd) { cmd.Cache = true },
		},
		{
			name: "Storage", componentKey: "storage", generatedPackage: filepath.Join("internal", "storages"),
			stalePackages:    []string{filepath.Join("internal", "storages")},
			environment:      "STORAGE_DRIVER=local\nSTORAGE_SUPPORTED_DRIVERS=local\n",
			disabledError:    "Storage component is disabled",
			selectExplicitly: func(cmd *Cmd) { cmd.Storage = true },
		},
		{
			name: "Events", componentKey: "events", generatedPackage: filepath.Join("internal", "events"),
			stalePackages:    []string{filepath.Join("internal", "events")},
			environment:      "EVENTS_DRIVER=inproc\nEVENTS_SUPPORTED_DRIVERS=inproc\n",
			disabledError:    "Events component is disabled",
			selectExplicitly: func(cmd *Cmd) { cmd.Events = true },
		},
		{
			name: "Background Jobs", componentKey: "jobs", generatedPackage: filepath.Join("internal", "queues"),
			stalePackages: []string{filepath.Join("internal", "jobs"), filepath.Join("internal", "queues")},
			environment:   "QUEUE_DRIVER=workerpool\nQUEUE_SUPPORTED_DRIVERS=workerpool\n",
			disabledError: "Background Jobs component is disabled", legacyDirectoryFallback: true,
			selectExplicitly: func(cmd *Cmd) { cmd.Queue = true },
		},
	}
	scenarios := []struct {
		name          string
		configEnabled *bool
		stalePackage  bool
		explicit      bool
		wantError     bool
		wantGenerated bool
		useLegacyRule bool
	}{
		{name: "disabled config ignores stale packages", configEnabled: &disabled, stalePackage: true},
		{name: "explicit request rejects disabled config", configEnabled: &disabled, stalePackage: true, explicit: true, wantError: true},
		{name: "enabled config creates generated package", configEnabled: &enabled, wantGenerated: true},
		{name: "legacy project follows directory policy", stalePackage: true, useLegacyRule: true},
	}

	for _, resource := range resources {
		t.Run(resource.name, func(t *testing.T) {
			for _, scenario := range scenarios {
				t.Run(scenario.name, func(t *testing.T) {
					root := t.TempDir()
					if scenario.stalePackage {
						for _, path := range resource.stalePackages {
							if err := os.MkdirAll(filepath.Join(root, path), 0o755); err != nil {
								t.Fatalf("create stale %s package %s: %v", resource.name, path, err)
							}
						}
					}
					if scenario.configEnabled != nil {
						writePrimitiveGenerationConfig(t, root, resource.componentKey, *scenario.configEnabled)
					}
					if err := os.WriteFile(filepath.Join(root, ".env"), []byte(resource.environment), 0o644); err != nil {
						t.Fatalf("write %s environment: %v", resource.name, err)
					}
					if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
						t.Fatalf("write go.mod: %v", err)
					}

					restoreWorkingDirectory := useGenerationTestRoot(t, root)
					defer restoreWorkingDirectory()

					cmd := &Cmd{}
					if scenario.explicit {
						resource.selectExplicitly(cmd)
					}
					err := cmd.run(skipModuleTidy)
					if scenario.wantError {
						if err == nil || !strings.Contains(err.Error(), resource.disabledError) {
							t.Fatalf("generation error = %v, want %q", err, resource.disabledError)
						}
					} else if err != nil {
						t.Fatalf("generate %s surface: %v", resource.name, err)
					}

					wantGenerated := scenario.wantGenerated
					if scenario.useLegacyRule {
						wantGenerated = resource.legacyDirectoryFallback
					}
					for _, name := range []string{"manager_gen.go", "accessors_gen.go"} {
						_, statErr := os.Stat(filepath.Join(resource.generatedPackage, name))
						if got := statErr == nil; got != wantGenerated {
							t.Fatalf("generated file %s presence = %t, want %t: %v", name, got, wantGenerated, statErr)
						}
					}
				})
			}
		})
	}
}

// writePrimitiveGenerationConfig writes the compact durable component selection consumed by generation.
func writePrimitiveGenerationConfig(t *testing.T, root string, component string, enabled bool) {
	t.Helper()
	components := "cli"
	if enabled {
		components += ", " + component
	}
	contents := "project_name: Test\nmodule_name: example.test/app\nrender:\n  components: [" + components + "]\n"
	if err := os.WriteFile(filepath.Join(root, ".goforj.yml"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
}

// useGenerationTestRoot changes into an isolated generation fixture and returns its restoration function.
func useGenerationTestRoot(t *testing.T, root string) func() {
	t.Helper()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	return func() { _ = os.Chdir(originalWD) }
}

func TestCmdRunGeneratesObservabilityTargetsWithoutGoModTidy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "containers", "observability", "vmagent"), 0o755); err != nil {
		t.Fatalf("mkdir vmagent dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "http"), 0o755); err != nil {
		t.Fatalf("mkdir http dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(strings.Join([]string{
		"APP_NAME=Test App",
		"APP_ENV=local",
		"OBSERVABILITY_METRICS_TARGET_HOST=localhost",
		"METRICS_API_PORT=9100",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	called := 0
	tidy := func(string) error {
		called++
		return nil
	}

	cmd := &Cmd{Observability: true}
	if err := cmd.run(tidy); err != nil {
		t.Fatalf("Cmd.Run returned error: %v", err)
	}
	if called != 0 {
		t.Fatalf("tidy calls = %d, want 0", called)
	}

	content, err := os.ReadFile(filepath.Join(root, "containers", "observability", "vmagent", "metrics-targets.json"))
	if err != nil {
		t.Fatalf("read metrics-targets.json: %v", err)
	}
	if string(content) == "" {
		t.Fatal("expected generated metrics targets content")
	}
}

func TestGenerateProjectFilesSkipsGoModTidyForObservabilityOnlyChanges(t *testing.T) {
	projectDir := t.TempDir()
	for _, dir := range []string{
		filepath.Join(projectDir, "internal", "storages"),
		filepath.Join(projectDir, "internal", "http"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	t.Setenv("STORAGE_DRIVER", "local")
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte(strings.Join([]string{
		"STORAGE_DRIVER=local",
		"STORAGE_SUPPORTED_DRIVERS=local",
		"APP_NAME=Test App",
		"OBSERVABILITY_METRICS_TARGET_HOST=host.docker.internal",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write environment: %v", err)
	}

	if _, err := GenerateStorageFiles(projectDir); err != nil {
		t.Fatalf("seed generated storage file: %v", err)
	}

	called := 0
	tidy := func(string) error {
		called++
		return nil
	}

	result, err := generateProjectFiles(projectDir, GenerationSelection{
		Storage:       true,
		Observability: true,
	}, tidy)
	if err != nil {
		t.Fatalf("GenerateProjectFiles returned error: %v", err)
	}
	if result.TotalFiles != 3 {
		t.Fatalf("total files = %d, want %d", result.TotalFiles, 3)
	}
	if result.ChangedFiles != 1 {
		t.Fatalf("changed files = %d, want %d", result.ChangedFiles, 1)
	}
	if called != 0 {
		t.Fatalf("tidy calls = %d, want 0", called)
	}
}

// TestGenerateProjectFilesUsesExplicitEnvironmentWithoutActivatingIt prevents builds from compiling the active stack instead of the selected definition.
func TestGenerateProjectFilesUsesExplicitEnvironmentWithoutActivatingIt(t *testing.T) {
	root := t.TempDir()
	local := "CACHE_DRIVER=memory\nCACHE_SUPPORTED_DRIVERS=memory\n"
	for name, content := range map[string]string{".env": local, "go.mod": "module example.org/stack\n\ngo 1.26.0\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	selection := GenerationSelection{Cache: true, Environment: map[string]string{"CACHE_DRIVER": "redis", "CACHE_SUPPORTED_DRIVERS": "memory,redis"}}
	if _, err := generateProjectFiles(root, selection, skipModuleTidy); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(root, "internal", "caches", "manager_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "/redis") {
		t.Fatalf("selected driver missing from generated support: %s", generated)
	}
	current, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != local {
		t.Fatal("generation activated the selected stack")
	}
}
