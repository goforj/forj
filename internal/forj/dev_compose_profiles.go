package forj

import (
	"os"
	"os/exec"
	"strings"

	"github.com/goforj/goforj/internal/envfile"
	"github.com/goforj/goforj/project"
	"gopkg.in/yaml.v3"
)

// effectiveDevPreTasks starts owner-selected Compose profiles even when generation had no active local service.
func effectiveDevPreTasks(config *project.Config) []project.DevTask {
	return effectiveDevPreTasksWithLegacy(config, installedDevComposeUsesLegacy())
}

// effectiveDevPreTasksWithLegacy resolves Compose commands against one stable frontend choice.
func effectiveDevPreTasksWithLegacy(config *project.Config, useLegacy bool) []project.DevTask {
	tasks := append([]project.DevTask(nil), config.Dev.Pre...)
	if !config.Render.Components.Docker {
		return tasks
	}
	profilesEnabled, profilesReliable := composeProfilesEnabled()
	usesGeneratedDefaults := composeUsesGeneratedDefaultFiles()
	unprofiledService := false
	composeInspected := false
	if usesGeneratedDefaults {
		unprofiledService, composeInspected = composeHasUnprofiledService("docker-compose.yml", "docker-compose.override.yml")
	}
	composeNeeded := profilesEnabled || unprofiledService || !profilesReliable || !usesGeneratedDefaults
	if usesGeneratedDefaults && generatedComposeFileExists() && !composeInspected {
		// A selected but unreadable model must reach Compose so it can report the real configuration error.
		composeNeeded = true
	}
	filtered := make([]project.DevTask, 0, len(tasks)+1)
	hasComposeTask := false
	for _, task := range tasks {
		if service := generatedDevTaskComposeService(task); service != "" && composeServiceDisabled(service) {
			continue
		}
		if strings.TrimSpace(task.Name) != "Run Docker Compose" {
			filtered = append(filtered, task)
			continue
		}
		if !composeNeeded && composeInspected && isGeneratedDockerComposeUpTask(task) {
			continue
		}
		hasComposeTask = true
		filtered = append(filtered, task)
	}
	if composeNeeded && !hasComposeTask {
		filtered = append(filtered, project.DevTask{Name: "Run Docker Compose", Cmd: dockerComposeUpDevCommand(config.Render.Components)})
	}
	return resolveDevComposeTasks(filtered, useLegacy)
}

// effectiveDevDownTasks keeps teardown independent from the profile value that happens to be active later.
func effectiveDevDownTasks(config *project.Config) []project.DevTask {
	return effectiveDevDownTasksWithLegacy(config, installedDevComposeUsesLegacy())
}

// effectiveDevDownTasksWithLegacy resolves teardown commands against one stable frontend choice.
func effectiveDevDownTasksWithLegacy(config *project.Config, useLegacy bool) []project.DevTask {
	tasks := append([]project.DevTask(nil), config.Dev.Down...)
	if !config.Render.Components.Docker {
		return tasks
	}
	if hasDockerComposeDownTask(tasks) {
		normalizeDockerComposeDownTask(&tasks)
		return resolveDevComposeTasks(tasks, useLegacy)
	}
	return resolveDevComposeTasks(append(tasks, project.DevTask{Name: "Docker Compose Down", Cmd: dockerComposeDownDevCommand()}), useLegacy)
}

// resolveDevComposeTasks selects one Compose frontend without changing command arguments.
func resolveDevComposeTasks(tasks []project.DevTask, useLegacy bool) []project.DevTask {
	resolved := append([]project.DevTask(nil), tasks...)
	for index := range resolved {
		resolved[index].Cmd = resolveDevComposeCommand(resolved[index].Cmd, useLegacy)
	}
	return resolved
}

// installedDevComposeUsesLegacy prefers the standalone frontend when it is installed.
func installedDevComposeUsesLegacy() bool {
	_, err := exec.LookPath("docker-compose")
	return err == nil
}

// resolveDevComposeCommand upgrades legacy syntax only when the standalone frontend is unavailable.
func resolveDevComposeCommand(command string, useLegacy bool) string {
	trimmed := strings.TrimSpace(command)
	if strings.HasPrefix(trimmed, "docker-compose ") && !useLegacy {
		return "docker compose " + strings.TrimPrefix(trimmed, "docker-compose ")
	}
	return command
}

// composeProfilesEnabled follows Compose's process-over-dotenv precedence and reports whether indirect dotenv syntax was resolved reliably.
func composeProfilesEnabled() (bool, bool) {
	profiles, _, reliable := composeEnvironmentValue("COMPOSE_PROFILES")
	for _, profile := range strings.Split(profiles, ",") {
		if strings.TrimSpace(profile) != "" {
			return true, reliable
		}
	}
	return false, reliable
}

// composeEnvironmentValue follows Compose's direct process-over-dotenv precedence and identifies values that need Compose-native interpolation.
func composeEnvironmentValue(key string) (string, bool, bool) {
	if value, set := os.LookupEnv(key); set {
		return value, true, true
	}
	source, err := os.ReadFile(".env")
	if os.IsNotExist(err) {
		return "", false, true
	}
	if err != nil {
		return "", false, false
	}
	lines := strings.Split(string(source), "\n")
	value, set := envfile.Lookup(lines, key)
	return value, set, !composeAssignmentUsesInterpolation(lines, key)
}

