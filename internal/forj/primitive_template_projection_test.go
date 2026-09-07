package forj

import (
	"bytes"
	"encoding/json"
	"go/format"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/goforj/goforj/project"
)

// primitiveTemplateMarker describes one token whose source shape follows either App or project participation.
type primitiveTemplateMarker struct {
	path  string
	token string
}

// primitiveTemplateContract captures the minimal generated surface that distinguishes one optional primitive.
type primitiveTemplateContract struct {
	key              project.ComponentKey
	appMarkers       []primitiveTemplateMarker
	projectMarkers   []primitiveTemplateMarker
	disabledBridge   primitiveTemplateMarker
	rootEnvironment  string
	namedEnvironment string
}

// TestPrimitiveTemplateProjection covers all-off and both mixed-App directions through one shared projection matrix.
func TestPrimitiveTemplateProjection(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	scenarios := []struct {
		name           string
		defaultEnabled bool
		workerEnabled  bool
	}{
		{name: "all Apps disabled"},
		{name: "default App only", defaultEnabled: true},
		{name: "named App only", workerEnabled: true},
	}

	for _, contract := range primitiveTemplateContracts() {
		contract := contract
		t.Run(string(contract.key), func(t *testing.T) {
			for _, scenario := range scenarios {
				scenario := scenario
				t.Run(scenario.name, func(t *testing.T) {
					config := primitiveProjectionConfig(t, contract.key, scenario.defaultEnabled, scenario.workerEnabled)
					projectEnabled := scenario.defaultEnabled || scenario.workerEnabled
					sharedData := workspace.templateDataForApp(config, project.DefaultApp())
					sharedData.Resources = primitiveProjectionResources()

					for _, marker := range contract.projectMarkers {
						assertProjectedTemplateMarker(t, marker, sharedData, projectEnabled)
					}
					environment := renderSharedTemplate(t, ".env.tmpl", sharedData)
					assertTemplateMarker(t, ".env.tmpl", environment, contract.rootEnvironment, projectEnabled)
					assertTemplateMarker(t, ".env.tmpl", environment, contract.namedEnvironment, scenario.workerEnabled)

					renderer := projectRendererForTest(t, config)
					apps := []struct {
						app     project.App
						enabled bool
					}{
						{app: project.DefaultApp(), enabled: scenario.defaultEnabled},
						{app: project.DefaultNamedApp("worker"), enabled: scenario.workerEnabled},
					}
					for _, target := range apps {
						target := target
						t.Run(target.app.Name, func(t *testing.T) {
							data := workspace.templateDataForApp(config, target.app)
							data.Resources = primitiveProjectionResources()
							for _, marker := range contract.appMarkers {
								assertProjectedTemplateMarker(t, marker, data, target.enabled)
							}
							if contract.disabledBridge.path != "" {
								bridgeEnabled := projectEnabled && !target.enabled
								assertProjectedTemplateMarker(t, contract.disabledBridge, data, bridgeEnabled)
							}
							assertPrimitiveAppMappings(t, renderer, contract.key, target.app, target.enabled)
						})
					}
				})
			}
		})
	}

	t.Run("Events owner compatibility", testEventCommandProjection)
	t.Run("Events shared examples", testEventSharedExamples)
	t.Run("dashboard conditionals", testPrimitiveDashboardProjection)
}

// TestSharedMetricsFollowProjectAndAppProjection verifies named-App-only capabilities still compile while runtime flags remain App-local.
func TestSharedMetricsFollowProjectAndAppProjection(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	config := &project.Config{
		GoModuleName: "example.com/metrics-projection",
		Render: project.RenderConfig{Components: project.Components{
			CLI: true, Metrics: true,
		}},
		Apps: map[string]project.AppConfig{
			"worker": {Components: project.Components{
				CLI: true, Auth: true, DatabaseSQLite: true, Scheduler: true,
			}},
		},
	}
	data := workspace.templateDataForApp(config, project.DefaultApp())
	manager := renderSharedTemplate(t, "internal/metrics/manager.go.tmpl", data)
	managerTests := renderSharedTemplate(t, "internal/metrics/manager_test.go.tmpl", data)
	assertFormattedGoTemplate(t, "internal/metrics/manager.go.tmpl", manager)
	assertFormattedGoTemplate(t, "internal/metrics/manager_test.go.tmpl", managerTests)

	for _, token := range []string{
		`"database/sql"`,
		`"github.com/goforj/scheduler/v2"`,
		"authFlows",
		"MustDurationHistogramVec",
		"func (m *Manager) RecordSchedulerJob",
		"type DatabaseStatementMetricEvent struct",
		`runtime.CurrentApp().Components.HasDatabase() && env.GetBool("METRICS_DATABASE_ENABLED", "true")`,
		`runtime.CurrentApp().Components.Auth && env.GetBool("METRICS_AUTH_ENABLED", "true")`,
		`runtime.CurrentApp().Components.Scheduler && env.GetBool("METRICS_SCHEDULER_ENABLED", "true")`,
	} {
		assertTemplateMarker(t, "internal/metrics/manager.go.tmpl", manager, token, true)
	}
	for _, token := range []string{
		"func TestRecordAuthFlowTracksOutcomeAndLatency",
		"func TestRecordSchedulerJobTracksOutcomesAndDuration",
		"func TestRecordDatabaseStatementTracksLabeledSeries",
		"if got, want := cfg.Database, false; got != want",
		"if got, want := cfg.Database, true; got != want",
	} {
		assertTemplateMarker(t, "internal/metrics/manager_test.go.tmpl", managerTests, token, true)
	}
	assertTemplateMarker(t, "internal/metrics/manager.go.tmpl", manager, "monitoringSidebarRequests", false)
	assertTemplateMarker(t, "internal/metrics/manager.go.tmpl", manager, "gmetrics.DurationBounds(", false)
}

