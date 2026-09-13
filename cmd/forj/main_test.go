package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/goforj/console"
	"github.com/goforj/goforj/internal/build"
	"github.com/goforj/goforj/internal/cmd"
	"github.com/goforj/goforj/internal/launcher"
)

type cliConsoleTestWriter struct {
	bytes.Buffer
}

// Fd exposes a synthetic descriptor for deterministic terminal capability checks.
func (*cliConsoleTestWriter) Fd() uintptr {
	return 91
}

// TestConfigureCLIConsoleEnablesTerminalOwnedLoaderProgress verifies dev bootstrap loaders reach supporting terminal chrome.
func TestConfigureCLIConsoleEnablesTerminalOwnedLoaderProgress(t *testing.T) {
	previous := console.Default()
	t.Cleanup(func() {
		console.SetDefault(previous)
	})

	for _, test := range []struct {
		name     string
		terminal bool
		wantOSC  bool
	}{
		{name: "supporting terminal", terminal: true, wantOSC: true},
		{name: "redirected output", wantOSC: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := &cliConsoleTestWriter{}
			animations := false
			unicode := false
			configureCLIConsole(console.Config{
				Stdout:            output,
				Stderr:            output,
				AnimationsEnabled: &animations,
				UnicodeEnabled:    &unicode,
				IsTerminal:        func(int) bool { return test.terminal },
			})
			loader := console.NewLoader("Building app")
			if err := loader.Start(); err != nil {
				t.Fatalf("start loader: %v", err)
			}
			loader.Success("App ready")

			hasOSC := strings.Contains(output.String(), "\x1b]9;4;")
			if hasOSC != test.wantOSC {
				t.Fatalf("terminal progress present = %t, want %t: %q", hasOSC, test.wantOSC, output.String())
			}
			if test.wantOSC {
				for _, sequence := range []string{"\x1b]9;4;3;0\x07", "\x1b]9;4;0;0\x07"} {
					if !strings.Contains(output.String(), sequence) {
						t.Fatalf("loader output omitted terminal lifecycle sequence %q: %q", sequence, output.String())
					}
				}
			}
			for _, expected := range []string{"Building app", "App ready"} {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("loader output omitted %q: %q", expected, output.String())
				}
			}
		})
	}
}

