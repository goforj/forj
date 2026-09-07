package forj

import "github.com/goforj/goforj/project"

const (
	generatedMySQLDevWaitCommand = "docker-compose exec -T mysql sh -c 'while ! mariadb-admin ping -h \"mysql\" --silent; do sleep .5; done; mariadb -h \"mysql\" -uroot -p\"$MARIADB_ROOT_PASSWORD\" -e \"CREATE DATABASE IF NOT EXISTS \\`$MARIADB_DATABASE\\`;\"'"
)

// generatedPostgresDevWaitCommand wraps the shared authenticated creation script for the initial Compose lifecycle.
var generatedPostgresDevWaitCommand = "docker-compose exec -T postgres sh -c " + shellSingleQuote(
	postgresCreateDatabasesScript([]string{`"$POSTGRES_DB"`}),
)

// newProjectServiceTaskPlan groups generated development lifecycle tasks without persisting transient resource topology.
type newProjectServiceTaskPlan struct {
	Pre  []project.DevTask
	Down []project.DevTask
}

// planNewProjectServiceTasks derives Compose lifecycle work from active local services and generated development tools.
func planNewProjectServiceTasks(resourcePlan project.ResourcePlan, servicePlan project.ServicePlan, components project.Components) newProjectServiceTaskPlan {
	if !newProjectNeedsDockerCompose(servicePlan, components) {
		return newProjectServiceTaskPlan{}
	}

	tasks := newProjectServiceTaskPlan{
		Pre: []project.DevTask{{
			Name: "Run Docker Compose",
			Cmd:  dockerComposeUpDevCommand(components),
		}},
		Down: []project.DevTask{{
			Name: "Docker Compose Down",
			Cmd:  dockerComposeDownDevCommand(),
		}},
	}
	if waitTask, ok := newProjectDatabaseWaitTask(resourcePlan, servicePlan); ok {
		tasks.Pre = append(tasks.Pre, waitTask)
	}
	return tasks
}

// newProjectNeedsDockerCompose keeps inactive profiled services from creating an empty startup while retaining selected development tools.
func newProjectNeedsDockerCompose(servicePlan project.ServicePlan, components project.Components) bool {
	if !components.Docker {
		return false
	}
	return servicePlan.HasActiveLocal() || newProjectHasDockerComposeTools(components)
}

// newProjectHasDockerComposeTools mirrors the Mailpit, VictoriaMetrics, and Grafana service gates in the generated Compose template.
func newProjectHasDockerComposeTools(components project.Components) bool {
	return components.Mail || components.Observability || components.Grafana
}

// newProjectDatabaseWaitTask returns a readiness task only when the selected SQL driver is managed by the local service plan.
func newProjectDatabaseWaitTask(resourcePlan project.ResourcePlan, servicePlan project.ServicePlan) (project.DevTask, bool) {
	selection, ok := resourcePlan.Selection(project.ResourceDatabase)
	if !ok {
		return project.DevTask{}, false
	}

	var serviceKey project.ServiceKey
	var command string
	switch selection.Active {
	case "mysql":
		serviceKey = project.ServiceMySQL
		command = generatedMySQLDevWaitCommand
	case "postgres":
		serviceKey = project.ServicePostgres
		command = generatedPostgresDevWaitCommand
	default:
		return project.DevTask{}, false
	}

	requirement, ok := servicePlan.Requirement(serviceKey)
	if !ok || requirement.State != project.ServiceStateActiveLocal {
		return project.DevTask{}, false
	}
	return project.DevTask{Name: "Waiting for Database to be ready", Cmd: command}, true
}