// TestStorageTemplatesExposeBackendDeleteCapabilities keeps generated docs, server payloads, and Lighthouse actions aligned.
func TestStorageTemplatesExposeBackendDeleteCapabilities(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	config := &project.Config{
		GoModuleName: "example.com/storage-capabilities",
		Render: project.RenderConfig{Components: project.Components{
			CLI: true, WebAPI: true, Storage: true,
		}},
	}
	data := workspace.templateDataForApp(config, project.DefaultApp())
	lighthouse := renderSharedTemplate(t, "internal/http/lighthouse.go.tmpl", data)
	serverTests := renderSharedTemplate(t, "internal/http/server_test.go.tmpl", data)
	assertFormattedGoTemplate(t, "internal/http/lighthouse.go.tmpl", lighthouse)
	assertFormattedGoTemplate(t, "internal/http/server_test.go.tmpl", serverTests)
	for _, token := range []string{
		`Deletable    bool   ` + "`json:\"deletable\"`",
		"func storageEntryDeletable(driver string, entry storage.Entry) bool",
		`!entry.IsDir || !strings.EqualFold(strings.TrimSpace(driver), "dropbox")`,
	} {
		assertTemplateMarker(t, "internal/http/lighthouse.go.tmpl", lighthouse, token, true)
	}
	for _, token := range []string{
		"func TestStorageEntryDeletableFollowsBackendContract",
		`{name: "Dropbox directory", driver: "dropbox", entry: storage.Entry{IsDir: true}, want: false}`,
		`{name: "Dropbox file", driver: "dropbox", entry: storage.Entry{IsDir: false}, want: true}`,
		`{name: "S3 directory", driver: "s3", entry: storage.Entry{IsDir: true}, want: true}`,
	} {
		assertTemplateMarker(t, "internal/http/server_test.go.tmpl", serverTests, token, true)
	}

	for _, check := range []struct {
		path  string
		token string
	}{
		{path: "internal/lighthouse/ui/src/views/StorageView.vue", token: `v-if="entry.deletable !== false"`},
		{path: "internal/storages/README.md.tmpl", token: "Dropbox supports file deletion only"},
	} {
		content, err := templatesFS.ReadFile(check.path)
		if err != nil {
			t.Fatalf("read template %s: %v", check.path, err)
		}
		assertTemplateMarker(t, check.path, string(content), check.token, true)
	}
}

// TestReactStarterUsesTypeScriptSevenToolchain keeps the starter compiler and configuration on the reviewed native generation.
func TestReactStarterUsesTypeScriptSevenToolchain(t *testing.T) {
	const packagePath = "starter-kits/react/frontend/package.json"
	packageJSON, err := templatesFS.ReadFile(packagePath)
	if err != nil {
		t.Fatalf("read template %s: %v", packagePath, err)
	}
	var manifest struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil {
		t.Fatalf("decode template %s: %v", packagePath, err)
	}
	if got, want := manifest.DevDependencies["typescript"], "~7.0.2"; got != want {
		t.Fatalf("TypeScript dependency = %q, want %q", got, want)
	}

	const lockPath = "starter-kits/react/frontend/package-lock.json"
	packageLock, err := templatesFS.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read template %s: %v", lockPath, err)
	}
	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(packageLock, &lock); err != nil {
		t.Fatalf("decode template %s: %v", lockPath, err)
	}
	typescript := lock.Packages["node_modules/typescript"]
	if got, want := typescript.Version, "7.0.2"; got != want {
		t.Fatalf("locked TypeScript version = %q, want %q", got, want)
	}
	if got, want := typescript.Integrity, "sha512-8FYau96o3NKOhbjKi/qNvG/W5jhzxkbdm5sj9AbZ/5T5sWqn3hJgLfGx27sRKZWTvyzCP8dLRBTf5tBTSRVUNA=="; got != want {
		t.Fatalf("locked TypeScript integrity = %q, want %q", got, want)
	}

	const configPath = "starter-kits/react/frontend/tsconfig.json"
	config, err := templatesFS.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read template %s: %v", configPath, err)
	}
	for _, marker := range []string{`"strict": true`, `"@/*": ["./src/*"]`} {
		assertTemplateMarker(t, configPath, string(config), marker, true)
	}
	assertTemplateMarker(t, configPath, string(config), `"baseUrl"`, false)
}

