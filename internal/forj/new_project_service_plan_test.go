package forj

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goforj/goforj/project"
)

// TestPlanNewProjectServiceTasksSkipsAvailableRedis verifies a support-only profiled definition does not create an empty Compose startup.
func TestPlanNewProjectServiceTasksSkipsAvailableRedis(t *testing.T) {
	components := project.Components{DatabaseSQLite: true, Docker: true, Cache: true}
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, project.LocalServiceIntent{}, false)
	requirement, exists := servicePlan.Requirement(project.ServiceRedis)
	if !exists || requirement.State != project.ServiceStateAvailableLocal {
		t.Fatalf("Redis requirement = %#v, exists %v; want available local", requirement, exists)
	}

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	if len(tasks.Pre) != 0 || len(tasks.Down) != 0 {
		t.Fatalf("service tasks = %#v, want no Compose lifecycle", tasks)
	}
}

// TestPlanNewProjectServiceTasksStartsActiveRedis verifies a locally managed active Redis resource starts one Compose lifecycle.
func TestPlanNewProjectServiceTasksStartsActiveRedis(t *testing.T) {
	components := project.Components{DatabaseSQLite: true, Docker: true, Cache: true}
	intent := project.LocalServiceIntent{}.WithMode(project.ServiceRedis, project.LocalServiceModeLocal)
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, intent, true)

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	wantPre := []project.DevTask{{Name: "Run Docker Compose", Cmd: "docker-compose up -d"}}
	wantDown := []project.DevTask{{Name: "Docker Compose Down", Cmd: dockerComposeDownDevCommand()}}
	if !reflect.DeepEqual(tasks.Pre, wantPre) {
		t.Fatalf("pre tasks = %#v, want %#v", tasks.Pre, wantPre)
	}
	if !reflect.DeepEqual(tasks.Down, wantDown) {
		t.Fatalf("down tasks = %#v, want %#v", tasks.Down, wantDown)
	}
}

// TestPlanNewProjectServiceTasksHonorsRetainedUnusedRedis verifies explicit owner intent remains an actual Compose lifecycle.
func TestPlanNewProjectServiceTasksHonorsRetainedUnusedRedis(t *testing.T) {
	components := project.Components{DatabaseSQLite: true, Docker: true, Cache: true}
	intent := project.LocalServiceIntent{}.WithMode(project.ServiceRedis, project.LocalServiceModeLocal)
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, intent, false)
	requirement, exists := servicePlan.Requirement(project.ServiceRedis)
	if !exists || requirement.State != project.ServiceStateLocalRequestedUnused {
		t.Fatalf("Redis requirement = %#v, exists %v; want retained unused local service", requirement, exists)
	}

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	if len(tasks.Pre) != 1 || tasks.Pre[0].Name != "Run Docker Compose" || len(tasks.Down) != 1 {
		t.Fatalf("retained unused Redis lifecycle = %#v", tasks)
	}
}

// TestPlanNewProjectServiceTasksSkipsExternalRedis verifies Docker capability alone does not start an externally placed active resource.
func TestPlanNewProjectServiceTasksSkipsExternalRedis(t *testing.T) {
	components := project.Components{DatabaseSQLite: true, Docker: true, Cache: true}
	intent := project.LocalServiceIntent{}.WithMode(project.ServiceRedis, project.LocalServiceModeExternal)
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, intent, true)

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	if len(tasks.Pre) != 0 || len(tasks.Down) != 0 {
		t.Fatalf("service tasks = %#v, want no Compose lifecycle", tasks)
	}
}

// TestPlanNewProjectServiceTasksStartsDockerBackedTools verifies each current Compose development-tool gate retains startup independently of App services.
func TestPlanNewProjectServiceTasksStartsDockerBackedTools(t *testing.T) {
	tests := []struct {
		name       string
		components project.Components
	}{
		{name: "Mailpit", components: project.Components{DatabaseSQLite: true, Docker: true, Mail: true}},
		{name: "VictoriaMetrics", components: project.Components{DatabaseSQLite: true, Docker: true, Observability: true}},
		{name: "Grafana", components: project.Components{DatabaseSQLite: true, Docker: true, Grafana: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, test.components, project.LocalServiceIntent{}, false)
			tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, test.components)
			if len(tasks.Pre) != 1 || tasks.Pre[0].Name != "Run Docker Compose" {
				t.Fatalf("pre tasks = %#v, want one Compose startup", tasks.Pre)
			}
			if len(tasks.Down) != 1 || tasks.Down[0].Name != "Docker Compose Down" {
				t.Fatalf("down tasks = %#v, want one Compose teardown", tasks.Down)
			}
		})
	}
}

