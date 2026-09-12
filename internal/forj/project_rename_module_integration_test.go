//go:build integration

package forj

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/goforj/goforj/internal/testkit"
	"github.com/goforj/goforj/project"
)

// TestProjectRenameModuleRenderedIntegration exercises real CLI dispatch, generated code, and later rendering on a full project.
func TestProjectRenameModuleRenderedIntegration(t *testing.T) {
	root := t.TempDir()
	repo, err := testkit.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	const oldModule = "example.com/rename-original"
	const newModule = "example.com/renamed/v2"
	testkit.RenderProjectWithForj(t, root, testkit.RenderProjectRequest{
		Config: project.Config{
			ProjectName: "Module Rename", GoModuleName: oldModule,
			Render: project.RenderConfig{Components: project.Components{
				CLI: true, DemoApp: true, Mail: true, Auth: true, OAuth: true,
				WebAPI: true, WebUI: true, Metrics: true, Observability: true, Grafana: true,
				Docker: true, DatabaseSQLite: true, Scheduler: true, Cache: true,
				Events: true, Storage: true, Jobs: true,
			}},
		},
		ModuleReplaces: map[string]string{"github.com/goforj/goforj": repo},
	})
	forj := testkit.EnsureIntegrationForjBinary(t)
	runModuleRenameIntegration(t, root, forj, "make:app", "admin", "--components", "web-api,jobs,scheduler,cache,events,storage")
	before := moduleRenameSnapshot(t, root)
	runModuleRenamePromptIntegration(t, root, forj, newModule+"\n", "--dry-run")
	if !reflect.DeepEqual(before, moduleRenameSnapshot(t, root)) {
		t.Fatal("real CLI dry-run changed project files")
	}
	runModuleRenamePromptIntegration(t, root, forj, newModule+"\nno\n")
	if !reflect.DeepEqual(before, moduleRenameSnapshot(t, root)) {
		t.Fatal("declining the real CLI confirmation changed project files")
	}
	runModuleRenamePromptIntegration(t, root, forj, newModule+"\nyes\n")
	config, err := project.LoadProjectConfigAt(root)
	if err != nil || config.GoModuleName != newModule || config.ProjectName != "Module Rename" {
		t.Fatalf("renamed config = %#v, %v", config, err)
	}
	for _, path := range []string{"app/wire/wire_gen.go", "app/admin/wire/wire_gen.go", "internal/runtime/about.go"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !strings.Contains(string(data), newModule) || strings.Contains(string(data), oldModule) {
			t.Fatalf("%s did not follow the module rename: %v", path, err)
		}
	}
	// Compiling before rendering prevents regenerated files from hiding an incomplete rename.
	runModuleRenameIntegration(t, root, "go", "test", "./...", "-run", "^$")
	runModuleRenameIntegration(t, root, forj, "make:command", "RenameCheck", "--no-open")
	runModuleRenameIntegration(t, root, forj, "render")
	runModuleRenameIntegration(t, root, forj, "build")
	runModuleRenameIntegration(t, root, "go", "test", "./...", "-run", "^$")
	plan, err := planProjectModuleRename(root, newModule)
	if err != nil || len(plan.files) != 0 {
		t.Fatalf("render lost renamed identity: %#v, %v", plan, err)
	}
}

// runModuleRenameIntegration bounds each subprocess and keeps rendered validation isolated from local workspaces.
func runModuleRenameIntegration(t *testing.T, root, executable string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = root
	command.Env = testkit.IntegrationGoProcessEnv(t, map[string]string{"GOWORK": "off", "FORJ_MAKE_OPEN": "never"})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", executable, strings.Join(args, " "), err, output)
	}
}

// runModuleRenamePromptIntegration drives the public interactive flow through a real terminal.
func runModuleRenamePromptIntegration(t *testing.T, root, forj, input string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, forj, append([]string{"project:rename-module"}, args...)...)
	command.Dir = root
	command.Env = testkit.IntegrationGoProcessEnv(t, map[string]string{"GOWORK": "off", "TERM": "dumb", "NO_COLOR": "1", "CI": "false"})
	terminal, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	outputDone := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(terminal)
		outputDone <- output
	}()
	if _, err := io.WriteString(terminal, input); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	output := <-outputDone
	if err != nil {
		t.Fatalf("interactive rename: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Current module:") || !strings.Contains(string(output), "New module name") {
		t.Fatalf("missing module prompts: %s", output)
	}
}

// TestProjectRenameModuleTemplIntegration proves regeneration uses the renamed imports in authoritative templ sources.
func TestProjectRenameModuleTemplIntegration(t *testing.T) {
	root := t.TempDir()
	repo, err := testkit.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	testkit.RenderProjectWithForj(t, root, testkit.RenderProjectRequest{
		Config: project.Config{
			ProjectName: "Templ Rename", GoModuleName: "example.com/templ-original",
			Render: project.RenderConfig{
				StarterKit: project.StarterKitTemplHTMX,
				Components: project.Components{CLI: true, WebAPI: true, WebUI: true},
			},
		},
		ModuleReplaces: map[string]string{"github.com/goforj/goforj": repo},
	})
	forj := testkit.EnsureIntegrationForjBinary(t)
	runModuleRenamePromptIntegration(t, root, forj, "example.org/templ-renamed\nyes\n")
	runModuleRenameIntegration(t, root, forj, "build")
	runModuleRenameIntegration(t, root, "go", "test", "./...", "-run", "^$")
}
