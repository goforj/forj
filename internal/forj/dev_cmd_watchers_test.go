package forj

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goforj/console"
	"github.com/goforj/goforj/internal/apiindex"
	"github.com/goforj/goforj/internal/build"
	"github.com/goforj/goforj/internal/devwatch"
	"github.com/goforj/goforj/internal/logger"
	"github.com/goforj/goforj/internal/projectlayout"
	"github.com/goforj/goforj/project"
)

func TestBuildWatcherExecUsesExec(t *testing.T) {
	script := buildWatcherExec("./bin/app http:serve")
	if script == "" {
		t.Fatal("expected watcher script to be non-empty")
	}
	if want := "__FORJ_WATCHER_TRIGGER__"; !contains(script, want) {
		t.Fatalf("expected watcher trigger marker in script: %q", script)
	}
	if want := "exec \"$forj_dev_exec_target\" http:serve"; !contains(script, want) {
		t.Fatalf("expected exec for watcher command in script: %q", script)
	}
	if contains(script, "exec ./bin/app http:serve") {
		t.Fatalf("expected watcher to avoid direct binary exec: %q", script)
	}
	if want := "forj_dev_target='./bin/app'"; !contains(script, want) {
		t.Fatalf("expected binary readiness target in script: %q", script)
	}
	if want := "forj_dev_ready='./bin/.app.ready'"; !contains(script, want) {
		t.Fatalf("expected build ready stamp target in script: %q", script)
	}
	if want := "wc -c"; !contains(script, want) {
		t.Fatalf("expected binary size stability check in script: %q", script)
	}
	if want := "forj_dev_binary_magic_ok"; !contains(script, want) {
		t.Fatalf("expected executable format check in script: %q", script)
	}
	if want := "mktemp"; !contains(script, want) {
		t.Fatalf("expected binary snapshot in script: %q", script)
	}
	if want := "forj_dev_ready_ok"; !contains(script, want) {
		t.Fatalf("expected readiness guard in script: %q", script)
	}
	if want := "exit 0"; !contains(script, want) {
		t.Fatalf("expected timeout to avoid executing stale binary: %q", script)
	}
}

func TestBuildWatcherExecSkipsReadinessForNonBinaryCommands(t *testing.T) {
	script := buildWatcherExec("npm run dev")
	if contains(script, "forj_dev_target") {
		t.Fatalf("did not expect binary readiness target for npm watcher: %q", script)
	}
	if want := "exec npm run dev"; !contains(script, want) {
		t.Fatalf("expected npm watcher exec to remain direct: %q", script)
	}
}

// TestBuildNativeRuntimeExecFailsClosedUntilPreparation prevents a dormant wrapper from copying after runtime stop.
func TestBuildNativeRuntimeExecFailsClosedUntilPreparation(t *testing.T) {
	script := buildNativeRuntimeExec("./bin/app run")
	for _, expected := range []string{"__FORJ_WATCHER_TRIGGER__", "refusing to start an unprepared native executable", "exit 1"} {
		if !contains(script, expected) {
			t.Fatalf("expected native runtime script to contain %q: %q", expected, script)
		}
	}
	for _, forbidden := range []string{"mktemp", "cp ", "trap ", "./bin/app"} {
		if contains(script, forbidden) {
			t.Fatalf("unprepared native runtime script retained %q: %q", forbidden, script)
		}
	}
}

// TestBuildPreparedNativeRuntimeExecLaunchesOnlyPreparedPath verifies the supervisor-owned artifact is the sole target.
func TestBuildPreparedNativeRuntimeExecLaunchesOnlyPreparedPath(t *testing.T) {
	script := buildPreparedNativeRuntimeExec("./bin/app run", "./bin/app", "/tmp/.app.run-prepared")
	if want := "exec '/tmp/.app.run-prepared' run"; !contains(script, want) {
		t.Fatalf("prepared runtime script missing %q: %q", want, script)
	}
	for _, forbidden := range []string{"mktemp", "cp ", "trap ", "exec ./bin/app"} {
		if contains(script, forbidden) {
			t.Fatalf("prepared native runtime script retained %q: %q", forbidden, script)
		}
	}
}

// TestBuildFullProcessRuntimeExecDoesNotRewriteMappedCommand keeps quoting and environment syntax under user control.
func TestBuildFullProcessRuntimeExecDoesNotRewriteMappedCommand(t *testing.T) {
	command := `MODE=dev "./bin/custom app" --flag='quoted value'`
	script := buildFullProcessRuntimeExec(command)
	if !strings.HasSuffix(script, command) {
		t.Fatalf("full process override changed command text: %q", script)
	}
	for _, rewritten := range []string{"mktemp", "forj_dev_snapshot", "cp \"$forj_dev_target\""} {
		if strings.Contains(script, rewritten) {
			t.Fatalf("full process override unexpectedly contained %q: %q", rewritten, script)
		}
	}
}

func TestDevExecutableTargetHandlesAbsoluteBinPath(t *testing.T) {
	target, ok := devExecutableTarget("/Users/cmiles/code/ditracker/bin/app run")
	if !ok {
		t.Fatal("expected absolute bin path to be treated as executable target")
	}
	if target != "/Users/cmiles/code/ditracker/bin/app" {
		t.Fatalf("target = %q", target)
	}
	ready := devExecutableReadyStampTarget(target)
	if ready != "/Users/cmiles/code/ditracker/bin/.app.ready" {
		t.Fatalf("ready stamp = %q", ready)
	}
}

func TestDevExecutableArgSuffixPreservesRuntimeArgs(t *testing.T) {
	if got := devExecutableArgSuffix("./bin/app run --port 3000", "./bin/app"); got != " run --port 3000" {
		t.Fatalf("arg suffix = %q", got)
	}
}

func TestClearDevRunReadyStampsRemovesAppsWithBuildWatchers(t *testing.T) {
	root := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	for _, path := range []string{"bin/.app.ready", "bin/.worker.ready", "bin/.orphan.ready"} {
		if err := os.WriteFile(path, []byte("ready\n"), 0o644); err != nil {
			t.Fatalf("write ready stamp %s: %v", path, err)
		}
	}

	clearDevBuildReadyStamps([]devBuildJob{
		{app: project.DefaultNamedApp("app")},
		{app: project.DefaultNamedApp("worker")},
	})

	for _, path := range []string{"bin/.app.ready", "bin/.worker.ready"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err: %v", path, err)
		}
	}
	if _, err := os.Stat("bin/.orphan.ready"); err != nil {
		t.Fatalf("expected orphan stamp to remain: %v", err)
	}
}

// TestPublishDevBuildReadyStampRequiresPublishedBinary verifies custom builds cannot publish readiness before their binary exists.
func TestPublishDevBuildReadyStampRequiresPublishedBinary(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := publishDevBuildReadyStamp(project.DefaultApp()); err != nil {
		t.Fatalf("skip absent binary: %v", err)
	}
	if _, err := os.Stat(filepath.Join("bin", ".app.ready")); !os.IsNotExist(err) {
		t.Fatalf("absent binary published a ready stamp: %v", err)
	}
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join("bin", "app"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := publishDevBuildReadyStamp(project.DefaultApp()); err != nil {
		t.Fatalf("publish ready stamp: %v", err)
	}
	if _, err := os.Stat(filepath.Join("bin", ".app.ready")); err != nil {
		t.Fatalf("expected ready stamp: %v", err)
	}
}

// TestRunDevTasksIncludesOutputTailOnFailure keeps Docker port errors visible after startup exits.
func TestRunDevTasksIncludesOutputTailOnFailure(t *testing.T) {
	err := runDevTasks([]project.DevTask{
		{
			Name: "Run Docker Compose",
			Cmd:  "printf 'docker: Error response from daemon: Ports are not available: bind: address already in use\\n' >&2; exit 1",
		},
	})
	if err == nil {
		t.Fatalf("expected pre-dev task failure")
	}
	for _, expected := range []string{
		"pre-dev task 'Run Docker Compose' failed with exit code 1",
		"Last task output:",
		"Ports are not available",
		"address already in use",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected error to contain %q, got:\n%s", expected, err.Error())
		}
	}
}

// TestDevTaskOutputBlockLabelsLiveOutput keeps chatty setup commands visually owned without changing their output.
func TestDevTaskOutputBlockLabelsLiveOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	loaderStops := 0
	block := newDevTaskOutputBlock("Run Docker Compose", &stdout, &stderr, func() {
		loaderStops++
	})

	stdoutWriter := block.stdoutWriter()
	if _, err := io.WriteString(stdoutWriter, "\r\x1b["); err != nil {
		t.Fatalf("write split ANSI prefix: %v", err)
	}
	if _, err := io.WriteString(stdoutWriter, "2K[+] up "); err != nil {
		t.Fatalf("write initial stdout fragment: %v", err)
	}
	if _, err := io.WriteString(block.stderrWriter(), "compose warning\n"); err != nil {
		t.Fatalf("write stderr: %v", err)
	}
	for _, fragment := range []string{"2/2\n", "container app running\r", "container db running\n"} {
		if _, err := io.WriteString(stdoutWriter, fragment); err != nil {
			t.Fatalf("write stdout fragment: %v", err)
		}
	}
	if _, err := block.finish(true, 125*time.Millisecond); err != nil {
		t.Fatalf("finish output block: %v", err)
	}

	if loaderStops != 1 {
		t.Fatalf("loader stops = %d, want 1", loaderStops)
	}
	plainStdout := stripANSI(stdout.String())
	success := stripANSI(console.SuccessMark())
	for _, expected := range []string{
		"┏ Run Docker Compose\n",
		"┃ [+] up 2/2\n",
		"┃ container app running\r┃ container db running\n",
		"┗ " + success + " Done · 125ms\n",
	} {
		if !strings.Contains(plainStdout, expected) {
			t.Fatalf("stdout omitted %q: %q", expected, plainStdout)
		}
	}
	plainStderr := stripANSI(stderr.String())
	if !strings.Contains(plainStderr, "┃ compose warning\n") {
		t.Fatalf("stderr was not inset: %q", plainStderr)
	}
}

// TestDevTaskOutputBlockRailsBlankLines keeps section spacing inside the command-owned inset.
func TestDevTaskOutputBlockRailsBlankLines(t *testing.T) {
	var output bytes.Buffer
	block := newDevTaskOutputBlock("App about", &output, &output, nil)
	if _, err := io.WriteString(block.stdoutWriter(), "Wire Generated   ✓ present\n\n♦ Network\n"); err != nil {
		t.Fatalf("write inset output: %v", err)
	}
	if _, err := block.finish(true, 22*time.Millisecond); err != nil {
		t.Fatalf("finish output block: %v", err)
	}

	plain := stripANSI(output.String())
	if !strings.Contains(plain, "┃ Wire Generated   ✓ present\n┃\n┃ ♦ Network\n") {
		t.Fatalf("blank section line escaped inset rail: %q", plain)
	}
}