// TestTemplHTMXStarterUsesTypeScriptSevenToolchain keeps the starter compiler and configuration on the reviewed native generation.
func TestTemplHTMXStarterUsesTypeScriptSevenToolchain(t *testing.T) {
	const packagePath = "starter-kits/templ-htmx/frontend/package.json"
	packageJSON, err := templatesFS.ReadFile(packagePath)
	if err != nil {
		t.Fatalf("read template %s: %v", packagePath, err)
	}
	var manifest struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil {
		t.Fatalf("decode template %s: %v", packagePath, err)
	}
	if got, want := manifest.DevDependencies["typescript"], "~7.0.2"; got != want {
		t.Fatalf("TypeScript dependency = %q, want %q", got, want)
	}

	const lockPath = "starter-kits/templ-htmx/frontend/package-lock.json"
	packageLock, err := templatesFS.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read template %s: %v", lockPath, err)
	}
	var lock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(packageLock, &lock); err != nil {
		t.Fatalf("decode template %s: %v", lockPath, err)
	}
	typescript := lock.Packages["node_modules/typescript"]
	if got, want := typescript.Version, "7.0.2"; got != want {
		t.Fatalf("locked TypeScript version = %q, want %q", got, want)
	}
	if got, want := typescript.Integrity, "sha512-8FYau96o3NKOhbjKi/qNvG/W5jhzxkbdm5sj9AbZ/5T5sWqn3hJgLfGx27sRKZWTvyzCP8dLRBTf5tBTSRVUNA=="; got != want {
		t.Fatalf("locked TypeScript integrity = %q, want %q", got, want)
	}

	const configPath = "starter-kits/templ-htmx/frontend/tsconfig.json"
	config, err := templatesFS.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read template %s: %v", configPath, err)
	}
	for _, marker := range []string{`"types": ["vite/client"]`, `"strict": true`, `"module": "ESNext"`, `"moduleResolution": "Bundler"`} {
		assertTemplateMarker(t, configPath, string(config), marker, true)
	}
	assertTemplateMarker(t, configPath, string(config), `"baseUrl"`, false)
}

