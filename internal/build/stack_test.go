package build

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildArgsUsesStructuredStackDefaults preserves comma-containing values independently of the legacy assignment flags.
func TestBuildArgsUsesStructuredStackDefaults(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module example.org/stack\n", ".goforj.yml": "project_name: stack\nmodule_name: example.org/stack\napps:\n  app:\n    components:\n      cli: true\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := Cmd{Args: []string{"./cmd/app"}, stackDefaults: map[string]string{"COMPOSE_PROFILES": "mysql,redis", "DB_SUPPORTED_DRIVERS": "mysql,sqlite", "DB_DRIVER": "sqlite"}}
	args, err := c.buildArgs(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	_, encoded, ok := strings.Cut(joined, "CompiledStackDefaultsBase64=")
	if !ok {
		t.Fatalf("missing stack defaults: %s", joined)
	}
	encoded = strings.Fields(encoded)[0]
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	if values["COMPOSE_PROFILES"] != "mysql,redis" || values["DB_SUPPORTED_DRIVERS"] != "mysql,sqlite" {
		t.Fatalf("corrupt defaults: %#v", values)
	}
}

// TestBuildStackFailsBeforeGenerationForMissingDefinitionsOrRuntimeSupport protects existing projects from an unsupported linker contract.
func TestBuildStackFailsBeforeGenerationForMissingDefinitionsOrRuntimeSupport(t *testing.T) {
	for _, scenario := range []string{"missing", "private", "old-runtime"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range map[string]string{"go.mod": "module example.org/stack\n", ".goforj.yml": "project_name: stack\nmodule_name: example.org/stack\napps:\n  app:\n    components: [cli]\n"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "missing" {
				content := "COMPOSE_PROFILES=\n"
				if scenario == "private" {
					content = "DB_PASSWORD=secret\n"
				}
				if err := os.WriteFile(filepath.Join(root, ".env.stack.test"), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := Cmd{Root: root, Stack: "test"}
			err := c.Run()
			if err == nil {
				t.Fatal("build unexpectedly proceeded")
			}
			if scenario == "old-runtime" && !strings.Contains(err.Error(), "forj render") {
				t.Fatalf("missing upgrade instruction: %v", err)
			}
		})
	}
}

// TestBuildStackDefaultsFollowCompiledApp checks the actual linker payload for routed, explicit, and fallback targets.
func TestBuildStackDefaultsFollowCompiledApp(t *testing.T) {
	for _, tc := range []struct {
		name, selected, want string
		args                 []string
		missingAdmin         bool
	}{
		{name: "default", want: "sqlite"},
		{name: "default-without-named-apps", want: "sqlite"},
		{name: "routed", selected: "admin", want: "mysql"},
		{name: "explicit", args: []string{"./cmd/admin"}, want: "mysql"},
		{name: "explicit-overrides-route", selected: "app", args: []string{"./cmd/admin"}, want: "mysql"},
		{name: "default-overrides-route", selected: "admin", args: []string{"./cmd/app"}, want: "sqlite"},
		{name: "flags", args: []string{"-o", "./bin/custom", "-tags=release", "-ldflags", "-s -w", "./cmd/admin"}, want: "mysql"},
		{name: "pgo", args: []string{"-pgo", "default.pgo", "./cmd/admin"}, want: "mysql"},
		{name: "coverage", args: []string{"-covermode", "atomic", "-coverpkg", "./...", "./cmd/admin"}, want: "mysql"},
		{name: "separator", args: []string{"--", "./cmd/admin"}, want: "mysql"},
		{name: "module", args: []string{"example.org/stack/cmd/admin"}, want: "mysql"},
		{name: "absolute", want: "mysql"},
		{name: "fallback", selected: "admin", missingAdmin: true, want: "sqlite"},
		{name: "root", selected: "admin", args: []string{"."}, want: "sqlite"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("FORJ_APP", tc.selected)
			config := "project_name: stack\nmodule_name: example.org/stack\nrender:\n  components: [cli]\n"
			if tc.name != "default-without-named-apps" {
				config += "apps:\n  admin:\n    components: [cli]\n"
			}
			for name, content := range map[string]string{
				"go.mod":      "module example.org/stack\n",
				".goforj.yml": config,
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"app", "admin"} {
				if name == "admin" && tc.missingAdmin {
					continue
				}
				if err := os.MkdirAll(filepath.Join(root, "cmd", name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "absolute" {
				tc.args = []string{filepath.Join(root, "cmd", "admin")}
			}
			c := Cmd{Args: tc.args, stackDefaults: map[string]string{"DB_DRIVER": "sqlite", "ADMIN_DB_DRIVER": "mysql"}}
			if tc.name == "default-without-named-apps" {
				delete(c.stackDefaults, "ADMIN_DB_DRIVER")
			}
			args, err := c.buildArgs(root)
			if err != nil {
				t.Fatal(err)
			}
			_, encoded, ok := strings.Cut(strings.Join(args, " "), "CompiledStackDefaultsBase64=")
			if !ok {
				t.Fatal("missing stack payload")
			}
			data, err := base64.StdEncoding.DecodeString(strings.Fields(encoded)[0])
			if err != nil {
				t.Fatal(err)
			}
			var defaults map[string]string
			if err := json.Unmarshal(data, &defaults); err != nil {
				t.Fatal(err)
			}
			if defaults["DB_DRIVER"] != tc.want {
				t.Fatalf("embedded driver %q, want %q", defaults["DB_DRIVER"], tc.want)
			}
			if _, exists := defaults["ADMIN_DB_DRIVER"]; exists {
				t.Fatal("unfolded App overlay would override runtime base keys")
			}
		})
	}
}

// TestBuildStackRejectsAmbiguousTargetsBeforeGeneration prevents one App's defaults from being shared across unknown binaries.
func TestBuildStackRejectsAmbiguousTargetsBeforeGeneration(t *testing.T) {
	for _, args := range [][]string{{"./cmd/app", "./cmd/admin"}, {"./cmd/..."}, {"./custom"}, {"main.go"}, {"./cmd/unknown"}, {"-C", "sub", "./cmd/app"}, {"-C=sub", "./cmd/app"}, {"--C=sub", "./cmd/app"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := t.TempDir()
			for name, content := range map[string]string{
				"go.mod":                       "module example.org/stack\n",
				".goforj.yml":                  "project_name: stack\nmodule_name: example.org/stack\napps:\n  app:\n    components: [cli]\n",
				".env.stack.portable":          "COMPOSE_PROFILES=\n",
				"internal/cmd/env_defaults.go": "var CompiledStackDefaultsBase64 string\n",
			} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			c := Cmd{Root: root, Stack: "portable", Args: args}
			if err := c.Run(); err == nil || !strings.Contains(err.Error(), "--stack requires") {
				t.Fatalf("expected target rejection before pipeline work, got %v", err)
			}
			c.stackDefaults = nil
			c.Stack = ""
			if _, err := c.buildArgs(root); err != nil {
				t.Fatalf("ordinary build acquired a new restriction: %v", err)
			}
		})
	}
}
