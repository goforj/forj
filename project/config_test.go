package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRenderConfigQueueDriverIsLoadOnly keeps legacy projects readable without persisting wizard-only state.
func TestRenderConfigQueueDriverIsLoadOnly(t *testing.T) {
	var config Config
	if err := yaml.Unmarshal([]byte(`render:
  components:
    jobs: true
  queue_driver: nats
`), &config); err != nil {
		t.Fatalf("unmarshal legacy queue driver: %v", err)
	}
	if config.Render.LegacyQueueDriver() != "nats" {
		t.Fatalf("legacy queue driver = %q, want nats", config.Render.LegacyQueueDriver())
	}
	if !config.Render.HasLegacyQueueDriver() {
		t.Fatal("legacy queue driver presence was not retained for migration")
	}
	encoded, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("marshal project config: %v", err)
	}
	if strings.Contains(string(encoded), "queue_driver:") {
		t.Fatalf("wizard-only queue driver remained in project config:\n%s", encoded)
	}

	var emptyLegacy Config
	if err := yaml.Unmarshal([]byte("render:\n  queue_driver: \"\"\n"), &emptyLegacy); err != nil {
		t.Fatalf("unmarshal empty legacy queue driver: %v", err)
	}
	if !emptyLegacy.Render.HasLegacyQueueDriver() {
		t.Fatal("explicitly empty legacy queue driver was indistinguishable from an absent key")
	}
	var current Config
	if err := yaml.Unmarshal([]byte("render: {}\n"), &current); err != nil {
		t.Fatalf("unmarshal current render config: %v", err)
	}
	if current.Render.HasLegacyQueueDriver() {
		t.Fatal("current render config was classified as legacy")
	}
}