// TestDevTaskOutputBlockStaysTransientWhenSilent preserves the compact loader experience for quiet setup commands.
func TestDevTaskOutputBlockStaysTransientWhenSilent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	loaderStops := 0
	_ = newDevTaskOutputBlock("Quiet setup", &stdout, &stderr, func() {
		loaderStops++
	})

	if loaderStops != 0 {
		t.Fatalf("loader stops = %d, want 0", loaderStops)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("silent task rendered output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// TestGeneratedFrontendInstallBuffersRoutineStdout keeps npm's success summary out of the startup transcript without discarding diagnostics.
func TestGeneratedFrontendInstallBuffersRoutineStdout(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join("cmd", "app", "frontend"), 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	tools := t.TempDir()
	npmPath := filepath.Join(tools, "npm")
	script := "#!/bin/sh\nprintf 'up to date in 309ms\\n'\nprintf 'npm warning\\n' >&2\n"
	if err := os.WriteFile(npmPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	outputTail := newDevTaskOutputTail(40)
	cmd := newDevTaskCommand(
		generatedDevFrontendInstallTask(project.DefaultApp()),
		strings.NewReader(""),
		&stdout,
		&stderr,
		outputTail,
	)
	result, err := cmd.Run()
	if err != nil {
		t.Fatalf("run generated frontend install: %v", err)
	}
	if !result.OK() {
		t.Fatalf("generated frontend install exit code = %d", result.ExitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("routine npm stdout reached transcript: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "npm warning") {
		t.Fatalf("npm stderr was hidden: %q", stderr.String())
	}
	for _, expected := range []string{"up to date in 309ms", "npm warning"} {
		if !strings.Contains(outputTail.String(), expected) {
			t.Fatalf("failure tail omitted %q: %q", expected, outputTail.String())
		}
	}
}

// TestGeneratedFrontendInstallFailureRetainsBufferedStdout proves compact success handling still reports every useful failure stream.
func TestGeneratedFrontendInstallFailureRetainsBufferedStdout(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join("cmd", "app", "frontend"), 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	tools := t.TempDir()
	npmPath := filepath.Join(tools, "npm")
	script := "#!/bin/sh\nprintf 'dependency resolution failed\\n'\nprintf 'npm error registry unavailable\\n' >&2\nexit 7\n"
	if err := os.WriteFile(npmPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runDevTasks([]project.DevTask{generatedDevFrontendInstallTask(project.DefaultApp())})
	if err == nil {
		t.Fatal("expected generated frontend install failure")
	}
	for _, expected := range []string{"failed with exit code 7", "dependency resolution failed", "npm error registry unavailable"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("frontend install error omitted %q: %v", expected, err)
		}
	}
}

// TestCustomizedFrontendInstallKeepsStreamingStdout prevents compact framework setup from changing owner-authored task behavior.
func TestCustomizedFrontendInstallKeepsStreamingStdout(t *testing.T) {
	task := generatedDevFrontendInstallTask(project.DefaultApp())
	task.Cmd = "printf 'owner install output\\n'"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	outputTail := newDevTaskOutputTail(40)
	cmd := newDevTaskCommand(task, strings.NewReader(""), &stdout, &stderr, outputTail)
	result, err := cmd.Run()
	if err != nil {
		t.Fatalf("run customized frontend install: %v", err)
	}
	if !result.OK() {
		t.Fatalf("customized frontend install exit code = %d", result.ExitCode)
	}
	if !strings.Contains(stdout.String(), "owner install output") {
		t.Fatalf("customized frontend stdout was hidden: %q", stdout.String())
	}
}

func TestDevWatchesForAppsExpandsDefaultWatchers(t *testing.T) {
	withConventionalApp(t, "customer-portal")

	watches := []project.DevWatch{
		{
			Name:  "Build App",
			Watch: "-file .go -xfile app/wire/wire_gen\\.go$",
			Exec:  "forj build -o ./bin/app",
		},
		{
			Name:  "Run App",
			Watch: "-file ./bin/app -file .env",
			Exec:  "./bin/app run",
		},
		{
			Name: "Custom",
			Exec: "echo ok",
		},
	}
	got := devWatchesForApps(&project.Config{
		Dev: project.DevConfig{
			Run: map[string]string{
				"app":             "run",
				"customer-portal": "run",
			},
		},
	}, watches)
	if len(got) != 5 {
		t.Fatalf("expected expanded watchers, got %#v", got)
	}
	if got[0].Name != "Build App" || got[0].Exec != "forj build -o ./bin/app" {
		t.Fatalf("expected default build watcher first, got %#v", got[0])
	}
	if got[1].Name != "Build customer-portal" || got[1].Exec != "forj customer-portal build -o ./bin/customer-portal" {
		t.Fatalf("expected named build watcher, got %#v", got[1])
	}
	if !strings.Contains(got[1].Watch, "app/customer-portal/wire/wire_gen\\.go$") {
		t.Fatalf("expected app wire exclusion, got %q", got[1].Watch)
	}
	if got[3].Name != "Run customer-portal" || got[3].Watch != "-file ./bin/.customer-portal.ready -file .env" || got[3].Exec != "./bin/customer-portal run" {
		t.Fatalf("expected named run watcher, got %#v", got[3])
	}
	if got[3].Env["FORJ_APP"] != "customer-portal" || got[3].Env["FORJ_COMMAND_PREFIX"] != "forj customer-portal" {
		t.Fatalf("expected app env, got %#v", got[3].Env)
	}
	if got[4].Name != "Custom" {
		t.Fatalf("expected custom watcher to be preserved, got %#v", got[4])
	}
	if watches[1].Exec != "./bin/app run" {
		t.Fatalf("expected original watches to remain unchanged, got %q", watches[1].Exec)
	}
}

func TestDevWatchesForAppsUsesReadyStampForDefaultRunWatcher(t *testing.T) {
	got := devWatchesForApps(&project.Config{
		Dev: project.DevConfig{
			Run: map[string]string{"app": "run"},
		},
	}, []project.DevWatch{
		{Name: "Run App", Watch: "-file ./bin/app -file bin/app -file .env", Exec: "./bin/app run"},
	})

	if len(got) != 1 {
		t.Fatalf("expected default run watcher, got %#v", got)
	}
	if got[0].Watch != "-file ./bin/.app.ready -file bin/.app.ready -file .env" {
		t.Fatalf("expected ready stamp watch, got %q", got[0].Watch)
	}
	if got[0].Exec != "./bin/app run" {
		t.Fatalf("expected run exec to stay on binary, got %q", got[0].Exec)
	}
}

// TestDevWatchForAppWithConfigDistinguishesAbsentAndEmptyLegacyRun preserves the pre-allowlist model without weakening an explicit empty allowlist.
func TestDevWatchForAppWithConfigDistinguishesAbsentAndEmptyLegacyRun(t *testing.T) {
	t.Parallel()
	watch := project.DevWatch{Name: "Run App", Watch: "-file ./bin/app", Exec: "./bin/app run"}
	tests := []struct {
		name   string
		config *project.Config
		wantOK bool
	}{
		{name: "nil config keeps raw command", wantOK: true},
		{name: "absent allowlist keeps raw command", config: &project.Config{}, wantOK: true},
		{name: "empty allowlist excludes App", config: &project.Config{Dev: project.DevConfig{Run: map[string]string{}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := devWatchForAppWithConfig(test.config, watch, project.DefaultApp())
			if ok != test.wantOK {
				t.Fatalf("devWatchForAppWithConfig() ok = %t, want %t", ok, test.wantOK)
			}
			if ok && (got.Exec != watch.Exec || got.Watch != "-file ./bin/.app.ready") {
				t.Fatalf("legacy Run App watcher changed unexpectedly: %#v", got)
			}
		})
	}
}

func TestNormalizeDevWatchesForRuntimeStopsTemplOutputLoops(t *testing.T) {
	watches := []project.DevWatch{
		{Name: "Build App", Watch: "-file .go -file .templ -xfile app/wire/wire_gen\\.go$ -postpone", Exec: "forj build -o ./bin/app"},
		{Name: "NPM", Watch: "-cd ./cmd/app/frontend -xdir _data -xdir .", Exec: "npm run dev"},
	}
	cfg := &project.Config{
		Render: project.RenderConfig{StarterKit: project.StarterKitTemplHTMX},
	}

	got := normalizeDevWatchesForRuntime(cfg, watches)

	if !strings.Contains(got[0].Watch, ".*_templ\\.go$") {
		t.Fatalf("expected templ build watcher to exclude generated templ go files, got %q", got[0].Watch)
	}
	for _, expected := range []string{"-xdir node_modules", "-xdir dist"} {
		if !strings.Contains(got[1].Watch, expected) {
			t.Fatalf("expected NPM watcher to contain %q, got %q", expected, got[1].Watch)
		}
	}
	if strings.Contains(got[1].Watch, "-xdir .") {
		t.Fatalf("expected NPM watcher to remove wildcard directory exclusion, got %q", got[1].Watch)
	}
	if strings.Contains(watches[1].Watch, "node_modules") || strings.Contains(watches[1].Watch, "dist") {
		t.Fatalf("expected original watches to remain unchanged, got %#v", watches)
	}
}

func TestNormalizeDevWatchesForRuntimeRemovesEnvTriggersFromBuildAndRun(t *testing.T) {
	watches := []project.DevWatch{
		{Name: "Build App", Watch: "-file .go -file .env -file .env.* -postpone", Exec: "forj build -o ./bin/app"},
		{Name: "Run App", Watch: "-file ./bin/app -file .env -file .env.*", Exec: "./bin/app run"},
		{Name: "NPM", Watch: "-file .env -xdir node_modules", Exec: "npm run dev"},
	}

	got := normalizeDevWatchesForRuntime(&project.Config{}, watches)

	if got[0].Watch != "-file .go -postpone" {
		t.Fatalf("expected build watcher env triggers removed, got %q", got[0].Watch)
	}
	if got[1].Watch != "-file ./bin/app" {
		t.Fatalf("expected run watcher env triggers removed, got %q", got[1].Watch)
	}
	if !strings.Contains(got[2].Watch, "-file .env") {
		t.Fatalf("expected unrelated watcher env trigger preserved, got %q", got[2].Watch)
	}
}

func TestDevWatchesForAppsCanScopeToExplicitApp(t *testing.T) {
	t.Setenv("FORJ_APP", "customer-portal")
	got := devWatchesForApps(&project.Config{
		Dev: project.DevConfig{
			Run: map[string]string{"customer-portal": "run"},
		},
	}, []project.DevWatch{
		{Name: "Build App", Watch: "-file .go -xfile app/wire/wire_gen\\.go$", Exec: "forj build -o ./bin/app"},
		{Name: "Run App", Watch: "-file ./bin/app -file .env", Exec: "./bin/app run"},
	})
	if len(got) != 2 {
		t.Fatalf("expected app-scoped watchers, got %#v", got)
	}
	if got[0].Name != "Build customer-portal" || got[1].Name != "Run customer-portal" {
		t.Fatalf("expected app-scoped watcher names, got %#v", got)
	}
}

func TestDevWatchesForAppsUsesDevRunCommand(t *testing.T) {
	withConventionalApp(t, "billing")
	got := devWatchesForApps(&project.Config{
		Dev: project.DevConfig{
			Run: map[string]string{
				"app":     "run",
				"billing": "sync --once",
			},
		},
	}, []project.DevWatch{
		{Name: "Run App", Watch: "-file ./bin/app", Exec: "./bin/app run"},
	})

	if len(got) != 2 {
		t.Fatalf("expected default and billing run watchers, got %#v", got)
	}
	if got[1].Name != "Run billing" || got[1].Exec != "./bin/billing sync --once" {
		t.Fatalf("expected custom billing dev run command, got %#v", got[1])
	}
}

func TestDevWatchesForAppsSkipsAppsMissingFromDevRun(t *testing.T) {
	withConventionalApp(t, "billing")
	got := devWatchesForApps(&project.Config{
		Dev: project.DevConfig{
			Run: map[string]string{
				"app": "run",
			},
		},
	}, []project.DevWatch{
		{Name: "Run App", Watch: "-file ./bin/app", Exec: "./bin/app run"},
	})

	if len(got) != 1 {
		t.Fatalf("expected disabled billing dev run to be omitted, got %#v", got)
	}
	if got[0].Name != "Run App" {
		t.Fatalf("expected default app run watcher to remain, got %#v", got)
	}
}

func TestDevWatchesForAppsDropsRemovedConventionalApp(t *testing.T) {
	withConventionalApp(t, "billing")
	base := []project.DevWatch{
		{Name: "Build App", Watch: "-file .go", Exec: "forj build -o ./bin/app"},
		{Name: "Run App", Watch: "-file ./bin/app", Exec: "./bin/app run"},
	}
	config := &project.Config{
		Dev: project.DevConfig{
			Run: map[string]string{"app": "run", "billing": "run"},
		},
	}
	if got := devWatchesForApps(config, base); len(got) != 4 {
		t.Fatalf("expected billing watchers before removal, got %#v", got)
	}
	if err := os.RemoveAll(filepath.Join("cmd", "billing")); err != nil {
		t.Fatalf("remove billing app: %v", err)
	}

	got := devWatchesForApps(config, base)
	if len(got) != 2 {
		t.Fatalf("expected only default watchers after removal, got %#v", got)
	}
	for _, watch := range got {
		if strings.Contains(watch.Name, "billing") || strings.Contains(watch.Exec, "billing") {
			t.Fatalf("did not expect stale billing watcher after removal: %#v", got)
		}
	}
}

func TestDevBuildJobsBuildEveryApp(t *testing.T) {
	withConventionalApp(t, "customer-portal")

	jobs := devBuildJobs(&project.Config{
		Dev: project.DevConfig{
			Watches: []project.DevWatch{
				{Name: "Build App", Exec: "FORJ_BUILD=1 forj build --race -o ./bin/app"},
			},
		},
	})
	if len(jobs) != 2 {
		t.Fatalf("devBuildJobs() = %#v, want two active App builds", jobs)
	}
	got := []string{jobs[0].command, jobs[1].command}
	want := []string{"FORJ_BUILD=1 forj build --race -o ./bin/app", "FORJ_BUILD=1 forj customer-portal build --race -o ./bin/customer-portal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected dev build commands: got %#v want %#v", got, want)
	}
}

func TestDevBuildJobsIncludeAppsWithExistingBinaries(t *testing.T) {
	withConventionalApp(t, "customer-portal")
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join("bin", "app"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write app binary: %v", err)
	}

	jobs := devBuildJobs(&project.Config{})
	if len(jobs) != 2 {
		t.Fatalf("devBuildJobs() = %#v, want existing and missing App binaries rebuilt", jobs)
	}
	got := []string{jobs[0].command, jobs[1].command}
	want := []string{"forj build -o ./bin/app", "forj customer-portal build -o ./bin/customer-portal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected initial dev build commands: got %#v want %#v", got, want)
	}
}

func TestDevBuildCommandForAppRewritesExistingPackageArgument(t *testing.T) {
	app := project.DefaultNamedApp("billing")
	got := devBuildCommandForApp("forj build --tags dev -o ./bin/app ./cmd/app", app)
	want := "forj billing build --tags dev -o ./bin/billing"
	if got != want {
		t.Fatalf("app build command = %q, want %q", got, want)
	}
}

func TestDevBuildJobsKeepAppLabels(t *testing.T) {
	withConventionalApp(t, "billing")

	got := devBuildJobs(&project.Config{})
	if len(got) != 2 {
		t.Fatalf("expected default and billing build jobs, got %#v", got)
	}
	if got[0].app.Name != "app" || got[1].app.Name != "billing" {
		t.Fatalf("unexpected build job apps: %#v", got)
	}
}

func TestDevDatabasesForAppsIncludesNamedAppDatabase(t *testing.T) {
	withConventionalApp(t, "billing")
	t.Setenv("DB_DRIVER", "mysql")
	t.Setenv("DB_DATABASE", "db")
	t.Setenv("BILLING_DB_DATABASE", "billing")

	got, err := devDatabasesForApps(&project.Config{
		Render: project.RenderConfig{
			Components: project.Components{DatabaseMySQL: true},
		},
		Apps: map[string]project.AppConfig{
			"billing": {
				Components: project.Components{DatabaseMySQL: true},
			},
		},
	}, activeDevApps())
	if err != nil {
		t.Fatalf("devDatabasesForApps returned error: %v", err)
	}
	want := []devDatabase{
		{App: "billing", Driver: "mysql", Name: "billing"},
		{App: "app", Driver: "mysql", Name: "db"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dev databases = %#v, want %#v", got, want)
	}
}

func TestDevDatabasesForAppsSupportsMixedDrivers(t *testing.T) {
	withConventionalApp(t, "reporting")
	t.Setenv("DB_DRIVER", "mysql")
	t.Setenv("DB_DATABASE", "db")
	t.Setenv("REPORTING_DB_DRIVER", "postgres")
	t.Setenv("REPORTING_DB_DATABASE", "reporting")

	got, err := devDatabasesForApps(&project.Config{
		Render: project.RenderConfig{
			Components: project.Components{DatabaseMySQL: true, DatabasePostgres: true},
		},
		Apps: map[string]project.AppConfig{
			"reporting": {
				Components: project.Components{DatabasePostgres: true},
			},
		},
	}, activeDevApps())
	if err != nil {
		t.Fatalf("devDatabasesForApps returned error: %v", err)
	}
	want := []devDatabase{
		{App: "app", Driver: "mysql", Name: "db"},
		{App: "reporting", Driver: "postgres", Name: "reporting"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dev databases = %#v, want %#v", got, want)
	}
}

func TestDevDatabasesForAppsRejectsUnsafeDatabaseNames(t *testing.T) {
	withConventionalApp(t, "billing")
	t.Setenv("DB_DRIVER", "mysql")
	t.Setenv("DB_DATABASE", "db")
	t.Setenv("BILLING_DB_DATABASE", "billing-prod")

	_, err := devDatabasesForApps(&project.Config{
		Render: project.RenderConfig{
			Components: project.Components{DatabaseMySQL: true},
		},
		Apps: map[string]project.AppConfig{
			"billing": {
				Components: project.Components{DatabaseMySQL: true},
			},
		},
	}, activeDevApps())
	if err == nil {
		t.Fatal("expected unsafe database name to return an error")
	}
	if !strings.Contains(err.Error(), "BILLING_DB_DATABASE") {
		t.Fatalf("expected app env key in error, got %v", err)
	}
}

func TestShouldRunDevAutoMigrateUsesNamedAppDatabase(t *testing.T) {
	withConventionalApp(t, "billing")

	if !shouldRunDevAutoMigrate(&project.Config{
		Dev: project.DevConfig{AutoMigrate: true},
		Apps: map[string]project.AppConfig{
			"billing": {
				Components: project.Components{DatabaseMySQL: true},
			},
		},
	}) {
		t.Fatal("expected named app database component to require auto-migrate")
	}
}

func TestDevAppWatcherDetectsNewConventionalApp(t *testing.T) {
	root := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	triggered := make(chan struct{}, 1)
	stop := startDevAppWatcher(ctx, func() {
		select {
		case triggered <- struct{}{}:
		default:
		}
	}, 10*time.Millisecond)
	defer stop()

	appMain := filepath.Join("cmd", "billing", "main.go")
	if err := os.MkdirAll(filepath.Dir(appMain), 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(appMain, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write app main: %v", err)
	}

	select {
	case <-triggered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected app watcher to trigger after new app appears")
	}
}

func TestDevAppsChangedComparesAppNames(t *testing.T) {
	prev := devAppFingerprint{names: []string{"app"}}
	current := devAppFingerprint{names: []string{"app", "billing"}}
	if !devAppsChanged(prev, current) {
		t.Fatal("expected added app to be detected")
	}
	if devAppsChanged(current, devAppFingerprint{names: []string{"app", "billing"}}) {
		t.Fatal("expected matching app snapshots to be unchanged")
	}
}

func TestCreateDatabaseScriptsIncludeAllDatabases(t *testing.T) {
	mysqlScript := mysqlCreateDatabasesScript([]string{"billing", "db"})
	for _, want := range []string{
		`mariadb-admin ping`,
		`mariadb -h "mysql"`,
		`for db in billing db`,
		"CREATE DATABASE IF NOT EXISTS \\`$db\\`;",
		"GRANT ALL PRIVILEGES ON \\`$db\\`.* TO '$MARIADB_USER'@'%';",
		`FLUSH PRIVILEGES;`,
	} {
		if !strings.Contains(mysqlScript, want) {
			t.Fatalf("mysql script missing %q:\n%s", want, mysqlScript)
		}
	}

	postgresScript := postgresCreateDatabasesScript([]string{"app", "reporting"})
	for _, want := range []string{
		`export PGPASSWORD="$POSTGRES_PASSWORD"`,
		`pg_isready`,
		`for db in app reporting`,
		`CREATE DATABASE \"$db\";`,
	} {
		if !strings.Contains(postgresScript, want) {
			t.Fatalf("postgres script missing %q:\n%s", want, postgresScript)
		}
	}
}

func TestDevAutoMigrateUsesUnqualifiedFrameworkPrefix(t *testing.T) {
	if got := devAutoMigrateShellCommand(&project.Config{}); got != "./bin/app migrate" {
		t.Fatalf("auto-migrate command = %q, want ./bin/app migrate", got)
	}
	if got := devAutoMigrateEnv()["FORJ_COMMAND_PREFIX"]; got != "forj" {
		t.Fatalf("auto-migrate prefix = %q, want forj", got)
	}
}

func TestDevAutoMigrateKeepsExplicitAppBinary(t *testing.T) {
	t.Setenv("FORJ_APP", "billing")

	if got := devAutoMigrateShellCommand(&project.Config{}); got != "./bin/billing migrate" {
		t.Fatalf("auto-migrate command = %q, want ./bin/billing migrate", got)
	}
	if got := devAutoMigrateEnv()["FORJ_COMMAND_PREFIX"]; got != "forj" {
		t.Fatalf("auto-migrate prefix = %q, want forj", got)
	}
}

// TestDevAutoMigrateUsesNamedStructuredParticipant avoids launching an omitted default App in sparse native config.
func TestDevAutoMigrateUsesNamedStructuredParticipant(t *testing.T) {
	t.Parallel()
	config := &project.Config{
		Dev: project.DevConfig{Apps: map[string]project.DevApp{"billing": {}}},
		Apps: map[string]project.AppConfig{
			"billing": {Components: project.Components{DatabaseSQLite: true}},
		},
	}
	if got := devAutoMigrateShellCommand(config); got != "./bin/billing migrate" {
		t.Fatalf("auto-migrate command = %q, want named participating binary", got)
	}
}

func TestRunDevRenderUsesTimingsFlag(t *testing.T) {
	toolsDir := t.TempDir()
	logPath := filepath.Join(toolsDir, "forj-args.log")
	forjPath := filepath.Join(toolsDir, "forj")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\nexit 0\n"
	if err := os.WriteFile(forjPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake forj: %v", err)
	}
	t.Setenv("PATH", toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	config := &project.Config{
		Dev: project.DevConfig{
			Watches: []project.DevWatch{
				{Name: "Build App", Exec: "forj build -o ./bin/app"},
			},
		},
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runDevRender(config, &out, &errOut); err != nil {
		t.Fatalf("runDevRender returned error: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake forj log: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "render --timings\n") {
		t.Fatalf("expected dev render to pass --timings, got:\n%s", got)
	}
	if !strings.Contains(got, "build -o ./bin/app\n") {
		t.Fatalf("expected dev render to build after render, got:\n%s", got)
	}
}

func TestRenderTimingLinesStreamInsideDevCommand(t *testing.T) {
	t.Setenv("FORJ_COMMAND_ORIGIN", "dev_command")

	renderer := NewProjectRenderer(nil)
	renderer.SetTimings(true)
	before := renderer.stats.counts()
	renderer.stats.recordSkipped("go.mod")

	out := captureStdout(t, func() {
		renderer.printStepSummary("Go Module Initialization", before, 12*time.Millisecond)
	})

	if !strings.Contains(out, "Go Module Initialization") || !strings.Contains(out, "12ms") {
		t.Fatalf("expected timed render phase to stream, got %q", out)
	}
	if len(renderer.lines) != 0 {
		t.Fatalf("expected streamed render line to be removed from buffered summary, got %#v", renderer.lines)
	}
}

func TestRunDevBuildRunsAppsInParallel(t *testing.T) {
	jobs := []devBuildJob{{app: project.AppForName("app")}, {app: project.AppForName("billing")}}
	entered := make(chan string, len(jobs))
	release := make(chan struct{})
	done := make(chan []devBuildResult, 1)
	go func() {
		done <- runDevBuildJobsConcurrently(jobs, func(job devBuildJob) devBuildResult {
			entered <- job.app.Name
			<-release
			return devBuildResult{job: job}
		})
	}()

	seen := make(map[string]bool, len(jobs))
	for range jobs {
		select {
		case app := <-entered:
			seen[app] = true
		case <-time.After(time.Second):
			close(release)
			t.Fatal("App builds did not enter the concurrent execution phase")
		}
	}
	close(release)
	results := <-done
	if !seen["app"] || !seen["billing"] {
		t.Fatalf("concurrent App builds = %#v, want app and billing", seen)
	}
	if results[0].job.app.Name != "app" || results[1].job.app.Name != "billing" {
		t.Fatalf("result order = %q, %q", results[0].job.app.Name, results[1].job.app.Name)
	}
}

// TestRunDevBuildWavePreparesEveryAppBeforeConcurrentCompilation verifies fanout starts from one stable snapshot.
func TestRunDevBuildWavePreparesEveryAppBeforeConcurrentCompilation(t *testing.T) {
	jobs := []devBuildJob{
		{app: project.AppForName("app"), phased: true},
		{app: project.AppForName("billing"), phased: true},
	}
	var mu sync.Mutex
	calls := make([]string, 0, len(jobs)*2)
	compileEntered := make(chan string, len(jobs))
	releaseCompile := make(chan struct{})
	done := make(chan []devBuildResult, 1)
	go func() {
		done <- runDevBuildWave(jobs, func(job devBuildJob, phase string, _ bool) devBuildResult {
			mu.Lock()
			calls = append(calls, phase+":"+job.app.Name)
			mu.Unlock()
			if phase == build.DevBuildPhaseCompile {
				compileEntered <- job.app.Name
				<-releaseCompile
			}
			return devBuildResult{job: job}
		})
	}()

	for range jobs {
		select {
		case <-compileEntered:
		case <-time.After(time.Second):
			close(releaseCompile)
			t.Fatal("prepared Apps did not enter concurrent compilation")
		}
	}
	close(releaseCompile)
	<-done
	mu.Lock()
	defer mu.Unlock()
	wantPreparation := []string{
		build.DevBuildPhasePrepare + ":app",
		build.DevBuildPhasePrepare + ":billing",
	}
	if !reflect.DeepEqual(calls[:len(wantPreparation)], wantPreparation) {
		t.Fatalf("build wave started before preparation completed: got %q, want prefix %q", calls, wantPreparation)
	}
}

// TestRunDevBuildWaveKeepsCustomCommandsSinglePhase avoids guessing whether an arbitrary command mutates project inputs.
func TestRunDevBuildWaveKeepsCustomCommandsSinglePhase(t *testing.T) {
	job := devBuildJob{app: project.AppForName("app"), command: "make app", phased: false}
	calls := 0
	results := runDevBuildWave([]devBuildJob{job}, func(got devBuildJob, phase string, publish bool) devBuildResult {
		calls++
		if phase != "" || !publish {
			t.Fatalf("custom build phase = %q, publish = %t; want one publishing phase", phase, publish)
		}
		return devBuildResult{job: got}
	})
	if calls != 1 || len(results) != 1 || results[0].err != nil {
		t.Fatalf("custom build calls = %d, results = %#v", calls, results)
	}
}

// TestIsManagedDevBuildCommand limits the private two-phase protocol to GoForj's conventional build command.
func TestIsManagedDevBuildCommand(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"forj build -o ./bin/app":           true,
		"/usr/local/bin/forj admin build":   true,
		"forj statuspage build --tags prod": true,
		"make app":                          false,
		"sh -c 'forj build'":                false,
		"forj dev":                          false,
	}
	for command, want := range tests {
		if got := isManagedDevBuildCommand(command); got != want {
			t.Errorf("isManagedDevBuildCommand(%q) = %t, want %t", command, got, want)
		}
	}
}

// TestRunDevBuildReplaysSingleAppFailureOutput keeps compiler diagnostics attached to the durable dev transcript.
func TestRunDevBuildReplaysSingleAppFailureOutput(t *testing.T) {
	config := &project.Config{
		Dev: project.DevConfig{
			Watches: []project.DevWatch{
				{Name: "Build App", Exec: `printf 'cmd/app/main.go:12: undefined: missingService\n' >&2; exit 7`},
			},
		},
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runDevBuild(config, &out, &errOut)
	if err == nil {
		t.Fatal("expected runDevBuild to fail")
	}
	if !strings.Contains(out.String(), "Build failed for app") {
		t.Fatalf("failure heading = %q, want default App context", out.String())
	}
	if !strings.Contains(errOut.String(), "cmd/app/main.go:12: undefined: missingService") {
		t.Fatalf("compiler diagnostics = %q, want buffered failure output", errOut.String())
	}
	if got := strings.Count(err.Error(), "forj build failed"); got != 1 {
		t.Fatalf("failure prefix count = %d in %q, want one", got, err)
	}
}

// shellQuote centralizes shell quote behavior so callers follow the same contract.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// captureStdout centralizes capture stdout behavior so callers follow the same contract.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(data)
}

func TestRunDevBuildBuffersFailureOutputByApp(t *testing.T) {
	withConventionalApp(t, "billing")

	config := &project.Config{
		Dev: project.DevConfig{
			Watches: []project.DevWatch{
				{Name: "Build App", Exec: `test ./bin/app = ./bin/billing && echo billing failed >&2 && exit 7 || echo app ok`},
			},
		},
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runDevBuild(config, &out, &errOut)
	if err == nil {
		t.Fatal("expected runDevBuild to fail")
	}
	if !strings.Contains(err.Error(), "billing") {
		t.Fatalf("expected app name in error, got %v", err)
	}
	if !strings.Contains(out.String(), "Build failed for billing") {
		t.Fatalf("expected app failure heading, got stdout %q", out.String())
	}
	if !strings.Contains(errOut.String(), "billing failed") {
		t.Fatalf("expected buffered stderr, got %q", errOut.String())
	}
}

func TestRunDevBuildKeepsMultiAppSuccessTranscriptCompact(t *testing.T) {
	withConventionalApp(t, "billing")

	config := &project.Config{
		Dev: project.DevConfig{
			Watches: []project.DevWatch{
				{Name: "Build App", Exec: "true"},
			},
		},
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := runDevBuild(config, &out, &errOut); err != nil {
		t.Fatalf("runDevBuild returned error: %v", err)
	}
	text := out.String()
	if strings.Contains(text, "Building app") || strings.Contains(text, "Building billing") {
		t.Fatalf("expected compact multi-app build transcript, got stdout %q", text)
	}
	if strings.Contains(text, "Built billing") {
		t.Fatalf("expected no per-app success timing lines, got stdout %q", text)
	}
	if !strings.Contains(text, stripANSI(console.SuccessMark())+" Build      · ") {
		t.Fatalf("expected aggregate build timing, got stdout %q", text)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", errOut.String())
	}
}

// TestRunDevPreflightBuildReportsSuccessOnlyOutsideCompactRestarts preserves plain-stream feedback without duplicating a grouped reconciliation result.
func TestRunDevPreflightBuildReportsSuccessOnlyOutsideCompactRestarts(t *testing.T) {
	config := &project.Config{Dev: project.DevConfig{Watches: []project.DevWatch{
		{Name: "Build App", Exec: "true"},
	}}}

	t.Run("plain output", func(t *testing.T) {
		withConventionalApp(t, project.DefaultAppName)
		var output bytes.Buffer
		if err := runDevPreflightBuild(config, &output, &output); err != nil {
			t.Fatalf("run preflight build: %v", err)
		}
		if !strings.Contains(stripANSI(output.String()), stripANSI(console.SuccessMark())+" Build") {
			t.Fatalf("plain preflight omitted success: %q", output.String())
		}
	})

	t.Run("compact restart", func(t *testing.T) {
		withConventionalApp(t, project.DefaultAppName)
		output := &devLifecycleOrchestrationWriter{compact: true}
		if err := runDevPreflightBuild(config, output, output); err != nil {
			t.Fatalf("run preflight build: %v", err)
		}
		if output.Len() != 0 {
			t.Fatalf("compact preflight duplicated successful build result: %q", output.String())
		}
	})
}

func TestRunDevBuildDoesNotReplayProgressMarkersOnFailure(t *testing.T) {
	withConventionalApp(t, "billing")

	config := &project.Config{
		Dev: project.DevConfig{
			Watches: []project.DevWatch{
				{Name: "Build App", Exec: `test ./bin/app = ./bin/billing && printf "__FORJ_BUILD_PROGRESS__ step 3/4 build:api-index\nreal failure\n" >&2 && exit 7 || true`},
			},
		},
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err := runDevBuild(config, &out, &errOut)
	if err == nil {
		t.Fatal("expected runDevBuild to fail")
	}
	if strings.Contains(out.String(), buildProgressMarker) || strings.Contains(errOut.String(), buildProgressMarker) {
		t.Fatalf("expected progress markers to be stripped, stdout %q stderr %q", out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "real failure") {
		t.Fatalf("expected non-protocol failure output to remain, got %q", errOut.String())
	}
}

func TestWriteDevAppBuildLineSkipsSingleDefaultApp(t *testing.T) {
	var out bytes.Buffer
	writeDevAppBuildLine(&out, []project.App{project.DefaultApp()})
	if out.Len() != 0 {
		t.Fatalf("expected no app line for default-only dev, got %q", out.String())
	}
}

func TestWriteDevAppBuildLineShowsExpandedApps(t *testing.T) {
	var out bytes.Buffer
	writeDevAppBuildLine(&out, []project.App{
		project.DefaultApp(),
		project.DefaultNamedApp("billing"),
	})
	if !contains(out.String(), "Building apps: app, billing") {
		t.Fatalf("expected app build note, got %q", out.String())
	}
}

func TestDevRuntimeWatcherAppsReturnsRunApps(t *testing.T) {
	got := devRuntimeWatcherApps([]project.DevWatch{
		{Name: "Build App"},
		{Name: "Run App"},
		{Name: "Build billing"},
		{Name: "Run billing"},
	})
	want := []string{"app", "billing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime watcher apps = %#v, want %#v", got, want)
	}
}

func TestDevAppNamesUsesActiveAppsOnly(t *testing.T) {
	got := devAppNames([]project.App{project.DefaultApp(), project.DefaultApp()})
	want := []string{"app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active app names = %#v, want %#v", got, want)
	}
}

func TestDecorateDevAppLogAppColumnAddsAppAfterTimestamp(t *testing.T) {
	line := "23:35:28.370 HTTP         Starting HTTP server"
	got := stripANSI(decorateDevAppLogAppColumn(line, "billing", len("billing"), true))
	want := "23:35:28.370 billing HTTP         Starting HTTP server"
	if got != want {
		t.Fatalf("decorated log line = %q, want %q", got, want)
	}
}

func TestDecorateDevAppLogAppColumnSkipsSingleAppMode(t *testing.T) {
	line := "23:35:28.370 HTTP         Starting HTTP server"
	got := decorateDevAppLogAppColumn(line, "app", len("app"), false)
	if got != line {
		t.Fatalf("expected single-app log line to remain unchanged, got %q", got)
	}
}

func TestDecorateDevAppLogAppColumnHandlesColoredTimestamp(t *testing.T) {
	line := "\x1b[90m23:35:28.370\x1b[0m HTTP         Starting HTTP server"
	got := stripANSI(decorateDevAppLogAppColumn(line, "app", len("billing"), true))
	want := "23:35:28.370 app     HTTP         Starting HTTP server"
	if got != want {
		t.Fatalf("decorated colored log line = %q, want %q", got, want)
	}
}

func TestDefaultDevAppColorDiffersFromTimestampGray(t *testing.T) {
	if devDefaultAppColor == console.ColorGray {
		t.Fatal("expected default app color to differ from timestamp gray")
	}
}

// withConventionalApp centralizes with conventional app behavior so callers follow the same contract.
func withConventionalApp(t *testing.T, name string) {
	t.Helper()
	root := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	appMain := filepath.Join("cmd", name, "main.go")
	if err := os.MkdirAll(filepath.Dir(appMain), 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	if err := os.WriteFile(appMain, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write app main: %v", err)
	}
}

func TestSplitWatcherEnvAssignments(t *testing.T) {
	env, cmd := splitWatcherEnvAssignments("FEATURE_FLAG=1 FOO=bar my-command --verbose")
	if env["FEATURE_FLAG"] != "1" || env["FOO"] != "bar" {
		t.Fatalf("expected leading env assignments to be extracted, got %#v", env)
	}
	if cmd != "my-command --verbose" {
		t.Fatalf("expected remaining command to be preserved, got %q", cmd)
	}
}

// TestSplitWatcherEnvAssignmentsPreservesShellSyntax verifies extracting simple
// prefixes never rewrites quoting or substitutions owned by the shell.
func TestSplitWatcherEnvAssignmentsPreservesShellSyntax(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantEnv map[string]string
		wantCmd string
	}{
		{
			name:    "quoted command argument",
			command: `MODE=dev my-command --label="hello  world"`,
			wantEnv: map[string]string{"MODE": "dev"},
			wantCmd: `my-command --label="hello  world"`,
		},
		{
			name:    "quoted assignment",
			command: `MODE="hello world" my-command`,
			wantCmd: `MODE="hello world" my-command`,
		},
		{
			name:    "substituted assignment after simple prefix",
			command: `MODE=dev TARGET="$WATCH_TARGET" my-command`,
			wantEnv: map[string]string{"MODE": "dev"},
			wantCmd: `TARGET="$WATCH_TARGET" my-command`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotEnv, gotCmd := splitWatcherEnvAssignments(test.command)
			if !reflect.DeepEqual(gotEnv, test.wantEnv) {
				t.Fatalf("splitWatcherEnvAssignments() env = %#v, want %#v", gotEnv, test.wantEnv)
			}
			if gotCmd != test.wantCmd {
				t.Fatalf("splitWatcherEnvAssignments() command = %q, want %q", gotCmd, test.wantCmd)
			}
		})
	}
}

func TestShellSplitArgsPreservesQuotedFragments(t *testing.T) {
	args, err := shellSplitArgs(`-file .env.* -xfile "wire/wire_gen\.go$" -xdir '_data'`)
	if err != nil {
		t.Fatalf("shellSplitArgs returned error: %v", err)
	}
	want := []string{"-file", ".env.*", "-xfile", `wire/wire_gen\.go$`, "-xdir", "_data"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args: got %#v want %#v", args, want)
	}
}

func TestCopyDevWatchesClonesEnvironment(t *testing.T) {
	original := []project.DevWatch{{Env: map[string]string{"FOO": "bar"}}}
	copied := copyDevWatches(original)
	copied[0].Env["FOO"] = "baz"

	if original[0].Env["FOO"] != "bar" {
		t.Fatalf("copyDevWatches() mutated the configured environment: %#v", original[0].Env)
	}
}

func TestContainsErrorWordTreatsSelectorCompileErrorsAsBuildErrors(t *testing.T) {
	line := `internal/hello/controller.go:12:2: use of package caches not in selector`
	if !containsErrorWord(line) {
		t.Fatalf("expected selector compile error to trigger watcher error alert")
	}
}

func TestContainsErrorWordTreatsBuildAppCommandFailuresAsBuildErrors(t *testing.T) {
	line := `Test › Build App › Error executing command error="go build: exit status 1 (# test/wire\nwire/wire_gen.go:83:66: too many arguments in call to hello.NewController)"`
	if !containsErrorWord(line) {
		t.Fatalf("expected build app command failure to trigger watcher error alert")
	}
}

func TestFormatWatcherLifecycleLine(t *testing.T) {
	line := formatWatcherLifecycleLine("API", watcherStateStarted)
	if !contains(line, "API") {
		t.Fatalf("expected watcher name in lifecycle line: %q", line)
	}
	if !contains(line, "started") {
		t.Fatalf("expected state in lifecycle line: %q", line)
	}
	if !contains(line, console.SuccessMark()) {
		t.Fatalf("expected success mark in started lifecycle line: %q", line)
	}
}

func TestFormatWatcherLifecycleSummary(t *testing.T) {
	line := formatWatcherLifecycleSummary([]string{"Build App", "Wire", "API"}, watcherStateStarted)
	if !contains(line, "Watchers") {
		t.Fatalf("expected watcher summary label in lifecycle line: %q", line)
	}
	if !contains(line, "started") {
		t.Fatalf("expected started state in summary line: %q", line)
	}
	if !contains(line, "Build App, Wire, API") {
		t.Fatalf("expected watcher names in summary line: %q", line)
	}
	if !contains(line, console.SuccessMark()) {
		t.Fatalf("expected success mark in started summary line: %q", line)
	}
}

// TestDevWatcherRuntimeDrainExitsEmitsStoppedLines keeps unexpected exits individually attributable.
func TestDevWatcherRuntimeDrainExitsEmitsStoppedLines(t *testing.T) {
	var out bytes.Buffer
	exitCh := make(chan watcherExit, 2)
	exitCh <- watcherExit{name: "API"}
	exitCh <- watcherExit{name: "Scheduler"}
	runtime := &devWatcherRuntime{
		session: &devWatchSession{outWriter: &out},
		exitCh:  exitCh,
	}

	runtime.drainExits(2, false)

	got := out.String()
	if !contains(got, "API") || !contains(got, "Scheduler") {
		t.Fatalf("expected watcher names in stopped output, got %q", got)
	}
	if strings.Count(got, "stopped") != 2 {
		t.Fatalf("expected two stopped markers, got %q", got)
	}
}

// TestDevWatcherRuntimeDrainExitsEmitsStoppedSummaryWhenCollapsed keeps coordinated shutdown output compact.
func TestDevWatcherRuntimeDrainExitsEmitsStoppedSummaryWhenCollapsed(t *testing.T) {
	var out bytes.Buffer
	exitCh := make(chan watcherExit, 2)
	exitCh <- watcherExit{name: "API"}
	exitCh <- watcherExit{name: "Scheduler"}
	runtime := &devWatcherRuntime{
		session: &devWatchSession{outWriter: &out},
		exitCh:  exitCh,
	}

	runtime.drainExits(2, true)

	got := out.String()
	if !contains(got, "Watchers") {
		t.Fatalf("expected watcher summary label in stopped output, got %q", got)
	}
	if !contains(got, "API, Scheduler") {
		t.Fatalf("expected watcher names in stopped summary, got %q", got)
	}
	if strings.Count(got, "stopped") != 1 {
		t.Fatalf("expected one stopped summary line, got %q", got)
	}
}

// TestStartDevWatcherRuntimeOwnsEmptyGeneration keeps the controller dependency required even when configuration has no logical watchers.
func TestStartDevWatcherRuntimeOwnsEmptyGeneration(t *testing.T) {
	runtime, err := startDevWatcherRuntime(&devWatchSession{
		config: &project.Config{}, outWriter: io.Discard, errWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("start empty watcher generation: %v", err)
	}
	if runtime.controller == nil {
		t.Fatal("empty watcher generation did not own a controller")
	}
	if len(runtime.watchers) != 0 || runtime.exitCh == nil {
		t.Fatalf("empty watcher generation = %#v, want no logical handles and an owned exit stream", runtime)
	}

	runtime.stopAndDrain(true)
}

// TestDevWatcherRuntimeStopEmitsStoppingSummaryWhenCollapsed keeps one lifecycle line for coordinated shutdowns.
func TestDevWatcherRuntimeStopEmitsStoppingSummaryWhenCollapsed(t *testing.T) {
	var out bytes.Buffer
	runtime := newDormantDevWatcherRuntime(t, &out, "Build App", "Wire", "API")

	runtime.stopAndDrain(true)

	got := out.String()
	if !contains(got, "Watchers") {
		t.Fatalf("expected watcher summary label in stopping output, got %q", got)
	}
	if !contains(got, "stopping") {
		t.Fatalf("expected stopping state in summary line, got %q", got)
	}
	if !contains(got, "Build App, Wire, API") {
		t.Fatalf("expected watcher names in stopping summary, got %q", got)
	}
	if strings.Count(got, "stopping") != 1 {
		t.Fatalf("expected one stopping summary line, got %q", got)
	}
}

// TestDevWatcherRuntimeBeginStopReturnsBeforeControllerExit preserves render work that overlaps controller shutdown.
func TestDevWatcherRuntimeBeginStopReturnsBeforeControllerExit(t *testing.T) {
	var output devOutputControllerRecorder
	runtime := newDormantDevWatcherRuntime(t, &output, "App")
	releaseShutdown := make(chan struct{})
	runtime.controller.wait.Add(1)
	go func() {
		<-releaseShutdown
		runtime.controller.wait.Done()
	}()

	returned := make(chan func(), 1)
	go func() {
		returned <- runtime.beginStop(2*time.Second, true)
	}()
	var waitForStop func()
	select {
	case waitForStop = <-returned:
		if len(output.transitions) != 1 {
			t.Fatalf("active shutdown transitions = %#v, want one", output.transitions)
		}
	case <-time.After(2 * time.Second):
		close(releaseShutdown)
		t.Fatal("beginStop waited for controller shutdown")
	}
	close(releaseShutdown)
	waitForStop()
	if len(output.transitions) != 0 || len(output.transitionEnds) != 1 {
		t.Fatalf("completed shutdown transitions = %#v ends=%#v, want none and one release", output.transitions, output.transitionEnds)
	}
	runtime.drainAllExits(true)
}

// TestDevWatcherRuntimeStopAfterExitReportsEveryLogicalHandle keeps one unexpected exit attributable while the shared generation shuts down.
func TestDevWatcherRuntimeStopAfterExitReportsEveryLogicalHandle(t *testing.T) {
	var out bytes.Buffer
	runtime := newDormantDevWatcherRuntime(t, &out, "API", "Scheduler")
	runtime.controller.publishExit("API", "API", &devwatch.Exit{Name: "API", ExitCode: 7}, nil)
	exit := <-runtime.exitCh

	runtime.stopAfterExit(exit, time.Second)

	got := out.String()
	if !contains(got, "API") || !contains(got, "Scheduler") {
		t.Fatalf("expected every watcher name in lifecycle output, got %q", got)
	}
	if strings.Count(got, "stopping") != 1 || strings.Count(got, "stopped") != 2 {
		t.Fatalf("unexpected single-exit lifecycle output %q", got)
	}
}

// newDormantDevWatcherRuntime creates one native generation whose logical handles can be tested without starting child processes.
func newDormantDevWatcherRuntime(t *testing.T, out io.Writer, names ...string) *devWatcherRuntime {
	t.Helper()
	compiled := make([]devCompiledWatcher, 0, len(names))
	watchers := make([]runningWatcher, 0, len(names))
	for _, name := range names {
		compiled = append(compiled, devCompiledWatcher{
			ID: name, Name: name, Kind: devWatcherCustom, Postpone: true,
			Command: devwatch.Command{Shell: "exit 0"},
		})
		watchers = append(watchers, runningWatcher{id: name, name: name})
	}
	controller := newDevWatcherRunnerTestController(t, compiled)
	t.Cleanup(func() {
		controller.stop(time.Second)
	})
	return &devWatcherRuntime{
		session:    &devWatchSession{outWriter: out},
		watchers:   watchers,
		controller: controller,
		exitCh:     controller.exitCh,
	}
}

func TestDecorateWatcherLineFormatsTriggerAsStarting(t *testing.T) {
	timestamp := time.Date(2026, time.August, 18, 20, 8, 57, 605_000_000, time.UTC)
	tests := []struct {
		name    string
		watcher string
		want    string
	}{
		{name: "default App", watcher: "Run App", want: "20:08:57.605 App          Starting"},
		{name: "named App", watcher: "Run billing", want: "20:08:57.605 App          Starting"},
		{name: "custom watcher", watcher: "API", want: "20:08:57.605 API          Starting"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := stripANSI(decorateWatcherLine(watcherTriggerMarker, test.watcher, timestamp)); got != test.want {
				t.Fatalf("decorateWatcherLine() = %q, want %q", got, test.want)
			}
		})
	}
	namedAppLine := decorateWatcherLine(watcherTriggerMarker, "Run billing", timestamp)
	namedAppLine = decorateDevAppLogAppColumn(namedAppLine, "billing", len("billing"), true)
	if got, want := stripANSI(namedAppLine), "20:08:57.605 billing App          Starting"; got != want {
		t.Fatalf("named App trigger line = %q, want %q", got, want)
	}
}

func TestDecorateWatcherLineFormatsANSIWrappedTriggerAsStarting(t *testing.T) {
	line := decorateWatcherLine("\x1b[32m__FORJ_WATCHER_TRIGGER__\x1b[0m", "API", time.Now())
	if !contains(line, "Starting") {
		t.Fatalf("expected starting label in trigger line: %q", line)
	}
	if contains(line, "__FORJ_WATCHER_TRIGGER__") {
		t.Fatalf("expected raw trigger marker to be hidden, got %q", line)
	}
}

func TestDecorateWatcherLineFormatsSpinnerPrefixedTriggerAsStarting(t *testing.T) {
	line := decorateWatcherLine("⠙__FORJ_WATCHER_TRIGGER__", "API", time.Now())
	if !contains(line, "Starting") {
		t.Fatalf("expected starting label in trigger line: %q", line)
	}
	if contains(line, "__FORJ_WATCHER_TRIGGER__") {
		t.Fatalf("expected raw trigger marker to be hidden, got %q", line)
	}
}

func TestDecorateWatcherLineUsesLastCarriageReturnSegment(t *testing.T) {
	line := decorateWatcherLine("\r⠙ 1/4 build\r__FORJ_WATCHER_TRIGGER__", "API", time.Now())
	if !contains(line, "Starting") {
		t.Fatalf("expected starting label in trigger line: %q", line)
	}
	if contains(line, "__FORJ_WATCHER_TRIGGER__") {
		t.Fatalf("expected raw trigger marker to be hidden, got %q", line)
	}
}

// TestFinalDevwatchTerminalFrameKeepsOnlyTheVisibleRedraw prevents spinner history from becoming one concatenated transcript line.
func TestFinalDevwatchTerminalFrameKeepsOnlyTheVisibleRedraw(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "ordinary line", line: "ready", want: "ready"},
		{name: "terminal newline carriage return", line: "ready\r", want: "ready"},
		{name: "redraw frames", line: "first\rsecond\rfinal\r", want: "final"},
		{name: "repeated terminal carriage return", line: "first\rfinal\r\r", want: "final"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := finalDevwatchTerminalFrame(test.line); got != test.want {
				t.Fatalf("finalDevwatchTerminalFrame(%q) = %q, want %q", test.line, got, test.want)
			}
		})
	}
}

func TestDevwatchWriterPreservesANSIForNormalOutput(t *testing.T) {
	var out bytes.Buffer
	writer := newDevwatchWriter(&out, nil, "stdout", "API", newDevwatchLifecycleState(0, nil))
	if _, err := writer.Write([]byte("\x1b[31mred\x1b[0m\n")); err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "\x1b[31mred\x1b[0m") {
		t.Fatalf("expected ANSI color to be preserved, got %q", got)
	}
}

func TestFormatWatcherNameList(t *testing.T) {
	got := formatWatcherNameList([]project.DevWatch{
		{Name: "Build App"},
		{Name: "Wire"},
		{Name: "API"},
	})
	if got != "Build App, Wire, API" {
		t.Fatalf("unexpected watcher summary: %q", got)
	}
}

func TestDevwatchLifecycleStateEmitsStartupSeparatorOnceAfterExpectedTriggers(t *testing.T) {
	state := newDevwatchLifecycleState(3, nil)
	if state.noteStartupTrigger() {
		t.Fatal("expected first trigger not to emit")
	}
	if state.noteStartupTrigger() {
		t.Fatal("expected second trigger not to emit")
	}
	if !state.noteStartupTrigger() {
		t.Fatal("expected third trigger to emit")
	}
	if state.noteStartupTrigger() {
		t.Fatal("expected separator emission only once")
	}
}

func TestDevEnvFilesChangedDetectsCreateUpdateAndDelete(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	initial, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatalf("snapshot initial env files: %v", err)
	}
	if len(initial) != 0 {
		t.Fatalf("expected empty initial env snapshot, got %#v", initial)
	}

	envPath := filepath.Join(dir, ".env")
	for _, generated := range []string{".env.example", ".env.testing"} {
		if err := os.WriteFile(filepath.Join(dir, generated), []byte("GENERATED=1\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", generated, err)
		}
	}
	if err := os.WriteFile(envPath, []byte("FEATURE=1\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	created, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatalf("snapshot created env files: %v", err)
	}
	if !devEnvFilesChanged(initial, created) {
		t.Fatal("expected create to change env snapshot")
	}
	if len(created) != 1 {
		t.Fatalf("generated contracts entered dev environment snapshot: %#v", created)
	}

	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(envPath, []byte("FEATURE=2\n"), 0o644); err != nil {
		t.Fatalf("update .env: %v", err)
	}
	updated, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatalf("snapshot updated env files: %v", err)
	}
	if !devEnvFilesChanged(created, updated) {
		t.Fatal("expected update to change env snapshot")
	}

	if err := os.Remove(envPath); err != nil {
		t.Fatalf("remove .env: %v", err)
	}
	deleted, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatalf("snapshot deleted env files: %v", err)
	}
	if !devEnvFilesChanged(updated, deleted) {
		t.Fatal("expected delete to change env snapshot")
	}
}

func TestConsumeSuppressedDevEnvTrigger(t *testing.T) {
	for consumeSuppressedDevEnvTrigger() {
	}
	suppressNextDevEnvTrigger()
	if !consumeSuppressedDevEnvTrigger() {
		t.Fatal("expected suppressed env trigger to be consumed")
	}
	if consumeSuppressedDevEnvTrigger() {
		t.Fatal("expected suppression token to be one-shot")
	}
}

func TestDevwatchLifecycleStateBuildsRestartApps(t *testing.T) {
	state := newDevwatchLifecycleState(0, []string{"Run App"})
	if state == nil {
		t.Fatal("expected lifecycle state")
	}
	if _, ok := state.restartExpected["Run App"]; !ok {
		t.Fatal("expected restart app to include Run App")
	}
}

func TestDevwatchLifecycleStateEmitsRestartSeparatorsAroundRunApp(t *testing.T) {
	state := newDevwatchLifecycleState(0, []string{"Run App"})
	if state == nil {
		t.Fatal("expected lifecycle state")
	}
	state.startupEmitted = true
	if state.noteRestartTrigger("Run App") != "" {
		t.Fatal("expected no labeled restart separator before shutdown")
	}
	if !state.noteRestartShutdown("Run App", "Test › API › Shutting down HTTP server #http.Server.Serve") {
		t.Fatal("expected first shutdown line to emit shutdown separator")
	}
	if state.noteRestartShutdown("Run App", "Test › Jobs › Queue worker shut down #jobs.Worker.StartWithContext") {
		t.Fatal("expected shutdown separator to emit once per restart")
	}
	if got := state.noteRestartTrigger("Run App"); got != "Start" {
		t.Fatalf("expected labeled start separator after shutdown, got %q", got)
	}
	if got := state.noteRestartTrigger("Run App"); got != "" {
		t.Fatalf("expected unlabeled trigger before next shutdown, got %q", got)
	}
}

func TestIsRuntimeShutdownLine(t *testing.T) {
	for _, line := range []string{
		"Test › API › Shutting down HTTP server #http.Server.Serve",
		"Test › API › HTTP server shut down #http.Server.Serve",
		"Test › Scheduler › Shutting down scheduler... #scheduler.Scheduler.StartWithContext",
		"Test › Scheduler › Scheduler shut down #scheduler.Scheduler.StartWithContext",
		"Test › Jobs › Shutting down queue worker... #jobs.Worker.StartWithContext",
		"Test › Jobs › Queue worker shut down #jobs.Worker.StartWithContext",
		"asynq: pid=1 INFO: Starting graceful shutdown",
		"asynq: pid=1 INFO: Waiting for all workers to finish...",
		"asynq: pid=1 INFO: All workers have finished",
		"asynq: pid=1 INFO: Exiting",
	} {
		if !isRuntimeShutdownLine(line) {
			t.Fatalf("expected runtime shutdown line: %q", line)
		}
	}
	if isRuntimeShutdownLine("Test › API › Starting HTTP server · port 3000 #http.Server.Serve") {
		t.Fatal("expected startup line not to be classified as shutdown output")
	}
}

func TestFormatBuildProgressStatus(t *testing.T) {
	line := ansiCSI.ReplaceAllString(formatBuildProgressStatus("2/4", "wire"), "")
	if !contains(line, "2/4") {
		t.Fatalf("expected step count in status line: %q", line)
	}
	if !contains(line, "wire") {
		t.Fatalf("expected step name in status line: %q", line)
	}
}

func TestHandleBuildProgressLineConsumesNamedAppBuildMarkers(t *testing.T) {
	var out bytes.Buffer
	if !handleBuildProgressLine(&out, "Build billing", "__FORJ_BUILD_PROGRESS__ step 2/4 wire") {
		t.Fatal("expected named app build progress marker to be handled")
	}
	if strings.Contains(out.String(), buildProgressMarker) {
		t.Fatalf("did not expect raw progress marker in output: %q", out.String())
	}
}

func TestHandleBuildProgressLineConsumesSpinnerPrefixedMarkers(t *testing.T) {
	var out bytes.Buffer
	if !handleBuildProgressLine(&out, "Build App", "⠙__FORJ_BUILD_PROGRESS__ step 2/4 wire") {
		t.Fatal("expected prefixed build progress marker to be handled")
	}
	if strings.Contains(out.String(), buildProgressMarker) {
		t.Fatalf("did not expect raw progress marker in output: %q", out.String())
	}
}

// TestRunDevWatcherReconciliationBuildsSPAJoinBeforeApp verifies one app publication follows all frontend assets.
func TestRunDevWatcherReconciliationBuildsSPAJoinBeforeApp(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "reconcile.log")
	appendLine := func(line string) string {
		return "printf '%s\\n' " + shellQuote(line) + " >> " + shellQuote(logPath)
	}
	config := &project.Config{Dev: project.DevConfig{Apps: map[string]project.DevApp{
		project.DefaultAppName: {
			Build: &project.DevAppCommand{Exec: appendLine("app")},
			SPAs: map[string]project.DevSPA{
				"admin":  {Path: ".", Build: appendLine("spa-admin")},
				"portal": {Path: ".", Build: appendLine("spa-portal")},
			},
		},
	}}}
	output := &devLifecycleOrchestrationWriter{compact: true}
	result, err := runDevWatcherReconciliation(config, output, output, false)
	if err != nil {
		t.Fatalf("runDevWatcherReconciliation() error = %v", err)
	}
	if result.BuildElapsed <= 0 || result.MigrateElapsed != 0 {
		t.Fatalf("reconciliation result = %#v, want one timed build and no migration", result)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	lines := strings.Fields(string(data))
	want := []string{"spa-admin", "spa-portal", "app"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("reconciliation order = %#v, want %#v", lines, want)
	}
	plain := stripANSI(output.String())
	success := stripANSI(console.SuccessMark())
	for _, summary := range []string{"SPA", "Build"} {
		if !strings.Contains(plain, success+" "+summary) {
			t.Fatalf("compact reconciliation omitted %s result: %q", summary, plain)
		}
	}
	for _, narration := range []string{"Building app frontend", "Building app"} {
		if strings.Contains(plain, narration) {
			t.Fatalf("compact reconciliation retained %q narration: %q", narration, plain)
		}
	}
}

// TestDevWatchSessionReloadPreservesSPAFailureDiagnostics verifies config refreshes cannot restore GoForj's obsolete silent build command.
func TestDevWatchSessionReloadPreservesSPAFailureDiagnostics(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	config := &project.Config{
		ProjectName:  "reload-diagnostics",
		GoModuleName: "example.com/reload-diagnostics",
		Dev: project.DevConfig{Apps: map[string]project.DevApp{
			project.DefaultAppName: {
				SPAs: map[string]project.DevSPA{
					generatedFrontendSPAName: {Path: "cmd/app/frontend", Build: legacyFrontendSPABuildCommand},
				},
			},
		}},
	}
	if err := writeProjectConfig(".goforj.yml", config); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	session := &devWatchSession{}
	if err := session.reloadProjectConfig(); err != nil {
		t.Fatalf("reloadProjectConfig() error = %v", err)
	}
	got := session.config.Dev.Apps[project.DefaultAppName].SPAs[generatedFrontendSPAName].Build
	if got != generatedFrontendSPABuildCommand {
		t.Fatalf("reloaded SPA build = %q, want %q", got, generatedFrontendSPABuildCommand)
	}
}

// TestDevPlanInitialTasksUsesFastPathForConventionalSetup verifies existing generated task identities need no new config phase.
func TestDevPlanInitialTasksUsesFastPathForConventionalSetup(t *testing.T) {
	pre := []project.DevTask{
		{Name: "Run Docker Compose", Cmd: "docker compose up -d"},
		{Name: "Waiting for Database to be ready", Cmd: strings.Replace(generatedMySQLDevWaitCommand, "docker-compose", "docker compose", 1)},
		{Name: "Install Frontend Dependencies", Cmd: "cd cmd/app/frontend && npm install --no-audit >/dev/null"},
	}
	config := &project.Config{Dev: project.DevConfig{
		Pre:  pre,
		Apps: map[string]project.DevApp{project.DefaultAppName: {}},
	}}

	plan := planDevInitialTasks(config)
	if !plan.fastPath {
		t.Fatal("conventional setup did not select the one-build startup path")
	}
	if !reflect.DeepEqual(plan.preBuild, pre) || len(plan.postMigrate) != 0 {
		t.Fatalf("startup task plan = %#v, want conventional tasks before build", plan)
	}
}

// TestDevPlanInitialTasksFallsBackForCustomSetup keeps arbitrary pre tasks on the historical binary-ready path.
func TestDevPlanInitialTasksFallsBackForCustomSetup(t *testing.T) {
	custom := project.DevTask{Name: "Warm App Cache", Cmd: "./bin/app cache:warm"}
	config := &project.Config{Dev: project.DevConfig{
		Pre:  []project.DevTask{custom},
		Apps: map[string]project.DevApp{project.DefaultAppName: {}},
	}}

	plan := planDevInitialTasks(config)
	if plan.fastPath {
		t.Fatal("custom setup unexpectedly selected the reordered startup path")
	}
	if len(plan.preBuild) != 0 || len(plan.postMigrate) != 0 {
		t.Fatalf("custom startup task plan = %#v, want compatibility fallback", plan)
	}
}

// TestDevPlanInitialTasksPreservesLegacyComposeBuildOrder keeps image-building owner hooks behind the historical bootstrap App build.
func TestDevPlanInitialTasksPreservesLegacyComposeBuildOrder(t *testing.T) {
	config := &project.Config{Dev: project.DevConfig{
		Pre: []project.DevTask{{
			Name: "Run Docker Compose",
			Cmd:  "docker compose up -d --build",
		}},
		Apps: map[string]project.DevApp{project.DefaultAppName: {}},
	}}

	plan := planDevInitialTasks(config)
	if plan.fastPath {
		t.Fatal("legacy Compose image build unexpectedly moved ahead of the initial App build")
	}
}

// TestDevPlanInitialTasksPreservesBinaryReadyGeneratorWithoutAutoMigrate keeps owner commands behind the first App build.
func TestDevPlanInitialTasksPreservesBinaryReadyGeneratorWithoutAutoMigrate(t *testing.T) {
	custom := project.DevTask{Name: "Generate reports", Cmd: "./bin/app reports:generate"}
	config := &project.Config{Dev: project.DevConfig{
		Pre:  []project.DevTask{custom},
		Apps: map[string]project.DevApp{project.DefaultAppName: {}},
	}}

	plan := planDevInitialTasks(config)
	if plan.fastPath {
		t.Fatal("owner generator unexpectedly moved ahead of the initial App build")
	}
	if len(plan.preBuild) != 0 || len(plan.postMigrate) != 0 {
		t.Fatalf("owner generator startup task plan = %#v, want compatibility fallback", plan)
	}
}

// TestDevPlanInitialTasksDefersGeneratorUntilAfterAutoMigrate keeps the established schema-before-generation order.
func TestDevPlanInitialTasksDefersGeneratorUntilAfterAutoMigrate(t *testing.T) {
	custom := project.DevTask{Name: "Generate reports", Cmd: "./bin/app reports:generate"}
	config := &project.Config{Dev: project.DevConfig{
		Pre:         []project.DevTask{custom},
		AutoMigrate: true,
		Apps:        map[string]project.DevApp{project.DefaultAppName: {}},
	}, Render: project.RenderConfig{Components: project.Components{DatabaseSQLite: true}}}

	plan := planDevInitialTasks(config)
	if !plan.fastPath {
		t.Fatal("post-migrate generator did not select the structured startup path")
	}
	if len(plan.preBuild) != 0 || !reflect.DeepEqual(plan.postMigrate, []project.DevTask{custom}) {
		t.Fatalf("post-migrate startup task plan = %#v, want generator after migration", plan)
	}
}

// TestRunDevInitialLifecycleBuildsStructuredAppOnce verifies conventional setup and SPA inputs settle before compilation.
func TestRunDevInitialLifecycleBuildsStructuredAppOnce(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join("cmd", "app", "frontend"), 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	logPath := filepath.Join(root, "startup.log")
	toolsDir := t.TempDir()
	npmPath := filepath.Join(toolsDir, "npm")
	if err := os.WriteFile(npmPath, []byte("#!/bin/sh\n"+appendDevLifecycleTestLine(logPath, "pre")+"\n"), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}
	t.Setenv("PATH", toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	config := devInitialLifecycleTestConfig(logPath)
	config.Dev.Pre = []project.DevTask{generatedDevFrontendInstallTask(project.DefaultApp())}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runDevInitialLifecycle(config, &stdout, &stderr); err != nil {
		t.Fatalf("runDevInitialLifecycle() error = %v", err)
	}
	assertDevLifecycleTestLines(t, logPath, []string{"pre", "spa", "app"})
	plain := stripANSI(stdout.String())
	success := stripANSI(console.SuccessMark())
	for _, expected := range []string{"┏ Preparing App", "┃ " + success + " Build", "┗ " + success + " Prepared"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("initial preparation output omitted %q:\n%s", expected, plain)
		}
	}
	for _, redundant := range []string{"┃ · Building app", "┃   Auto-migrate"} {
		if strings.Contains(plain, redundant) {
			t.Fatalf("initial preparation output retained redundant narration %q:\n%s", redundant, plain)
		}
	}
}

// TestDevPhaseSuccessLabelCompletesAppPhaseLanguage verifies every startup phase closes naturally.
func TestDevPhaseSuccessLabelCompletesAppPhaseLanguage(t *testing.T) {
	tests := map[string]string{
		"Preparing App":  "Prepared",
		"Preparing Apps": "Prepared",
		"Building App":   "Built",
		"Finalizing App": "Finalized",
		"Custom":         "Done",
	}
	for input, want := range tests {
		if got := devPhaseSuccessLabel(input); got != want {
			t.Fatalf("devPhaseSuccessLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestDevMigrationTotalExtractsStableCompletionCount verifies compact preparation output uses the command's final result.
func TestDevMigrationTotalExtractsStableCompletionCount(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
		ok     bool
	}{
		{name: "no migrations", output: "✔ migrations complete (0)\n", want: 0, ok: true},
		{name: "applied migrations", output: "✔ applied create_users\n✔ migrations complete (2)\n", want: 2, ok: true},
		{name: "colored output", output: "\x1b[32m✔\x1b[0m migrations complete (3)\n", want: 3, ok: true},
		{name: "unrecognized output", output: "migration command finished\n", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := devMigrationTotal(test.output)
			if got != test.want || ok != test.ok {
				t.Fatalf("devMigrationTotal() = (%d, %t), want (%d, %t)", got, ok, test.want, test.ok)
			}
		})
	}
}

// TestRunDevAppSetupSummarizesMigrationCommand verifies preparation owns the migration result instead of replaying command prose.
func TestRunDevAppSetupSummarizesMigrationCommand(t *testing.T) {
	withConventionalApp(t, project.DefaultAppName)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	command := "#!/bin/sh\nprintf '✔ applied first\\n✔ applied second\\n✔ migrations complete (2)\\n'\n"
	if err := os.WriteFile(filepath.Join("bin", project.DefaultAppName), []byte(command), 0o755); err != nil {
		t.Fatalf("write migration command: %v", err)
	}

	config := &project.Config{
		Dev:    project.DevConfig{AutoMigrate: true},
		Render: project.RenderConfig{Components: project.Components{DatabaseSQLite: true}},
	}
	var output bytes.Buffer
	if err := runDevPhaseOutput("Preparing App", &output, &output, func(phaseOut io.Writer, phaseErr io.Writer) error {
		return runDevAppSetup(config, phaseOut, phaseErr)
	}); err != nil {
		t.Fatalf("run dev app setup: %v", err)
	}
	plain := stripANSI(output.String())
	if !strings.Contains(plain, stripANSI(console.SuccessMark())+" Migrations · 2 ·") {
		t.Fatalf("preparation output omitted timed migration result: %q", plain)
	}
	if strings.Contains(plain, "migrations complete") || strings.Contains(plain, "applied first") {
		t.Fatalf("preparation output replayed migration command prose: %q", plain)
	}
}

// TestRunDevAppSetupSummarizesCompactRestartMigration verifies restart groups receive one timed migration outcome without command narration.
func TestRunDevAppSetupSummarizesCompactRestartMigration(t *testing.T) {
	withConventionalApp(t, project.DefaultAppName)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	command := "#!/bin/sh\nprintf '✔ applied first\\n✔ migrations complete (1)\\n'\n"
	if err := os.WriteFile(filepath.Join("bin", project.DefaultAppName), []byte(command), 0o755); err != nil {
		t.Fatalf("write migration command: %v", err)
	}

	config := &project.Config{
		Dev:    project.DevConfig{AutoMigrate: true},
		Render: project.RenderConfig{Components: project.Components{DatabaseSQLite: true}},
	}
	output := &devLifecycleOrchestrationWriter{compact: true}
	if _, err := runDevAppSetupWithResult(config, output, output); err != nil {
		t.Fatalf("run dev app setup: %v", err)
	}
	plain := stripANSI(output.String())
	if !strings.Contains(plain, stripANSI(console.SuccessMark())+" Migrations") || !strings.Contains(plain, "· 1 ·") {
		t.Fatalf("compact restart omitted timed migration result: %q", plain)
	}
	for _, narration := range []string{"Running auto-migrate", "migrations complete", "applied first"} {
		if strings.Contains(plain, narration) {
			t.Fatalf("compact restart replayed %q narration: %q", narration, plain)
		}
	}
}

// TestRunDevAppSetupReplaysMigrationFailure verifies compact preparation retains captured diagnostics on failure.
func TestRunDevAppSetupReplaysMigrationFailure(t *testing.T) {
	withConventionalApp(t, project.DefaultAppName)
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	command := "#!/bin/sh\nprintf 'attempting migration\\n'\nprintf 'migration failed detail\\n' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join("bin", project.DefaultAppName), []byte(command), 0o755); err != nil {
		t.Fatalf("write migration command: %v", err)
	}

	config := &project.Config{
		Dev:    project.DevConfig{AutoMigrate: true},
		Render: project.RenderConfig{Components: project.Components{DatabaseSQLite: true}},
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runDevPhaseOutput("Preparing App", &stdout, &stderr, func(phaseOut io.Writer, phaseErr io.Writer) error {
		return runDevAppSetup(config, phaseOut, phaseErr)
	})
	if err == nil {
		t.Fatal("run dev app setup succeeded, want migration failure")
	}
	if !strings.Contains(stripANSI(stdout.String()), "attempting migration") {
		t.Fatalf("preparation stdout omitted captured diagnostics: %q", stdout.String())
	}
	if !strings.Contains(stripANSI(stderr.String()), "migration failed detail") {
		t.Fatalf("preparation stderr omitted captured diagnostics: %q", stderr.String())
	}
}

// TestRunDevInitialLifecyclePreservesCustomPreCompatibility keeps unknown tasks between a bootstrap build and the SPA-owning rebuild.
func TestRunDevInitialLifecyclePreservesCustomPreCompatibility(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join("cmd", "app", "frontend"), 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	logPath := filepath.Join(root, "startup.log")
	config := devInitialLifecycleTestConfig(logPath)
	config.Dev.Pre = []project.DevTask{{Name: "Owner setup", Cmd: appendDevLifecycleTestLine(logPath, "pre")}}

	if err := runDevInitialLifecycle(config, io.Discard, io.Discard); err != nil {
		t.Fatalf("runDevInitialLifecycle() error = %v", err)
	}
	assertDevLifecycleTestLines(t, logPath, []string{"app", "pre", "spa", "app"})
}

// TestRunDevInitialLifecycleBuildsFreshEmbeddedSPAOnce proves a clean checkout can create embedded assets before its only App compile.
func TestRunDevInitialLifecycleBuildsFreshEmbeddedSPAOnce(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	frontendRoot := filepath.Join("cmd", "app", "frontend")
	if err := os.MkdirAll(frontendRoot, 0o755); err != nil {
		t.Fatalf("mkdir frontend: %v", err)
	}
	if err := os.WriteFile("go.mod", []byte("module example.com/fresh-embedded-spa\n\ngo 1.22.0\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	mainSource := `package main

import "embed"

//go:embed all:frontend/dist/*
var frontend embed.FS

func main() {
	_ = frontend
}
`
	if err := os.WriteFile(filepath.Join("cmd", "app", "main.go"), []byte(mainSource), 0o644); err != nil {
		t.Fatalf("write embedded App: %v", err)
	}
	toolsDir := t.TempDir()
	npmPath := filepath.Join(toolsDir, "npm")
	if err := os.WriteFile(npmPath, []byte("#!/bin/sh\ntouch .prepared\n"), 0o755); err != nil {
		t.Fatalf("write fake npm: %v", err)
	}
	t.Setenv("PATH", toolsDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	buildCountPath := filepath.Join(root, "build-count.log")
	config := &project.Config{Dev: project.DevConfig{
		Pre: []project.DevTask{generatedDevFrontendInstallTask(project.DefaultApp())},
		Apps: map[string]project.DevApp{
			project.DefaultAppName: {
				Build: &project.DevAppCommand{
					Exec: "mkdir -p bin && go build -o ./bin/app ./cmd/app && " + appendDevLifecycleTestLine(buildCountPath, "build"),
					Env:  map[string]string{"GOCACHE": "/tmp/gocache"},
				},
				Run: &project.DevAppCommand{Disabled: true},
				SPAs: map[string]project.DevSPA{
					"frontend": {
						Path: frontendRoot,
						Build: "test -f .prepared && mkdir -p dist && " +
							"printf '%s\\n' '<!doctype html>' > dist/index.html",
					},
				},
			},
		},
	}}

	if err := runDevInitialLifecycle(config, io.Discard, io.Discard); err != nil {
		t.Fatalf("runDevInitialLifecycle() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(frontendRoot, "dist", "index.html")); err != nil {
		t.Fatalf("SPA build did not create embedded dist output: %v", err)
	}
	if _, err := os.Stat(filepath.Join("bin", "app")); err != nil {
		t.Fatalf("real Go build did not create App binary: %v", err)
	}
	assertDevLifecycleTestLines(t, buildCountPath, []string{"build"})
}

// TestDevBuildWavePublishesFreshEmbeddedSPAAndAPIIndex exercises the production index and Go compiler against one stabilized frontend snapshot.
func TestDevBuildWavePublishesFreshEmbeddedSPAAndAPIIndex(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": `module example.com/dev-wave

go 1.25.0

require github.com/goforj/web v0.0.0

replace github.com/goforj/web => ./webstub
`,
		"webstub/go.mod": "module github.com/goforj/web\n\ngo 1.25.0\n",
		"webstub/web.go": `package web
type Route struct{}
type RouteGroup struct{}
func NewRoute(method string, path string, handler any, middleware ...any) Route { return Route{} }
func NewRouteGroup(prefix string, routes []any, middleware ...any) RouteGroup { return RouteGroup{} }
`,
		"internal/hello/controller.go": `package hello
import (
	"net/http"
	"github.com/goforj/web"
)
type Controller struct{}
func (c *Controller) Routes() []any {
	return []any{web.NewRoute(http.MethodGet, "/hello", c.Hello)}
}
func (c *Controller) Hello(ctx any) error { return nil }
`,
		"app/routes.go": `package app
import (
	"github.com/goforj/web"
	"example.com/dev-wave/internal/hello"
)
func ProvideRoutes(controller *hello.Controller) []web.RouteGroup {
	return []web.RouteGroup{web.NewRouteGroup("/api", controller.Routes())}
}
`,
		"cmd/app/main.go": `package main
import "embed"
//go:embed all:frontend/dist/*
var frontend embed.FS
func main() { _, _ = frontend.ReadFile("frontend/dist/index.html") }
`,
	}
	for path, contents := range files {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatalf("create fixture directory %s: %v", path, err)
		}
		if err := os.WriteFile(absolute, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}
	frontendRoot := filepath.Join(root, "cmd", "app", "frontend")
	if err := os.MkdirAll(frontendRoot, 0o755); err != nil {
		t.Fatalf("create frontend root: %v", err)
	}
	config := &project.Config{Dev: project.DevConfig{Apps: map[string]project.DevApp{
		project.DefaultAppName: {
			SPAs: map[string]project.DevSPA{
				"frontend": {Path: frontendRoot, Build: "mkdir -p dist && printf '%s\\n' '<main>fresh</main>' > dist/index.html"},
			},
		},
	}}}
	if _, err := runDevInitialSPABuilds(config, io.Discard, io.Discard); err != nil {
		t.Fatalf("build fresh SPA: %v", err)
	}

	runner := apiindex.NewRunner(func() project.App { return project.DefaultApp() })
	pipeline := build.NewPipeline(logger.NewSilentLogger(), runner)
	job := devBuildJob{app: project.DefaultApp(), phased: true}
	results := runDevBuildWave([]devBuildJob{job}, func(job devBuildJob, phase string, _ bool) devBuildResult {
		result := devBuildResult{job: job}
		if phase == build.DevBuildPhasePrepare {
			return result
		}
		result.err = pipeline.Run(root, "build", build.Step{Name: "go build", Run: func(stepRoot string) (string, error) {
			binary := filepath.Join(stepRoot, "bin", "app")
			if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
				return "", err
			}
			// Reusing the invoking test's content-addressed cache avoids rebuilding the Windows standard library;
			// serial compilation still bounds fixture-owned compiler fan-out on constrained runners.
			command := exec.Command("go", "build", "-p=1", "-o", binary, "./cmd/app")
			command.Dir = stepRoot
			output, err := command.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("go build: %w\n%s", err, output)
			}
			return "compiled", nil
		}}, build.RunOptions{SkipPreparation: true, SkipWire: true})
		return result
	})
	if len(results) != 1 {
		t.Fatalf("coordinated embedded-SPA build results = %#v, want one", results)
	}
	if results[0].err != nil {
		t.Fatalf("coordinated embedded-SPA build error = %v; results = %#v", results[0].err, results)
	}
	for _, path := range []string{
		filepath.Join(root, "bin", "app"),
		filepath.Join(root, "build", "api_index.json"),
		filepath.Join(frontendRoot, "dist", "index.html"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("coordinated build omitted %s: %v", path, err)
		}
	}
	index, err := os.ReadFile(filepath.Join(root, "build", "api_index.json"))
	if err != nil {
		t.Fatalf("read API index: %v", err)
	}
	if !bytes.Contains(index, []byte("/hello")) {
		t.Fatalf("API index omitted stabilized route:\n%s", index)
	}
}

// devInitialLifecycleTestConfig returns one structured app whose commands record their execution order.
func devInitialLifecycleTestConfig(logPath string) *project.Config {
	return &project.Config{Dev: project.DevConfig{
		Apps: map[string]project.DevApp{
			project.DefaultAppName: {
				Build: &project.DevAppCommand{
					Exec: "mkdir -p bin && : > bin/app && " + appendDevLifecycleTestLine(logPath, "app"),
				},
				Run: &project.DevAppCommand{Disabled: true},
				SPAs: map[string]project.DevSPA{
					"frontend": {Path: filepath.Join("cmd", "app", "frontend"), Build: appendDevLifecycleTestLine(logPath, "spa")},
				},
			},
		},
	}}
}

// appendDevLifecycleTestLine returns a shell command that records one lifecycle phase without hiding quoting behavior.
func appendDevLifecycleTestLine(logPath string, line string) string {
	return "printf '%s\\n' " + shellQuote(line) + " >> " + shellQuote(logPath)
}

// assertDevLifecycleTestLines compares the durable phase transcript with the expected startup graph.
func assertDevLifecycleTestLines(t *testing.T, logPath string, want []string) {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := strings.Fields(string(data)); !reflect.DeepEqual(got, want) {
		t.Fatalf("startup order = %#v, want %#v", got, want)
	}
}

// TestDevBuildJobsPreserveStructuredExecutionContext verifies bootstrap and outer builds honor app overrides.
func TestDevBuildJobsPreserveStructuredExecutionContext(t *testing.T) {
	config := &project.Config{Dev: project.DevConfig{Apps: map[string]project.DevApp{
		"billing": {
			Build: &project.DevAppCommand{
				Exec: "make billing", WorkDir: "tools/billing", Env: map[string]string{
					"CUSTOM_BUILD":        "yes",
					"FORJ_APP":            "wrong",
					"FORJ_COMMAND_PREFIX": "wrong",
				},
			},
		},
	}}}
	jobs := devBuildJobs(config)
	if len(jobs) != 1 {
		t.Fatalf("devBuildJobs() = %#v, want one billing build", jobs)
	}
	job := jobs[0]
	if job.command != "make billing" || job.dir != "tools/billing" {
		t.Fatalf("build execution = command %q dir %q", job.command, job.dir)
	}
	for key, want := range map[string]string{
		"CUSTOM_BUILD": "yes", "FORJ_APP": "billing", "FORJ_COMMAND_PREFIX": "forj billing",
	} {
		if job.env[key] != want {
			t.Fatalf("build env %s = %q, want %q", key, job.env[key], want)
		}
	}
	if _, ok := job.env["FORJ_BUILD_PROGRESS"]; ok {
		t.Fatalf("bootstrap build env enabled watcher progress protocol: %#v", job.env)
	}
}

// TestRunDevSubprocessDisablesWatcherProgressProtocol keeps machine records out of human build output.
func TestRunDevSubprocessDisablesWatcherProgressProtocol(t *testing.T) {
	t.Setenv("FORJ_BUILD_PROGRESS", "1")
	testCases := []struct {
		name string
		env  map[string]string
	}{
		{name: "inherited"},
		{name: "configured", env: map[string]string{"FORJ_BUILD_PROGRESS": "1"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var out bytes.Buffer
			var errOut bytes.Buffer
			err := runDevSubprocess(devSubprocessRun{
				command:    `printf '%s' "$FORJ_BUILD_PROGRESS"`,
				env:        testCase.env,
				stdout:     &out,
				stderr:     &errOut,
				transcript: true,
			})
			if err != nil {
				t.Fatalf("runDevSubprocess() error = %v", err)
			}
			if got := out.String(); got != "0" {
				t.Fatalf("FORJ_BUILD_PROGRESS = %q, want disabled", got)
			}
			if errOut.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", errOut.String())
			}
		})
	}
}

// TestDevBuildJobsTreatExplicitEmptyAppsAsAuthority keeps custom-watcher-only native configs from enrolling Apps.
func TestDevBuildJobsTreatExplicitEmptyAppsAsAuthority(t *testing.T) {
	t.Setenv("FORJ_APP", "")
	native := &project.Config{Dev: project.DevConfig{
		Apps: map[string]project.DevApp{},
		Watches: []project.DevWatch{
			{Name: "Docs", Include: []string{".md"}, Exec: "make docs"},
		},
	}}
	if jobs := devBuildJobs(native); len(jobs) != 0 {
		t.Fatalf("explicit empty dev.apps produced build jobs: %#v", jobs)
	}
	if apps := activeDevAppsForConfig(native); len(apps) != 0 {
		t.Fatalf("explicit empty dev.apps selected Apps: %#v", apps)
	}

	legacy := &project.Config{Dev: project.DevConfig{Watches: []project.DevWatch{
		{Name: "Docs", Watch: "-file .md", Exec: "make docs"},
	}}}
	if jobs := devBuildJobs(legacy); len(jobs) == 0 {
		t.Fatal("omitted dev.apps lost legacy App discovery")
	}
}

// TestRunDevFrontendDependencySetupIncludesNamedApps verifies render-created app SPAs receive their own install task.
func TestRunDevFrontendDependencySetupIncludesNamedApps(t *testing.T) {
	root := t.TempDir()
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	for _, app := range []project.App{project.DefaultApp(), project.DefaultNamedApp("portal")} {
		if err := os.MkdirAll(projectlayout.FrontendDir(".", app), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", app.Name, err)
		}
	}
	tools := t.TempDir()
	logPath := filepath.Join(root, "npm.log")
	npmPath := filepath.Join(tools, "npm")
	if err := os.WriteFile(npmPath, []byte("#!/bin/sh\npwd >> "+shellQuote(logPath)+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(npm) error = %v", err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	defaultTask := generatedDevFrontendInstallTask(project.DefaultApp())
	namedTask := generatedDevFrontendInstallTask(project.DefaultNamedApp("portal"))
	config := &project.Config{Dev: project.DevConfig{
		Apps: map[string]project.DevApp{
			project.DefaultAppName: {SPAs: map[string]project.DevSPA{"frontend": {Path: "./cmd/app/frontend"}}},
			"portal":               {SPAs: map[string]project.DevSPA{"frontend": {Path: "./cmd/portal/frontend"}}},
		},
		Pre: []project.DevTask{defaultTask, namedTask},
	}}
	if err := runDevFrontendDependencySetup(config); err != nil {
		t.Fatalf("runDevFrontendDependencySetup() error = %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	output := filepath.ToSlash(string(data))
	for _, path := range []string{"/cmd/app/frontend", "/cmd/portal/frontend"} {
		if !strings.Contains(output, path) {
			t.Fatalf("npm working directories = %q, missing %q", output, path)
		}
	}
}

// contains centralizes contains behavior so callers follow the same contract.
func contains(s, sub string) bool { return strings.Contains(s, sub) }
