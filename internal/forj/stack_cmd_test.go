package forj

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/goforj/console"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/goforj/goforj/internal/stacks"
)

// stackWizardFixture models the no-argument services-to-portable authoring flow.
func stackWizardFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		".goforj.yml": "project_name: demo\nmodule_name: example.org/demo\napps:\n  app:\n    components:\n      database_mysql: true\n      cache: true\n",
		".env":        "APP_KEY=must-not-print\nDB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\nDB_PASSWORD=must-not-print\nCACHE_DRIVER=redis\nCACHE_SUPPORTED_DRIVERS=memory,redis\nCOMPOSE_PROFILES=mysql,redis\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestStackCommandNoArgumentsPreviewsAndConfirms verifies real command parsing and interactive activation.
func TestStackCommandNoArgumentsPreviewsAndConfirms(t *testing.T) {
	root := stackWizardFixture(t)
	var output bytes.Buffer
	command := RootCmd{StackCmd: StackCmd{root: root, ui: moduleRenameTestConsole("1\n1\n1\nyes\n", &output)}}
	parser, err := kong.New(&command)
	if err != nil {
		t.Fatal(err)
	}
	context, err := parser.Parse([]string{"stack"})
	if err != nil {
		t.Fatal(err)
	}
	if err := context.Run(); err != nil {
		t.Fatal(err)
	}
	s, err := stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Current["DB_DRIVER"] != "sqlite" || s.Current["CACHE_DRIVER"] != "memory" || s.Current["COMPOSE_PROFILES"] != "" {
		t.Fatalf("unexpected config: %#v", s.Current)
	}
	for _, want := range []string{"DB_DRIVER: mysql -> sqlite", "Apply these settings to .env", "Database contents stay in place", "Updated .env"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q in %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "must-not-print") {
		t.Fatal("secret printed")
	}
}

