package forj

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/goforj/goforj/internal/logger"
	"github.com/goforj/goforj/internal/projectlayout"
	"github.com/goforj/goforj/project"
)

// starterKitFrontendDir keeps starter-kit assertions on the canonical default-App layout.
func starterKitFrontendDir() string {
	return projectlayout.FrontendDir(".", project.DefaultApp())
}

func TestFrontendEnvironmentHelpersDocumentGeneratedContract(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve current file path")
	}
	templatesRoot := filepath.Join(filepath.Dir(currentFile), "..", "..", "templates")
	files := []string{
		filepath.Join(templatesRoot, "starter-kits", "react", "frontend", "goforj.env.ts"),
		filepath.Join(templatesRoot, "starter-kits", "vue", "frontend", "goforj.env.ts"),
		filepath.Join(templatesRoot, "demo", "frontend", "goforj.env.ts"),
	}
	comments := []string{
		"// defaultApp ",
		"// resolveGoForjFrontendEnv ",
		"// resolveApp ",
		"// collectFrontendDefines ",
		"// resolveBackendTarget ",
		"// frontendEnvValue ",
		"// targetHTTPPort ",
		"// discoverApps ",
		"// defineMissing ",
		"// envPrefix ",
	}

	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read frontend environment helper %s: %v", path, err)
		}
		for _, comment := range comments {
			if !strings.Contains(string(content), comment) {
				t.Errorf("frontend environment helper %s is missing %q", path, comment)
			}
		}
	}
}