// TestInsertBuildPassthroughBoundaryPreservesGoFlags verifies raw Go syntax reaches the build command intact.
func TestInsertBuildPassthroughBoundaryPreservesGoFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "tags", args: []string{"build", "-tags", "dev"}, want: []string{"build", "--", "-tags", "dev"}},
		{name: "output", args: []string{"build", "-o", "./bin/app"}, want: []string{"build", "--", "-o", "./bin/app"}},
		{name: "framework then Go", args: []string{"build", "--api-index-strict", "--root", ".", "-modfile", "alternate.mod"}, want: []string{"build", "--api-index-strict", "--root", ".", "--", "-modfile", "alternate.mod"}},
		{name: "root flag before build", args: []string{"--dev", "build", "-tags", "dev"}, want: []string{"--dev", "build", "--", "-tags", "dev"}},
		{name: "inherited root flag after build", args: []string{"build", "--dev", "-tags", "dev"}, want: []string{"build", "--dev", "--", "-tags", "dev"}},
		{name: "package begins passthrough", args: []string{"build", "./cmd/app", "-race"}, want: []string{"build", "--", "./cmd/app", "-race"}},
		{name: "explicit boundary", args: []string{"build", "--", "-overlay", "overlay.json"}, want: []string{"build", "--", "-overlay", "overlay.json"}},
		{name: "inline output", args: []string{"build", "-o=./bin/app"}, want: []string{"build", "--", "-o=./bin/app"}},
		{name: "other command", args: []string{"run", "-config", "app.yml"}, want: []string{"run", "-config", "app.yml"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := insertBuildPassthroughBoundary(test.args); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("normalized args = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestNormalizeLegacyLongAliases verifies the former long alias remains compatible without rewriting unrelated arguments.
func TestNormalizeLegacyLongAliases(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "legacy alias", args: []string{"--x", "build"}, want: []string{"--dev", "build"}},
		{name: "legacy alias true", args: []string{"--x=true", "build"}, want: []string{"--dev=true", "build"}},
		{name: "legacy alias false", args: []string{"--x=false", "build"}, want: []string{"--dev=false", "build"}},
		{name: "short alias", args: []string{"-x", "build"}, want: []string{"-x", "build"}},
		{name: "explicit boundary", args: []string{"build", "--", "--x"}, want: []string{"build", "--", "--x"}},
		{name: "unrelated prefix", args: []string{"--xray", "build"}, want: []string{"--xray", "build"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeLegacyLongAliases(test.args); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("normalized args = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestBuildPassthroughBoundarySurvivesKong verifies parser behavior rather than only the pre-parser transformation.
func TestBuildPassthroughBoundarySurvivesKong(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		wantGoArgs       []string
		wantStrict       bool
		wantRoot         string
		wantDev          bool
		wantEnvOverrides string
		wantStack        string
	}{
		{name: "tags", args: []string{"build", "-tags", "dev"}, wantGoArgs: []string{"-tags", "dev"}},
		{name: "root flag before build", args: []string{"--dev", "build", "-tags", "dev"}, wantGoArgs: []string{"-tags", "dev"}, wantDev: true},
		{name: "root alias before build", args: []string{"--x", "build", "-tags", "dev"}, wantGoArgs: []string{"-tags", "dev"}, wantDev: true},
		{name: "root short alias before build", args: []string{"-x", "build", "-tags", "dev"}, wantGoArgs: []string{"-tags", "dev"}, wantDev: true},
		{name: "inherited root flag after build", args: []string{"build", "--dev", "-tags", "dev"}, wantGoArgs: []string{"-tags", "dev"}, wantDev: true},
		{name: "root alias after build", args: []string{"build", "--x", "-tags=dev"}, wantGoArgs: []string{"-tags=dev"}, wantDev: true},
		{name: "Go x flag", args: []string{"build", "-x"}, wantGoArgs: []string{"-x"}},
		{name: "explicit boundary preserves Go x", args: []string{"build", "--", "-x"}, wantGoArgs: []string{"-x"}},
		{name: "source flags", args: []string{"build", "--api-index-strict", "-overlay", "overlay.json"}, wantGoArgs: []string{"-overlay", "overlay.json"}, wantStrict: true},
		{name: "root and modfile", args: []string{"build", "--root", "project", "-modfile", "alternate.mod"}, wantGoArgs: []string{"-modfile", "alternate.mod"}, wantRoot: "project"},
		{name: "output", args: []string{"build", "-o", "./bin/app"}, wantGoArgs: []string{"-o", "./bin/app"}},
		{name: "inline output", args: []string{"build", "-o=./bin/app"}, wantGoArgs: []string{"-o=./bin/app"}},
		{name: "linker flags", args: []string{"build", "-ldflags", "-X example.com/app.Value=dev"}, wantGoArgs: []string{"-ldflags", "-X example.com/app.Value=dev"}},
		{name: "stack selection", args: []string{"build", "--stack", "portable"}, wantStack: "portable"},
		{name: "inline stack selection", args: []string{"build", "--stack=services", "-tags", "dev"}, wantStack: "services", wantGoArgs: []string{"-tags", "dev"}},
		{name: "environment overrides", args: []string{"build", "--env-overrides", "FEATURE_A=true"}, wantEnvOverrides: "FEATURE_A=true"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := struct {
				Dev      bool      `name:"dev" aliases:"x"`
				BuildCmd build.Cmd `cmd:""`
			}{}
			parser, err := kong.New(&root)
			if err != nil {
				t.Fatalf("create parser: %v", err)
			}
			args := normalizeLegacyLongAliases(test.args)
			if _, err := parser.Parse(insertBuildPassthroughBoundary(args)); err != nil {
				t.Fatalf("parse build command: %v", err)
			}
			goArgs := buildPassthroughArgs(root.BuildCmd.Args)
			if !reflect.DeepEqual(goArgs, test.wantGoArgs) {
				t.Fatalf("Go build args = %#v, want %#v", goArgs, test.wantGoArgs)
			}
			if root.BuildCmd.APIIndexStrict != test.wantStrict {
				t.Fatalf("strict = %t, want %t", root.BuildCmd.APIIndexStrict, test.wantStrict)
			}
			if root.Dev != test.wantDev {
				t.Fatalf("dev = %t, want %t", root.Dev, test.wantDev)
			}
			if test.wantRoot != "" && root.BuildCmd.Root != test.wantRoot {
				t.Fatalf("root = %q, want %q", root.BuildCmd.Root, test.wantRoot)
			}
			if root.BuildCmd.Stack != test.wantStack {
				t.Fatalf("stack = %q, want %q", root.BuildCmd.Stack, test.wantStack)
			}
			if root.BuildCmd.EnvOverrides != test.wantEnvOverrides {
				t.Fatalf("environment overrides = %q, want %q", root.BuildCmd.EnvOverrides, test.wantEnvOverrides)
			}
		})
	}
}

func TestShouldDelegateToAppCommand(t *testing.T) {
	parseErr := errors.New("unexpected argument route:list")
	if !shouldDelegateToAppCommand([]string{"route:list"}, parseErr, true) {
		t.Fatal("expected unresolved app command to pass through")
	}
	if shouldDelegateToAppCommand([]string{"--bad"}, parseErr, true) {
		t.Fatal("expected flags to remain owned by forj")
	}
	if shouldDelegateToAppCommand([]string{"route:list"}, errors.New("invalid flag --bad"), true) {
		t.Fatal("expected non-command parser errors to remain owned by forj")
	}
	if shouldDelegateToAppCommand([]string{"route:list"}, parseErr, false) {
		t.Fatal("expected delegation to require a generated app")
	}
}

func TestIsGeneratedAppDirRequiresProjectMarkers(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if isGeneratedAppDir() {
		t.Fatal("expected empty directory not to be a generated app")
	}
	if err := os.WriteFile(".goforj.yml", []byte("project_name: Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if isGeneratedAppDir() {
		t.Fatal("expected go.mod to be required")
	}
	if err := os.WriteFile("go.mod", []byte("module example.com/testapp\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isGeneratedAppDir() {
		t.Fatal("expected generated app markers")
	}
}

func TestAppRootHelpArgsUsesAppHelp(t *testing.T) {
	args := appRootHelpArgs()
	if len(args) != 1 || args[0] != "--help" {
		t.Fatalf("expected app root help args to request app help, got %#v", args)
	}
}

func TestResolveAppPrefixUsesConventionalSourceApp(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeGeneratedAppMarker(t)
	writeMain := filepath.Join("cmd", "reporting", "main.go")
	if err := os.MkdirAll(filepath.Dir(writeMain), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writeMain, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build", "dev"}

	appName, remaining, ok := resolveAppPrefix([]string{"reporting", "build"}, true)
	if !ok || appName != "reporting" || len(remaining) != 1 || remaining[0] != "build" {
		t.Fatalf("app prefix = (%q, %#v, %t), want reporting, [build], true", appName, remaining, ok)
	}
}

func TestAppPrefixedNativeCommandStaysInSourceMode(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeGeneratedAppMarker(t)
	writeSourceApp(t, "billing")
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "billing"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build", "dev"}

	appName, remaining, ok := resolveAppPrefix([]string{"billing", "build", "-o", "./bin/billing"}, true)
	if !ok || appName != "billing" {
		t.Fatalf("app prefix = (%q, %#v, %t), want billing app", appName, remaining, ok)
	}
	if !shouldRunAppNativeCommand(remaining) {
		t.Fatalf("expected app-prefixed native command to stay source-scoped, got %#v", remaining)
	}
}

func TestAppPrefixedSourceCommandWinsOverBuiltBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeGeneratedAppMarker(t)
	writeSourceApp(t, "billing")
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "billing"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build", "dev"}

	if !shouldRunAppThroughSource("billing", []string{"make:controller", "checkout"}, true) {
		t.Fatal("expected source app commands to avoid stale built app binaries")
	}
}

func TestAppPrefixedBinaryOnlyCommandUsesBuiltBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "billing"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build", "dev"}

	if shouldRunAppThroughSource("billing", []string{"route:list"}, false) {
		t.Fatal("expected binary-only app commands to delegate to the built app binary")
	}
}

func TestResolveAppPrefixPreservesNativeCommandPrecedence(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeGeneratedAppMarker(t)
	writeMain := filepath.Join("cmd", "build", "main.go")
	if err := os.MkdirAll(filepath.Dir(writeMain), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writeMain, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build"}

	_, _, ok := resolveAppPrefix([]string{"build"}, true)
	if ok {
		t.Fatal("expected native build command to keep precedence over a conventional app")
	}
}

func TestResolveAppPrefixPreservesNativeCommandPrecedenceOverBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeGeneratedAppMarker(t)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "build"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build"}

	_, _, ok := resolveAppPrefix([]string{"build"}, true)
	if ok {
		t.Fatal("expected native build command to keep precedence over a built app binary")
	}
}

func TestConventionalAppHelpIncludesSourceAndBinaryApps(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeGeneratedAppMarker(t)
	writeSourceApp(t, "admin")
	writeSourceApp(t, "billing")
	writeSourceApp(t, "build")
	writeSourceApp(t, "wire")
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "reporting"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	previousNativeNames := cliNativeCommandNames
	defer func() { cliNativeCommandNames = previousNativeNames }()
	cliNativeCommandNames = []string{"build"}

	apps := conventionalAppHelpApps(true)
	want := []string{"admin", "app", "billing", "reporting"}
	if len(apps) != len(want) {
		t.Fatalf("help apps = %#v, want %#v", apps, want)
	}
	for i := range want {
		if apps[i] != want[i] {
			t.Fatalf("help apps = %#v, want %#v", apps, want)
		}
	}
}

func TestConventionalAppHelpSkipsNonGeneratedProjects(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeSourceApp(t, "billing")

	if apps := conventionalAppHelpApps(false); len(apps) != 0 {
		t.Fatalf("expected no generated app help entries, got %#v", apps)
	}
}

// TestEnsureGeneratedAppHelpBinariesBuildsMissingSourceAppsOnce verifies help bootstraps only absent App binaries.
func TestEnsureGeneratedAppHelpBinariesBuildsMissingSourceAppsOnce(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	writeSourceApp(t, "app")
	writeSourceApp(t, "admin")
	writeSourceApp(t, "existing")
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("bin", "existing"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var built []string
	buildApp := func(appName string) error {
		built = append(built, appName)
		return os.WriteFile(filepath.Join("bin", appName), []byte("binary"), 0o755)
	}
	apps := []string{"admin", "app", "binary-only", "existing"}
	if err := ensureGeneratedAppHelpBinaries(apps, buildApp); err != nil {
		t.Fatal(err)
	}
	if err := ensureGeneratedAppHelpBinaries(apps, buildApp); err != nil {
		t.Fatal(err)
	}

	want := []string{"admin", "app"}
	if !reflect.DeepEqual(built, want) {
		t.Fatalf("built Apps = %#v, want %#v", built, want)
	}
}

// TestEnsureGeneratedAppHelpBinariesRequiresPublishedBinary prevents successful no-op builds from hiding App help.
func TestEnsureGeneratedAppHelpBinariesRequiresPublishedBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	writeSourceApp(t, "app")
	err := ensureGeneratedAppHelpBinaries([]string{"app"}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "completed without producing bin/app") {
		t.Fatalf("missing build output error = %v", err)
	}
}

// TestEnsureGeneratedAppHelpBinariesReportsBuildFailure preserves the failed public invocation in diagnostics.
func TestEnsureGeneratedAppHelpBinariesReportsBuildFailure(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	writeSourceApp(t, "admin")
	buildErr := errors.New("compile failed")
	err := ensureGeneratedAppHelpBinaries([]string{"admin"}, func(string) error { return buildErr })
	if !errors.Is(err, buildErr) || !strings.Contains(err.Error(), "forj admin build") {
		t.Fatalf("build failure = %v", err)
	}
}

// TestAppHelpBuildArgsUsesPublicAppRouting verifies default and named Apps use their normal build commands.
func TestAppHelpBuildArgsUsesPublicAppRouting(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		want    []string
	}{
		{name: "default", appName: "app", want: []string{"build"}},
		{name: "named", appName: "admin", want: []string{"admin", "build"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := appHelpBuildArgs(test.appName); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("build args = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestWithAppHelpBuildOriginReplacesInheritedOrigin prevents parent workflow policy from leaking into help builds.
func TestWithAppHelpBuildOriginReplacesInheritedOrigin(t *testing.T) {
	env := withAppHelpBuildOrigin([]string{
		"PATH=/tmp/bin",
		"FORJ_COMMAND_ORIGIN=dev_command",
		"APP_ENV=local",
	})
	want := []string{
		"PATH=/tmp/bin",
		"APP_ENV=local",
		"FORJ_COMMAND_ORIGIN=" + build.AppHelpCommandOrigin,
	}
	if !reflect.DeepEqual(env, want) {
		t.Fatalf("App help build environment = %#v, want %#v", env, want)
	}
}

func TestRunAppHelpForAppUsesExistingBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join("bin", "billing")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho \"$1|$2|$FORJ_APP\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := runAppHelpForApp("billing", true)
	if err != nil {
		t.Fatalf("run app help: %v\n%s", err, output)
	}
	if output != "--help||billing\n" {
		t.Fatalf("unexpected help command output: %q", output)
	}
}

func TestRunAppHelpForAppMarksMultiAppHelp(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join("bin", "app")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho \"$FORJ_MULTI_APP_HELP\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := runAppHelpForApp("app", true)
	if err != nil {
		t.Fatalf("run app help: %v\n%s", err, output)
	}
	if output != "1\n" {
		t.Fatalf("unexpected multi-app marker: %q", output)
	}
}

func TestRunAppHelpForAppSkipsMissingBinaryWithoutBuildingSource(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()
	writeSourceApp(t, "billing")

	_, err := runAppHelpForApp("billing", true)
	if !errors.Is(err, errAppBinaryNotFound) {
		t.Fatalf("missing binary error = %v, want app binary not found", err)
	}
}

func TestRunAppHelpForAppBoundsUnresponsiveBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join("bin", "billing")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, err := runAppHelpForApp("billing", true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unresponsive binary error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("unresponsive binary elapsed = %s, want bounded help probe", elapsed)
	}
}

// TestPrintGeneratedAppHelpAllowsColdValidBinary keeps cold App startup from silently hiding project commands.
func TestPrintGeneratedAppHelpAllowsColdValidBinary(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	appScript := "#!/bin/sh\nsleep 0.75\nprintf '%s\\n\\n%s\\n  %s\\n' 'test · app' 'app' 'about    Show environment'\n"
	if err := os.WriteFile(filepath.Join("bin", "app"), []byte(appScript), 0o755); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := printGeneratedAppHelp([]string{"app"}); err != nil {
			t.Fatalf("print app help: %v", err)
		}
	})
	if !strings.Contains(output, "about") {
		t.Fatalf("expected delayed valid App help, got %q", output)
	}
}

func TestPrintGeneratedAppHelpIgnoresUnavailableAppHelp(t *testing.T) {
	restore := chdirTemp(t)
	defer restore()

	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	appScript := "#!/bin/sh\nprintf '%s\\n\\n%s\\n  %s\\n' 'test · app' 'app' 'about    Show environment'\n"
	if err := os.WriteFile(filepath.Join("bin", "app"), []byte(appScript), 0o755); err != nil {
		t.Fatal(err)
	}
	customScript := "#!/bin/sh\necho 'custom binary has no framework help' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join("bin", "ship-smoke"), []byte(customScript), 0o755); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := printGeneratedAppHelp([]string{"app", "ship-smoke"}); err != nil {
			t.Fatalf("print app help: %v", err)
		}
	})
	if !strings.Contains(output, "test · app") {
		t.Fatalf("expected generated app help to be printed, got:\n%s", output)
	}
	if strings.Contains(output, "ship-smoke") || strings.Contains(output, "custom binary") {
		t.Fatalf("expected unavailable app help to be ignored, got:\n%s", output)
	}
}

func TestGeneratedAppHelpResultsIgnoreCustomHelp(t *testing.T) {
	results := generatedAppHelpResults([]appHelpResult{
		{
			app: "app",
			output: strings.Join([]string{
				"test · app",
				"",
				"app",
				"  about    Show environment",
				"",
			}, "\n"),
		},
		{
			app:    "ship-smoke",
			output: "Usage: ship-smoke [options]\n",
		},
	})

	if len(results) != 1 || results[0].app != "app" {
		t.Fatalf("filtered app help = %#v, want only app", results)
	}
}

func TestCompactGeneratedAppHelpDeduplicatesSharedCommands(t *testing.T) {
	results := []appHelpResult{
		{app: "app", output: strings.Join([]string{
			"test · app",
			"",
			"app",
			"  about    Show environment",
			"  health   Query readiness",
			"monitor",
			"  seed     Seed monitors",
			"",
		}, "\n")},
		{app: "billing", output: strings.Join([]string{
			"test · billing",
			"",
			"app",
			"  about    Show environment",
			"  health   Query readiness",
			"queue",
			"  invoice  Process invoices",
			"",
		}, "\n")},
		{app: "reporting", output: strings.Join([]string{
			"test · reporting",
			"",
			"app",
			"  about    Show environment",
			"  health   Query readiness",
			"",
		}, "\n")},
	}

	output, ok := compactGeneratedAppHelp(results)
	if !ok {
		t.Fatal("expected compact help output")
	}
	for _, want := range []string{
		"test · available in all apps",
		"  about   Show environment",
		"  health  Query readiness",
		"test · app",
		"  seed  Seed monitors",
		"test · billing",
		"  invoice  Process invoices",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected compact help to include %q, got:\n%s", want, output)
		}
	}
	for _, unexpected := range []string{"app only", "billing only", "test · reporting"} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("expected compact help to omit %q, got:\n%s", unexpected, output)
		}
	}
}