// TestDemoFrontendUsesTypeScriptSevenCompatibilityToolchain keeps native checking and Vue compiler API consumers on their supported runtimes.
func TestDemoFrontendUsesTypeScriptSevenCompatibilityToolchain(t *testing.T) {
	const packagePath = "demo/frontend/package.json"
	packageJSON, err := templatesFS.ReadFile(packagePath)
	if err != nil {
		t.Fatalf("read template %s: %v", packagePath, err)
	}
	var manifest struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil {
		t.Fatalf("decode template %s: %v", packagePath, err)
	}
	for dependency, want := range map[string]string{
		"@typescript/native": "npm:typescript@~7.0.2",
		"typescript":         "npm:@typescript/typescript6@^6.0.2",
	} {
		if got := manifest.DevDependencies[dependency]; got != want {
			t.Errorf("%s dependency = %q, want %q", dependency, got, want)
		}
	}

	const lockPath = "demo/frontend/package-lock.json"
	packageLock, err := templatesFS.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read template %s: %v", lockPath, err)
	}
	var lock struct {
		Packages map[string]struct {
			Name      string `json:"name"`
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(packageLock, &lock); err != nil {
		t.Fatalf("decode template %s: %v", lockPath, err)
	}
	for path, want := range map[string]struct {
		name      string
		version   string
		integrity string
	}{
		"node_modules/@typescript/native": {name: "typescript", version: "7.0.2", integrity: "sha512-8FYau96o3NKOhbjKi/qNvG/W5jhzxkbdm5sj9AbZ/5T5sWqn3hJgLfGx27sRKZWTvyzCP8dLRBTf5tBTSRVUNA=="},
		"node_modules/typescript":         {name: "@typescript/typescript6", version: "6.0.2", integrity: "sha512-mbCddXd+jm7hfx7w2YU64/Av4/NqqeG3GoRZgxPcgoTxYjhrcfJRw9ULch71SS4G+Q3bOXFhRvPqjguN0Hyp5w=="},
	} {
		dependency := lock.Packages[path]
		if dependency.Name != want.name || dependency.Version != want.version || dependency.Integrity != want.integrity {
			t.Errorf("locked %s = %s@%s with integrity %q, want %s@%s with integrity %q", path, dependency.Name, dependency.Version, dependency.Integrity, want.name, want.version, want.integrity)
		}
	}

	for _, config := range []struct {
		path    string
		markers []string
	}{
		{path: "demo/frontend/tsconfig.json", markers: []string{`"@/*": [`, `"./src/*"`}},
		{path: "demo/frontend/tsconfig.app.json", markers: []string{`"strict": false`, `"@/*": ["./src/*"]`}},
	} {
		content, err := templatesFS.ReadFile(config.path)
		if err != nil {
			t.Fatalf("read template %s: %v", config.path, err)
		}
		for _, marker := range config.markers {
			assertTemplateMarker(t, config.path, string(content), marker, true)
		}
		assertTemplateMarker(t, config.path, string(content), `"baseUrl"`, false)
	}
}

// TestLighthouseUIUsesViteEightToolchain keeps the embedded frontend on the reviewed Vite generation and config API.
func TestLighthouseUIUsesViteEightToolchain(t *testing.T) {
	const packagePath = "internal/lighthouse/ui/package.json"
	packageJSON, err := templatesFS.ReadFile(packagePath)
	if err != nil {
		t.Fatalf("read template %s: %v", packagePath, err)
	}
	var manifest struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil {
		t.Fatalf("decode template %s: %v", packagePath, err)
	}
	if got, want := manifest.DevDependencies["vite"], "^8.2.2"; got != want {
		t.Fatalf("Vite dependency = %q, want %q", got, want)
	}

	const lockPath = "internal/lighthouse/ui/package-lock.json"
	packageLock, err := templatesFS.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read template %s: %v", lockPath, err)
	}
	var lock struct {
		Packages map[string]struct {
			Version string            `json:"version"`
			Engines map[string]string `json:"engines"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(packageLock, &lock); err != nil {
		t.Fatalf("decode template %s: %v", lockPath, err)
	}
	vite := lock.Packages["node_modules/vite"]
	if got, want := vite.Version, "8.2.2"; got != want {
		t.Fatalf("locked Vite version = %q, want %q", got, want)
	}
	if got, want := vite.Engines["node"], "^20.19.0 || >=22.12.0"; got != want {
		t.Fatalf("locked Vite Node requirement = %q, want %q", got, want)
	}

	const configPath = "internal/lighthouse/ui/vite.config.ts"
	config, err := templatesFS.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read template %s: %v", configPath, err)
	}
	assertTemplateMarker(t, configPath, string(config), `resolve(import.meta.dirname, "src")`, true)
	for _, obsolete := range []string{"__dirname", "rollupOptions", "esbuild"} {
		assertTemplateMarker(t, configPath, string(config), obsolete, false)
	}

	const indexPath = "internal/lighthouse/ui/dist/index.html"
	indexHTML, err := templatesFS.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read template %s: %v", indexPath, err)
	}
	const assetsPath = "internal/lighthouse/ui/dist/assets"
	assets, err := templatesFS.ReadDir(assetsPath)
	if err != nil {
		t.Fatalf("read template %s: %v", assetsPath, err)
	}
	bundleCounts := map[string]int{".css": 0, ".js": 0}
	for _, asset := range assets {
		extension := filepath.Ext(asset.Name())
		if _, tracked := bundleCounts[extension]; !tracked {
			continue
		}
		bundleCounts[extension]++
		if !strings.Contains(string(indexHTML), "/lighthouse/assets/"+asset.Name()) {
			t.Errorf("%s does not reference built asset %s", indexPath, asset.Name())
		}
	}
	for extension, count := range bundleCounts {
		if count != 1 {
			t.Errorf("built Lighthouse %s bundle count = %d, want 1", extension, count)
		}
	}
}

// TestLighthouseUIUsesTypeScriptSevenCompatibilityToolchain keeps native checking and Vue compiler API consumers on their supported runtimes.
func TestLighthouseUIUsesTypeScriptSevenCompatibilityToolchain(t *testing.T) {
	const packagePath = "internal/lighthouse/ui/package.json"
	packageJSON, err := templatesFS.ReadFile(packagePath)
	if err != nil {
		t.Fatalf("read template %s: %v", packagePath, err)
	}
	var manifest struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(packageJSON, &manifest); err != nil {
		t.Fatalf("decode template %s: %v", packagePath, err)
	}
	for dependency, want := range map[string]string{
		"@typescript/native": "npm:typescript@~7.0.2",
		"typescript":         "npm:@typescript/typescript6@^6.0.2",
	} {
		if got := manifest.DevDependencies[dependency]; got != want {
			t.Errorf("%s dependency = %q, want %q", dependency, got, want)
		}
	}

	const lockPath = "internal/lighthouse/ui/package-lock.json"
	packageLock, err := templatesFS.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read template %s: %v", lockPath, err)
	}
	var lock struct {
		Packages map[string]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(packageLock, &lock); err != nil {
		t.Fatalf("decode template %s: %v", lockPath, err)
	}
	for path, want := range map[string]struct {
		name    string
		version string
	}{
		"node_modules/@typescript/native": {name: "typescript", version: "7.0.2"},
		"node_modules/typescript":         {name: "@typescript/typescript6", version: "6.0.2"},
	} {
		dependency := lock.Packages[path]
		if dependency.Name != want.name || dependency.Version != want.version {
			t.Errorf("locked %s = %s@%s, want %s@%s", path, dependency.Name, dependency.Version, want.name, want.version)
		}
	}

	const configPath = "internal/lighthouse/ui/tsconfig.json"
	config, err := templatesFS.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read template %s: %v", configPath, err)
	}
	for _, marker := range []string{`"strict": false`, `"@/*": ["./src/*"]`} {
		assertTemplateMarker(t, configPath, string(config), marker, true)
	}
	assertTemplateMarker(t, configPath, string(config), `"baseUrl"`, false)
}

// TestMailAboutBehaviorCoversEveryAppProjection verifies generated behavior coverage includes Mail-enabled and Mail-disabled Apps.
func TestMailAboutBehaviorCoversEveryAppProjection(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	config := &project.Config{
		GoModuleName: "example.com/mail-projection",
		Render: project.RenderConfig{Components: project.Components{
			CLI: true, Mail: true,
		}},
		Apps: map[string]project.AppConfig{
			"worker": {Components: project.Components{CLI: true}},
		},
	}
	data := workspace.templateDataForApp(config, project.DefaultApp())
	tests := renderSharedTemplate(t, "internal/runtime/apps_test.go.tmpl", data)
	assertFormattedGoTemplate(t, "internal/runtime/apps_test.go.tmpl", tests)

	for _, token := range []string{
		"func TestAboutServiceFollowsCurrentAppMailComponent",
		`{name: "app", want: true}`,
		`{name: "worker", want: false}`,
		`slices.Contains(report.Build.Components, "mail")`,
		`slices.ContainsFunc(report.Sections`,
	} {
		assertTemplateMarker(t, "internal/runtime/apps_test.go.tmpl", tests, token, true)
	}
}

// TestCacheMetricsConfigTestsCoverEveryApp verifies generated tests exercise each participating and excluded App independently.
func TestCacheMetricsConfigTestsCoverEveryApp(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	config := &project.Config{
		GoModuleName: "example.com/cache-metrics-projection",
		Render: project.RenderConfig{Components: project.Components{
			CLI: true, Cache: true, Metrics: true,
		}},
		Apps: map[string]project.AppConfig{
			"observer": {Components: project.Components{CLI: true}},
			"worker":   {Components: project.Components{CLI: true, Cache: true}},
		},
	}
	data := workspace.templateDataForApp(config, project.DefaultApp())
	tests := renderSharedTemplate(t, "internal/metrics/cache_metrics_gen_test.go.tmpl", data)
	assertFormattedGoTemplate(t, "internal/metrics/cache_metrics_gen_test.go.tmpl", tests)

	for _, token := range []string{
		`t.Run("app enables Cache metrics"`,
		`t.Run("worker enables Cache metrics"`,
		`t.Run("observer excludes Cache metrics"`,
	} {
		assertTemplateMarker(t, "internal/metrics/cache_metrics_gen_test.go.tmpl", tests, token, true)
	}
	if got, want := strings.Count(tests, `t.Setenv("FORJ_APP"`), 3; got != want {
		t.Fatalf("generated Cache config App selections = %d, want %d", got, want)
	}
}

// primitiveTemplateContracts returns the high-signal markers that are not already exercised by real render sentinels.
func primitiveTemplateContracts() []primitiveTemplateContract {
	return []primitiveTemplateContract{
		{
			key: project.ComponentEvents,
			appMarkers: []primitiveTemplateMarker{
				{path: "wire/app.go.tmpl", token: "func (a *App) Events() *events.Manager"},
				{path: "app/root_cmd.go.tmpl", token: "MakeEventCmd makecmd.EventCmd"},
				{path: "wire/inject_managers.go.tmpl", token: "func provideEventManager("},
				{path: "wire/inject_http.go.tmpl", token: "for _, check := range events.ReadinessChecks()"},
			},
			projectMarkers: []primitiveTemplateMarker{
				{path: "internal/runtime/about.go.tmpl", token: "report.Events = aboutEventReports()"},
				{path: "internal/runtime/discovery.go.tmpl", token: "func DiscoverEventInstances("},
				{path: "internal/metrics/manager.go.tmpl", token: "func (m *Manager) RecordEventPublish"},
				{path: "internal/metrics/manager.go.tmpl", token: `runtime.CurrentApp().Components.Events && env.GetBool("METRICS_EVENTS_ENABLED", "true")`},
				{path: "containers/observability/grafana/seed-dashboards.sh.tmpl", token: "goforj-events-overview"},
				{path: "internal/makecmd/README.md.tmpl", token: "make:event"},
			},
			rootEnvironment:  "\nEVENTS_DRIVER=inproc",
			namedEnvironment: "\nWORKER_EVENTS_DRIVER=inproc",
		},
		{
			key: project.ComponentStorage,
			appMarkers: []primitiveTemplateMarker{
				{path: "wire/app.go.tmpl", token: "func (a *App) Storage() *storages.Manager"},
				{path: "wire/app.go.tmpl", token: "registerStorageShutdown(lifecycleManager, appLogger, storage)"},
				{path: "wire/inject_managers.go.tmpl", token: "func provideStorageManager("},
				{path: "wire/inject_http.go.tmpl", token: "for _, check := range storage.ReadinessChecks()"},
			},
			projectMarkers: []primitiveTemplateMarker{
				{path: "internal/runtime/about.go.tmpl", token: "report.Storages = aboutStorageReports()"},
				{path: "internal/runtime/discovery.go.tmpl", token: "func DiscoverStorageInstances("},
				{path: "internal/metrics/manager.go.tmpl", token: "func (m *Manager) RecordStorageOperation"},
				{path: "containers/observability/grafana/seed-dashboards.sh.tmpl", token: "goforj-storage-overview"},
				{path: "internal/observability/README.md.tmpl", token: "Storage Overview"},
			},
			disabledBridge:   primitiveTemplateMarker{path: "wire/inject_managers.go.tmpl", token: "func provideDisabledStorageManager("},
			rootEnvironment:  "\nSTORAGE_DRIVER=local",
			namedEnvironment: "\nWORKER_STORAGE_DRIVER=local",
		},
		{
			key: project.ComponentJobs,
			appMarkers: []primitiveTemplateMarker{
				{path: "wire/app.go.tmpl", token: "func (a *App) Queues() *queues.Manager"},
				{path: "wire/inject_cmd.go.tmpl", token: "jobsRuntime *jobs.Runtime"},
				{path: "wire/inject_managers.go.tmpl", token: "func provideQueueManager("},
				{path: "wire/inject_http.go.tmpl", token: "for _, check := range queues.ReadinessChecks()"},
			},
			projectMarkers: []primitiveTemplateMarker{
				{path: "internal/cmd/run_cmd.go.tmpl", token: "jobsRuntime *jobs.Runtime"},
				{path: "internal/http/health.go.tmpl", token: "func queueDriver("},
				{path: "internal/runtime/timeouts.go.tmpl", token: `env.GetDuration("QUEUE_SHUTDOWN_TIMEOUT"`},
				{path: "internal/runtime/timeouts.go.tmpl", token: "func (s *Timeouts) QueueShutdownTimeout()"},
				{path: "internal/runtime/apps.go.tmpl", token: "func WorkerMetricsPortForApp("},
				{path: "internal/runtime/apps_test.go.tmpl", token: `t.Setenv("WORKER_METRICS_PORT"`},
				{path: "internal/runtime/about.go.tmpl", token: "report.Queues = aboutQueueReports()"},
				{path: "internal/runtime/about.go.tmpl", token: "type AboutQueue struct"},
				{path: "internal/runtime/discovery.go.tmpl", token: "func DiscoverQueueInstances("},
				{path: "internal/runtime/discovery.go.tmpl", token: "func NormalizeQueueDriver("},
				{path: "internal/metrics/manager.go.tmpl", token: "func (m *Manager) RecordQueueEvent"},
				{path: "containers/observability/grafana/seed-dashboards.sh.tmpl", token: "goforj-queue-overview"},
				{path: "internal/makecmd/README.md.tmpl", token: "make:job"},
			},
			disabledBridge:   primitiveTemplateMarker{path: "wire/inject_cmd.go.tmpl", token: "(*jobs.Runtime)(nil)"},
			rootEnvironment:  "\nQUEUE_DRIVER=workerpool",
			namedEnvironment: "\nWORKER_QUEUE_DRIVER=workerpool",
		},
	}
}

// primitiveProjectionConfig creates two Apps whose primitive participation can vary independently.
func primitiveProjectionConfig(t *testing.T, key project.ComponentKey, defaultEnabled bool, workerEnabled bool) *project.Config {
	t.Helper()
	defaultComponents := primitiveProjectionBaseComponents()
	workerComponents := primitiveProjectionBaseComponents()
	setPrimitiveProjectionComponent(t, &defaultComponents, key, defaultEnabled)
	setPrimitiveProjectionComponent(t, &workerComponents, key, workerEnabled)
	return &project.Config{
		GoModuleName: "example.com/primitive-projection",
		Render: project.RenderConfig{
			Components: defaultComponents,
		},
		Apps: map[string]project.AppConfig{
			"worker": {Components: workerComponents},
		},
	}
}

// primitiveProjectionBaseComponents keeps unrelated template dependencies stable while a primitive varies.
func primitiveProjectionBaseComponents() project.Components {
	return project.Components{
		CLI: true, WebAPI: true, Cache: true, Metrics: true, Observability: true, Grafana: true,
	}
}

// primitiveProjectionResources supplies deterministic environment values without invoking resource reconciliation.
func primitiveProjectionResources() resourceRenderValues {
	return resourceRenderValues{
		CacheDriver:             "memory",
		CacheSupportedDrivers:   "memory",
		EventsDriver:            "inproc",
		EventsSupportedDrivers:  "inproc,redis",
		StorageDriver:           "local",
		StorageSupportedDrivers: "local,memory",
		StoragePublicDriver:     "local",
		QueueDriver:             "workerpool",
		QueueSupportedDrivers:   "workerpool,redis",
	}
}

// setPrimitiveProjectionComponent rejects unsupported keys so a table mistake cannot silently erase coverage.
func setPrimitiveProjectionComponent(t *testing.T, components *project.Components, key project.ComponentKey, enabled bool) {
	t.Helper()
	switch key {
	case project.ComponentEvents, project.ComponentStorage, project.ComponentJobs:
		components.SetEnabled(key, enabled)
	default:
		t.Fatalf("unsupported primitive projection component %q", key)
	}
	if got := components.Enabled(key); got != enabled {
		t.Fatalf("primitive projection component %q enabled = %t, want %t", key, got, enabled)
	}
}

// assertProjectedTemplateMarker renders one template, checks Go syntax when applicable, and verifies its projection token.
func assertProjectedTemplateMarker(t *testing.T, marker primitiveTemplateMarker, data templateRenderConfig, want bool) {
	t.Helper()
	if marker.path == "" {
		return
	}
	source := renderSharedTemplate(t, marker.path, data)
	if strings.HasSuffix(marker.path, ".go.tmpl") {
		assertFormattedGoTemplate(t, marker.path, source)
	}
	assertTemplateMarker(t, marker.path, source, marker.token, want)
}

// assertPrimitiveAppMappings verifies component-specific framework and owner files stay App-local.
func assertPrimitiveAppMappings(t *testing.T, renderer *ProjectRenderer, key project.ComponentKey, app project.App, want bool) {
	t.Helper()
	var frameworkPath string
	frameworkWant := want
	var ownerPath string
	switch key {
	case project.ComponentEvents:
		frameworkPath = filepath.Join(app.AppDir, "event_commands.go")
		frameworkWant = false
		ownerPath = filepath.Join(app.WireDir, "inject_subscribers_app.go")
	case project.ComponentJobs:
		frameworkPath = filepath.Join(app.WireDir, "inject_jobs.go")
		ownerPath = filepath.Join(app.WireDir, "inject_jobs_app.go")
	case project.ComponentStorage:
		return
	default:
		t.Fatalf("unsupported primitive mapping component %q", key)
	}
	if got := templateMappingDestExists(renderer.appFrameworkMappings(app), frameworkPath); got != frameworkWant {
		t.Fatalf("%s framework mapping %s presence = %t, want %t", key, frameworkPath, got, frameworkWant)
	}
	if got := templateMappingDestExists(renderer.appOwnedMappings(app), ownerPath); got != want {
		t.Fatalf("%s owner mapping %s presence = %t, want %t", key, ownerPath, got, want)
	}
}

// templateMappingDestExists reports whether a mapping set contains one normalized destination.
func templateMappingDestExists(mappings []templateMapping, want string) bool {
	want = filepath.Clean(want)
	for _, mapping := range mappings {
		if filepath.Clean(mapping.dest) == want {
			return true
		}
	}
	return false
}

// testEventCommandProjection preserves legacy command owners while keeping future owner templates Events-neutral.
func testEventCommandProjection(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	config := primitiveProjectionConfig(t, project.ComponentEvents, true, false)
	base := workspace.templateDataForApp(config, project.DefaultApp())
	tests := []struct {
		name                 string
		legacyField          bool
		legacyProvider       bool
		wantRootPipeline     bool
		wantPipelineProvider bool
	}{
		{name: "new App", wantRootPipeline: true, wantPipelineProvider: true},
		{name: "legacy field and provider", legacyField: true, legacyProvider: true},
		{name: "legacy field without provider", legacyField: true, wantPipelineProvider: true},
		{name: "legacy provider without field", legacyProvider: true, wantRootPipeline: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := base
			data.LegacyEventPipelineField = test.legacyField
			data.LegacyEventPipelineProvider = test.legacyProvider
			root := renderSharedTemplate(t, "app/root_cmd.go.tmpl", data)
			wiring := renderSharedTemplate(t, "wire/inject_cmd.go.tmpl", data)
			assertFormattedGoTemplate(t, "app/root_cmd.go.tmpl", root)
			assertFormattedGoTemplate(t, "wire/inject_cmd.go.tmpl", wiring)
			assertTemplateMarker(t, "app/root_cmd.go.tmpl", root, "TestEventPipelineCmd", test.wantRootPipeline)
			assertTemplateMarker(t, "wire/inject_cmd.go.tmpl", wiring, "cmd.NewTestEventPipelineCmd,", test.wantPipelineProvider)
			for _, marker := range []string{"MakeEventCmd", "MakeSubscriberCmd"} {
				assertTemplateMarker(t, "app/root_cmd.go.tmpl", root, marker, true)
			}
			for _, marker := range []string{"makecmd.NewEventCmd,", "makecmd.NewSubscriberCmd,"} {
				assertTemplateMarker(t, "wire/inject_cmd.go.tmpl", wiring, marker, true)
			}
			for _, marker := range []string{"GeneratedEventCommands", "NewGeneratedEventCommands"} {
				assertTemplateMarker(t, "app/root_cmd.go.tmpl", root, marker, false)
				assertTemplateMarker(t, "wire/inject_cmd.go.tmpl", wiring, marker, false)
			}
		})
	}

	for _, path := range []string{"app/commands.go.tmpl", "wire/inject_cmd_app.go.tmpl", "wire/inject_services_app.go.tmpl"} {
		content, err := templatesFS.ReadFile(path)
		if err != nil {
			t.Fatalf("read owner template %s: %v", path, err)
		}
		for _, token := range []string{"MakeEventCmd", "MakeSubscriberCmd", "TestEventPipeline", "NewEventCmd", "NewSubscriberCmd"} {
			assertTemplateMarker(t, path, string(content), token, false)
		}
	}
}

// testEventSharedExamples keeps demo declarations out of the generic Events transport and examples.
func testEventSharedExamples(t *testing.T) {
	for _, demoEnabled := range []bool{false, true} {
		data := templateRenderConfig{
			Components:        project.Components{Events: true},
			ProjectComponents: project.Components{Events: true, DemoApp: demoEnabled},
		}
		topics := renderSharedTemplate(t, "internal/events/topics.go.tmpl", data)
		assertFormattedGoTemplate(t, "internal/events/topics.go.tmpl", topics)
		assertTemplateMarker(t, "internal/events/topics.go.tmpl", topics, "type MonitorDown struct", demoEnabled)
	}

	data := templateRenderConfig{
		Config:            &project.Config{GoModuleName: "example.com/events-only"},
		Components:        project.Components{Events: true},
		ProjectComponents: project.Components{Events: true},
	}
	integration := renderSharedTemplate(t, "internal/events/bus_integration_test.go.tmpl", data)
	assertFormattedGoTemplate(t, "internal/events/bus_integration_test.go.tmpl", integration)
	assertTemplateMarker(t, "internal/events/bus_integration_test.go.tmpl", integration, "type inprocIntegrationEvent struct", true)
	assertTemplateMarker(t, "internal/events/bus_integration_test.go.tmpl", integration, "MonitorDown", false)
}

// testPrimitiveDashboardProjection verifies optional dashboard fragments remain valid JSON in both component states.
func testPrimitiveDashboardProjection(t *testing.T) {
	workspace := currentProjectRenderWorkspace(t)
	for _, enabled := range []bool{false, true} {
		components := primitiveProjectionBaseComponents()
		components.Cache = enabled
		config := &project.Config{Render: project.RenderConfig{Components: components}}
		data := workspace.templateDataForApp(config, project.DefaultApp())
		body := renderSharedTemplate(t, "containers/observability/grafana/dashboards/platform-overview.json.tmpl", data)
		var decoded any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("Cache enabled=%t Platform Overview is invalid JSON: %v\n%s", enabled, err, body)
		}
		assertTemplateMarker(t, "platform-overview.json.tmpl", body, "cache_operations_total", enabled)
		assertTemplateMarker(t, "platform-overview.json.tmpl", body, "cache read p95", enabled)

		seed := renderSharedTemplate(t, "containers/observability/grafana/seed-dashboards.sh.tmpl", data)
		assertTemplateMarker(t, "seed-dashboards.sh.tmpl", seed, "goforj-cache-overview", enabled)

		readme := renderSharedTemplate(t, "internal/observability/README.md.tmpl", data)
		assertTemplateMarker(t, "internal/observability/README.md.tmpl", readme, "Cache Overview", enabled)
		assertTemplateMarker(t, "internal/observability/README.md.tmpl", readme, "named cache", enabled)
		assertTemplateMarker(t, "internal/observability/README.md.tmpl", readme, "`cache`: which named cache handled the work", enabled)
	}

	for _, contract := range primitiveTemplateContracts() {
		for _, enabled := range []bool{false, true} {
			components := primitiveProjectionBaseComponents()
			setPrimitiveProjectionComponent(t, &components, contract.key, enabled)
			config := &project.Config{Render: project.RenderConfig{Components: components}}
			data := workspace.templateDataForApp(config, project.DefaultApp())
			body := renderSharedTemplate(t, "containers/observability/grafana/dashboards/platform-overview.json.tmpl", data)
			var decoded any
			if err := json.Unmarshal([]byte(body), &decoded); err != nil {
				t.Fatalf("%s enabled=%t Platform Overview is invalid JSON: %v\n%s", contract.key, enabled, err, body)
			}
		}
	}
}

// renderSharedTemplate executes one embedded template against explicit projection data without writing a rendered project.
func renderSharedTemplate(t *testing.T, path string, data templateRenderConfig) string {
	t.Helper()
	content, err := templatesFS.ReadFile(path)
	if err != nil {
		t.Fatalf("read template %s: %v", path, err)
	}
	tmpl, err := template.New(path).Parse(string(content))
	if err != nil {
		t.Fatalf("parse template %s: %v", path, err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		t.Fatalf("execute template %s: %v", path, err)
	}
	return rendered.String()
}

// assertFormattedGoTemplate verifies a rendered Go template is syntactically valid and gofmt-compatible.
func assertFormattedGoTemplate(t *testing.T, path string, source string) {
	t.Helper()
	if _, err := format.Source([]byte(source)); err != nil {
		t.Fatalf("format rendered template %s: %v\n%s", path, err, source)
	}
}

// assertTemplateMarker verifies one rendered marker is present exactly when expected.
func assertTemplateMarker(t *testing.T, path string, source string, marker string, want bool) {
	t.Helper()
	if got := strings.Contains(source, marker); got != want {
		t.Fatalf("template %s marker %q presence = %t, want %t\n%s", path, marker, got, want, source)
	}
}