// TestScaffoldStarterKitPreservesExistingFrontendFiles verifies re-rendering overlays starter assets without clearing the frontend workspace.
func TestScaffoldStarterKitPreservesExistingFrontendFiles(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	frontendDir := starterKitFrontendDir()
	if err := os.MkdirAll(filepath.Join(frontendDir, "node_modules", "local-package"), 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	preservedFiles := map[string]string{
		filepath.Join(frontendDir, "custom.txt"):                          "user file",
		filepath.Join(frontendDir, "node_modules", "local-package", "id"): "installed dependency",
	}
	for path, content := range preservedFiles {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write preserved frontend file %s: %v", path, err)
		}
	}

	renderer := NewProjectRenderer(logger.NewAppLogger())
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitVue, true); err != nil {
		t.Fatalf("scaffold vue starter kit: %v", err)
	}

	for path, want := range preservedFiles {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read preserved frontend file %s: %v", path, err)
		}
		if string(content) != want {
			t.Fatalf("preserved frontend file %s = %q, want %q", path, content, want)
		}
	}
	for _, path := range []string{
		filepath.Join(frontendDir, "package.json"),
		filepath.Join(frontendDir, "components.json"),
		filepath.Join(frontendDir, "src", "App.vue"),
		filepath.Join(frontendDir, "dist", "index.html"),
		filepath.Join(frontendDir, "dist", "goforj-logo.png"),
		filepath.Join(frontendDir, "public", "goforj-tile-dark.svg"),
		filepath.Join(frontendDir, "public", "goforj-tile-light.svg"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
	packageJSON, err := os.ReadFile(filepath.Join(frontendDir, "package.json"))
	if err != nil {
		t.Fatalf("read Vue starter package: %v", err)
	}
	for _, expected := range []string{"@internationalized/date", "@lucide/vue", `"vite":`, `"zod": "^4.5.4"`} {
		if !strings.Contains(string(packageJSON), expected) {
			t.Errorf("Vue starter package omitted %q:\n%s", expected, packageJSON)
		}
	}
	for _, obsolete := range []string{"lucide-vue-next", `"shadcn-vue"`, `"@vee-validate/zod"`} {
		if strings.Contains(string(packageJSON), obsolete) {
			t.Errorf("Vue starter package retained obsolete install dependency %q:\n%s", obsolete, packageJSON)
		}
	}
	if _, err := os.Stat(filepath.Join(frontendDir, "src", "lib", "zod-rule.ts")); err != nil {
		t.Fatalf("expected Zod rule adapter to exist: %v", err)
	}
}

// TestScaffoldStarterKitsWithoutComponentLibrary verifies every official kit keeps its shell while omitting showcase navigation and pages.
func TestScaffoldStarterKitsWithoutComponentLibrary(t *testing.T) {
	tests := []struct {
		name       string
		starterKit project.StarterKit
		check      func(*testing.T)
	}{
		{
			name:       "vue",
			starterKit: project.StarterKitVue,
			check: func(t *testing.T) {
				assertFileOmits(t, filepath.Join(starterKitFrontendDir(), "src", "router.ts"), "/components")
				assertFileOmits(t, filepath.Join(starterKitFrontendDir(), "src", "lib", "navigation.ts"), "title: 'Components'")
				assertStarterKitFileContains(t, filepath.Join(starterKitFrontendDir(), "src", "views", "DashboardView.vue"), "Empty dashboard canvas")
				if _, err := os.Stat(filepath.Join(starterKitFrontendDir(), "src", "views", "components")); !os.IsNotExist(err) {
					t.Fatalf("Vue showcase views should be omitted, stat error = %v", err)
				}
			},
		},
		{
			name:       "react",
			starterKit: project.StarterKitReact,
			check: func(t *testing.T) {
				path := filepath.Join(starterKitFrontendDir(), "src", "App.tsx")
				assertFileOmits(t, path, `path="/components`)
				assertFileOmits(t, path, "function ComponentsOverviewView")
				assertStarterKitFileContains(t, path, "Empty dashboard canvas")
			},
		},
		{
			name:       "templ htmx",
			starterKit: project.StarterKitTemplHTMX,
			check: func(t *testing.T) {
				assertFileOmits(t, filepath.Join("internal", "starterui", "controller.go"), `"/components`)
				assertStarterKitFileContains(t, filepath.Join("internal", "starterui", "dashboard.templ"), "Empty dashboard canvas")
				if _, err := os.Stat(filepath.Join("internal", "starterui", "components_views.templ")); !os.IsNotExist(err) {
					t.Fatalf("templ showcase views should be omitted, stat error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original, err := os.Getwd()
			if err != nil {
				t.Fatalf("get working directory: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(original) })
			if err := os.Chdir(t.TempDir()); err != nil {
				t.Fatalf("change working directory: %v", err)
			}
			renderer := &ProjectRenderer{
				workspace: currentProjectRenderWorkspace(t),
				config: &project.Config{
					GoModuleName: "example.com/testapp",
					Render: project.RenderConfig{
						Components:        project.Components{WebUI: true, WebAPI: true},
						StarterKit:        test.starterKit,
						StarterKitOptions: project.NewStarterKitOptions(false),
					},
				},
				stats: &renderStats{},
			}
			initializeDefaultResourceStateForTest(t, renderer)
			if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), test.starterKit, true); err != nil {
				t.Fatalf("scaffold starter kit: %v", err)
			}
			test.check(t)
		})
	}
}

// TestDisablingComponentLibraryPreservesEditedShowcaseFiles verifies rerenders remove only unchanged framework copies.
func TestDisablingComponentLibraryPreservesEditedShowcaseFiles(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}

	config := &project.Config{
		Render: project.RenderConfig{
			Components: project.Components{WebUI: true},
			StarterKit: project.StarterKitVue,
		},
	}
	renderer := &ProjectRenderer{workspace: currentProjectRenderWorkspace(t), config: config, stats: &renderStats{}}
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitVue, true); err != nil {
		t.Fatalf("scaffold enabled starter kit: %v", err)
	}
	edited := filepath.Join(starterKitFrontendDir(), "src", "views", "components", "ComponentsOverviewView.vue")
	if err := os.WriteFile(edited, []byte("owner customization\n"), 0o644); err != nil {
		t.Fatalf("edit showcase file: %v", err)
	}
	config.Render.StarterKitOptions = project.NewStarterKitOptions(false)
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitVue, true); err != nil {
		t.Fatalf("scaffold disabled starter kit: %v", err)
	}
	assertStarterKitFileContains(t, edited, "owner customization")
	unchanged := filepath.Join(starterKitFrontendDir(), "src", "views", "components", "ComponentsFormsView.vue")
	if _, err := os.Stat(unchanged); !os.IsNotExist(err) {
		t.Fatalf("unchanged showcase file should be removed, stat error = %v", err)
	}
}

// assertStarterKitFileContains verifies a generated file retains a required starter-kit surface.
func assertStarterKitFileContains(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(content), expected) {
		t.Fatalf("%s does not contain %q", path, expected)
	}
}