// composeUsesGeneratedDefaultFiles limits suppression to a Compose selection whose merge semantics GoForj can inspect safely.
func composeUsesGeneratedDefaultFiles() bool {
	if selected, set, reliable := composeEnvironmentValue("COMPOSE_FILE"); !reliable || set && strings.TrimSpace(selected) != "" {
		return false
	}
	if selected, set, reliable := composeEnvironmentValue("COMPOSE_ENV_FILES"); !reliable || set && strings.TrimSpace(selected) != "" {
		return false
	}
	if disabled, set, reliable := composeEnvironmentValue("COMPOSE_DISABLE_ENV_FILE"); !reliable || set && composeEnvironmentFlagEnabled(disabled) {
		return false
	}
	for _, alternate := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml"} {
		_, err := os.Stat(alternate)
		if err == nil || !os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// composeEnvironmentFlagEnabled recognizes Compose's documented true-like control values.
func composeEnvironmentFlagEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// composeAssignmentUsesInterpolation keeps unresolved shell-backed dotenv values on the fail-open path.
func composeAssignmentUsesInterpolation(lines []string, key string) bool {
	assignment := ""
	for _, line := range lines {
		candidate, _, ok := envfile.ParseAssignment(line)
		if ok && candidate == key {
			assignment = line
		}
	}
	return strings.Contains(assignment, "$")
}

// generatedComposeFileExists distinguishes an absent project model from one Compose should diagnose itself.
func generatedComposeFileExists() bool {
	for _, path := range []string{"docker-compose.yml", "docker-compose.override.yml"} {
		if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

// composeHasUnprofiledService distinguishes required Compose work from dormant catalog-only definitions.
func composeHasUnprofiledService(paths ...string) (bool, bool) {
	services, inspected := composeServiceProfiles(paths...)
	for _, profiles := range services {
		if profiles == nil || len(*profiles) == 0 {
			return true, inspected
		}
	}
	return false, inspected
}

// composeServiceDisabled suppresses container-specific bootstrap only when the owner's effective profiles clearly exclude it.
func composeServiceDisabled(name string) bool {
	if !composeUsesGeneratedDefaultFiles() {
		return false
	}
	selected, _, reliable := composeEnvironmentValue("COMPOSE_PROFILES")
	if !reliable || exactCSVToken(selected, "*") {
		return false
	}
	services, inspected := composeServiceProfiles("docker-compose.yml", "docker-compose.override.yml")
	profiles := services[name]
	if !inspected || profiles == nil || len(*profiles) == 0 {
		return false
	}
	for _, profile := range *profiles {
		if exactCSVToken(selected, profile) {
			return false
		}
	}
	return true
}

// generatedDevTaskComposeService preserves custom tasks while identifying the built-in container bootstrap commands.
func generatedDevTaskComposeService(task project.DevTask) string {
	if !isConventionalDevBootstrapTask(task) {
		return ""
	}
	switch normalizeDevComposeExecutable(task.Cmd) {
	case generatedMySQLDevWaitCommand:
		return "mysql"
	case generatedPostgresDevWaitCommand:
		return "postgres"
	case "docker-compose up -d --force-recreate grafana-seed", "docker-compose run --rm --no-deps grafana-seed":
		return "grafana-seed"
	default:
		return ""
	}
}

// composeServiceProfiles merges profile lists and honors explicit reset/override tags in the default Compose files.
func composeServiceProfiles(paths ...string) (map[string]*[]string, bool) {
	inspected := false
	services := map[string]*[]string{}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, false
		}
		var model struct {
			Services map[string]struct {
				Profiles yaml.Node `yaml:"profiles"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(source, &model); err != nil || model.Services == nil {
			return nil, false
		}
		inspected = true
		for name, service := range model.Services {
			previous, exists := services[name]
			if service.Profiles.Kind == 0 {
				if !exists {
					services[name] = nil
				}
				continue
			}
			profiles := []string{}
			if service.Profiles.Tag != "!reset" {
				if service.Profiles.Kind != yaml.SequenceNode ||
					(service.Profiles.Tag != "!!seq" && service.Profiles.Tag != "!override") {
					return nil, false
				}
				if err := service.Profiles.Decode(&profiles); err != nil {
					return nil, false
				}
				if previous != nil && service.Profiles.Tag != "!override" {
					profiles = append(append([]string(nil), (*previous)...), profiles...)
				}
			}
			services[name] = &profiles
		}
	}
	return services, inspected
}

// isGeneratedDockerComposeUpTask limits runtime suppression to GoForj's conventional command.
func isGeneratedDockerComposeUpTask(task project.DevTask) bool {
	return strings.TrimSpace(task.Name) == "Run Docker Compose" && isConventionalDevComposeUpCommand(task.Cmd)
}

// hasDockerComposeDownTask preserves an owner-customized teardown task with the conventional generated identity.
func hasDockerComposeDownTask(tasks []project.DevTask) bool {
	for _, task := range tasks {
		if strings.TrimSpace(task.Name) == "Docker Compose Down" {
			return true
		}
	}
	return false
}
