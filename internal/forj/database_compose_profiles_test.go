package forj

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goforj/goforj/internal/envfile"
	"github.com/goforj/goforj/project"
	"gopkg.in/yaml.v3"
)

// TestDatabaseComposeProfilesFollowProjectSelection keeps every generated dependency under owner-controlled activation.
func TestDatabaseComposeProfilesFollowProjectSelection(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres", "sqlite"} {
		t.Run(driver, func(t *testing.T) {
			components := project.Components{CLI: true, Docker: true, Mail: true, Auth: true, WebAPI: true, WebUI: true, Cache: true, Jobs: true, Events: true, Storage: true, Scheduler: true, Metrics: true, Observability: true, Grafana: true}
			components.DatabaseMySQL = driver == "mysql"
			components.DatabasePostgres = driver == "postgres"
			components.DatabaseSQLite = driver == "sqlite"
			plan := redisResourcePlanForTest(t, components)
			intent := project.LocalServiceIntent{}.WithMode(project.ServiceRedis, project.LocalServiceModeLocal)
			environment, compose := renderResourceTemplates(t, components, plan, intent)
			profiles, set := envfile.Lookup(strings.Split(environment, "\n"), "COMPOSE_PROFILES")
			if !set {
				t.Fatal("generated environment omitted COMPOSE_PROFILES")
			}
			for _, service := range []string{"mysql", "postgres", "redis", "mailpit", "victoriametrics", "grafana"} {
				want := service != "mysql" && service != "postgres" || service == driver
				if exactCSVToken(profiles, service) != want {
					t.Fatalf("profile %s in %q, want enabled=%t", service, profiles, want)
				}
			}
			var model struct {
				Services map[string]struct {
					Profiles []string `yaml:"profiles"`
				} `yaml:"services"`
			}
			if err := yaml.Unmarshal([]byte(compose), &model); err != nil {
				t.Fatal(err)
			}
			for name, service := range model.Services {
				if len(service.Profiles) == 0 {
					t.Errorf("service %s would start with empty COMPOSE_PROFILES", name)
				}
			}
			if driver != "sqlite" && !reflect.DeepEqual(model.Services[driver].Profiles, []string{driver}) {
				t.Fatalf("database profiles = %v, want [%s]", model.Services[driver].Profiles, driver)
			}
		})
	}
}

// TestDatabaseComposeProfileMigrationPreservesSubsequentOwnerChoices keeps the first rerender compatible without re-enabling disabled databases later.
func TestDatabaseComposeProfileMigrationPreservesSubsequentOwnerChoices(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		for _, initial := range []string{"", "owner-tool", driver + ",owner-tool"} {
			t.Run(driver+"/"+initial, func(t *testing.T) {
				t.Chdir(t.TempDir())
				components := project.Components{Docker: true, DatabaseMySQL: driver == "mysql", DatabasePostgres: driver == "postgres"}
				plan := defaultResourcePlanForTest(t, components)
				renderer := &ProjectRenderer{
					workspace: currentProjectRenderWorkspace(t),
					config:    &project.Config{Render: project.RenderConfig{Components: components}},
					resources: resourceRenderState{plan: plan},
				}
				writeDatabaseProfileFixture(t, ".env", "COMPOSE_PROFILES="+initial+"\n")
				writeDatabaseProfileFixture(t, "docker-compose.yml", "services:\n  "+driver+":\n    image: database\n")
				if err := renderer.prepareResourceEnvironment(); err != nil {
					t.Fatal(err)
				}
				profiles, _ := envfile.Lookup(strings.Split(string(renderer.resources.pendingEnvironment), "\n"), "COMPOSE_PROFILES")
				want := initial
				if !exactCSVToken(initial, driver) {
					want = strings.Trim(initial+","+driver, ",")
				}
				if profiles != want {
					t.Fatalf("migrated profiles = %q, want %q", profiles, want)
				}
				writeDatabaseProfileFixture(t, "docker-compose.yml", "services:\n  "+driver+":\n    profiles: ["+driver+"]\n    image: database\n")
				for _, owner := range []string{"", "owner-tool", driver + "-debug"} {
					writeDatabaseProfileFixture(t, ".env", "COMPOSE_PROFILES="+owner+"\n")
					for pass := 0; pass < 2; pass++ {
						if err := renderer.prepareResourceEnvironment(); err != nil {
							t.Fatal(err)
						}
						profiles, _ := envfile.Lookup(strings.Split(string(renderer.resources.pendingEnvironment), "\n"), "COMPOSE_PROFILES")
						if profiles != owner {
							t.Fatalf("rerender replaced owner profiles %q with %q", owner, profiles)
						}
					}
				}
			})
		}
	}
}