// TestStackCancellationNeverWrites covers EOF, menu cancellation, and rejected activation.
func TestStackCancellationNeverWrites(t *testing.T) {
	for _, input := range []string{"7\n", "1\n1\n1\nno\n", "1\n1\n1\n", ""} {
		t.Run(input, func(t *testing.T) {
			root := stackWizardFixture(t)
			before, _ := os.ReadFile(filepath.Join(root, ".env"))
			var output bytes.Buffer
			_ = (&StackCmd{root: root, ui: moduleRenameTestConsole(input, &output)}).Run()
			after, _ := os.ReadFile(filepath.Join(root, ".env"))
			if !bytes.Equal(before, after) {
				t.Fatal("canceled wizard changed env")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 2 {
				t.Fatal("canceled wizard created files")
			}
		})
	}
}

// TestStackWizardSavesWithoutActivating preserves the distinction between a definition and current settings.
func TestStackWizardSavesWithoutActivating(t *testing.T) {
	root := stackWizardFixture(t)
	before, _ := os.ReadFile(filepath.Join(root, ".env"))
	var output bytes.Buffer
	if err := (&StackCmd{root: root, ui: moduleRenameTestConsole("3\nservices\nyes\n", &output)}).Run(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !bytes.Equal(before, after) {
		t.Fatal("save activated stack")
	}
	s, err := stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	values, err := s.Load("services", true)
	if err != nil {
		t.Fatal(err)
	}
	if values["DB_PASSWORD"] != "must-not-print" {
		t.Fatal("lost saved credentials")
	}
}

// TestStackWizardCreatePortableSaveAndActivate covers both confirmations in one interactive flow.
func TestStackWizardCreatePortableSaveAndActivate(t *testing.T) {
	root := stackWizardFixture(t)
	var output bytes.Buffer
	if err := (&StackCmd{root: root, ui: moduleRenameTestConsole("4\n1\n1\nportable\nyes\nyes\n", &output)}).Run(); err != nil {
		t.Fatal(err)
	}
	s, err := stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Active != "portable" || s.Current["DB_DRIVER"] != "sqlite" {
		t.Fatal("portable stack not active")
	}
}

// TestStackWizardSwitchRestoreAndEmptyInventory covers saved and unnamed round trips through the menu.
func TestStackWizardSwitchRestoreAndEmptyInventory(t *testing.T) {
	root := stackWizardFixture(t)
	var output bytes.Buffer
	for _, input := range []string{"2\n", "5\n", "6\n"} {
		if err := (&StackCmd{root: root, ui: moduleRenameTestConsole(input, &output)}).Run(); err != nil {
			t.Fatal(err)
		}
	}
	s, err := stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	portable, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("portable", portable); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"2\n2\n", "2\n1\nyes\n", "6\nyes\n"} {
		if err := (&StackCmd{root: root, ui: moduleRenameTestConsole(input, &output)}).Run(); err != nil {
			t.Fatal(err)
		}
	}
	s, err = stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Current["DB_DRIVER"] != "mysql" {
		t.Fatal("restore did not recover services")
	}
}

// TestStackDriverEditorSupportsDriversAndEmptyProfiles exercises editable choices after taking the current configuration as a base.
func TestStackDriverEditorSupportsDriversAndEmptyProfiles(t *testing.T) {
	root := stackWizardFixture(t)
	s, err := stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	input := fmt.Sprintf("2\n1\n%d\n\n1\n", len(s.Resources)+2)
	values, err := editStackDrivers(moduleRenameTestConsole(input, &output), s, s.Current)
	if err != nil {
		t.Fatal(err)
	}
	if values["CACHE_DRIVER"] != "memory" || values["COMPOSE_PROFILES"] != "" {
		t.Fatalf("editor lost selections: %#v; %s", values, output.String())
	}
	values, err = editStack(moduleRenameTestConsole("2\n1\n", &output), s, s.Current)
	if err != nil {
		t.Fatal(err)
	}
	if values["DB_DRIVER"] != "mysql" {
		t.Fatal("current base changed driver")
	}
}

// TestStackActiveEditsCanBeKeptDiscardedOrCanceled verifies explicit working-copy decisions.
func TestStackActiveEditsCanBeKeptDiscardedOrCanceled(t *testing.T) {
	for _, choice := range []string{"1\nyes\n", "2\nyes\n", "3\n"} {
		t.Run(choice, func(t *testing.T) {
			root := stackWizardFixture(t)
			s, err := stacks.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Save("services", s.Current); err != nil {
				t.Fatal(err)
			}
			s, err = stacks.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Activate("services", s.Current, true); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(filepath.Join(root, ".env"), os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString("DB_PASSWORD=edited\n"); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = stacks.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			portable, err := s.Portable()
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := activateStack(moduleRenameTestConsole(choice, &output), s, "", portable); err != nil {
				t.Fatal(err)
			}
			after, err := stacks.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if choice == "3\n" {
				if after.Current["DB_DRIVER"] != "mysql" {
					t.Fatal("cancel activated")
				}
				return
			}
			saved, err := after.Load("services", true)
			if err != nil {
				t.Fatal(err)
			}
			want := "must-not-print"
			if strings.HasPrefix(choice, "1") {
				want = "edited"
			}
			if saved["DB_PASSWORD"] != want {
				t.Fatal("incorrect working copy")
			}
		})
	}
}

// TestStackCreateFromCurrentAndExistingCoversReplacement confirms neither creation base implicitly activates.
func TestStackCreateFromCurrentAndExistingCoversReplacement(t *testing.T) {
	root := stackWizardFixture(t)
	var output bytes.Buffer
	for _, input := range []string{"4\n2\n1\nservices\nyes\nno\n", "4\n3\n1\nservices\nyes\nno\n", "3\n\n", "3\nINVALID\n"} {
		err := (&StackCmd{root: root, ui: moduleRenameTestConsole(input, &output)}).Run()
		if strings.Contains(input, "INVALID") {
			if err == nil {
				t.Fatal("accepted invalid name")
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	s, err := stacks.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Active != "" {
		t.Fatal("saving activated stack")
	}
}

// TestStackHiddenSettingsNeverEchoSecrets covers accepted, canceled, invalid, and failing secret entry.
func TestStackHiddenSettingsNeverEchoSecrets(t *testing.T) {
	for _, scenario := range []string{"valid", "invalid", "cancel", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			root := stackWizardFixture(t)
			s, err := stacks.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			key := "DB_PASSWORD"
			if scenario == "invalid" {
				key = "APP_KEY"
			}
			if scenario == "cancel" {
				key = ""
			}
			var output bytes.Buffer
			interactive := true
			enabled := false
			ui := console.New(console.Config{Stdin: strings.NewReader(fmt.Sprintf("%d\n%s\n1\n", len(s.Resources)+3, key)), Stdout: &output, Stderr: &output, InteractiveEnabled: &interactive, ColorEnabled: &enabled, UnicodeEnabled: &enabled, ReadSecret: func() (string, error) {
				if scenario == "failure" {
					return "", errors.New("secret reader failed")
				}
				return "hidden-new-secret", nil
			}})
			values, err := editStackDrivers(ui, s, s.Current)
			if scenario == "failure" {
				if err == nil {
					t.Fatal("ignored secret failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "valid" && values["DB_PASSWORD"] != "hidden-new-secret" {
				t.Fatal("secret not saved")
			}
			if scenario == "invalid" {
				if _, ok := values["APP_KEY"]; ok {
					t.Fatal("claimed unrelated key")
				}
			}
			if strings.Contains(output.String(), "hidden-new-secret") {
				t.Fatal("secret echoed")
			}
		})
	}
}

// TestStackPromptFailuresPropagateBeforeActivation exercises interrupted prompts throughout the menu.
func TestStackPromptFailuresPropagateBeforeActivation(t *testing.T) {
	for _, input := range []string{"2\n", "4\n", "4\n1\n", "4\n2\n1\n", "4\n3\n1\n", "5\n", "5\n1\n", "1\n", "1\n1\n2\n", "3\n", "3\nservices\n"} {
		t.Run(input, func(t *testing.T) {
			root := stackWizardFixture(t)
			s, err := stacks.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Save("services", s.Current); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(root, ".env"))
			var output bytes.Buffer
			err = (&StackCmd{root: root, ui: moduleRenameTestConsole(input, &output)}).Run()
			if err == nil {
				t.Fatal("expected EOF")
			}
			after, _ := os.ReadFile(filepath.Join(root, ".env"))
			if !bytes.Equal(before, after) {
				t.Fatal("interrupted prompt changed env")
			}
		})
	}
}

// TestStackCommandReportsMalformedProjectsAndProfiles checks command-level read failures.
func TestStackCommandReportsMalformedProjectsAndProfiles(t *testing.T) {
	for _, name := range []string{".goforj.yml", ".env", ".env.stack.bad.name"} {
		t.Run(name, func(t *testing.T) {
			root := stackWizardFixture(t)
			if err := os.WriteFile(filepath.Join(root, name), []byte("malformed '"), 0600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := (&StackCmd{root: root, ui: moduleRenameTestConsole("7\n", &output)}).Run(); err == nil {
				t.Fatal("accepted malformed project")
			}
		})
	}
}

// TestStackFilesDoNotTriggerDevEnvironmentRebuilds keeps saving inactive definitions distinct from activating their settings.
func TestStackFilesDoNotTriggerDevEnvironmentRebuilds(t *testing.T) {
	root := stackWizardFixture(t)
	t.Chdir(root)
	before, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env.stack.services", ".env.stack.services.local", ".env.stack-state.local", ".env.stack-lock.local", ".env.stack-tmp-123.local"} {
		if err := os.WriteFile(name, []byte("DB_DRIVER=postgres\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	after, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatal(err)
	}
	if devEnvFilesChanged(before, after) {
		t.Fatal("saving inactive stack triggered an environment rebuild")
	}
	if err := os.WriteFile(".env", []byte("DB_DRIVER=sqlite\n"), 0600); err != nil {
		t.Fatal(err)
	}
	activated, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !devEnvFilesChanged(after, activated) {
		t.Fatal("activation did not trigger the normal environment rebuild")
	}
	if err := os.WriteFile(".env.stack-custom", []byte("DB_DRIVER=postgres\n"), 0600); err != nil {
		t.Fatal(err)
	}
	custom, err := snapshotDevEnvFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !devEnvFilesChanged(activated, custom) {
		t.Fatal("unrelated existing runtime layer was suppressed")
	}
}