func TestPrintAppUsageHelpOnlyForMultiApp(t *testing.T) {
	single := captureStdout(t, func() {
		printAppUsageHelp([]string{"app"})
	})
	if single != "" {
		t.Fatalf("expected no app usage for single app, got:\n%s", single)
	}

	multi := captureStdout(t, func() {
		printAppUsageHelp([]string{"app", "billing"})
	})
	for _, want := range []string{
		"app usage",
		"forj <app> <command>",
		"Run a command for a specific app",
		"forj <app> build",
		"Build a specific app binary",
		"forj dev",
		"Build and run all apps in development",
	} {
		if !strings.Contains(multi, want) {
			t.Fatalf("expected app usage help to include %q, got:\n%s", want, multi)
		}
	}
}

func TestWithAppEnvOverridesExistingAppIdentity(t *testing.T) {
	env := withAppEnv([]string{
		"FORJ_COMMAND_PREFIX=forj billing",
		"FORJ_APP=billing",
		"KEEP=value",
	}, "reporting")

	for _, want := range []string{
		"FORJ_COMMAND_PREFIX=forj reporting",
		"FORJ_APP=reporting",
		"KEEP=value",
	} {
		if !envHasEntry(env, want) {
			t.Fatalf("expected env to include %q, got %#v", want, env)
		}
	}
}