// TestPlanNewProjectServiceTasksAddsMySQLWait verifies local MySQL readiness follows Compose startup.
func TestPlanNewProjectServiceTasksAddsMySQLWait(t *testing.T) {
	components := project.Components{DatabaseMySQL: true, Docker: true}
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, project.LocalServiceIntent{}, false)

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	if len(tasks.Pre) != 2 {
		t.Fatalf("pre task count = %d, want Compose and MySQL wait", len(tasks.Pre))
	}
	if tasks.Pre[1].Name != "Waiting for Database to be ready" || !containsAllNewProjectServiceCommandFragments(tasks.Pre[1].Cmd, "exec -T mysql", "mariadb-admin ping", "mariadb -h") {
		t.Fatalf("MySQL wait task = %#v", tasks.Pre[1])
	}
}

// TestPlanNewProjectServiceTasksAddsPostgresWait verifies local Postgres readiness follows Compose startup.
func TestPlanNewProjectServiceTasksAddsPostgresWait(t *testing.T) {
	components := project.Components{DatabasePostgres: true, Docker: true}
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, project.LocalServiceIntent{}, false)

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	if len(tasks.Pre) != 2 {
		t.Fatalf("pre task count = %d, want Compose and Postgres wait", len(tasks.Pre))
	}
	if tasks.Pre[1].Name != "Waiting for Database to be ready" || !containsAllNewProjectServiceCommandFragments(
		tasks.Pre[1].Cmd,
		"exec -T postgres",
		`export PGPASSWORD="$POSTGRES_PASSWORD"`,
		"pg_isready",
		"psql",
	) {
		t.Fatalf("Postgres wait task = %#v", tasks.Pre[1])
	}
}

// TestPlanNewProjectServiceTasksSkipsExternalDatabaseWait verifies a remote SQL service never receives a local Compose readiness command.
func TestPlanNewProjectServiceTasksSkipsExternalDatabaseWait(t *testing.T) {
	components := project.Components{DatabaseMySQL: true}
	resourcePlan, servicePlan := resolveNewProjectServiceTestPlans(t, components, project.LocalServiceIntent{}, false)

	tasks := planNewProjectServiceTasks(resourcePlan, servicePlan, components)
	if len(tasks.Pre) != 0 || len(tasks.Down) != 0 {
		t.Fatalf("service tasks = %#v, want no local lifecycle", tasks)
	}
}

// TestNewProjectDatabaseWaitTaskRequiresMatchingResource verifies service state cannot add a wait for a database the App does not select.
func TestNewProjectDatabaseWaitTaskRequiresMatchingResource(t *testing.T) {
	components := project.Components{DatabaseSQLite: true, Docker: true}
	resourcePlan := defaultResourcePlanForTest(t, components)
	servicePlan := project.ServicePlan{Requirements: []project.ServiceRequirement{{Key: project.ServiceMySQL, State: project.ServiceStateActiveLocal}}}

	if task, ok := newProjectDatabaseWaitTask(resourcePlan, servicePlan); ok {
		t.Fatalf("database wait task = %#v, want none for SQLite", task)
	}
}

// resolveNewProjectServiceTestPlans creates validated concrete plans so task tests exercise only lifecycle policy.
func resolveNewProjectServiceTestPlans(t *testing.T, components project.Components, intent project.LocalServiceIntent, redisActive bool) (project.ResourcePlan, project.ServicePlan) {
	t.Helper()
	resourcePlan := defaultResourcePlanForTest(t, components)
	if redisActive {
		resourcePlan = redisResourcePlanForTest(t, components)
	}
	servicePlan, err := project.ResolveServicePlan(resourcePlan, components, intent)
	if err != nil {
		t.Fatalf("ResolveServicePlan returned error: %v", err)
	}
	return resourcePlan, servicePlan
}

// containsAllNewProjectServiceCommandFragments reports whether text includes every fragment needed to identify a generated readiness command.
func containsAllNewProjectServiceCommandFragments(text string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			return false
		}
	}
	return true
}
