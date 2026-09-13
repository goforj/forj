package stacks

import (
	"reflect"
	"testing"

	"github.com/goforj/goforj/internal/generate"
	"github.com/goforj/goforj/project"
)

// TestPortableNamedDatabaseDiscoveryStaysStable protects repeated conversion and real names containing sqlite.
func TestPortableNamedDatabaseDiscoveryStaysStable(t *testing.T) {
	root := fixture(t)
	put(t, root, ".env", "DB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\nDB_ANALYTICS_DATABASE=analytics\nDB_REPORTING_SQLITE_DRIVER=mysql\nADMIN_DB_REPORTS_DATABASE=reports\n")
	want := []string{"analytics", "reporting_sqlite", "reports"}
	for i := 0; i < 4; i++ {
		s := openTest(t, root)
		if got := generate.ResourceNames(root, s.Current)[project.ResourceDatabase]; !reflect.DeepEqual(got, want) {
			t.Fatalf("pass %d: names=%v, want %v", i, got, want)
		}
		portable, err := s.Portable()
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Activate("", portable, false); err != nil {
			t.Fatal(err)
		}
	}
}

// TestStackBaselineDriversMatchGeneration keeps optional manifests compatible with all implicitly compiled providers.
func TestStackBaselineDriversMatchGeneration(t *testing.T) {
	root := fixture(t)
	s := openTest(t, root)
	for _, def := range project.ResourceCatalog() {
		for _, local := range generate.BaselineDrivers(def.Key) {
			t.Run(string(def.Key)+"/"+local, func(t *testing.T) {
				external := ""
				for _, driver := range def.Drivers {
					if driver.Service != "" {
						external = driver.Name
						break
					}
				}
				values := map[string]string{def.EnvironmentKey("DRIVER"): local, def.EnvironmentKey("SUPPORTED_DRIVERS"): external + ", ", def.EnvironmentPrefix + "_REPORTS_DRIVER": local, "ADMIN_" + def.EnvironmentKey("DRIVER"): local}
				name := string(def.Key) + "-" + local
				s = openTest(t, root)
				if err := s.Save(name, values); err != nil {
					t.Fatal(err)
				}
				s = openTest(t, root)
				loaded, err := s.Load(name, true)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Activate(name, loaded, false); err != nil {
					t.Fatal(err)
				}
				baked, err := BuildDefaults(root, name)
				if err != nil {
					t.Fatal(err)
				}
				if !equalValues(baked, values) {
					t.Fatal("save/build changed the existing manifest")
				}
			})
		}
	}
}

// TestStackStillRejectsDriversOutsideTheCompiledContract distinguishes built-in baseline providers from optional local providers.
func TestStackStillRejectsDriversOutsideTheCompiledContract(t *testing.T) {
	s := openTest(t, fixture(t))
	for _, values := range []map[string]string{
		{"CACHE_DRIVER": "sqlite", "CACHE_SUPPORTED_DRIVERS": "redis"},
		{"STORAGE_DRIVER": "memory", "STORAGE_SUPPORTED_DRIVERS": "s3"},
		{"DB_DRIVER": "postgres", "DB_SUPPORTED_DRIVERS": "mysql"},
		{"DB_DRIVER": "sqlite", "DB_SUPPORTED_DRIVERS": ","},
		{"CACHE_DRIVER": "memory", "CACHE_SUPPORTED_DRIVERS": "unknown"},
	} {
		if err := s.Validate(values); err == nil {
			t.Fatalf("accepted invalid contract: %v", values)
		}
	}
	if err := s.Validate(map[string]string{"CACHE_DRIVER": "memory", "CACHE_SUPPORTED_DRIVERS": " "}); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(map[string]string{"CACHE_DRIVER": "redis", "CACHE_SUPPORTED_DRIVERS": ","}); err != nil {
		t.Fatal(err)
	}
}

// TestStackOverlappingAppsUseLongestPrefix keeps validation, ownership, and baked defaults deterministic.
func TestStackOverlappingAppsUseLongestPrefix(t *testing.T) {
	root := fixture(t)
	put(t, root, ".goforj.yml", "project_name: demo\nmodule_name: example.org/demo\nrender:\n  components: [cli]\napps:\n  admin:\n    components: [cli]\n  admin-db:\n    components: [cli, cache]\n")
	put(t, root, ".env", "ADMIN_DB_CACHE_DRIVER=redis\nCACHE_SUPPORTED_DRIVERS=memory,redis\nADMIN_DB_DRIVER=mysql\nDB_SUPPORTED_DRIVERS=mysql\nADMIN_APP_LABEL=owner-text\n")
	s := openTest(t, root)
	for i := 0; i < 100; i++ {
		if err := s.Validate(s.Current); err != nil {
			t.Fatal(err)
		}
		if managedKey(s.config, "ADMIN_APP_LABEL") {
			t.Fatal("claimed another App's unrelated setting")
		}
		for _, app := range []string{"admin", "admin-db"} {
			defaults, err := AppDefaults(root, s.Current, app)
			if err != nil {
				t.Fatal(err)
			}
			if app == "admin-db" && defaults["CACHE_DRIVER"] != "redis" {
				t.Fatal("lost nested App's driver")
			}
			if app == "admin" {
				if defaults["DB_DRIVER"] != "mysql" {
					t.Fatal("lost shorter App's database driver")
				}
				for key := range defaults {
					if key == "DB_CACHE_DRIVER" {
						t.Fatal("included another App's defaults")
					}
				}
			}
		}
	}
	values, err := s.Portable()
	if err != nil {
		t.Fatal(err)
	}
	if values["ADMIN_DB_CACHE_DRIVER"] != "memory" {
		t.Fatal("portable used the wrong resource type")
	}
	if err := s.Activate("", values, false); err != nil {
		t.Fatal(err)
	}
	after := openTest(t, root)
	if after.env.values["ADMIN_APP_LABEL"] != "owner-text" {
		t.Fatal("activation removed unrelated App setting")
	}
}