// captureStdout centralizes capture stdout behavior so callers follow the same contract.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	previous := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	defer func() {
		os.Stdout = previous
	}()

	fn()

	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, read); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestDelegatedAppEnvRemovesOnlyCLIDefaults(t *testing.T) {
	previousDefaults := cliDefaultedEnv
	previousAppName, hadAppName := os.LookupEnv("APP_NAME")
	previousAppEnv, hadAppEnv := os.LookupEnv("APP_ENV")
	defer func() {
		cliDefaultedEnv = previousDefaults
		restoreEnv("APP_NAME", previousAppName, hadAppName)
		restoreEnv("APP_ENV", previousAppEnv, hadAppEnv)
	}()

	cliDefaultedEnv = map[string]bool{}
	_ = os.Unsetenv("APP_NAME")
	_ = os.Unsetenv("APP_ENV")

	setCLIDefaultEnv("APP_NAME", "GoForj")
	_ = os.Setenv("APP_ENV", "testing")

	env := delegatedAppEnv()
	if envHasEntry(env, "APP_NAME=GoForj") {
		t.Fatal("expected CLI-defaulted APP_NAME to be removed")
	}
	if !envHasEntry(env, "APP_ENV=testing") {
		t.Fatal("expected caller-provided APP_ENV to be preserved")
	}
	if !envHasEntry(env, "FORJ_COMMAND_PREFIX=forj") {
		t.Fatal("expected delegated app env to include the forj command prefix")
	}
}

