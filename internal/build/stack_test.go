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