// assertFileOmits verifies a generated file does not expose a disabled starter-kit surface.
func assertFileOmits(t *testing.T, path, unexpected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(content), unexpected) {
		t.Fatalf("%s unexpectedly contains %q", path, unexpected)
	}
}

// TestFilterStarterKitFeature validates variant selection and each malformed-marker failure mode.
func TestFilterStarterKitFeature(t *testing.T) {
	content := []byte("always\n// goforj:component-library:on:start\non\n// goforj:component-library:end\n// goforj:component-library:off:start\noff\n// goforj:component-library:end\n")
	enabled, err := filterStarterKitFeature(content, true)
	if err != nil {
		t.Fatalf("filter enabled feature: %v", err)
	}
	if string(enabled) != "always\non\n" {
		t.Fatalf("enabled feature = %q, want always and on blocks", enabled)
	}
	disabled, err := filterStarterKitFeature(content, false)
	if err != nil {
		t.Fatalf("filter disabled feature: %v", err)
	}
	if string(disabled) != "always\noff\n" {
		t.Fatalf("disabled feature = %q, want always and off blocks", disabled)
	}

	invalid := map[string]string{
		"nested":   "// goforj:component-library:on:start\n// goforj:component-library:off:start\n",
		"orphaned": "// goforj:component-library:end\n",
		"unclosed": "// goforj:component-library:on:start\n",
	}
	for name, source := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := filterStarterKitFeature([]byte(source), true); err == nil {
				t.Fatal("filterStarterKitFeature() error = nil, want malformed marker error")
			}
		})
	}
}

func TestScaffoldReactStarterKit(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	renderer := &ProjectRenderer{
		workspace: currentProjectRenderWorkspace(t),
		config: &project.Config{
			Render: project.RenderConfig{Components: project.Components{WebUI: true}, StarterKit: project.StarterKitReact},
		},
		stats: &renderStats{},
	}
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitReact, true); err != nil {
		t.Fatalf("scaffold react starter kit: %v", err)
	}

	for _, path := range []string{
		filepath.Join(starterKitFrontendDir(), "package.json"),
		filepath.Join(starterKitFrontendDir(), "components.json"),
		filepath.Join(starterKitFrontendDir(), "src", "App.tsx"),
		filepath.Join(starterKitFrontendDir(), "src", "lib", "auth.ts"),
		filepath.Join(starterKitFrontendDir(), "dist", "index.html"),
		filepath.Join(starterKitFrontendDir(), "dist", "goforj-logo.png"),
		filepath.Join(starterKitFrontendDir(), "dist", "assets"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
	index, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "dist", "index.html"))
	if err != nil {
		t.Fatalf("read react dist index: %v", err)
	}
	if !strings.Contains(string(index), "/assets/") || strings.Contains(string(index), "Read the docs") {
		t.Fatalf("react starter copied fallback index instead of built app index:\n%s", string(index))
	}
	appSource, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "src", "App.tsx"))
	if err != nil {
		t.Fatalf("read react app source: %v", err)
	}
	for _, expected := range []string{
		"/login",
		"/register",
		"/forgot-password",
		"/reset-password",
		"/verify-email",
		"/settings/profile",
		"/settings/password",
		"/settings/appearance",
		"/components/overview",
		"/components/forms",
		"/components/navigation",
		"/components/overlays",
		"/components/data",
		"await login(identifier, password, remember)",
		"useLayoutEffect(() =>",
		"window.scrollTo(0, 0)",
		"team-switcher-mark",
		"team-switcher-label",
		`side={isMobile ? "bottom" : "right"}`,
		"className=\"w-56\"",
		"NavigationToolbarDemo",
		"DropdownMenuShortcut",
	} {
		if !strings.Contains(string(appSource), expected) {
			t.Fatalf("react app source missing %q", expected)
		}
	}
	styleSource, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "src", "style.css"))
	if err != nil {
		t.Fatalf("read react style source: %v", err)
	}
	for _, expected := range []string{`position: sticky`, `[data-slot="native-select-wrapper"]`, `toolbar-status`} {
		if !strings.Contains(string(styleSource), expected) {
			t.Fatalf("react style source missing %q", expected)
		}
	}
	viteConfig, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "vite.config.ts"))
	if err != nil {
		t.Fatalf("read react vite config: %v", err)
	}
	frontendEnv, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "goforj.env.ts"))
	if err != nil {
		t.Fatalf("read react frontend env helper: %v", err)
	}
	reactDevConfig := string(viteConfig) + "\n" + string(frontendEnv)
	for _, expected := range []string{`loadEnv(mode, projectRoot, "")`, `target: frontendEnv.backendTarget`, `http://localhost:${targetHTTPPort(projectRoot, appName)}`} {
		if !strings.Contains(reactDevConfig, expected) {
			t.Fatalf("react dev config missing %q", expected)
		}
	}
	if strings.Contains(reactDevConfig, "localhost:8080") {
		t.Fatalf("react dev config should not default the backend proxy to localhost:8080")
	}
	if _, err := os.Stat(filepath.Join(starterKitFrontendDir(), "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("expected node_modules to be excluded, stat err = %v", err)
	}
}