// TestLauncherEnvironmentExcludesCLIDefaults ensures framework defaults do not become launcher-owned overrides.
func TestLauncherEnvironmentExcludesCLIDefaults(t *testing.T) {
	previousAppName, hadAppName := os.LookupEnv("APP_NAME")
	previousAppEnv, hadAppEnv := os.LookupEnv("APP_ENV")
	previousDefaults := cliDefaultedEnv
	defer func() {
		cliDefaultedEnv = previousDefaults
		restoreEnv("APP_NAME", previousAppName, hadAppName)
		restoreEnv("APP_ENV", previousAppEnv, hadAppEnv)
		launcher.Capture()
	}()

	cliDefaultedEnv = map[string]bool{}
	_ = os.Unsetenv("APP_NAME")
	_ = os.Unsetenv("APP_ENV")
	launcher.Capture()
	captured := launcher.Snapshot()
	setCLIDefaultEnv("APP_NAME", "GoForj")
	setCLIDefaultEnv("APP_ENV", "local")
	if _, ok := captured["APP_NAME"]; ok {
		t.Fatal("APP_NAME default was captured as launcher environment")
	}
	if _, ok := captured["APP_ENV"]; ok {
		t.Fatal("APP_ENV default was captured as launcher environment")
	}
}

// TestReadOnlyCLIRequestRecognizesNonMutatingInvocations verifies help and version stay read-only regardless of flag position.
func TestReadOnlyCLIRequestRecognizesNonMutatingInvocations(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "root help flag", args: []string{"--help"}, want: true},
		{name: "command help flag", args: []string{"build", "--help"}, want: true},
		{name: "leading help flag", args: []string{"--help", "build"}, want: true},
		{name: "version flag", args: []string{"--version"}, want: true},
		{name: "build", args: []string{"build"}},
		{name: "passthrough help", args: []string{"run", "--", "--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := readOnlyCLIRequest(test.args); got != test.want {
				t.Fatalf("readOnlyCLIRequest(%v) = %t, want %t", test.args, got, test.want)
			}
		})
	}
}

