package stacks

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixture models a services-first project with a second App and named resources.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, ".goforj.yml", "project_name: stacks\nmodule_name: example.org/stacks\napps:\n  app:\n    components:\n      database_mysql: true\n      cache: true\n      jobs: true\n      events: true\n      storage: true\n      mail: true\n  admin:\n    components:\n      database_mysql: true\n      cache: true\n")
	put(t, root, ".env", "# Keep my comments\nAPP_KEY='unrelated-secret'\nAPP_NAME=Stacks\nDB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\nDB_DATABASE=production\nDB_PASSWORD='line one\nline two'\nCACHE_DRIVER=redis\nCACHE_SUPPORTED_DRIVERS=memory,redis\nCACHE_SESSIONS_DRIVER=redis\nADMIN_CACHE_DRIVER=redis\nQUEUE_DRIVER=redis\nEVENTS_DRIVER=redis\nSTORAGE_DRIVER=s3\nMAIL_DRIVER=smtp\nCOMPOSE_PROFILES=mysql,redis\n")
	return root
}

// put creates fixture files with private permissions.
func put(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// openTest fails close to the configuration that could not be read.
func openTest(t *testing.T, root string) *Session {
	t.Helper()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestPortableRoundTrip preserves private values, named overrides, and unrelated source bytes.
func TestPortableRoundTrip(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	original := clone(s.Current)
	if err := s.Save("services", s.Current); err != nil {
		t.Fatal(err)
	}
	public, err := os.ReadFile(filepath.Join(root, ".env.stack.services"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(public, []byte("PASSWORD")) || bytes.Contains(public, []byte("production")) {
		t.Fatalf("private values leaked: %s", public)
	}
	s = openTest(t, root)
	portable, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"DB_DRIVER": "sqlite", "CACHE_DRIVER": "memory", "CACHE_SESSIONS_DRIVER": "memory", "ADMIN_CACHE_DRIVER": "memory", "QUEUE_DRIVER": "workerpool", "EVENTS_DRIVER": "inproc", "STORAGE_DRIVER": "local", "MAIL_DRIVER": "log", "COMPOSE_PROFILES": ""} {
		if portable[key] != want {
			t.Errorf("%s = %s, want %s", key, portable[key], want)
		}
	}
	if !strings.Contains(portable["DB_SUPPORTED_DRIVERS"], "mysql") {
		t.Fatal("lost previous driver support")
	}
	if err := s.Save("portable", portable); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	if err := s.Activate("portable", portable, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !bytes.Contains(raw, []byte("# Keep my comments\nAPP_KEY='unrelated-secret'\nAPP_NAME=Stacks\n")) {
		t.Fatalf("changed unrelated settings: %s", raw)
	}
	s = openTest(t, root)
	if s.Active != "portable" || s.Changed() {
		t.Fatal("incorrect active stack state")
	}
	services, err := s.Load("services", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate("services", services, true); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	if !reflect.DeepEqual(s.Current, original) {
		t.Fatalf("round trip changed settings: %#v", s.Current)
	}
	for _, name := range []string{stateName, ".env.stack.services.local", ".env.stack.portable.local"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s permissions = %o", name, info.Mode().Perm())
		}
	}
}

// TestWorkingCopyPreservesDeletedAndEmptyValues avoids reviving a removed committed assignment.
func TestWorkingCopyPreservesDeletedAndEmptyValues(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	if err := s.Save("services", s.Current); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	if err := s.Activate("services", s.Current, true); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	edited := clone(s.Current)
	delete(edited, "COMPOSE_PROFILES")
	edited["DB_PASSWORD"] = ""
	put(t, root, ".env", string(s.env.replace(func(key string) bool { return managedKey(s.config, key) }, edited)))
	s = openTest(t, root)
	if !s.Changed() {
		t.Fatal("manual edits not detected")
	}
	portable, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate("", portable, true); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	saved, err := s.Load("services", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved["COMPOSE_PROFILES"]; ok {
		t.Fatal("removed public setting revived")
	}
	if value, ok := saved["DB_PASSWORD"]; !ok || value != "" {
		t.Fatal("explicitly empty password lost")
	}
	if !equalValues(s.Previous(), edited) {
		t.Fatal("recovery snapshot does not preserve exact managed settings")
	}
}

// TestActivationRejectsConcurrentChanges checks the environment and selected definition after prompting.
func TestActivationRejectsConcurrentChanges(t *testing.T) {
	for _, name := range []string{".env", ".env.stack.services", ".gitignore"} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			s := openTest(t, root)
			if err := s.Save("services", s.Current); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, root)
			values, err := s.Load("services", true)
			if err != nil {
				t.Fatal(err)
			}
			put(t, root, name, "# concurrent edit\n")
			err = s.Activate("services", values, true)
			if err == nil || !strings.Contains(err.Error(), "changed while") {
				t.Fatalf("got %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, stateName)); !os.IsNotExist(err) {
				t.Fatal("wrote state after concurrent edit")
			}
		})
	}
}

// TestCommitRollsBackNewAndExistingFiles exercises a failure after a replacement was already published.
func TestCommitRollsBackNewAndExistingFiles(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[exists], func(t *testing.T) {
			root := t.TempDir()
			if exists {
				put(t, root, "a", "original")
			}
			put(t, root, "b", "untouched")
			a, _ := readFile(root, "a")
			b, _ := readFile(root, "b")
			a.after = []byte("changed")
			b.after = []byte("changed")
			count := 0
			err := commit(root, []file{a, b}, func(from, to string) error {
				count++
				if count == 2 {
					return errors.New("injected")
				}
				return os.Rename(from, to)
			})
			if err == nil {
				t.Fatal("expected failure")
			}
			raw, readErr := os.ReadFile(filepath.Join(root, "a"))
			if exists && (readErr != nil || string(raw) != "original") {
				t.Fatalf("restore failed: %q %v", raw, readErr)
			}
			if !exists && !os.IsNotExist(readErr) {
				t.Fatal("new file survived rollback")
			}
			matches, _ := filepath.Glob(filepath.Join(root, ".env.stack-tmp-*"))
			if len(matches) > 0 {
				t.Fatal("temporary files survived")
			}
		})
	}
}