// TestDisabledDatabaseProfilesSkipContainerBootstrap proves an empty selection removes startup and readiness work without editing project config.
func TestDisabledDatabaseProfilesSkipContainerBootstrap(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("COMPOSE_PROFILES", "")
			t.Setenv("DB_DRIVER", driver)
			t.Setenv("DB_DATABASE", "app")
			components := project.Components{Docker: true, DatabaseMySQL: driver == "mysql", DatabasePostgres: driver == "postgres"}
			resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, project.LocalServiceIntent{}, false)
			planned := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
			config := &project.Config{Render: project.RenderConfig{Components: components}, Dev: project.DevConfig{Pre: planned.Pre}}
			writeDatabaseProfileFixture(t, "docker-compose.yml", "services:\n  "+driver+":\n    profiles: ["+driver+"]\n    image: database\n")
			writeDatabaseProfileFixture(t, ".env", "COMPOSE_PROFILES="+driver+"\n")
			for _, command := range []string{"docker-compose up -d", "docker compose up -d"} {
				config.Dev.Pre[0].Cmd = command
				if tasks := effectiveDevPreTasksForTest(config); len(tasks) != 0 {
					t.Fatalf("disabled database left bootstrap tasks: %#v", tasks)
				}
			}
			config.Dev.Pre[0].Cmd = "docker-compose up -d"
			for _, databaseName := range []string{"app", "", "external-name"} {
				t.Setenv("DB_DATABASE", databaseName)
				if err := ensureDevDatabaseExistsWithWriters(config, io.Discard, io.Discard); err != nil {
					t.Fatalf("disabled database attempted container provisioning for %q: %v", databaseName, err)
				}
			}
			t.Setenv("COMPOSE_PROFILES", driver)
			if tasks := effectiveDevPreTasksForTest(config); !reflect.DeepEqual(tasks, planned.Pre) {
				t.Fatalf("enabling profile did not restore startup and readiness: %#v", tasks)
			}
			t.Setenv("COMPOSE_PROFILES", "mailpit")
			if tasks := effectiveDevPreTasksForTest(config); len(tasks) != 1 || tasks[0].Name != "Run Docker Compose" {
				t.Fatalf("other profile retained database readiness: %#v", tasks)
			}
		})
	}
}

// TestComposeServiceDisabledHonorsOwnerConfiguration avoids suppressing bootstrap when Compose must resolve custom selection semantics.
func TestComposeServiceDisabledHonorsOwnerConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, profiles, compose, override, file, envFiles string
		want                                              bool
	}{
		{name: "empty", want: true},
		{name: "selected", profiles: "mysql"},
		{name: "wildcard", profiles: "*"},
		{name: "exact token", profiles: "mysql-debug", want: true},
		{name: "whitespace", profiles: " mailpit, mysql "},
		{name: "interpolation", profiles: "${OWNER_PROFILES}"},
		{name: "missing service", compose: "services:\n  mailpit:\n    profiles: [mailpit]\n"},
		{name: "legacy unprofiled", compose: "services:\n  mysql:\n    image: database\n"},
		{name: "invalid compose", compose: "services: ["},
		{name: "missing services", compose: "volumes: {}\n"},
		{name: "override profile", profiles: "owner", override: "services:\n  mysql:\n    profiles: [owner]\n"},
		{name: "override removes profile", override: "services:\n  mysql:\n    profiles: !reset []\n"},
		{name: "merged profiles retain base", profiles: "mysql", override: "services:\n  mysql:\n    profiles: [owner]\n"},
		{name: "empty list retains base", override: "services:\n  mysql:\n    profiles: []\n", want: true},
		{name: "explicit replacement", profiles: "mysql", override: "services:\n  mysql:\n    profiles: !override [owner]\n", want: true},
		{name: "invalid profile type", override: "services:\n  mysql:\n    profiles: owner\n"},
		{name: "invalid profile item", override: "services:\n  mysql:\n    profiles: [{}]\n"},
		{name: "unknown profile tag", override: "services:\n  mysql:\n    profiles: !unknown [owner]\n"},
		{name: "custom file", file: "other.yml"},
		{name: "custom env file", envFiles: "other.env"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("COMPOSE_FILE", test.file)
			t.Setenv("COMPOSE_ENV_FILES", test.envFiles)
			writeDatabaseProfileFixture(t, ".env", "COMPOSE_PROFILES="+test.profiles+"\n")
			compose := test.compose
			if compose == "" {
				compose = "services:\n  mysql:\n    profiles: [mysql]\n"
			}
			writeDatabaseProfileFixture(t, "docker-compose.yml", compose)
			if test.override != "" {
				writeDatabaseProfileFixture(t, "docker-compose.override.yml", test.override)
			}
			if got := composeServiceDisabled("mysql"); got != test.want {
				t.Fatalf("disabled = %t, want %t", got, test.want)
			}
		})
	}
}

// TestGeneratedDevTaskComposeServicePreservesCustomTasks limits profile suppression to known framework commands.
func TestGeneratedDevTaskComposeServicePreservesCustomTasks(t *testing.T) {
	for _, test := range []struct {
		task project.DevTask
		want string
	}{
		{task: project.DevTask{Name: "Waiting for Database to be ready", Cmd: generatedMySQLDevWaitCommand}, want: "mysql"},
		{task: project.DevTask{Name: "Waiting for Database to be ready", Cmd: strings.Replace(generatedPostgresDevWaitCommand, "docker-compose", "docker compose", 1)}, want: "postgres"},
		{task: project.DevTask{Name: "Seed Grafana Dashboards", Cmd: "docker-compose run --rm --no-deps grafana-seed"}, want: "grafana-seed"},
		{task: project.DevTask{Name: "Seed Grafana Dashboards", Cmd: "docker-compose up -d --force-recreate grafana-seed"}, want: "grafana-seed"},
		{task: project.DevTask{Name: "Waiting for Database to be ready", Cmd: "./scripts/wait-for-db"}},
		{task: project.DevTask{Name: "Owner database task", Cmd: generatedMySQLDevWaitCommand}},
		{task: project.DevTask{Name: "Run Docker Compose", Cmd: "docker-compose up -d"}},
	} {
		if got := generatedDevTaskComposeService(test.task); got != test.want {
			t.Errorf("%#v: service = %q, want %q", test.task, got, test.want)
		}
	}
}

// writeDatabaseProfileFixture keeps test inputs in isolated project directories.
func writeDatabaseProfileFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Clean(path), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