// TestLoadProjectConfigAtDoesNotChangeWorkingDirectory verifies App-scoped build helpers can inspect an explicit project root safely.
func TestLoadProjectConfigAtDoesNotChangeWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".goforj.yml"), []byte("project_name: Rooted API\nmodule_name: example.com/rooted\n"), 0o644); err != nil {
		t.Fatalf("write rooted config: %v", err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	config, err := LoadProjectConfigAt(root)
	if err != nil {
		t.Fatalf("load rooted config: %v", err)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory after load: %v", err)
	}
	if config.ProjectName != "Rooted API" || after != before {
		t.Fatalf("rooted config = %#v, working directory before=%q after=%q", config, before, after)
	}
}

func TestLoadProjectConfigSupportsWatcherEnv(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".goforj.yml")
	if err := os.WriteFile(configPath, []byte(`project_name: Test
module_name: example.com/test
updated_at: 2026-03-14 00:00:00 CDT
dev:
  sound_on_watch_error: true
  watches:
    - name: Wire
      watch: -file .go
      exec: wire
      env:
        WIRE_INCREMENTAL: "1"
render:
  components:
    cli: true
  goforj_version: 0.18.0
  module_replaces:
    github.com/goforj/web: /Users/cmiles/code/web
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	cfg, err := LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig returned error: %v", err)
	}
	if len(cfg.Dev.Watches) != 1 {
		t.Fatalf("expected 1 watch, got %d", len(cfg.Dev.Watches))
	}
	if cfg.Dev.Watches[0].Env["WIRE_INCREMENTAL"] != "1" {
		t.Fatalf("expected watcher env to be loaded, got %#v", cfg.Dev.Watches[0].Env)
	}
	if got := cfg.Render.ModuleReplaces["github.com/goforj/web"]; got != "/Users/cmiles/code/web" {
		t.Fatalf("expected module replace to be loaded, got %#v", cfg.Render.ModuleReplaces)
	}
}

func TestLoadProjectConfigSupportsDevRunAllowlist(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".goforj.yml")
	if err := os.WriteFile(configPath, []byte(`project_name: Test
module_name: example.com/test
updated_at: 2026-03-14 00:00:00 CDT
dev:
  run:
    app: run
    worker: queue:work
render:
  components:
    cli: true
  goforj_version: 0.18.0
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	cfg, err := LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig returned error: %v", err)
	}

	if got := cfg.Dev.Run["app"]; got != "run" {
		t.Fatalf("expected app dev run command to load, got %q", got)
	}
	if got := cfg.Dev.Run["worker"]; got != "queue:work" {
		t.Fatalf("expected worker dev run command to load, got %q", got)
	}
}

// TestAppForNameUsesConvention verifies app names map to the established project layout.
func TestAppForNameUsesConvention(t *testing.T) {
	app := AppForName("reporting")
	if app.Entrypoint != filepath.Join("cmd", "reporting", "main.go") ||
		app.AppDir != filepath.Join("app", "reporting") ||
		app.WireDir != filepath.Join("app", "reporting", "wire") {
		t.Fatalf("expected conventional reporting paths, got %#v", app)
	}
}

// TestDefaultNamedAppPreservesCompatibility verifies the retired spelling remains a source-compatible alias.
func TestDefaultNamedAppPreservesCompatibility(t *testing.T) {
	if got, want := DefaultNamedApp("reporting"), AppForName("reporting"); got != want {
		t.Fatalf("DefaultNamedApp() = %#v, want %#v", got, want)
	}
}

func TestDefaultSelectedComponentsIncludeMetricsStack(t *testing.T) {
	components := DefaultSelectedComponents()
	if !components.Metrics || !components.Observability || !components.Grafana {
		t.Fatalf("expected metrics, observability, and grafana to be selected by default: %#v", components)
	}
	if !components.Cache || !components.Events || !components.Storage || !components.Jobs {
		t.Fatalf("expected App primitives and Background Jobs to be selected by default: %#v", components)
	}
}

// TestPrimitiveComponentsAreVisibleRecommendedDefaults keeps the component catalog aligned with the single-stage wizard experience.
func TestPrimitiveComponentsAreVisibleRecommendedDefaults(t *testing.T) {
	visible := make(map[ComponentKey]bool)
	for _, definition := range ProjectWizardComponentDefinitions() {
		visible[definition.Key] = true
	}
	for key, label := range map[ComponentKey]string{
		ComponentCache:   "Cache",
		ComponentEvents:  "Events",
		ComponentStorage: "File Storage",
		ComponentJobs:    "Background Jobs",
	} {
		definition, ok := ComponentDefinitionByKey(key)
		if !ok || definition.Label != label || !definition.DefaultSelected {
			t.Fatalf("primitive definition %q = %#v", key, definition)
		}
		if !visible[key] {
			t.Fatalf("primitive %q is missing from the project component wizard", key)
		}
	}
}

// TestDemoAppRequiresEveryPrimitiveComponent keeps the example feature set coherent across every dependency-resolution caller.
func TestDemoAppRequiresEveryPrimitiveComponent(t *testing.T) {
	components := Components{DemoApp: true}.WithResolvedDependencies()
	if !components.Cache || !components.Events || !components.Storage || !components.Jobs {
		t.Fatalf("Demo App dependency closure = %#v, want Cache, Events, Storage, and Background Jobs", components)
	}
}

func TestComponentCatalogDefinitionsHaveDescriptions(t *testing.T) {
	for _, definition := range ComponentCatalog() {
		if definition.Description == "" {
			t.Fatalf("expected component %q to have a wizard description", definition.Key)
		}
	}
}

// TestHelpFormatCatalogPresentsGuidedAsExternalDefault keeps Guided as the first user-facing CLI recommendation.
func TestHelpFormatCatalogPresentsGuidedAsExternalDefault(t *testing.T) {
	definitions := HelpFormatCatalog()
	if len(definitions) < 3 {
		t.Fatalf("expected help format catalog to contain at least three choices, got %#v", definitions)
	}
	if definitions[0].Key != HelpFormatFramework {
		t.Fatalf("expected framework to remain first, got %#v", definitions)
	}
	if definitions[1].Key != HelpFormatGuided {
		t.Fatalf("expected guided to be second for user-facing CLIs, got %#v", definitions)
	}
	if definitions[2].Key != HelpFormatExternalCLI {
		t.Fatalf("expected compact external CLI to remain available after guided, got %#v", definitions)
	}
	if !strings.Contains(definitions[1].Description, "external/user-facing CLI") {
		t.Fatalf("expected guided description to frame external CLI usage, got %q", definitions[1].Description)
	}
}

func TestIsSafeAppName(t *testing.T) {
	for _, name := range []string{"app", "reporting", "customer-portal", "v2"} {
		if !IsSafeAppName(name) {
			t.Fatalf("expected %q to be safe", name)
		}
	}
	for _, name := range []string{"", ".", "..", "../reporting", "reporting/api", "reporting api", "ops_api", "CustomerPortal", "2fa", "-admin", "admin-", "admin--api"} {
		if IsSafeAppName(name) {
			t.Fatalf("expected %q to be unsafe", name)
		}
	}
}

func TestIsReservedAppName(t *testing.T) {
	if !IsReservedAppName("wire") {
		t.Fatal("expected wire to be reserved")
	}
	if IsReservedAppName("reporting") {
		t.Fatal("expected reporting not to be reserved")
	}
}

func TestIsNativeFrameworkCommandName(t *testing.T) {
	for _, name := range []string{"build", "dev", "render", "run", "x", "help", "version", "stack", "stack:configure"} {
		if !IsNativeFrameworkCommandName(name) {
			t.Fatalf("expected %q to be a native framework command", name)
		}
	}
	for _, name := range []string{"app", "reporting", "customer-portal"} {
		if IsNativeFrameworkCommandName(name) {
			t.Fatalf("expected %q not to be a native framework command", name)
		}
	}
}

func TestAppPackageName(t *testing.T) {
	tests := map[string]string{
		"app":             "app",
		"reporting":       "reportingapp",
		"customer-portal": "customerportalapp",
		"2fa":             "app2faapp",
	}
	for input, want := range tests {
		if got := AppPackageName(input); got != want {
			t.Fatalf("AppPackageName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestComponentsNormalizedAppliesDependencies(t *testing.T) {
	components := Components{
		Grafana: true,
		OAuth:   true,
	}

	normalized := components.WithResolvedDependencies()

	if !normalized.OAuth || !normalized.Auth || !normalized.Mail || !normalized.Cache {
		t.Fatalf("expected oauth normalization to enable auth, mail, and cache: %#v", normalized)
	}
	if !normalized.Grafana || !normalized.Observability || !normalized.Metrics || !normalized.WebAPI || !normalized.Docker {
		t.Fatalf("expected grafana normalization to enable observability, metrics, web api, and docker: %#v", normalized)
	}
	if components.Auth || components.Mail || components.Cache || components.Jobs || components.WebAPI || components.Observability || components.Metrics || components.Docker {
		t.Fatalf("expected Normalized to leave the original value unchanged: %#v", components)
	}
}

// TestComponentsHasRuntimeClassifiesAppRuntimeCapabilities verifies command generation and launch behavior share one capability boundary.
func TestComponentsHasRuntimeClassifiesAppRuntimeCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		components Components
		want       bool
	}{
		{name: "empty", components: Components{}, want: false},
		{name: "cli only", components: Components{CLI: true}, want: false},
		{name: "database only", components: Components{DatabaseSQLite: true}, want: false},
		{name: "web api", components: Components{WebAPI: true}, want: true},
		{name: "web ui", components: Components{WebUI: true}, want: true},
		{name: "scheduler", components: Components{Scheduler: true}, want: true},
		{name: "jobs", components: Components{Jobs: true}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.components.HasRuntime(); got != test.want {
				t.Fatalf("HasRuntime() = %t, want %t for %#v", got, test.want, test.components)
			}
		})
	}
}

func TestValidateRenderContractHonorsDependencies(t *testing.T) {
	components := Components{
		OAuth:            true,
		WebAPI:           true,
		DatabasePostgres: true,
	}

	if err := components.ValidateRenderContract(); err != nil {
		t.Fatalf("expected oauth config to validate through normalized auth dependency, got %v", err)
	}
}

func TestStarterKitDefaultsToNone(t *testing.T) {
	if got := DefaultStarterKit(); got != StarterKitNone {
		t.Fatalf("default starter kit = %q, want %q", got, StarterKitNone)
	}
}

func TestValidateStarterKitContractRequiresWebUI(t *testing.T) {
	err := ValidateStarterKitContract(StarterKitVue, Components{})
	if err == nil {
		t.Fatalf("expected vue starter kit without web ui to fail validation")
	}

	err = ValidateStarterKitContract(StarterKitVue, Components{WebUI: true})
	if err != nil {
		t.Fatalf("expected vue starter kit with web ui to validate, got %v", err)
	}
}

func TestAppComponentsFromKeysUsesProjectCapabilities(t *testing.T) {
	available := Components{
		WebAPI:        true,
		WebUI:         true,
		Metrics:       true,
		DatabaseMySQL: true,
		Auth:          true,
		Mail:          true,
		Jobs:          true,
	}

	components, err := AppComponentsFromKeys(available, []ComponentKey{ComponentAuth, ComponentJobs})
	if err != nil {
		t.Fatalf("AppComponentsFromKeys returned error: %v", err)
	}
	if !components.Auth || !components.WebAPI || !components.DatabaseMySQL || !components.Mail || !components.Cache || !components.Jobs || !components.Metrics {
		t.Fatalf("app components missing expected dependencies: %#v", components)
	}
	if components.WebUI || components.Docker || components.Observability || components.Grafana || components.DemoApp {
		t.Fatalf("app components included non-selected project-level components: %#v", components)
	}
}

func TestAppComponentsAllowNewDatabaseDriver(t *testing.T) {
	available := Components{
		WebAPI:        true,
		DatabaseMySQL: true,
	}

	components, err := AppComponentsFromKeys(available, []ComponentKey{ComponentWebAPI, ComponentDatabasePostgres})
	if err != nil {
		t.Fatalf("AppComponentsFromKeys returned error: %v", err)
	}
	if !components.WebAPI || !components.DatabasePostgres {
		t.Fatalf("app components missing selected postgres driver: %#v", components)
	}
	if components.DatabaseMySQL || components.DatabaseSQLite {
		t.Fatalf("expected app database selection to be exclusive: %#v", components)
	}
}

func TestAppComponentsKeepLastDatabaseDriver(t *testing.T) {
	components, err := AppComponentsFromKeys(Components{}, []ComponentKey{ComponentDatabaseMySQL, ComponentDatabasePostgres})
	if err != nil {
		t.Fatalf("AppComponentsFromKeys returned error: %v", err)
	}
	if !components.DatabasePostgres || components.DatabaseMySQL || components.DatabaseSQLite {
		t.Fatalf("expected last database driver to win: %#v", components)
	}
}

func TestPromoteAppComponentsAddsProjectCapabilities(t *testing.T) {
	available := Components{
		WebAPI:        true,
		DatabaseMySQL: true,
		Docker:        true,
	}
	selected := Components{
		WebAPI:           true,
		Auth:             true,
		DatabasePostgres: true,
		Jobs:             true,
	}

	promoted := PromoteAppComponents(available, selected)
	if !promoted.WebAPI || !promoted.Auth || !promoted.Mail || !promoted.Cache || !promoted.DatabaseMySQL || !promoted.DatabasePostgres || !promoted.Jobs {
		t.Fatalf("promoted components missing expected capabilities: %#v", promoted)
	}
	if !promoted.Docker {
		t.Fatalf("expected project-level docker capability to be preserved: %#v", promoted)
	}
}

// TestProjectComponentsDerivesNamedAppCapabilitiesWithoutChangingDefaultApp verifies shared support does not mutate the default App.
func TestProjectComponentsDerivesNamedAppCapabilitiesWithoutChangingDefaultApp(t *testing.T) {
	defaultComponents := Components{
		CLI:           true,
		WebAPI:        true,
		DatabaseMySQL: true,
		Docker:        true,
		Metrics:       true,
	}
	config := &Config{
		Render: RenderConfig{Components: defaultComponents},
		Apps: map[string]AppConfig{
			"reporting": {
				Components: Components{CLI: true, Mail: true, WebAPI: true, DatabasePostgres: true, Jobs: true},
			},
		},
	}

	envelope := ProjectComponents(config)
	if !envelope.Mail || !envelope.DatabaseMySQL || !envelope.DatabasePostgres || !envelope.Jobs {
		t.Fatalf("project envelope missing named-App capabilities: %#v", envelope)
	}
	if !envelope.Docker || !envelope.Metrics {
		t.Fatalf("project envelope lost project-only capabilities: %#v", envelope)
	}
	if config.Render.Components != defaultComponents {
		t.Fatalf("default App components changed while deriving the project envelope: %#v", config.Render.Components)
	}
}

// TestProjectComponentsNormalizesAppsAgainstStableDefaultCapabilities verifies sibling database choices cannot change implicit App dependencies.
func TestProjectComponentsNormalizesAppsAgainstStableDefaultCapabilities(t *testing.T) {
	config := &Config{
		Render: RenderConfig{Components: Components{CLI: true}},
		Apps: map[string]AppConfig{
			"accounts": {
				Components: Components{CLI: true, Auth: true},
			},
			"reporting": {
				Components: Components{CLI: true, DatabasePostgres: true},
			},
		},
	}

	for range 20 {
		envelope := ProjectComponents(config)
		if !envelope.DatabaseMySQL || !envelope.DatabasePostgres {
			t.Fatalf("project envelope depended on App map iteration: %#v", envelope)
		}
	}

	accounts := NormalizeConfiguredAppComponents(config, config.Apps["accounts"].Components)
	if !accounts.DatabaseMySQL || accounts.DatabasePostgres {
		t.Fatalf("accounts App database leaked from reporting App: %#v", accounts)
	}
}

func TestAppComponentsRejectProjectOnlyComponent(t *testing.T) {
	_, err := AppComponentsFromKeys(Components{Docker: true}, []ComponentKey{ComponentDocker})
	if err == nil {
		t.Fatal("expected project-only component to be rejected")
	}
}

func TestParseComponentKeyAcceptsCliSpelling(t *testing.T) {
	key, err := ParseComponentKey("web-api")
	if err != nil {
		t.Fatalf("ParseComponentKey returned error: %v", err)
	}
	if key != ComponentWebAPI {
		t.Fatalf("key = %q, want %q", key, ComponentWebAPI)
	}
}

func TestLoadProjectConfigPreservesRawComponentSelections(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".goforj.yml")
	if err := os.WriteFile(configPath, []byte(`project_name: Test
module_name: example.com/test
updated_at: 2026-03-14 00:00:00 CDT
render:
  component_contract: 1
  components:
    auth: true
    web_api: true
    database_sqlite: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	cfg, err := LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig returned error: %v", err)
	}
	if !cfg.Render.Components.Auth {
		t.Fatalf("expected auth to be loaded")
	}
	if cfg.Render.Components.Mail || cfg.Render.Components.Cache {
		t.Fatalf("expected raw config load to preserve unresolved dependencies, got %#v", cfg.Render.Components)
	}
	effective := cfg.Render.Components.WithResolvedDependencies()
	if !effective.Mail || !effective.Cache {
		t.Fatalf("expected effective Auth dependencies to include Mail and Cache, got %#v", effective)
	}
}

func TestLoadProjectConfigSupportsStarterKit(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".goforj.yml")
	if err := os.WriteFile(configPath, []byte(`project_name: Test
module_name: example.com/test
updated_at: 2026-03-14 00:00:00 CDT
render:
  starter_kit: vue
  components:
    web_ui: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	cfg, err := LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig returned error: %v", err)
	}
	if cfg.Render.StarterKit != StarterKitVue {
		t.Fatalf("expected vue starter kit, got %q", cfg.Render.StarterKit)
	}
}

// TestLoadProjectConfigSupportsNestedStarterKitOptions verifies the scalar starter-kit selection remains compatible while options stay grouped.
func TestLoadProjectConfigSupportsNestedStarterKitOptions(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".goforj.yml")
	if err := os.WriteFile(configPath, []byte(`project_name: Test
module_name: example.com/test
render:
  starter_kit: vue
  starter_kit_options:
    component_library: false
  components:
    web_ui: true
apps:
  photos:
    starter_kit: react
    starter_kit_options:
      component_library: false
    components:
      web_ui: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadProjectConfigAt(root)
	if err != nil {
		t.Fatalf("LoadProjectConfigAt returned error: %v", err)
	}
	if cfg.Render.StarterKit != StarterKitVue {
		t.Fatalf("starter kit = %q, want vue", cfg.Render.StarterKit)
	}
	if cfg.Render.StarterKitOptions.ComponentLibraryEnabled() {
		t.Fatal("default App component library = true, want false")
	}
	if cfg.Apps["photos"].StarterKitOptions.ComponentLibraryEnabled() {
		t.Fatal("photos App component library = true, want false")
	}
	if !(*StarterKitOptions)(nil).ComponentLibraryEnabled() {
		t.Fatal("omitted starter-kit options must preserve the default-on behavior")
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if !strings.Contains(string(encoded), "starter_kit: vue") ||
		!strings.Contains(string(encoded), "starter_kit_options:\n        component_library: false") {
		t.Fatalf("encoded default App did not retain scalar starter kit with nested options:\n%s", encoded)
	}
}