// TestScaffoldTemplHTMXStarterKit verifies the templ starter's complete frontend and server-rendered surface.
func TestScaffoldTemplHTMXStarterKit(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	renderer := &ProjectRenderer{
		workspace: currentProjectRenderWorkspace(t),
		config: &project.Config{
			GoModuleName: "example.com/testapp",
			Render: project.RenderConfig{
				Components: project.Components{WebUI: true, WebAPI: true, Auth: true, DatabaseSQLite: true},
				StarterKit: project.StarterKitTemplHTMX,
			},
		},
		stats: &renderStats{},
	}
	initializeDefaultResourceStateForTest(t, renderer)
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitTemplHTMX, true); err != nil {
		t.Fatalf("scaffold templ htmx starter kit: %v", err)
	}

	for _, path := range []string{
		filepath.Join(starterKitFrontendDir(), "package.json"),
		filepath.Join(starterKitFrontendDir(), "dist", "app.js"),
		filepath.Join(starterKitFrontendDir(), "dist", "goforj-logo.png"),
		filepath.Join("internal", "starterui", "controller.go"),
		filepath.Join("internal", "starterui", "controller_test.go"),
		filepath.Join("internal", "starterui", "viewmodels.go"),
		filepath.Join("internal", "starterui", "auth_views.templ"),
		filepath.Join("internal", "starterui", "components_data.templ"),
		filepath.Join("internal", "starterui", "components_forms.templ"),
		filepath.Join("internal", "starterui", "components_navigation.templ"),
		filepath.Join("internal", "starterui", "components_overlays.templ"),
		filepath.Join("internal", "starterui", "components_views.templ"),
		filepath.Join("internal", "starterui", "dashboard.templ"),
		filepath.Join("internal", "starterui", "icons.templ"),
		filepath.Join("internal", "starterui", "layout.templ"),
		filepath.Join("internal", "starterui", "settings_views.templ"),
		filepath.Join("internal", "starterui", "ui.templ"),
		filepath.Join("internal", "starterui", "views.templ"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(starterKitFrontendDir(), "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("expected node_modules to be excluded, stat err = %v", err)
	}

	viteConfig, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "vite.config.ts"))
	if err != nil {
		t.Fatalf("read templ vite config: %v", err)
	}
	for _, expected := range []string{`const isWatchBuild`, `watch: isWatchBuild ?`, `exclude: ["dist/**", "node_modules/**"]`, `emptyOutDir: false`} {
		if !strings.Contains(string(viteConfig), expected) {
			t.Fatalf("templ vite config missing %q", expected)
		}
	}
	packageJSON, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "package.json"))
	if err != nil {
		t.Fatalf("read templ package json: %v", err)
	}
	frontendSource, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "src", "app.ts"))
	if err != nil {
		t.Fatalf("read templ frontend source: %v", err)
	}
	styleSource, err := os.ReadFile(filepath.Join(starterKitFrontendDir(), "src", "style.css"))
	if err != nil {
		t.Fatalf("read templ style source: %v", err)
	}
	for _, expected := range []string{`"basecoat-css"`, `"shadcn"`, `import "basecoat-css/all"`, `@import "basecoat-css"`, `@import "shadcn/tailwind.css"`} {
		if !strings.Contains(string(packageJSON)+"\n"+string(frontendSource)+"\n"+string(styleSource), expected) {
			t.Fatalf("templ starter missing frontend styling marker %q", expected)
		}
	}
	if !strings.Contains(string(styleSource), `@source "../../../../internal/starterui/**/*.templ"`) {
		t.Fatalf("templ style source must scan generated starter UI templates")
	}

	controllerSource, err := os.ReadFile(filepath.Join("internal", "starterui", "controller.go"))
	if err != nil {
		t.Fatalf("read templ controller source: %v", err)
	}
	controllerTestSource, err := os.ReadFile(filepath.Join("internal", "starterui", "controller_test.go"))
	if err != nil {
		t.Fatalf("read templ controller test source: %v", err)
	}
	viewSource := readStarterUITemplSurface(t)
	for _, expected := range []string{`class="sidebar app-sidebar"`, `data-side="left"`, `class="gf-shell"`, `sidebar-collapsed`, `document.documentElement.classList.add("sidebar-collapsed")`, `:is(.gf-shell.sidebar-collapsed, .sidebar-collapsed .gf-shell)`, `data-gf-sidebar-toggle`} {
		if !strings.Contains(viewSource, expected) {
			t.Fatalf("templ views source missing Basecoat sidebar marker %q", expected)
		}
	}
	templSurface := string(controllerSource) + "\n" + viewSource
	for _, expected := range []string{
		"/login",
		"/register",
		"/forgot-password",
		"/reset-password",
		"/verify-email",
		"/settings/profile",
		"/settings/password",
		"/settings/appearance",
		"/components/overview",
		"/components/forms",
		"/components/navigation",
		"/components/overlays",
		"/components/data",
		`data-gf-controller="table"`,
		`name="compute-environment"`,
		`name="recovery-priority"`,
		`class="team-switcher"`,
		`class="app-user-trigger"`,
		`class="command-dialog gf-command-compact"`,
		`Basecoat component coverage`,
		`class="table"`,
		`class="tabs"`,
		`class="select w-full"`,
		`class="popover"`,
		`class="alert"`,
		`class="alert-destructive"`,
		`class="button-group"`,
		`class="progress-track"`,
		`class="chart-canvas"`,
		`data-gf-controller="chart"`,
		`class="slider"`,
		`class="slides"`,
		`class="group/item`,
		`class="empty-box"`,
		`animate-spin`,
		`data-tooltip`,
		`class="section-stack"`,
	} {
		if !strings.Contains(templSurface, expected) {
			t.Fatalf("templ starter surface missing %q", expected)
		}
	}

	controllerTestText := string(controllerTestSource)
	for _, expected := range []string{
		`func TestStarterUIUnauthenticatedPageRedirectsToLogin`,
		`func TestStarterUIUnauthenticatedHTMXPageRedirectsToLogin`,
		`func TestStarterUILoginFlowIntegration`,
		`service.Register`,
		`starterUITestLogin`,
		`rec.Result().Cookies()`,
		`req.AddCookie(cookie)`,
		`HX-Redirect`,
		`auth.NewService`,
	} {
		if !strings.Contains(controllerTestText, expected) {
			t.Fatalf("templ starter controller tests missing auth integration marker %q", expected)
		}
	}
	for _, unexpected := range []string{`/api/v1/auth/login`, `/api/v1/auth/logout`, `/api/v1/auth/me`} {
		if strings.Contains(controllerTestText, unexpected) {
			t.Fatalf("templ starter controller tests should exercise page auth routes, found API auth route %q", unexpected)
		}
	}
}