// TestFirstCLICommandSkipsInheritedFlags verifies parsed initialization classifies the actual command.
func TestFirstCLICommandSkipsInheritedFlags(t *testing.T) {
	if got := firstCLICommand([]string{"--dev", "build"}); got != "build" {
		t.Fatalf("firstCLICommand() = %q, want build", got)
	}
}

// TestInitializeParsedEnvironmentUsesSelectedBuildRoot verifies cross-project builds initialize their target, not the caller.
func TestInitializeParsedEnvironmentUsesSelectedBuildRoot(t *testing.T) {
	caller := t.TempDir()
	target := t.TempDir()
	for name, content := range map[string]string{
		".goforj.yml":  "project_name: Target\n",
		"go.mod":       "module example.com/target\n\ngo 1.24\n",
		".env.example": "APP_ENV=local\nAPP_KEY=\n",
	} {
		if err := os.WriteFile(filepath.Join(target, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write target %s: %v", name, err)
		}
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("get caller directory: %v", err)
	}
	if err := os.Chdir(caller); err != nil {
		t.Fatalf("change caller directory: %v", err)
	}
	defer func() { _ = os.Chdir(previous) }()
	root := &cmd.RootCmd{}
	root.RootCmd.BuildCmd.Root = target
	if err := initializeParsedEnvironment([]string{"build", "--root", target}, root, false); err != nil {
		t.Fatalf("initializeParsedEnvironment() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".env")); err != nil {
		t.Fatalf("selected build root was not initialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(caller, ".env")); !os.IsNotExist(err) {
		t.Fatalf("caller directory was initialized: %v", err)
	}
}

func TestDelegatedAppEnvRespectsCommandPrefixOverride(t *testing.T) {
	previousPrefix, hadPrefix := os.LookupEnv("FORJ_COMMAND_PREFIX")
	defer restoreEnv("FORJ_COMMAND_PREFIX", previousPrefix, hadPrefix)

	_ = os.Setenv("FORJ_COMMAND_PREFIX", "./bin/app")

	env := delegatedAppEnv()
	if !envHasEntry(env, "FORJ_COMMAND_PREFIX=./bin/app") {
		t.Fatalf("expected delegated app env to preserve existing command prefix")
	}
	if envHasEntry(env, "FORJ_COMMAND_PREFIX=forj") {
		t.Fatalf("expected existing command prefix to prevent forj override")
	}
}

func TestDelegatedAppEnvIncludesNativeCommandNames(t *testing.T) {
	previousNativeNames := cliNativeCommandNames
	defer func() {
		cliNativeCommandNames = previousNativeNames
	}()

	cliNativeCommandNames = []string{"build", "dev"}

	env := delegatedAppEnv()
	if !envHasEntry(env, "FORJ_NATIVE_COMMAND_NAMES=build,dev") {
		t.Fatalf("expected delegated app env to include native command names, got %#v", env)
	}
}

func TestShouldForceDelegatedAppColor(t *testing.T) {
	previousNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	previousForce, hadForce := os.LookupEnv("CLICOLOR_FORCE")
	defer func() {
		restoreEnv("NO_COLOR", previousNoColor, hadNoColor)
		restoreEnv("CLICOLOR_FORCE", previousForce, hadForce)
	}()

	_ = os.Unsetenv("NO_COLOR")
	_ = os.Unsetenv("CLICOLOR_FORCE")
	if !shouldForceDelegatedAppColor(true) {
		t.Fatal("expected TTY delegated app output to force color")
	}
	if shouldForceDelegatedAppColor(false) {
		t.Fatal("expected non-TTY delegated app output not to force color")
	}

	_ = os.Setenv("NO_COLOR", "1")
	if shouldForceDelegatedAppColor(true) {
		t.Fatal("expected NO_COLOR to prevent forced color")
	}

	_ = os.Unsetenv("NO_COLOR")
	_ = os.Setenv("CLICOLOR_FORCE", "1")
	if shouldForceDelegatedAppColor(true) {
		t.Fatal("expected existing CLICOLOR_FORCE to prevent duplicate forced color")
	}
}

// chdirTemp centralizes chdir temp behavior so callers follow the same contract.
func chdirTemp(t *testing.T) func() {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	return func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	}
}

// writeSourceApp centralizes write source app persistence for the surrounding workflow.
func writeSourceApp(t *testing.T, appName string) {
	t.Helper()

	writeMain := filepath.Join("cmd", appName, "main.go")
	if err := os.MkdirAll(filepath.Dir(writeMain), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writeMain, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeGeneratedAppMarker centralizes write generated app marker persistence for the surrounding workflow.
func writeGeneratedAppMarker(t *testing.T) {
	t.Helper()

	if err := os.WriteFile(".goforj.yml", []byte("project_name: Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("go.mod", []byte("module example.com/testapp\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// restoreEnv centralizes restore env behavior so callers follow the same contract.
func restoreEnv(key, value string, existed bool) {
	if !existed {
		_ = os.Unsetenv(key)
		return
	}
	_ = os.Setenv(key, value)
}

// envHasEntry centralizes env has entry behavior so callers follow the same contract.
func envHasEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