// TestValidationAndFileFailures rejects unsafe names, malformed values, and ambiguous filesystem ownership.
func TestValidationAndFileFailures(t *testing.T) {
	for _, name := range []string{"", "../secret", "production.local", "UPPER", "a/b", strings.Repeat("a", 49)} {
		if err := ValidateName(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	root := fixture(t)
	s := openTest(t, root)
	for _, values := range []map[string]string{{"APP_KEY": "secret"}, {"DB_BAD\nKEY": "x"}, {"DB_DRIVER": "unknown"}, {"CACHE_DRIVER": "redis", "CACHE_SUPPORTED_DRIVERS": "memory"}, {"CACHE_SUPPORTED_DRIVERS": "wrong"}} {
		if err := s.Validate(values); err == nil {
			t.Errorf("accepted %#v", values)
		}
	}
	put(t, root, stateName, `{"version":99}`)
	if _, err := Open(root); err == nil {
		t.Fatal("accepted unknown state version")
	}
	put(t, root, stateName, `broken`)
	if _, err := Open(root); err == nil {
		t.Fatal("accepted broken state")
	}
	if err := os.Remove(filepath.Join(root, stateName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".env")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("accepted missing env")
	}
	put(t, root, "target", "DB_DRIVER=mysql\n")
	if err := os.Symlink(filepath.Join(root, "target"), filepath.Join(root, ".env")); err != nil {
		t.Skip(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("accepted symlink")
	}
}

// TestBuildDefaultsNeverIncludesPrivateOverrides protects the binary boundary independently of activation.
func TestBuildDefaultsNeverIncludesPrivateOverrides(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	if err := s.Save("services", s.Current); err != nil {
		t.Fatal(err)
	}
	put(t, root, ".env.stack.services.local", "DB_PASSWORD=very-private\nCOMPOSE_PROFILES=private\n")
	values, err := BuildDefaults(root, "services")
	if err != nil {
		t.Fatal(err)
	}
	if values["COMPOSE_PROFILES"] != "mysql,redis" {
		t.Fatal("lost comma-containing definition")
	}
	if _, ok := values["DB_PASSWORD"]; ok {
		t.Fatal("embedded private password")
	}
	put(t, root, ".env.stack.services", "DB_PASSWORD=secret\n")
	if _, err := BuildDefaults(root, "services"); err == nil {
		t.Fatal("accepted connection settings for embedding")
	}
}

// TestGitProtectionKeepsDefinitionsShareableAndPrivateFilesIgnored verifies real Git precedence.
func TestGitProtectionKeepsDefinitionsShareableAndPrivateFilesIgnored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip(err)
	}
	root := fixture(t)
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	put(t, root, ".gitignore", ".env*\n")
	s := openTest(t, root)
	if err := s.Save("services", s.Current); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{".env.stack.services": false, ".env.stack.services.local": true, stateName: true, ".env.stack-lock.local": true} {
		cmd := exec.Command("git", "check-ignore", "-q", name)
		cmd.Dir = root
		got := cmd.Run() == nil
		if got != want {
			t.Errorf("%s ignored=%v", name, got)
		}
	}
	cmd = exec.Command("git", "add", "-f", ".env.stack.services.local")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	if err := s.Save("services", s.Current); err == nil {
		t.Fatal("wrote tracked private file")
	}
}

// TestPortableDiscoversImplicitNamedDatabasesAndClearsDSNs prevents inherited connection strings from defeating the selected driver.
func TestPortableDiscoversImplicitNamedDatabasesAndClearsDSNs(t *testing.T) {
	root := fixture(t)
	f, err := os.OpenFile(filepath.Join(root, ".env"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("DB_DSN=old-mysql-dsn\nDB_ANALYTICS_DATABASE=analytics\nDB_ANALYTICS_DSN=old-analytics-dsn\nADMIN_DB_REPORTS_DATABASE=reports\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, root)
	values, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, prefix := range []string{"DB_", "DB_ANALYTICS_", "ADMIN_DB_", "ADMIN_DB_REPORTS_"} {
		if values[prefix+"DRIVER"] != "sqlite" || values[prefix+"DSN"] != "" {
			t.Errorf("%s not portable", prefix)
		}
		path := values[prefix+"SQLITE_DATABASE"]
		if path == "" || paths[path] {
			t.Errorf("missing or shared path: %s", path)
		}
		paths[path] = true
	}
	if err := s.Save("portable", values); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".env")); err != nil {
		t.Fatal(err)
	}
	defaults, err := BuildDefaults(root, "portable")
	if err != nil {
		t.Fatal(err)
	}
	if defaults["DB_SQLITE_DATABASE"] != values["DB_SQLITE_DATABASE"] {
		t.Fatal("portable path missing from baked defaults")
	}
	app, err := AppDefaults(root, defaults, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if app["DB_SQLITE_DATABASE"] != values["ADMIN_DB_SQLITE_DATABASE"] {
		t.Fatal("selected App path not folded")
	}
	for key := range app {
		if strings.HasPrefix(key, "ADMIN_") {
			t.Fatal("baked App overlay would override explicit runtime base keys")
		}
	}
}

// TestOverridesNamesLayersWithoutLeakingValues keeps the activation preview honest about runtime precedence.
func TestOverridesNamesLayersWithoutLeakingValues(t *testing.T) {
	root := fixture(t)
	put(t, root, ".env.local", "DB_DRIVER=postgres\nDB_PASSWORD=never-display-this\nAPP_NAME=unrelated\n")
	t.Setenv("CACHE_DRIVER", "redis")
	s := openTest(t, root)
	layers, err := s.Overrides()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(layers, "\n")
	if !strings.Contains(joined, ".env.local: DB_DRIVER, DB_PASSWORD") || !strings.Contains(joined, "process environment: CACHE_DRIVER") {
		t.Fatalf("missing layers: %s", joined)
	}
	if strings.Contains(joined, "never-display-this") || strings.Contains(joined, "APP_NAME") {
		t.Fatal("printed values or unrelated keys")
	}
	put(t, root, ".env.local", "DB_PASSWORD='unterminated")
	if _, err := s.Overrides(); err == nil {
		t.Fatal("accepted invalid overlay")
	}
}

// TestPrepareSaveDetectsAConcurrentPrivateEdit protects existing credentials while the confirmation prompt is open.
func TestPrepareSaveDetectsAConcurrentPrivateEdit(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	if err := s.PrepareSave("new"); err != nil {
		t.Fatal(err)
	}
	put(t, root, ".env.stack.new.local", "DB_PASSWORD=someone-elses-edit\n")
	if err := s.Save("new", s.Current); err == nil {
		t.Fatal("overwrote concurrent private edit")
	}
}

// TestPortableUsesExampleInventoryWithoutChangingAbsentOriginalValues includes named resources before their first local override.
func TestPortableUsesExampleInventoryWithoutChangingAbsentOriginalValues(t *testing.T) {
	root := fixture(t)
	put(t, root, ".env.example", "DB_ANALYTICS_DATABASE=analytics\n")
	s := openTest(t, root)
	values, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	if values["DB_ANALYTICS_DRIVER"] != "sqlite" {
		t.Fatal("example-only named resource missing")
	}
	if _, ok := s.Current["DB_ANALYTICS_DRIVER"]; ok {
		t.Fatal("changed original absence")
	}
}

// TestReadOnlyAndLockedConfigurationsFailBeforeWriting covers mutation preflight failures.
func TestReadOnlyAndLockedConfigurationsFailBeforeWriting(t *testing.T) {
	root := fixture(t)
	if err := os.Chmod(filepath.Join(root, ".env"), 0400); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, root)
	if err := s.Activate("", s.Current, false); err == nil {
		t.Fatal("wrote read-only env")
	}
	if err := os.Chmod(filepath.Join(root, ".env"), 0600); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	put(t, root, ".env.stack-lock.local", "")
	if err := s.Save("services", s.Current); err == nil {
		t.Fatal("ignored operation lock")
	}
}

// TestOpenRejectsAnUnsafeActiveName prevents private state from escaping the definition filename convention.
func TestOpenRejectsAnUnsafeActiveName(t *testing.T) {
	root := fixture(t)
	put(t, root, stateName, `{"version":1,"active":"../../outside"}`)
	if _, err := Open(root); err == nil {
		t.Fatal("accepted unsafe active name")
	}
}

// TestLocalIsAValidStackName keeps conventional local profiles distinct from their private .local override suffix.
func TestLocalIsAValidStackName(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	if err := s.Save("local", s.Current); err != nil {
		t.Fatal(err)
	}
	fresh := openTest(t, root)
	names, err := fresh.Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "local" {
		t.Fatalf("names = %#v", names)
	}
	if _, err := fresh.Load("local", true); err != nil {
		t.Fatal(err)
	}
}

// TestRecoveryIsPublishedBeforeTheActiveEnvironment preserves a discoverable backup if a process is interrupted during publication.
func TestRecoveryIsPublishedBeforeTheActiveEnvironment(t *testing.T) {
	root := t.TempDir()
	put(t, root, ".env", "DB_DRIVER=mysql\n")
	active, _ := readFile(root, ".env")
	recovery, _ := readFile(root, stateName)
	ignore, _ := readFile(root, ".gitignore")
	active.after = []byte("DB_DRIVER=sqlite\n")
	recovery.after = []byte(`{"previous":{"DB_DRIVER":"mysql"}}`)
	ignore.after = []byte(".env.stack-state.local\n")
	sawActive := false
	err := commit(root, []file{active, recovery, ignore}, func(from, to string) error {
		switch filepath.Base(to) {
		case stateName:
			if _, err := os.Stat(filepath.Join(root, ".gitignore")); err != nil {
				t.Fatal("private snapshot published before ignore rules")
			}
		case ".env":
			saved, err := os.ReadFile(filepath.Join(root, stateName))
			if err != nil || !bytes.Equal(saved, recovery.after) {
				t.Fatal("active environment published before recovery snapshot")
			}
			sawActive = true
		}
		return os.Rename(from, to)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawActive {
		t.Fatal("active file not published")
	}
}
