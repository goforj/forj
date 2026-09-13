package stacks

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goforj/goforj/project"
)

// TestStackDriverAliasesRoundTrip keeps configurations accepted by generation usable in saved Stacks.
func TestStackDriverAliasesRoundTrip(t *testing.T) {
	for _, pair := range [][2]string{{"mysql", "mariadb"}, {"postgres", "postgresql"}, {"sqlite", "sqlite3"}} {
		for _, reverse := range []bool{false, true} {
			active, supported := pair[0], pair[1]
			if reverse {
				active, supported = supported, active
			}
			t.Run(active+"-"+supported, func(t *testing.T) {
				root := fixture(t)
				s := openTest(t, root)
				values := map[string]string{"DB_DRIVER": active, "DB_SUPPORTED_DRIVERS": supported, "DB_REPORTS_DRIVER": active, "ADMIN_DB_DRIVER": active}
				if err := s.Save("aliases", values); err != nil {
					t.Fatal(err)
				}
				s = openTest(t, root)
				loaded, err := s.Load("aliases", true)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Activate("aliases", loaded, false); err != nil {
					t.Fatal(err)
				}
				if !equalValues(openTest(t, root).Current, values) {
					t.Fatal("alias configuration changed during round trip")
				}
				for _, resource := range s.Resources {
					if resource.Key == "DB_DRIVER" {
						values["DB_SUPPORTED_DRIVERS"] = " " + pair[1] + "," + pair[0]
						if err := SetDriver(values, resource, active); err != nil {
							t.Fatal(err)
						}
						if values["DB_SUPPORTED_DRIVERS"] != pair[0] {
							t.Fatalf("duplicate alias support: %s", values["DB_SUPPORTED_DRIVERS"])
						}
					}
				}
			})
		}
	}
}

// TestStackPortableRetainsPrivateServiceDatabaseNames prevents SQLite paths from becoming service database names or exposing them in definitions.
func TestStackPortableRetainsPrivateServiceDatabaseNames(t *testing.T) {
	root := fixture(t)
	put(t, root, ".env", "DB_DRIVER=mysql\nDB_DATABASE=production\nDB_ANALYTICS_DATABASE=analytics\nADMIN_DB_DATABASE=admin\nADMIN_DB_REPORTS_DATABASE=reports\n")
	s := openTest(t, root)
	values, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	for key, original := range s.Current {
		if values[key] != original && key != "DB_DRIVER" {
			t.Fatalf("portable replaced %s", key)
		}
	}
	if err := s.Save("portable", values); err != nil {
		t.Fatal(err)
	}
	defaults, err := BuildDefaults(root, "portable")
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"DB_", "DB_ANALYTICS_", "ADMIN_DB_", "ADMIN_DB_REPORTS_"} {
		if _, ok := defaults[prefix+"DATABASE"]; ok {
			t.Fatalf("private service database embedded: %s", prefix)
		}
		if defaults[prefix+"SQLITE_DATABASE"] == "" {
			t.Fatalf("missing portable path: %s", prefix)
		}
	}
	s = openTest(t, root)
	loaded, err := s.Load("portable", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, driver := range []string{"mysql", "postgres"} {
		for _, resource := range s.Resources {
			if resource.Definition.Key == project.ResourceDatabase {
				if err := SetDriver(loaded, resource, driver); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, key := range []string{"DB_DATABASE", "DB_ANALYTICS_DATABASE", "ADMIN_DB_DATABASE", "ADMIN_DB_REPORTS_DATABASE"} {
			if loaded[key] != s.Current[key] {
				t.Fatalf("%s reused a SQLite path after selecting %s", key, driver)
			}
		}
	}
}

// TestStackSQLiteShareableFallback preserves legacy SQLite paths while keeping unused service names private.
func TestStackSQLiteShareableFallback(t *testing.T) {
	for _, driver := range []string{"sqlite", "sqlite3"} {
		values := map[string]string{"DB_DRIVER": driver, "DB_DATABASE": "legacy.db"}
		if shareable(values)["DB_DATABASE"] != "legacy.db" {
			t.Fatal("lost legacy SQLite database path")
		}
		values["DB_SQLITE_DATABASE"] = "portable.db"
		public := shareable(values)
		if _, ok := public["DB_DATABASE"]; ok {
			t.Fatal("included unused generic database name")
		}
		if public["DB_SQLITE_DATABASE"] != "portable.db" {
			t.Fatal("lost dedicated SQLite path")
		}
	}
}

// TestStackSaveActiveUpdatesBaselineOnlyForCurrentValues preserves recovery and distinguishes saving from activation.
func TestStackSaveActiveUpdatesBaselineOnlyForCurrentValues(t *testing.T) {
	for _, scenario := range []string{"current", "different-values", "different-name", "concurrent-state"} {
		t.Run(scenario, func(t *testing.T) {
			root := fixture(t)
			s := openTest(t, root)
			if err := s.Save("services", s.Current); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, root)
			if err := s.Activate("services", s.Current, false); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, root)
			previous := s.Previous()
			edited := clone(s.Current)
			edited["DB_PASSWORD"] = "edited"
			put(t, root, ".env", string(s.env.replace(func(key string) bool { return managedKey(s.config, key) }, edited)))
			s = openTest(t, root)
			values := clone(s.Current)
			name := "services"
			if scenario == "different-values" {
				values["DB_PASSWORD"] = "planned"
			}
			if scenario == "different-name" {
				name = "other"
			}
			before, err := os.ReadFile(filepath.Join(root, ".env.stack.services.local"))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "concurrent-state" {
				put(t, root, stateName, "concurrent")
			}
			err = s.Save(name, values)
			if scenario == "concurrent-state" {
				if err == nil || !s.Changed() {
					t.Fatal("failed save advanced the baseline")
				}
				after, err := os.ReadFile(filepath.Join(root, ".env.stack.services.local"))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("failed save changed private settings")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantChanged := scenario != "current"
			if s.Changed() != wantChanged {
				t.Fatal("in-memory baseline is stale")
			}
			s = openTest(t, root)
			if s.Changed() != wantChanged || !s.HasPrevious() || !equalValues(s.Previous(), previous) || !equalValues(s.Current, edited) {
				t.Fatal("save changed activation or recovery state")
			}
		})
	}
}

// TestStackUnchangedDeparturePreservesKeptSnapshot covers stale same-active targets even outside the wizard.
func TestStackUnchangedDeparturePreservesKeptSnapshot(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	if err := s.Save("services", s.Current); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	if err := s.Activate("services", s.Current, false); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	edited := clone(s.Current)
	edited["DB_PASSWORD"] = "kept-edit"
	put(t, root, ".env", string(s.env.replace(func(key string) bool { return managedKey(s.config, key) }, edited)))
	s = openTest(t, root)
	loaded, err := s.Load("services", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate("services", loaded, true); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, root)
	portable, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Activate("", portable, true); err != nil {
		t.Fatal(err)
	}
	loaded, err = openTest(t, root).Load("services", true)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["DB_PASSWORD"] != "kept-edit" {
		t.Fatal("unchanged departure overwrote kept edit")
	}
}