func TestScaffoldTemplHTMXStarterKitWithoutAuthOmitsAuthRoutes(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	renderer := &ProjectRenderer{
		workspace: currentProjectRenderWorkspace(t),
		config: &project.Config{
			GoModuleName: "example.com/testapp",
			Render: project.RenderConfig{
				Components: project.Components{WebUI: true},
				StarterKit: project.StarterKitTemplHTMX,
			},
		},
		stats: &renderStats{},
	}
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitTemplHTMX, true); err != nil {
		t.Fatalf("scaffold templ htmx starter kit: %v", err)
	}

	controllerSource, err := os.ReadFile(filepath.Join("internal", "starterui", "controller.go"))
	if err != nil {
		t.Fatalf("read templ controller source: %v", err)
	}
	controllerText := string(controllerSource)
	for _, unexpected := range []string{
		`web.NewRoute(http.MethodGet, "/login", c.LoginPage)`,
		`web.NewRoute(http.MethodPost, "/login", c.Login)`,
		`web.NewRoute(http.MethodPost, "/logout", c.Logout)`,
	} {
		if strings.Contains(controllerText, unexpected) {
			t.Fatalf("auth-disabled templ controller included auth route %q:\n%s", unexpected, controllerText)
		}
	}

	templSurface := readStarterUITemplSurface(t)
	for _, unexpected := range []string{`action="/logout"`, `>Log out</span>`} {
		if strings.Contains(templSurface, unexpected) {
			t.Fatalf("auth-disabled templ surface included auth affordance %q", unexpected)
		}
	}
}

// TestScaffoldTemplHTMXStarterKitOverwriteRefreshesServerViews verifies overwrite mode replaces framework-owned server views.
func TestScaffoldTemplHTMXStarterKitOverwriteRefreshesServerViews(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	starterUIDir := filepath.Join("internal", "starterui")
	if err := os.MkdirAll(starterUIDir, 0o755); err != nil {
		t.Fatalf("mkdir starter ui: %v", err)
	}
	staleView := filepath.Join(starterUIDir, "views.templ")
	if err := os.WriteFile(staleView, []byte(`package starterui

templ stale() {
	<div class="stale-sidebar">DB</div>
}
`), 0o644); err != nil {
		t.Fatalf("write stale view: %v", err)
	}

	renderer := &ProjectRenderer{
		workspace: currentProjectRenderWorkspace(t),
		config: &project.Config{
			GoModuleName: "example.com/testapp",
			Render: project.RenderConfig{
				Components: project.Components{WebUI: true, WebAPI: true, Auth: true, DatabaseSQLite: true},
				StarterKit: project.StarterKitTemplHTMX,
			},
		},
		stats: &renderStats{},
	}
	initializeDefaultResourceStateForTest(t, renderer)
	if err := renderer.scaffoldStarterKitForApp(project.DefaultApp(), project.StarterKitTemplHTMX, true); err != nil {
		t.Fatalf("scaffold templ htmx starter kit: %v", err)
	}

	viewSource, err := os.ReadFile(staleView)
	if err != nil {
		t.Fatalf("read refreshed view: %v", err)
	}
	viewText := string(viewSource)
	for _, unexpected := range []string{`class="stale-sidebar"`, `>DB</div>`} {
		if strings.Contains(viewText, unexpected) {
			t.Fatalf("templ overwrite kept stale marker %q in views.templ", unexpected)
		}
	}
	templSurface := readStarterUITemplSurface(t)
	for _, expected := range []string{`class="sidebar app-sidebar"`, `@iconDatabase()`} {
		if !strings.Contains(templSurface, expected) {
			t.Fatalf("templ overwrite did not refresh views.templ with %q", expected)
		}
	}
}

// readStarterUITemplSurface centralizes read starter uitempl surface lookup for the surrounding workflow.
func readStarterUITemplSurface(t *testing.T) string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("internal", "starterui", "*.templ"))
	if err != nil {
		t.Fatalf("glob starter ui templ files: %v", err)
	}
	sort.Strings(paths)

	var builder strings.Builder
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read templ source %s: %v", path, err)
		}
		builder.Write(source)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func TestFrontendDistPlaceholderUsesNamedApps(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	root := t.TempDir()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join("cmd", "customer-portal"), 0o755); err != nil {
		t.Fatalf("mkdir named app entrypoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join("cmd", "customer-portal", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write named app entrypoint: %v", err)
	}

	renderer := &ProjectRenderer{
		workspace: currentProjectRenderWorkspace(t),
		config: &project.Config{
			Render: project.RenderConfig{
				Components: project.Components{WebUI: true},
			},
		},
		stats: &renderStats{},
	}
	if err := renderer.ensureFrontendDistPlaceholder(); err != nil {
		t.Fatalf("ensure frontend dist placeholder: %v", err)
	}

	for _, path := range []string{
		filepath.Join("cmd", "app", "frontend", "dist", "index.html"),
		filepath.Join("cmd", "app", "frontend", "dist", "goforj-logo.png"),
		filepath.Join("cmd", "customer-portal", "frontend", "dist", "index.html"),
		filepath.Join("cmd", "customer-portal", "frontend", "dist", "goforj-logo.png"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}
