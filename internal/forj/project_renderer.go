package forj

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/goforj/str/v2"

	"charm.land/lipgloss/v2"
	"github.com/goforj/console"
	"github.com/goforj/crypt"
	"github.com/goforj/goforj/internal/commanddiag"
	"github.com/goforj/goforj/internal/coredeps"
	"github.com/goforj/goforj/internal/devservices"
	"github.com/goforj/goforj/internal/envcontract"
	"github.com/goforj/goforj/internal/envfile"
	"github.com/goforj/goforj/internal/forj/atlas"
	"github.com/goforj/goforj/internal/forj/makeapp"
	"github.com/goforj/goforj/internal/generate"
	"github.com/goforj/goforj/internal/logger"
	"github.com/goforj/goforj/internal/projectlayout"
	"github.com/goforj/goforj/project"
	"github.com/goforj/goforj/templates"
	"gopkg.in/yaml.v3"
)

var (
	markStep     = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Render("▸")
	markCreate   = lipgloss.NewStyle().Foreground(lipgloss.Color("84")).Render("✔")
	markSkip     = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("·")
	markAction   = lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Render("›")
	headerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	summaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("84")).Bold(true)
	nextStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	timingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	bulletStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Render("·")
	commandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	boxBorder    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)

var templatesFS = templates.FS

type wireGenerateError struct {
	dir    string
	output string
	err    error
	stale  bool
}

// Error retains the App-local Wire directory and captured output needed to repair generation failures.
func (e *wireGenerateError) Error() string {
	return commanddiag.Format("wire "+e.dir, e.err, e.output)
}

// Unwrap preserves the underlying process failure for stale-toolchain classification.
func (e *wireGenerateError) Unwrap() error {
	return e.err
}

// ComponentRenderInput controls whether rendering uses explicit components or the stored project config.
type ComponentRenderInput struct {
	components         project.Components
	renderAll          bool
	initializeProject  bool
	root               string
	resourcePlan       project.ResourcePlan
	localServiceIntent project.LocalServiceIntent
	serviceConsumers   []project.EffectiveResourceConsumer
}

// resourceRenderState keeps transient resource decisions together so repeated renders cannot reuse partial state.
type resourceRenderState struct {
	plan                    project.ResourcePlan
	serviceIntent           project.LocalServiceIntent
	serviceConsumers        []project.EffectiveResourceConsumer
	explicitPlan            bool
	pendingEnvironment      []byte
	pendingEnvironmentWrite bool
}

// wireTool keeps Wire discovery and replacement scoped to one renderer invocation owner.
type wireTool struct {
	mu       sync.Mutex
	resolved bool
	path     string
	err      error
}

// resolve reuses one Wire lookup or installation without sharing mutable state across renderers.
func (tool *wireTool) resolve() (string, error) {
	tool.mu.Lock()
	defer tool.mu.Unlock()
	if !tool.resolved {
		tool.path, tool.err = exec.LookPath("wire")
		if tool.err != nil {
			tool.path, tool.err = installWire()
		}
		tool.resolved = true
	}
	return tool.path, tool.err
}

// reinstall replaces a stale Wire binary using the active Go toolchain for this renderer.
func (tool *wireTool) reinstall() (string, error) {
	tool.mu.Lock()
	defer tool.mu.Unlock()
	path, err := installWire()
	if err != nil {
		return "", err
	}
	tool.path = path
	tool.err = nil
	tool.resolved = true
	return path, nil
}

// ProjectRenderer renders project files from the current config and template set.
type ProjectRenderer struct {
	logger                  *logger.AppLogger
	config                  *project.Config
	workspace               projectRenderWorkspace
	stats                   *renderStats
	lines                   []string
	timings                 bool
	resources               resourceRenderState
	wire                    wireTool
	removeLegacyQueueDriver bool
	writeEnvironmentFile    func(string, []byte, fs.FileMode) error
	tidyModule              func(*ProjectRenderer) error
	generateWire            func(*ProjectRenderer) error
}

type renderStats struct {
	mu      sync.Mutex
	created []string
	skipped []string
}

// recordCreated centralizes record created behavior so callers follow the same contract.
func (s *renderStats) recordCreated(path string) {
	if path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, path)
}

// recordSkipped centralizes record skipped behavior so callers follow the same contract.
func (s *renderStats) recordSkipped(path string) {
	if path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skipped = append(s.skipped, path)
}

type renderCounts struct {
	created int
	skipped int
}

// templateRenderConfig carries both App-local and project-envelope capability projections into generated templates.
type templateRenderConfig struct {
	*project.Config
	Components                  project.Components
	ProjectComponents           project.Components
	StarterKit                  project.StarterKit
	ComponentLibrary            bool
	HelpFormat                  project.HelpFormat
	Resources                   resourceRenderValues
	HelpFormatterFunc           string
	HelpCommandFunc             string
	App                         project.App
	AppPackageName              string
	AppImportPath               string
	WireImportPath              string
	AppIsDefault                bool
	HasNamedApps                bool
	RuntimeApps                 []runtimeAppMetadata
	LegacyEventPipelineField    bool
	LegacyEventPipelineProvider bool
}

type runtimeAppMetadata struct {
	Name        string
	Index       int
	EnvPrefix   string
	HTTPPort    int
	RuntimeBase int
	Components  project.Components
}

type templateMapping struct {
	tmpl string
	dest string
}

// mapTemplate centralizes map template behavior so callers follow the same contract.
func mapTemplate(tmpl string) templateMapping {
	return templateMapping{
		tmpl: tmpl,
		dest: strings.TrimSuffix(tmpl, ".tmpl"),
	}
}

// mapTemplateTo centralizes map template to behavior so callers follow the same contract.
func mapTemplateTo(tmpl, dest string) templateMapping {
	return templateMapping{
		tmpl: tmpl,
		dest: dest,
	}
}

// counts centralizes counts behavior so callers follow the same contract.
func (s *renderStats) counts() renderCounts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return renderCounts{
		created: len(s.created),
		skipped: len(s.skipped),
	}
}

// renderCountsLine keeps the render counts line representation consistent.
func renderCountsLine(title string, created, skipped int, unit string) string {
	label := fmt.Sprintf("%-32s", title)
	line := fmt.Sprintf("%s %s %s %d", markStep, label, markCreate, created)
	if unit != "" {
		line += " " + unit
	}
	if skipped > 0 {
		line += fmt.Sprintf(" %s %d skipped", markSkip, skipped)
	}
	return line
}

// renderCountsLineWithTiming keeps the render counts line with timing representation consistent.
func renderCountsLineWithTiming(title string, created, skipped int, unit string, elapsed time.Duration) string {
	line := renderCountsLine(title, created, skipped, unit)
	return appendRenderTiming(line, elapsed)
}

// formatRenderElapsed keeps the format render elapsed representation consistent.
func formatRenderElapsed(elapsed time.Duration) string {
	return formatDevElapsed(elapsed)
}

// appendRenderTiming centralizes append render timing behavior so callers follow the same contract.
func appendRenderTiming(line string, elapsed time.Duration) string {
	if elapsed <= 0 {
		return line
	}
	return line + " " + markSkip + " " + timingStyle.Render(formatRenderElapsed(elapsed))
}

// maybeFormatGoSource centralizes maybe format go source behavior so callers follow the same contract.
func maybeFormatGoSource(destPath string, content []byte) ([]byte, error) {
	if !strings.HasSuffix(destPath, ".go") {
		return content, nil
	}
	formatted, err := format.Source(content)
	if err != nil {
		return nil, fmt.Errorf("gofmt %s: %w", destPath, err)
	}
	return formatted, nil
}

// generateLighthouseSecret centralizes generate lighthouse secret behavior so callers follow the same contract.
func generateLighthouseSecret() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	return generateRandomToken(charset, 32)
}

// generateJWTSecretKey centralizes generate jwtsecret key behavior so callers follow the same contract.
func generateJWTSecretKey() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	return generateRandomToken(charset, 48)
}

// generateAppDiagToken centralizes generate app diag token behavior so callers follow the same contract.
func generateAppDiagToken() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	return generateRandomToken(charset, 32)
}

// generateRandomToken centralizes generate random token behavior so callers follow the same contract.
func generateRandomToken(charset string, length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	for i := 0; i < length; i++ {
		buf[i] = charset[int(buf[i])%len(charset)]
	}
	return string(buf), nil
}

// NewProjectRenderer creates a renderer with atomic environment publication enabled.
func NewProjectRenderer(logger *logger.AppLogger) *ProjectRenderer {
	workspace, err := resolveProjectRenderWorkspace(".")
	if err != nil {
		panic(err)
	}
	renderer := &ProjectRenderer{
		logger:               logger,
		workspace:            workspace,
		stats:                &renderStats{},
		writeEnvironmentFile: writeFileAtomically,
	}
	renderer.tidyModule = (*ProjectRenderer).goModTidy
	renderer.generateWire = (*ProjectRenderer).runWireGenerate
	return renderer
}

// beginRenderInvocation resets invocation-local state and resolves the project workspace before any renderer boundary runs.
func (p *ProjectRenderer) beginRenderInvocation(root string) error {
	workspace, err := resolveProjectRenderWorkspace(root)
	if err != nil {
		return err
	}
	for _, path := range []string{".env", ".env.host", ".env.local", ".env.example", ".env.testing", ".gitignore"} {
		if err := workspace.rejectEnvironmentSpecialFile(path); err != nil {
			return err
		}
	}
	if err := envcontract.ValidateMigration(workspace.root); err != nil {
		return err
	}
	p.workspace = workspace
	p.stats = &renderStats{}
	p.lines = nil
	return nil
}

// Render reconciles project-owned inputs before publishing a complete scaffold.
func (p *ProjectRenderer) Render(input ComponentRenderInput) error {
	if err := p.beginRenderInvocation(input.root); err != nil {
		return err
	}
	p.resources = resourceRenderState{}
	p.removeLegacyQueueDriver = false

	if input.renderAll {
		cfg, err := p.workspace.loadProjectConfig()
		if err != nil {
			return err
		}
		p.config = cfg
		p.removeLegacyQueueDriver = cfg.Render.HasLegacyQueueDriver()
	} else {
		p.config = &project.Config{
			Render: project.RenderConfig{Components: input.components},
		}
	}
	configuredComponents := p.config.Render.Components
	if p.config.Render.Components.DemoApp {
		p.config.Render.Components.Auth = true
		p.config.Render.StarterKit = project.StarterKitNone
	}
	p.config.Render.Components.ResolveDependencies()
	if err := p.config.Render.Components.ValidateRenderContract(); err != nil {
		return err
	}
	projectComponents := project.ProjectComponents(p.config)
	if err := projectComponents.ValidateRenderContract(); err != nil {
		return fmt.Errorf("project component envelope: %w", err)
	}
	if err := p.validateEventsRenderTransition(projectComponents); err != nil {
		return err
	}
	if err := p.validateStorageRenderTransition(projectComponents); err != nil {
		return err
	}
	if err := p.validateJobsRenderTransition(projectComponents); err != nil {
		return err
	}
	if err := p.validateCacheRenderTransition(projectComponents); err != nil {
		return err
	}
	if err := p.prepareResourceRenderState(input, projectComponents); err != nil {
		return err
	}
	projectComponents = p.projectRenderComponents()
	p.config.Render.StarterKit = project.NormalizeStarterKit(p.config.Render.StarterKit)
	if !p.config.Render.Components.WebUI {
		p.config.Render.StarterKit = project.StarterKitNone
	}
	if err := project.ValidateStarterKitContract(p.config.Render.StarterKit, p.config.Render.Components); err != nil {
		return err
	}
	if err := p.migrateAppOwnedWireFilenames(); err != nil {
		return err
	}

	steps := []struct {
		title               string
		enabled             bool
		templates           []string
		renderOnceTemplates []string
		raw                 []string
		action              func() error
	}{
		{
			title:   "Go Module Initialization",
			enabled: input.renderAll,
			action:  p.createGoMod,
		},
		{
			title:   "Default App Rendering",
			enabled: input.renderAll,
			action: func() error {
				return p.renderApp(project.DefaultApp())
			},
		},
		{
			title:   "Environment Files Initialization",
			enabled: input.renderAll,
			action: func() error {
				if err := p.publishPendingResourceEnvironment(); err != nil {
					return err
				}
				envTemplates := []string{
					".env.tmpl",
					".env.host.tmpl",
				}
				localEnvTemplate := ".env.local.tmpl"
				missingEnvTemplates := make([]string, 0, len(envTemplates))
				for _, tmpl := range envTemplates {
					name := str.Of(tmpl).TrimSuffix(".tmpl").String()
					if exists, err := p.workspace.exists(name); err != nil {
						return err
					} else if exists {
						if err := p.ensureEnvironmentDefaults(name); err != nil {
							return err
						}
						continue
					}
					missingEnvTemplates = append(missingEnvTemplates, tmpl)
				}
				if len(missingEnvTemplates) > 0 {
					key, err := crypt.GenerateAppKey()
					if err != nil {
						return fmt.Errorf("failed to generate app key: %w", err)
					}
					secret, err := generateLighthouseSecret()
					if err != nil {
						return fmt.Errorf("failed to generate lighthouse secret: %w", err)
					}
					appDiagToken, err := generateAppDiagToken()
					if err != nil {
						return fmt.Errorf("failed to generate app diagnostics token: %w", err)
					}
					jwtSecret, err := generateJWTSecretKey()
					if err != nil {
						return fmt.Errorf("failed to generate JWT secret: %w", err)
					}
					p.config.AppKey = key
					p.config.AppDiagToken = appDiagToken
					p.config.LighthouseSecret = secret
					p.config.JWTSecretKey = jwtSecret
					if err := p.writeEnvironmentTemplates(missingEnvTemplates); err != nil {
						return err
					}
				}
				for _, app := range projectlayout.RuntimeApps(p.workspace.discoveryRoot(), p.config) {
					if appRenderComponents(p.config, app).Cache {
						continue
					}
					if _, err := p.workspace.removeGeneratedAppCacheDriverDefault(".env.host", app.Name); err != nil {
						return err
					}
				}
				if err := p.writeEnvironmentTemplates([]string{localEnvTemplate}); err != nil {
					return err
				}
				if err := p.workspace.ensureGitignoreEnvironmentRules(); err != nil {
					return fmt.Errorf("update environment ignore rules: %w", err)
				}
				return p.syncProjectConfigForRender(configuredComponents)
			},
		},
		{
			title:   "Bin Directory Initialization",
			enabled: input.renderAll,
			action: func() error {
				if err := p.workspace.ensureDir("bin"); err != nil {
					return err
				}
				p.stats.recordCreated("bin/")
				return nil
			},
		},
		{
			title:               "Project Documentation Initialization",
			enabled:             input.initializeProject,
			renderOnceTemplates: []string{"README.md.tmpl"},
		},
		{
			title:   "Core Components Rendering",
			enabled: input.renderAll,
			templates: []string{
				"internal/makecmd/editor.go.tmpl",
				"internal/makecmd/env_section_editor.go.tmpl",
				"internal/makecmd/generator_helpers.go.tmpl",
				"internal/makecmd/generator_helpers_test.go.tmpl",
				"internal/makecmd/help.go.tmpl",
				"internal/makecmd/resource_maker.go.tmpl",
				"internal/makecmd/command_names.go.tmpl",
				"internal/makecmd/make_command_cmd.go.tmpl",
				"internal/makecmd/make_command_cmd_test.go.tmpl",
				"internal/makecmd/README.md.tmpl",
				"internal/makecmd/make_migration_cmd.go.tmpl",
				"internal/makecmd/make_migration_cmd_test.go.tmpl",
				"internal/runtime/lifecycle.go.tmpl",
				"internal/runtime/lifecycle_test.go.tmpl",
				"internal/runtime/runtime.go.tmpl",
				"internal/runtime/source.go.tmpl",
				"internal/runtime/apps.go.tmpl",
				"internal/runtime/apps_test.go.tmpl",
				"internal/runtime/runtime_host.go.tmpl",
				"internal/runtime/runtime_host_test.go.tmpl",
				"internal/runtime/timeouts.go.tmpl",
				"internal/runtime/README.md.tmpl",
				"internal/observability/mail_observer.go.tmpl",
				"internal/runtime/about.go.tmpl",
				"internal/runtime/discovery.go.tmpl",
				"internal/cmd/about_cmd.go.tmpl",
				"internal/cmd/about_cmd_test.go.tmpl",
				"internal/cmd/about_grid.go.tmpl",
				"internal/cmd/hello_world_cmd.go.tmpl",
				"internal/cmd/json_helpers.go.tmpl",
				"internal/cmd/resources_cmd.go.tmpl",
				"internal/cmd/command_exit_code.go.tmpl",
				"internal/cmd/command_exit_code_test.go.tmpl",
				"internal/cmd/launch.go.tmpl",
				"internal/cmd/launch_test.go.tmpl",
				"internal/monitoring/seed_cmd.go.tmpl",
				"internal/monitoring/reset_cmd.go.tmpl",
				"internal/monitoring/retention_cmd.go.tmpl",
				"internal/monitoring/json_helpers.go.tmpl",
				"internal/monitoring/poll_cmd.go.tmpl",
				"internal/monitoring/push_trigger_cmd.go.tmpl",
				"internal/monitoring/test_poll_loop_cmd.go.tmpl",
				"internal/konghelp/help_doc.go.tmpl",
				"internal/konghelp/help.go.tmpl",
				"internal/konghelp/help_framework.go.tmpl",
				"internal/konghelp/help_external.go.tmpl",
				"internal/konghelp/help_guided.go.tmpl",
				"internal/konghelp/help_render.go.tmpl",
				"internal/konghelp/help_parse_error.go.tmpl",
				"internal/konghelp/help_preview.go.tmpl",
				"internal/cmd/default_launch.go.tmpl",
				"internal/cmd/default_launch_test.go.tmpl",
				"internal/cmd/env_defaults.go.tmpl",
				"internal/cmd/env_defaults_test.go.tmpl",
				"internal/cmd/preboot.go.tmpl",
				"internal/cmd/preboot_test.go.tmpl",
				"internal/cmd/app_identity.go.tmpl",
				"internal/cmd/run_cmd.go.tmpl",
				"internal/logger/app.go.tmpl",
				"internal/logger/bench_test.go.tmpl",
				"internal/logger/app_test.go.tmpl",
				"internal/logger/dedupe.go.tmpl",
				"internal/logger/dedupe_test.go.tmpl",
				"internal/logger/event.go.tmpl",
				"internal/logger/redaction.go.tmpl",
				"internal/logger/sink.go.tmpl",
				"internal/logger/wire.go.tmpl",
				"internal/inspects/README.md.tmpl",
				"internal/inspects/manager.go.tmpl",
				"internal/inspects/store.go.tmpl",
				"internal/inspects/store_test.go.tmpl",
				"internal/inspects/manager_test.go.tmpl",
				"internal/inspects/manager_bench_test.go.tmpl",
				"internal/lighthouse/project_config_patch.go.tmpl",
			},
			renderOnceTemplates: []string{
				".gitignore.tmpl",
				".db-relationships.yaml.tmpl",
			},
			raw: []string{
				"internal/makecmd/make_command.tmpl",
			},
		},
		{
			title:   "Cache Components Rendering",
			enabled: projectComponents.Cache,
			templates: []string{
				"internal/caches/README.md.tmpl",
				"internal/observability/cache_observer.go.tmpl",
				"internal/observability/cache_observer_test.go.tmpl",
				"internal/cmd/cache_shell_cmd.go.tmpl",
				"internal/cmd/cache_shell_cmd_test.go.tmpl",
			},
		},
		{
			title:   "Cache Metrics Rendering",
			enabled: projectComponents.Cache && projectComponents.Metrics,
			templates: []string{
				"internal/metrics/cache_metrics_gen.go.tmpl",
				"internal/metrics/cache_metrics_gen_test.go.tmpl",
			},
		},
		{
			title:   "Events Components Rendering",
			enabled: projectComponents.Events,
			templates: []string{
				"internal/events/event.go.tmpl",
				"internal/events/topics.go.tmpl",
				"internal/events/bus_transport.go.tmpl",
				"internal/events/bus_integration_test.go.tmpl",
				"internal/events/README.md.tmpl",
				"internal/makecmd/make_event_cmd.go.tmpl",
				"internal/makecmd/make_event_cmd_test.go.tmpl",
				"internal/makecmd/make_subscriber_cmd.go.tmpl",
				"internal/makecmd/make_subscriber_cmd_test.go.tmpl",
				"internal/observability/event_observer.go.tmpl",
				"internal/cmd/test_event_pipeline_cmd.go.tmpl",
			},
			raw: []string{
				"internal/makecmd/event.tmpl",
				"internal/makecmd/subscriber.tmpl",
			},
		},
		{
			title:   "Storage Components Rendering",
			enabled: projectComponents.Storage,
			templates: []string{
				"internal/storages/README.md.tmpl",
				"internal/observability/storage_observer.go.tmpl",
				"internal/observability/storage_observer_test.go.tmpl",
			},
		},
		{
			title:   "Cache Components Cleanup",
			enabled: input.renderAll && !projectComponents.Cache,
			action:  p.workspace.cleanupDisabledCacheGeneratedFiles,
		},
		{
			title:   "Legacy File Cleanup",
			enabled: input.renderAll,
			action:  p.cleanupLegacyGeneratedFiles,
		},
		{
			title:   "Docker Components Rendering",
			enabled: p.config.Render.Components.Docker,
			templates: append([]string{
				"docker-compose.yml.tmpl",
				"containers/goforj-development-services.md.tmpl",
				"containers/elasticmq/elasticmq.conf.tmpl",
			},
				func() []string {
					if p.config.Render.Components.DatabaseMySQL {
						return []string{
							"containers/mariadb/Dockerfile",
							"containers/mariadb/my.cnf",
						}
					}
					return nil
				}()...,
			),
		},
		{
			title:   "Maintenance Components Rendering",
			enabled: projectComponents.WebAPI || projectComponents.WebUI,
			templates: []string{
				"internal/maintenance/state.go.tmpl",
				"internal/maintenance/state_test.go.tmpl",
				"internal/maintenance/cmd.go.tmpl",
				"internal/maintenance/cmd_test.go.tmpl",
			},
		},
		{
			title:   "Dev Console Components Rendering",
			enabled: projectComponents.HasRuntime(),
			templates: []string{
				"internal/lighthouse/agent.go.tmpl",
				"internal/lighthouse/cli.go.tmpl",
				"internal/lighthouse/conn.go.tmpl",
				"internal/lighthouse/conn_test.go.tmpl",
				"internal/lighthouse/enable.go.tmpl",
				"internal/lighthouse/hub.go.tmpl",
				"internal/lighthouse/hub_test.go.tmpl",
				"internal/lighthouse/inspects.go.tmpl",
				"internal/lighthouse/inspects_test.go.tmpl",
				"internal/lighthouse/log_hook.go.tmpl",
				"internal/lighthouse/protocol.go.tmpl",
				"internal/lighthouse/server.go.tmpl",
				"internal/lighthouse/editor.go.tmpl",
				"internal/lighthouse/ui.go.tmpl",
			},
			raw: []string{
				"internal/lighthouse/ui/dist",
			},
		},
		{
			title:   "Web API Components Rendering",
			enabled: projectComponents.WebAPI || projectComponents.WebUI,
			templates: []string{
				"internal/http/lighthouse.go.tmpl",
				"internal/http/maintenance.go.tmpl",
				"internal/http/maintenance_test.go.tmpl",
				"internal/http/README.md.tmpl",
				"internal/http/cors.go.tmpl",
				"internal/http/routes_list_cmd.go.tmpl",
				"internal/http/health.go.tmpl",
				"internal/http/health_test.go.tmpl",
				"internal/http/inspect_child_event_test.go.tmpl",
				"internal/http/inspects_bench_test.go.tmpl",
				"internal/http/runtime_bench_test.go.tmpl",
				"internal/http/runtime_test.go.tmpl",
				"internal/http/server_test.go.tmpl",
				"internal/http/swagger.go.tmpl",
				"internal/http/swagger_test.go.tmpl",
				"internal/http/readiness_checks.go.tmpl",
				"internal/http/runtime.go.tmpl",
				"internal/http/serve_cmd.go.tmpl",
				"internal/http/server.go.tmpl",
				"internal/http/spa.go.tmpl",
				"internal/http/types.go.tmpl",
				"internal/hello/controller.go.tmpl",
				"internal/makecmd/make_controller_cmd.go.tmpl",
				"internal/makecmd/make_controller_cmd_test.go.tmpl",
			},
			action: func() error {
				if input.renderAll {
					return nil
				}
				if err := p.writeTemplateMappings([]templateMapping{
					mapTemplateTo("wire/inject_http.go.tmpl", "app/wire/inject_http.go"),
				}); err != nil {
					return err
				}
				return p.writeTemplateMappingsOnce([]templateMapping{
					mapTemplateTo("wire/inject_http_controllers_app.go.tmpl", "app/wire/inject_http_controllers_app.go"),
				})
			},
		},
		{
			title:   "Web UI Components Rendering",
			enabled: p.config.Render.Components.WebUI,
			action: func() error {
				if input.renderAll {
					return nil
				}
				if err := p.writeTemplateMappingsOnce([]templateMapping{
					mapTemplateTo("frontend/dist/index.html.tmpl", projectlayout.FrontendDistIndex(".", project.DefaultApp())),
				}); err != nil {
					return err
				}
				return p.ensureFrontendPlaceholderAssets(project.DefaultApp())
			},
		},
		{
			title:   "Starter Kit Rendering",
			enabled: p.config.Render.Components.WebUI && p.config.Render.StarterKit != project.StarterKitNone && !p.config.Render.Components.DemoApp,
			action:  p.scaffoldDefaultStarterKit,
		},
		{
			title:   "Metrics Components Rendering",
			enabled: projectComponents.Metrics,
			templates: []string{
				"internal/metrics/README.md.tmpl",
				"internal/metrics/endpoint.go.tmpl",
				"internal/metrics/manager.go.tmpl",
				"internal/metrics/manager_test.go.tmpl",
			},
			action: func() error {
				if !projectComponents.WebAPI {
					return nil
				}
				return p.writeTemplates([]string{
					"internal/http/metrics.go.tmpl",
					"internal/http/metrics_test.go.tmpl",
				})
			},
		},
		{
			title:   "Observability Components Rendering",
			enabled: p.config.Render.Components.Observability,
			templates: []string{
				"internal/observability/README.md.tmpl",
				"containers/observability/vmagent/Dockerfile.tmpl",
				"containers/observability/vmagent/metrics-targets.json.tmpl",
				"containers/observability/vmagent/prometheus.yml.tmpl",
			},
			action: func() error {
				templates := []string{}
				if p.config.Render.Components.Grafana {
					templates = append(templates,
						"containers/observability/grafana/Dockerfile.tmpl",
						"containers/observability/grafana/Dockerfile.seed.tmpl",
						"containers/observability/grafana/provisioning/datasources/datasource.yml.tmpl",
						"containers/observability/grafana/provisioning/dashboards/dashboards.yml.tmpl",
						"containers/observability/grafana/seed-dashboards.sh.tmpl",
						"containers/observability/grafana/dashboards/platform-overview.json.tmpl",
						"containers/observability/grafana/dashboards/lighthouse-inspects-overview.json.tmpl",
						"containers/observability/grafana/dashboards/http-overview.json.tmpl",
						"containers/observability/grafana/dashboards/auth-overview.json.tmpl",
						"containers/observability/grafana/dashboards/scheduler-overview.json.tmpl",
					)
					if projectComponents.Cache {
						templates = append(templates, "containers/observability/grafana/dashboards/cache-overview.json.tmpl")
					}
					if projectComponents.Jobs {
						templates = append(templates, "containers/observability/grafana/dashboards/queue-overview.json.tmpl")
					}
					if projectComponents.Storage {
						templates = append(templates, "containers/observability/grafana/dashboards/storage-overview.json.tmpl")
					}
					if projectComponents.Events {
						templates = append(templates, "containers/observability/grafana/dashboards/events-overview.json.tmpl")
					}
					if projectComponents.Mail {
						templates = append(templates, "containers/observability/grafana/dashboards/mail-overview.json.tmpl")
					}
					if projectComponents.HasDatabase() {
						templates = append(templates, "containers/observability/grafana/dashboards/database-overview.json.tmpl")
					}
				}
				if len(templates) == 0 {
					return nil
				}
				return p.writeTemplates(templates)
			},
		},
		{
			title:   "Mail Components Rendering",
			enabled: projectComponents.Mail,
			templates: []string{
				"internal/mail/README.md.tmpl",
			},
		},
		{
			title:   "Auth Components Rendering",
			enabled: projectComponents.Auth && projectComponents.HasDatabase(),
			templates: []string{
				"internal/mail/auth_delivery.go.tmpl",
				"internal/auth/README.md.tmpl",
				"internal/auth/controller.go.tmpl",
				"internal/auth/delivery.go.tmpl",
				"internal/auth/bootstrap_cmd.go.tmpl",
				"internal/auth/create_user_cmd.go.tmpl",
				"internal/auth/email_verification.go.tmpl",
				"internal/auth/login_attempt.go.tmpl",
				"internal/auth/service_integration_test.go.tmpl",
				"internal/auth/password_reset.go.tmpl",
				"internal/auth/set_password_cmd.go.tmpl",
				"internal/auth/service.go.tmpl",
				"internal/auth/session.go.tmpl",
				"internal/auth/user.go.tmpl",
			},
			action: func() error {
				if !input.renderAll {
					if err := p.writeTemplateMappings([]templateMapping{
						mapTemplate("app/routes.go.tmpl"),
						mapTemplateTo("wire/inject_auth.go.tmpl", "app/wire/inject_auth.go"),
					}); err != nil {
						return err
					}
					if err := p.writeTemplateMappingsOnce([]templateMapping{
						mapTemplateTo("wire/inject_http_controllers_app.go.tmpl", "app/wire/inject_http_controllers_app.go"),
					}); err != nil {
						return err
					}
				}
				return p.writeTemplateMappingsOnce([]templateMapping{
					mapTemplate("migrations/2026_04_09_000001_auth_users.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000001_auth_users.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000001_auth_users.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000001_auth_users.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000001_auth_users.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000001_auth_users.sqlite.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000002_auth_sessions.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000002_auth_sessions.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000002_auth_sessions.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000002_auth_sessions.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000002_auth_sessions.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000002_auth_sessions.sqlite.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000003_auth_password_resets.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000003_auth_password_resets.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000003_auth_password_resets.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000003_auth_password_resets.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000003_auth_password_resets.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000003_auth_password_resets.sqlite.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000004_auth_login_attempts.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000004_auth_login_attempts.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000004_auth_login_attempts.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000004_auth_login_attempts.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000004_auth_login_attempts.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000004_auth_login_attempts.sqlite.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000005_auth_email_verifications.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000005_auth_email_verifications.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000005_auth_email_verifications.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000005_auth_email_verifications.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000005_auth_email_verifications.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000005_auth_email_verifications.sqlite.down.sql.tmpl"),
				})
			},
		},
		{
			title:   "OAuth Components Rendering",
			enabled: projectComponents.Auth && projectComponents.OAuth && projectComponents.HasDatabase(),
			templates: []string{
				"internal/auth/identity.go.tmpl",
				"internal/auth/oauth_provider.go.tmpl",
				"internal/auth/oauth_provider_apple.go.tmpl",
				"internal/auth/oauth_provider_github.go.tmpl",
				"internal/auth/oauth_provider_google.go.tmpl",
				"internal/auth/oauth_provider_microsoft.go.tmpl",
				"internal/auth/oauth_integration_test.go.tmpl",
				"internal/auth/oauth_state.go.tmpl",
			},
			action: func() error {
				return p.writeTemplateMappingsOnce([]templateMapping{
					mapTemplate("migrations/2026_04_09_000006_auth_identities.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000006_auth_identities.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000006_auth_identities.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000006_auth_identities.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000006_auth_identities.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000006_auth_identities.sqlite.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000007_auth_oauth_states.mysql.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000007_auth_oauth_states.mysql.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000007_auth_oauth_states.postgres.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000007_auth_oauth_states.postgres.down.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000007_auth_oauth_states.sqlite.up.sql.tmpl"),
					mapTemplate("migrations/2026_04_09_000007_auth_oauth_states.sqlite.down.sql.tmpl"),
				})
			},
		},
		{
			title:   "Demo App Components Rendering",
			enabled: input.renderAll && p.config.Render.Components.DemoApp,
			action: func() error {
				if !p.config.Render.Components.HasDatabase() {
					return nil
				}

				includeDemoInternal := func(tmpl string) bool {
					if p.config.Render.Components.Jobs {
						return !strings.HasPrefix(filepath.ToSlash(tmpl), "demo/internal/migrations/")
					}
					s := filepath.ToSlash(tmpl)
					if strings.HasPrefix(s, "demo/internal/migrations/") {
						return false
					}
					if strings.HasPrefix(s, "demo/internal/alerts/") {
						return false
					}
					if strings.HasSuffix(s, "demo/internal/monitoring/check_service.go.tmpl") {
						return false
					}
					if strings.HasSuffix(s, "demo/internal/monitoring/monitor_check_job.go.tmpl") {
						return false
					}
					return true
				}
				if err := p.writeTemplatesUnder("demo/internal", "internal", includeDemoInternal); err != nil {
					return err
				}
				if err := p.writeTemplatesUnder("demo/internal/migrations", "migrations", nil); err != nil {
					return err
				}
				return nil
			},
		},
		{
			title:   "Demo App Frontend Scaffolding",
			enabled: input.renderAll && p.config.Render.Components.DemoApp && p.config.Render.Components.WebUI,
			action:  p.scaffoldDemoFrontend,
		},
		{
			title:   "Database Components Rendering",
			enabled: projectComponents.HasDatabase(),
			templates: append([]string{
				"internal/database/connections.go.tmpl",
				"internal/database/fingerprinting.go.tmpl",
				"internal/database/gorm_log_writer.go.tmpl",
				"internal/database/connections_test.go.tmpl",
				"internal/database/fingerprinting_test.go.tmpl",
				"internal/cmd/db_shell_cmd.go.tmpl",
				"internal/cmd/db_shell_cmd_test.go.tmpl",
				"internal/makecmd/make_model_cmd.go.tmpl",
				"internal/makecmd/make_model_mysql_integration_test.go.tmpl",
				"internal/makecmd/make_model_postgres_integration_test.go.tmpl",
				"internal/makecmd/make_model_sqlite_integration_test.go.tmpl",
				"internal/makecmd/repository_wire_test.go.tmpl",
			}, func() []string {
				if projectComponents.Metrics {
					return []string{"internal/database/metrics_logger.go.tmpl"}
				}
				return nil
			}()...),
			raw: []string{"internal/makecmd/model.tmpl"},
			action: func() error {
				if !input.renderAll {
					if err := p.writeTemplateMappings([]templateMapping{
						mapTemplateTo("wire/inject_db.go.tmpl", "app/wire/inject_db.go"),
					}); err != nil {
						return err
					}
					if err := p.writeTemplateMappingsOnce([]templateMapping{
						mapTemplateTo("wire/inject_repositories_app.go.tmpl", "app/wire/inject_repositories_app.go"),
					}); err != nil {
						return err
					}
				}
				return p.writeTemplateMappings([]templateMapping{
					mapTemplate("migrations/migrations.go.tmpl"),
					mapTemplate("migrations/migrations_test.go.tmpl"),
					mapTemplate("migrations/migration_connection_test.go.tmpl"),
					mapTemplate("migrations/migration_commands_test.go.tmpl"),
					mapTemplate("migrations/migrate_cmd.go.tmpl"),
					mapTemplate("migrations/migrate_rollback_cmd.go.tmpl"),
					mapTemplate("migrations/.goforj/placeholder.txt.tmpl"),
				})
			},
		},
		{
			title:   "Scheduler Components Rendering",
			enabled: projectComponents.Scheduler,
			templates: []string{
				"internal/schedules/lighthouse.go.tmpl",
				"internal/schedules/runtime.go.tmpl",
				"internal/schedules/scheduler.go.tmpl",
				"internal/schedules/app_schedules.go.tmpl",
				"internal/schedules/cmd.go.tmpl",
				"internal/schedules/registration.go.tmpl",
				"internal/makecmd/make_schedule_cmd.go.tmpl",
				"internal/makecmd/make_schedule_cmd_test.go.tmpl",
			},
			action: func() error {
				if input.renderAll {
					return nil
				}
				if err := p.writeTemplateMappings([]templateMapping{
					mapTemplateTo("wire/inject_scheduler.go.tmpl", "app/wire/inject_scheduler.go"),
				}); err != nil {
					return err
				}
				return p.writeTemplateMappingsOnce([]templateMapping{
					mapTemplateTo("wire/inject_schedules_app.go.tmpl", "app/wire/inject_schedules_app.go"),
				})
			},
			raw: []string{"internal/makecmd/schedule.tmpl"},
		},
		{
			title:   "Job Components Rendering",
			enabled: projectComponents.Jobs,
			templates: []string{
				"internal/queues/README.md.tmpl",
				"internal/jobs/example_hello_job.go.tmpl",
				"internal/jobs/example_hello_job_cmd.go.tmpl",
				"internal/jobs/benchmark_run_cmd.go.tmpl",
				"internal/jobs/benchmark_system.go.tmpl",
				"internal/makecmd/make_job_cmd.go.tmpl",
				"internal/makecmd/make_job_cmd_test.go.tmpl",
				"internal/makecmd/make_queue_cmd.go.tmpl",
				"internal/makecmd/make_queue_cmd_test.go.tmpl",
				"internal/jobs/lighthouse.go.tmpl",
				"internal/jobs/lighthouse_benchmark.go.tmpl",
				"internal/jobs/lighthouse_queue.go.tmpl",
				"internal/jobs/runtime.go.tmpl",
				"internal/jobs/worker.go.tmpl",
				"internal/jobs/worker_logger.go.tmpl",
				"internal/jobs/worker_cmd.go.tmpl",
				"internal/observability/queue_observer.go.tmpl",
				"internal/observability/queue_observer_test.go.tmpl",
			},
			raw: []string{"internal/makecmd/job.tmpl"},
			action: func() error {
				if input.renderAll {
					return nil
				}
				if err := p.writeTemplateMappings([]templateMapping{
					mapTemplateTo("wire/inject_jobs.go.tmpl", "app/wire/inject_jobs.go"),
				}); err != nil {
					return err
				}
				return p.writeTemplateMappingsOnce([]templateMapping{
					mapTemplateTo("wire/inject_jobs_app.go.tmpl", "app/wire/inject_jobs_app.go"),
				})
			},
		},
		{
			title:   "Additional App Rendering",
			enabled: input.renderAll,
			action:  p.renderNamedApps,
		},
	}

	for _, step := range steps {
		if !step.enabled {
			continue
		}

		before := p.stats.counts()
		started := time.Now()

		if len(step.templates) > 0 {
			if err := p.writeTemplates(step.templates); err != nil {
				return err
			}
		}

		if len(step.renderOnceTemplates) > 0 {
			if err := p.writeTemplatesOnce(step.renderOnceTemplates); err != nil {
				return err
			}
		}

		if len(step.raw) > 0 {
			if err := p.writeRawFiles(step.raw); err != nil {
				return err
			}
		}
		if step.action != nil {
			if err := step.action(); err != nil {
				return err
			}
		}

		p.printStepSummary(step.title, before, time.Since(started))
	}

	// Run go mod tidy to ensure all dependencies are downloaded
	if err := p.timeRenderStage("applyModuleReplaces", p.applyModuleReplaces); err != nil {
		return fmt.Errorf("apply module replaces: %w", err)
	}

	// Sync core libraries so generated templates and module APIs stay aligned.
	if err := p.timeRenderStage("syncCoreLibraries", p.syncCoreLibraries); err != nil {
		return fmt.Errorf("sync core libraries: %w", err)
	}

	usesTemplHTMX := appRenderStarterKit(p.config, project.DefaultApp()) == project.StarterKitTemplHTMX
	if usesTemplHTMX {
		if err := p.timeRenderStage("runTemplGenerate", p.runTemplGenerate); err != nil {
			return fmt.Errorf("templ generate: %w", err)
		}
	}

	if input.renderAll {
		if err := p.timeRenderStage("generateProjectFiles", p.runGenerateAll); err != nil {
			return fmt.Errorf("generate: %w", err)
		}
	}

	// Run go mod tidy to ensure all dependencies are downloaded
	if err := p.timeRenderStage("goModTidy", func() error { return p.tidyModule(p) }); err != nil {
		if commanddiag.HasAction(err, "go mod tidy") {
			return err
		}
		return fmt.Errorf("go mod tidy: %w", err)
	}

	// Run wire install + generate to make main runnable immediately.
	if err := p.timeRenderStage("runWireGenerate", func() error { return p.generateWire(p) }); err != nil {
		return fmt.Errorf("wire generate: %w", err)
	}
	if input.renderAll {
		if _, err := atlas.ReconcileAgentGuidance(p.workspace.root, p.config.Render.AgentGuidance); err != nil {
			return fmt.Errorf("reconcile agent guidance: %w", err)
		}
	}

	p.printRenderDetails()
	p.printOverallSummary()

	return nil
}

// legacyQueueDriverDefault keeps old config readable while new choices flow through ResourcePlan.
func legacyQueueDriverDefault(legacy string) string {
	if driver := normalizeQueueDriver(legacy); driver != "" {
		return driver
	}
	return "workerpool"
}

// driverListContains compares driver names using the normalized environment representation.
func driverListContains(value string, want string) bool {
	for _, driver := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(driver), want) {
			return true
		}
	}
	return false
}

// RenderAppOnly renders one additional app without replaying the full project scaffold.
func (p *ProjectRenderer) RenderAppOnly(app project.App, opts makeapp.RenderOptions) error {
	if err := p.beginRenderInvocation("."); err != nil {
		return err
	}

	cfg, err := p.workspace.loadProjectConfig()
	if err != nil {
		return err
	}
	p.config = cfg
	if p.config.Render.Components.DemoApp {
		p.config.Render.Components.Auth = true
		p.config.Render.StarterKit = project.StarterKitNone
	}
	p.config.Render.Components.ResolveDependencies()
	if err := p.config.Render.Components.ValidateRenderContract(); err != nil {
		return err
	}
	p.config.Render.StarterKit = project.NormalizeStarterKit(p.config.Render.StarterKit)
	if !p.config.Render.Components.WebUI {
		p.config.Render.StarterKit = project.StarterKitNone
	}
	if err := project.ValidateStarterKitContract(p.config.Render.StarterKit, p.config.Render.Components); err != nil {
		return err
	}
	if err := p.validateEventsRenderTransition(project.ProjectComponents(p.config)); err != nil {
		return err
	}
	if err := p.validateStorageRenderTransition(project.ProjectComponents(p.config)); err != nil {
		return err
	}
	if err := p.validateJobsRenderTransition(project.ProjectComponents(p.config)); err != nil {
		return err
	}
	if err := p.validateCacheRenderTransition(project.ProjectComponents(p.config)); err != nil {
		return err
	}
	projectCapabilitiesChanged := false
	appConfigChanged := false
	if app.Name != "" && app.Name != project.DefaultAppName {
		if err := p.prepareDevAppsForAppMutation(); err != nil {
			return err
		}
		changed, err := p.setAppConfig(app.Name, opts.Components, opts.StarterKit, opts.StarterKitOptions, opts.HelpFormat)
		if err != nil {
			return err
		}
		if err := p.validateEventsRenderTransition(project.ProjectComponents(p.config)); err != nil {
			return err
		}
		if err := p.validateStorageRenderTransition(project.ProjectComponents(p.config)); err != nil {
			return err
		}
		if err := p.validateJobsRenderTransition(project.ProjectComponents(p.config)); err != nil {
			return err
		}
		if err := p.validateCacheRenderTransition(project.ProjectComponents(p.config)); err != nil {
			return err
		}
		projectCapabilitiesChanged = changed
		appConfigChanged = true
		p.setAppDevRun(app.Name, opts.DevRunCommand)
	}
	if err := p.prepareResourceRenderState(ComponentRenderInput{renderAll: true}, project.ProjectComponents(p.config)); err != nil {
		return err
	}
	if err := p.publishPendingResourceEnvironment(); err != nil {
		return err
	}
	if err := p.migrateAppOwnedWireFilenames(); err != nil {
		return err
	}
	if err := p.syncLegacyGeneratedTemplates(); err != nil {
		return err
	}
	if appConfigChanged {
		if err := p.writeAppEnvDefaults(app, appRenderComponents(p.config, app)); err != nil {
			return err
		}
		if err := p.workspace.writeProjectConfig(p.config); err != nil {
			return err
		}
	}

	if projectCapabilitiesChanged {
		// A new App has no conventional files for the full renderer to discover yet, so this reconciles shared capabilities while the App-specific render below materializes its graph.
		if err := p.Render(ComponentRenderInput{renderAll: true, root: p.workspace.root}); err != nil {
			return err
		}
	}
	if err := p.renderApp(app); err != nil {
		return err
	}
	if err := p.writeTemplates([]string{
		"internal/cmd/launch.go.tmpl",
		"internal/cmd/launch_test.go.tmpl",
		"internal/runtime/apps.go.tmpl",
		"internal/runtime/apps_test.go.tmpl",
	}); err != nil {
		return err
	}
	if p.projectRenderComponents().HasDatabase() {
		if err := p.expandDefaultMigrationsForNamedApps(); err != nil {
			return err
		}
	}
	if !opts.SkipWire {
		if appRenderStarterKit(p.config, app) == project.StarterKitTemplHTMX {
			if err := p.runTemplGenerate(); err != nil {
				return err
			}
		}
		if err := p.runWireGenerate(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveApp removes conventional app-owned files and refreshes generated app metadata.
func (p *ProjectRenderer) RemoveApp(app project.App) (makeapp.RemoveResult, error) {
	if err := p.beginRenderInvocation("."); err != nil {
		return makeapp.RemoveResult{}, err
	}

	app = projectlayout.NormalizeApp(app)
	if app.Name == "" || app.Name == project.DefaultAppName {
		return makeapp.RemoveResult{}, fmt.Errorf("default app cannot be removed")
	}

	cfg, err := p.workspace.loadProjectConfig()
	if err != nil {
		return makeapp.RemoveResult{}, err
	}
	p.config = cfg
	beforeProjectComponents := project.ProjectComponents(p.config)
	if err := p.validateRemoveAppTransition(app); err != nil {
		return makeapp.RemoveResult{}, err
	}

	var result makeapp.RemoveResult
	for _, path := range []string{
		projectlayout.AppDir(".", app),
		projectlayout.FrontendDir(".", app),
	} {
		removed, err := p.workspace.removeTreeIfExists(path)
		if err != nil {
			return result, err
		}
		if removed {
			result.Removed = append(result.Removed, path)
		}
	}
	for _, path := range []string{
		projectlayout.Entrypoint(".", app),
		projectlayout.RuntimeBinary(".", app),
	} {
		removed, err := p.workspace.removeFileIfExists(path)
		if err != nil {
			return result, err
		}
		if removed {
			result.Removed = append(result.Removed, path)
		}
	}

	cmdDir := projectlayout.CommandDir(".", app)
	removed, err := p.workspace.removeEmptyDir(cmdDir)
	if err != nil {
		return result, err
	}
	if removed {
		result.Removed = append(result.Removed, cmdDir)
	}
	for _, path := range []string{".env", ".env.host", ".env.example"} {
		updated, err := p.workspace.removeAppEnvDefaults(path, app.Name)
		if err != nil {
			return result, err
		}
		if updated {
			result.Updated = append(result.Updated, path)
		}
	}
	if p.removeAppConfig(app.Name) {
		if err := p.workspace.writeProjectConfig(p.config); err != nil {
			return result, err
		}
		result.Updated = append(result.Updated, ".goforj.yml")
	}
	if !result.Changed() {
		return result, nil
	}
	afterProjectComponents := project.ProjectComponents(p.config)
	if beforeProjectComponents.Cache && !afterProjectComponents.Cache {
		if err := p.Render(ComponentRenderInput{renderAll: true, root: p.workspace.root}); err != nil {
			return result, fmt.Errorf("reconcile shared Cache surface after removing App %q: %w", app.Name, err)
		}
		return result, nil
	}
	if err := p.prepareResourceRenderState(ComponentRenderInput{renderAll: true}, afterProjectComponents); err != nil {
		return result, err
	}
	if err := p.publishPendingResourceEnvironment(); err != nil {
		return result, err
	}
	if err := p.writeTemplates([]string{
		"internal/runtime/apps.go.tmpl",
		"internal/runtime/apps_test.go.tmpl",
	}); err != nil {
		return result, err
	}
	result.Updated = append(result.Updated,
		filepath.Join("internal", "runtime", "apps.go"),
		filepath.Join("internal", "runtime", "apps_test.go"),
	)
	return result, nil
}

// validateRemoveAppTransition rejects removal of the last App owning a capability whose shared generated surface cannot be reconciled safely.
func (p *ProjectRenderer) validateRemoveAppTransition(app project.App) error {
	prospective := *p.config
	prospective.Apps = make(map[string]project.AppConfig, len(p.config.Apps))
	for name, appConfig := range p.config.Apps {
		if name != app.Name {
			prospective.Apps[name] = appConfig
		}
	}
	before := project.ProjectComponents(p.config)
	after := project.ProjectComponents(&prospective)
	checks := []struct {
		name     string
		removed  bool
		residues []string
	}{
		{name: "Events", removed: before.Events && !after.Events, residues: projectEventsResiduePaths()},
		{name: "Storage", removed: before.Storage && !after.Storage, residues: projectStorageResiduePaths()},
	}
	for _, check := range checks {
		if !check.removed {
			continue
		}
		for _, path := range check.residues {
			exists, err := p.workspace.renderPathExists(path)
			if err != nil {
				return err
			}
			if exists {
				return fmt.Errorf("cannot remove App %q because it is the last App using %s while %s exists; automatic %s removal is not supported", app.Name, check.name, path, check.name)
			}
		}
	}
	if before.Jobs && !after.Jobs {
		path, exists, err := p.workspace.projectJobsRemovalResiduePath()
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("cannot remove App %q because it is the last App using Jobs while %s exists; automatic Jobs removal is not supported", app.Name, path)
		}
		path, exists, err = p.workspace.appJobsRemovalResiduePath(app)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("cannot remove App %q because it is the last App using Jobs while %s contains generated Jobs accessors or wiring; automatic Jobs removal is not supported", app.Name, path)
		}
	}
	if before.Cache && !after.Cache {
		if err := p.validateCacheRenderTransition(after, app); err != nil {
			return fmt.Errorf("cannot remove App %q because it is the last App using Cache: %w", app.Name, err)
		}
	}
	return nil
}

// removeAppConfig forgets app-local render choices without downgrading project capabilities.
func (p *ProjectRenderer) removeAppConfig(name string) bool {
	if p.config == nil {
		return false
	}
	changed := false
	if tasks, removed := removeGeneratedDevFrontendInstallTask(p.config.Dev.Pre, project.AppForName(name)); removed {
		p.config.Dev.Pre = tasks
		changed = true
	}
	if p.config.Apps != nil {
		if _, ok := p.config.Apps[name]; ok {
			delete(p.config.Apps, name)
			changed = true
		}
		if len(p.config.Apps) == 0 {
			p.config.Apps = nil
		}
	}
	if p.config.Dev.Run != nil {
		if _, ok := p.config.Dev.Run[name]; ok {
			delete(p.config.Dev.Run, name)
			changed = true
		}
	}
	if p.config.Dev.Apps != nil {
		if _, ok := p.config.Dev.Apps[name]; ok {
			delete(p.config.Dev.Apps, name)
			changed = true
		}
	}
	return changed
}

// setAppConfig persists app participation so future full renders keep the same app shape.
func (p *ProjectRenderer) setAppConfig(name string, components project.Components, starterKit project.StarterKit, starterKitOptions *project.StarterKitOptions, helpFormat project.HelpFormat) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == project.DefaultAppName {
		return false, nil
	}
	if p.config.Apps == nil {
		p.config.Apps = map[string]project.AppConfig{}
	}
	before := project.ProjectComponents(p.config)
	available := p.config.Render.Components.WithResolvedDependencies()
	if components == (project.Components{}) {
		components = project.AppDefaultComponents(available)
	}
	components = project.NormalizeConfiguredAppComponents(p.config, components)
	if err := components.ValidateRenderContract(); err != nil {
		return false, err
	}
	starterKit = project.NormalizeStarterKit(starterKit)
	if !components.WebUI {
		starterKit = project.StarterKitNone
	}
	if starterKit == project.StarterKitNone {
		starterKitOptions = nil
	}
	if err := project.ValidateStarterKitContract(starterKit, components); err != nil {
		return false, err
	}
	helpFormat = project.NormalizeHelpFormat(helpFormat)
	existing := p.config.Apps[name]
	p.config.Apps[name] = project.AppConfig{
		Components:        components,
		StarterKit:        starterKit,
		StarterKitOptions: starterKitOptions,
		HelpFormat:        helpFormat,
		Extra:             existing.Extra,
	}
	return project.ProjectComponents(p.config) != before, nil
}

// prepareDevAppsForAppMutation establishes native App presence before make:app can change filesystem discovery.
func (p *ProjectRenderer) prepareDevAppsForAppMutation() error {
	if p.config.Dev.UsesStructuredApps() {
		return nil
	}
	if p.workspace.migrateGeneratedDevWatchers(p.config) {
		return nil
	}
	if hasLegacyDevAppLifecycle(p.config) {
		return fmt.Errorf("make app: customized legacy Build App/Run App lifecycle cannot be safely combined with dev.apps; migrate the lifecycle to dev.apps first")
	}
	p.config.Dev.Apps = map[string]project.DevApp{}
	return nil
}

// hasLegacyDevAppLifecycle identifies discovery-based App watchers that require a deliberate migration.
func hasLegacyDevAppLifecycle(config *project.Config) bool {
	if config.Dev.Run != nil {
		return true
	}
	for _, watch := range config.Dev.Watches {
		if watch.IsLegacy() && (watch.Name == "Build App" || watch.Name == "Run App") {
			return true
		}
	}
	return false
}

// setAppDevRun normalizes the legacy make:app choice into presence-based native lifecycle configuration.
func (p *ProjectRenderer) setAppDevRun(name string, command string) {
	name = strings.TrimSpace(name)
	command = strings.TrimSpace(command)
	if p.config == nil || name == "" || name == project.DefaultAppName {
		return
	}
	if p.config.Dev.Run != nil {
		delete(p.config.Dev.Run, name)
	}
	app := project.AppForName(name)
	if tasks, migrated := migrateGeneratedDevFrontendInstallTask(p.config.Dev.Pre, app); migrated {
		p.config.Dev.Pre = tasks
	}
	if command == "" {
		if p.config.Dev.Apps != nil {
			delete(p.config.Dev.Apps, name)
		}
		if tasks, removed := removeGeneratedDevFrontendInstallTask(p.config.Dev.Pre, app); removed {
			p.config.Dev.Pre = tasks
		}
		return
	}
	if p.config.Dev.Apps == nil {
		p.config.Dev.Apps = map[string]project.DevApp{}
	}
	configured := generatedDevAppConfig(p.config, app, command)
	if command == "run" && !appRenderComponents(p.config, app).HasRuntime() {
		// A CLI-only App has no conventional runtime, so an explicit run choice must remain an actual command override.
		configured.Run = &project.DevAppCommand{Exec: command, Shorthand: true}
	}
	p.config.Dev.Apps[name] = configured
	if len(configured.SPAs) > 0 {
		task := generatedDevFrontendInstallTask(app)
		if !hasDevFrontendInstallTask(p.config.Dev.Pre, app) {
			p.config.Dev.Pre = append(p.config.Dev.Pre, task)
		}
	} else if tasks, removed := removeGeneratedDevFrontendInstallTask(p.config.Dev.Pre, app); removed {
		p.config.Dev.Pre = tasks
	}
}

// writeAppEnvDefaults appends app-scoped env defaults without changing the default App values.
func (p *ProjectRenderer) writeAppEnvDefaults(app project.App, components project.Components) error {
	if app.Name == "" || app.Name == project.DefaultAppName {
		return nil
	}
	prefix := project.AppEnvironmentPrefix(app.Name)
	if prefix == "" {
		return nil
	}
	const environmentPath = ".env"
	const hostEnvironmentPath = ".env.host"
	if !components.Cache {
		for _, path := range []string{environmentPath, hostEnvironmentPath, ".env.example"} {
			if _, err := p.workspace.removeGeneratedAppCacheDriverDefault(path, app.Name); err != nil {
				return err
			}
		}
	}
	metadata := p.workspace.runtimeAppMetadataForConfiguredApp(p.config, app)
	metadata.HTTPPort = p.workspace.nextAvailableAppHTTPPort(environmentPath, prefix, metadata.HTTPPort)
	envDefaults := appRuntimeEnvDefaults(prefix, metadata, components)
	if components.Cache {
		cacheDriver, err := p.workspace.cacheDriverDefaultFromEnv(environmentPath)
		if err != nil {
			return err
		}
		envDefaults[prefix+"_CACHE_DRIVER"] = cacheDriver
	}
	if components.Events {
		eventsDriver, err := p.workspace.eventDriverDefaultFromEnv(environmentPath)
		if err != nil {
			return err
		}
		envDefaults[prefix+"_EVENTS_DRIVER"] = eventsDriver
	}
	if components.Storage {
		storageDriver, err := p.workspace.storageDriverDefaultFromEnv(environmentPath)
		if err != nil {
			return err
		}
		envDefaults[prefix+"_STORAGE_DRIVER"] = storageDriver
	}
	if components.Jobs {
		queueDriver, err := p.workspace.queueDriverDefaultFromEnv(environmentPath)
		if err != nil {
			return err
		}
		envDefaults[prefix+"_QUEUE_DRIVER"] = queueDriver
	}
	if driver := components.DatabaseDriver(); driver != "" {
		baseDriver := ""
		if p.config != nil {
			baseDriver = p.config.Render.Components.DatabaseDriver()
		}
		envDefaults = mergeEnvDefaults(envDefaults, appDatabaseEnvDefaults(prefix, driver, baseDriver, false))
	}
	envGlobals, envAppDefaults := splitEnvDefaultsByPrefix(envDefaults, prefix)
	if p.config != nil && p.config.Render.Components.DemoApp {
		// Demo migrations still exercise SQLite, so an additional-app render must not create a MySQL-only build contract before the full renderer runs.
		if err := p.workspace.upsertEnvDefaults(environmentPath, map[string]string{"DB_SUPPORTED_DRIVERS": "sqlite"}); err != nil {
			return err
		}
	}
	if err := p.workspace.upsertEnvDefaults(environmentPath, envGlobals); err != nil {
		return err
	}
	if err := p.workspace.upsertAppEnvDefaults(environmentPath, app.Name, prefix, envAppDefaults); err != nil {
		return err
	}

	hostDefaults := map[string]string{}
	if driver := components.DatabaseDriver(); driver != "" {
		baseDriver := ""
		if p.config != nil {
			baseDriver = p.config.Render.Components.DatabaseDriver()
		}
		hostDefaults = appDatabaseEnvDefaults(prefix, driver, baseDriver, true)
	}
	delete(hostDefaults, "DB_SUPPORTED_DRIVERS")
	hostGlobals, hostAppDefaults := splitEnvDefaultsByPrefix(hostDefaults, prefix)
	if err := p.workspace.upsertEnvDefaults(hostEnvironmentPath, hostGlobals); err != nil {
		return err
	}
	if err := p.workspace.upsertAppEnvDefaults(hostEnvironmentPath, app.Name, prefix, hostAppDefaults); err != nil {
		return err
	}
	return nil
}

// mergeEnvDefaults combines generated env defaults while letting later defaults win.
func mergeEnvDefaults(base map[string]string, overlays ...map[string]string) map[string]string {
	merged := make(map[string]string, len(base))
	for key, value := range base {
		merged[key] = value
	}
	for _, overlay := range overlays {
		for key, value := range overlay {
			merged[key] = value
		}
	}
	return merged
}

// splitEnvDefaultsByPrefix keeps global defaults out of app-specific env sections.
func splitEnvDefaultsByPrefix(defaults map[string]string, prefix string) (map[string]string, map[string]string) {
	globals := map[string]string{}
	appDefaults := map[string]string{}
	appPrefix := prefix + "_"
	for key, value := range defaults {
		if strings.HasPrefix(key, appPrefix) {
			appDefaults[key] = value
			continue
		}
		globals[key] = value
	}
	return globals, appDefaults
}

// appRuntimeEnvDefaults writes only app values that app owners commonly edit.
func appRuntimeEnvDefaults(prefix string, metadata runtimeAppMetadata, components project.Components) map[string]string {
	values := map[string]string{}
	if components.WebAPI || components.WebUI {
		values[prefix+"_APP_URL"] = fmt.Sprintf("http://localhost:%d", metadata.HTTPPort)
		values[prefix+"_API_HTTP_PORT"] = strconv.Itoa(metadata.HTTPPort)
	}
	return values
}

// cacheDriverDefaultFromEnv keeps an incrementally added App aligned with the project's active Cache backend.
func (w projectRenderWorkspace) cacheDriverDefaultFromEnv(path string) (string, error) {
	return w.resourceDriverDefaultFromEnv(path, project.ResourceCache, "CACHE", "memory")
}

// eventDriverDefaultFromEnv keeps an incrementally added App aligned with the project's active Events transport.
func (w projectRenderWorkspace) eventDriverDefaultFromEnv(path string) (string, error) {
	return w.resourceDriverDefaultFromEnv(path, project.ResourceEvents, "EVENTS", "inproc")
}

// storageDriverDefaultFromEnv keeps an incrementally added App aligned with the project's active Storage backend.
func (w projectRenderWorkspace) storageDriverDefaultFromEnv(path string) (string, error) {
	return w.resourceDriverDefaultFromEnv(path, project.ResourceStorage, "STORAGE", "local")
}

// queueDriverDefaultFromEnv keeps an incrementally added App aligned with the project's active Queue backend.
func (w projectRenderWorkspace) queueDriverDefaultFromEnv(path string) (string, error) {
	return w.resourceDriverDefaultFromEnv(path, project.ResourceQueue, "QUEUE", "workerpool")
}

// resourceDriverDefaultFromEnv validates one owner-controlled root driver before projecting it into an additional-app overlay.
func (w projectRenderWorkspace) resourceDriverDefaultFromEnv(path string, resource project.ResourceKey, envPrefix string, fallback string) (string, error) {
	data, err := w.readFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s environment: %w", envPrefix, err)
	}
	activeKey := envPrefix + "_DRIVER"
	supportedKey := envPrefix + "_SUPPORTED_DRIVERS"
	lines := strings.Split(string(data), "\n")
	driver, found := envfile.Lookup(lines, activeKey)
	if !found || strings.TrimSpace(driver) == "" {
		driver = fallback
	}
	driver = project.CanonicalResourceDriver(resource, driver)
	definition, ok := project.ResourceDefinitionByKey(resource)
	if !ok {
		return "", fmt.Errorf("%s resource definition is unavailable", envPrefix)
	}
	if _, ok := definition.Driver(driver); !ok {
		return "", fmt.Errorf("%s in %s selects unsupported driver %q", activeKey, path, driver)
	}
	supported, supportedSet := envfile.Lookup(lines, supportedKey)
	if supportedSet && strings.TrimSpace(supported) != "" && !driverListContains(supported, driver) {
		return "", fmt.Errorf("%s in %s excludes active %s %q", supportedKey, path, activeKey, driver)
	}
	return driver, nil
}

// nextAvailableAppHTTPPort keeps sequential App defaults unique within one project workspace.
func (w projectRenderWorkspace) nextAvailableAppHTTPPort(path string, prefix string, preferred int) int {
	if preferred <= 0 {
		preferred = 3001
	}
	used := w.appHTTPPortsFromEnv(path, prefix)
	if _, exists := used[preferred]; !exists {
		return preferred
	}
	for port := 3001; port < 4000; port++ {
		if _, exists := used[port]; !exists {
			return port
		}
	}
	return preferred
}

// appHTTPPortsFromEnv reads assigned additional-app ports from one project workspace.
func (w projectRenderWorkspace) appHTTPPortsFromEnv(path string, currentPrefix string) map[int]struct{} {
	used := map[int]struct{}{}
	data, err := w.readFile(path)
	if err != nil {
		return used
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := envfile.ParseAssignment(line)
		if !ok || !isNamedAppHTTPPortKey(key, currentPrefix) {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || port <= 0 {
			continue
		}
		used[port] = struct{}{}
	}
	return used
}

// isNamedAppHTTPPortKey centralizes the is named app httpport key decision for its callers.
func isNamedAppHTTPPortKey(key string, currentPrefix string) bool {
	key = strings.TrimSpace(key)
	if key == "" || currentPrefix == "" {
		return false
	}
	if strings.HasPrefix(key, currentPrefix+"_") {
		return false
	}
	if key == "PORT" || key == "API_HTTP_PORT" {
		return false
	}
	return strings.HasSuffix(key, "_API_HTTP_PORT") || strings.HasSuffix(key, "_PORT")
}

// appDatabaseEnvDefaults creates conventional env keys for one app database driver.
func appDatabaseEnvDefaults(prefix string, driver string, baseDriver string, host bool) map[string]string {
	values := map[string]string{}
	if !host {
		values["DB_SUPPORTED_DRIVERS"] = driver
		values[prefix+"_DB_DATABASE"] = appDatabaseName(prefix, driver)
		if driver != "sqlite" {
			values[prefix+"_DB_SQLITE_DATABASE"] = appDatabaseName(prefix, "sqlite")
		}
	}
	if baseDriver != "" && driver == baseDriver {
		return values
	}
	if !host {
		values[prefix+"_DB_DRIVER"] = driver
	}
	switch driver {
	case "mysql":
		values[prefix+"_DB_HOST"] = appDatabaseHost("mysql", host)
		if !host {
			values[prefix+"_DB_USERNAME"] = "user"
			values[prefix+"_DB_PASSWORD"] = "password"
			values[prefix+"_DB_PORT"] = "3306"
		}
	case "postgres":
		values[prefix+"_DB_HOST"] = appDatabaseHost("postgres", host)
		if !host {
			values[prefix+"_DB_USERNAME"] = "postgres"
			values[prefix+"_DB_PASSWORD"] = "postgres"
			values[prefix+"_DB_PORT"] = "5432"
		}
	case "sqlite":
	}
	return values
}

// appDatabaseName keeps app database names compact while preserving SQLite paths.
func appDatabaseName(prefix string, driver string) string {
	if driver == "sqlite" {
		return "./_data/sqlite/" + strings.ToLower(prefix) + ".db"
	}
	return prefixDatabaseName(prefix, "db")
}

// appDatabaseHost maps container defaults to localhost when writing host override env.
func appDatabaseHost(service string, host bool) string {
	if host {
		return "localhost"
	}
	return service
}

// prefixDatabaseName keeps generated app database names compact and deterministic.
func prefixDatabaseName(prefix string, fallback string) string {
	name := str.Of(prefix).ReplaceAll("_", "").ToLower().String()
	if name == "" {
		return fallback
	}
	return name
}

// upsertEnvDefaults preserves existing env files while filling missing app defaults.
func (w projectRenderWorkspace) upsertEnvDefaults(path string, defaults map[string]string) error {
	if len(defaults) == 0 {
		return nil
	}
	if err := w.rejectEnvironmentSpecialFile(path); err != nil {
		return err
	}
	data, err := w.readFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	for key, value := range defaults {
		if key == "DB_SUPPORTED_DRIVERS" {
			lines = upsertSupportedDriver(lines, value)
			continue
		}
		lines = upsertEnvLine(lines, key, value)
	}
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := w.writeFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return w.logicalError(os.Chmod(w.path(path), 0o600))
}

// upsertAppEnvDefaults groups app overrides so multi-app env files remain readable.
func (w projectRenderWorkspace) upsertAppEnvDefaults(path string, appName string, prefix string, defaults map[string]string) error {
	if len(defaults) == 0 {
		return nil
	}
	if err := w.rejectEnvironmentSpecialFile(path); err != nil {
		return err
	}
	data, err := w.readFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(data), "\n")
	lines = removeEnvKeys(lines, defaults)
	lines = removeEnvSectionHeader(lines, appName)
	lines = trimTrailingBlankLines(lines)
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, "# "+envSectionTitle(appName))
	for _, key := range orderedEnvDefaultKeys(defaults, prefix) {
		lines = append(lines, key+"="+defaults[key])
	}
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return w.writeFile(path, []byte(content), 0o644)
}

// removeEnvKeys prevents old loose app entries from surviving after sectioned rendering.
func removeEnvKeys(lines []string, defaults map[string]string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		key, _, ok := envfile.ParseAssignment(line)
		if ok {
			if _, exists := defaults[key]; exists {
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

// removeEnvSectionHeader avoids duplicating the generated heading on repeated renders.
func removeEnvSectionHeader(lines []string, appName string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if isEnvSectionHeader(line, appName) {
			continue
		}
		out = append(out, line)
	}
	return out
}

// removeAppEnvDefaults deletes app-prefixed overrides when an app is removed.
func (w projectRenderWorkspace) removeAppEnvDefaults(path string, appName string) (bool, error) {
	prefix := project.AppEnvironmentPrefix(appName)
	if prefix == "" {
		return false, nil
	}
	if err := w.rejectEnvironmentSpecialFile(path); err != nil {
		return false, err
	}
	data, err := w.readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	original := string(data)
	lines := strings.Split(original, "\n")
	out := make([]string, 0, len(lines))
	appPrefix := prefix + "_"
	for _, line := range lines {
		key, _, ok := envfile.ParseAssignment(line)
		if ok && strings.HasPrefix(key, appPrefix) {
			continue
		}
		if isEnvSectionHeader(line, appName) {
			continue
		}
		out = append(out, line)
	}
	out = collapseBlankLines(trimTrailingBlankLines(out))
	content := strings.Join(out, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content == original {
		return false, nil
	}
	return true, w.writeFile(path, []byte(content), 0o644)
}

// isEnvSectionHeader recognizes generated app headings case-insensitively for cleanup.
func isEnvSectionHeader(line string, appName string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	title := str.Of(trimmed).TrimPrefix("#").Trim().String()
	return strings.EqualFold(title, envSectionTitle(appName)) || strings.EqualFold(title, appName)
}

// collapseBlankLines removes empty gaps left after deleting app env sections.
func collapseBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	previousBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank && previousBlank {
			continue
		}
		out = append(out, line)
		previousBlank = blank
	}
	return out
}

// trimTrailingBlankLines keeps generated sections separated by one predictable blank line.
func trimTrailingBlankLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// orderedEnvDefaultKeys writes generated app env keys in the same order every render.
func orderedEnvDefaultKeys(defaults map[string]string, prefix string) []string {
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	rank := map[string]int{
		prefix + "_APP_URL":                10,
		prefix + "_API_HTTP_PORT":          20,
		prefix + "_METRICS_API_PORT":       30,
		prefix + "_METRICS_SCHEDULER_PORT": 40,
		prefix + "_METRICS_JOBS_PORT":      50,
		prefix + "_EVENTS_DRIVER":          60,
		prefix + "_QUEUE_DRIVER":           70,
		prefix + "_DB_DRIVER":              80,
		prefix + "_DB_DATABASE":            90,
		prefix + "_DB_SQLITE_DATABASE":     95,
		prefix + "_DB_HOST":                100,
		prefix + "_DB_PORT":                110,
		prefix + "_DB_USERNAME":            120,
		prefix + "_DB_PASSWORD":            130,
	}
	sort.Slice(keys, func(left, right int) bool {
		leftRank, leftKnown := rank[keys[left]]
		rightRank, rightKnown := rank[keys[right]]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return keys[left] < keys[right]
	})
	return keys
}

// envSectionTitle formats app slugs as readable env section headings.
func envSectionTitle(appName string) string {
	parts := strings.FieldsFunc(appName, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})
	for idx, part := range parts {
		part = strings.ToLower(part)
		if part == "" {
			continue
		}
		parts[idx] = strings.ToUpper(part[:1]) + part[1:]
	}
	title := strings.Join(parts, " ")
	if title == "" {
		return appName
	}
	return title
}

// upsertSupportedDriver appends a database driver while preserving existing driver order.
func upsertSupportedDriver(lines []string, driver string) []string {
	for idx, line := range lines {
		key, value, ok := envfile.ParseAssignment(line)
		if !ok || key != "DB_SUPPORTED_DRIVERS" {
			continue
		}
		drivers := appendDriver(value, driver)
		lines[idx] = key + "=" + strings.Join(drivers, ",")
		return lines
	}
	return append(lines, "DB_SUPPORTED_DRIVERS="+driver)
}

// appendDriver normalizes a comma-separated driver list before adding a new driver.
func appendDriver(value string, driver string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(value, ",") {
		normalized := str.Of(part).Trim().ToLower().String()
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	driver = str.Of(driver).Trim().ToLower().String()
	if driver != "" && !seen[driver] {
		out = append(out, driver)
	}
	return out
}

// upsertEnvLine replaces an existing env key or appends it when missing.
func upsertEnvLine(lines []string, key string, value string) []string {
	for idx, line := range lines {
		currentKey, _, ok := envfile.ParseAssignment(line)
		if !ok || currentKey != key {
			continue
		}
		lines[idx] = key + "=" + value
		return lines
	}
	return append(lines, key+"="+value)
}

// SetTimings controls whether render summaries include elapsed phase timings.
func (p *ProjectRenderer) SetTimings(enabled bool) {
	p.timings = enabled
}

// timeRenderStage centralizes time render stage behavior so callers follow the same contract.
func (p *ProjectRenderer) timeRenderStage(name string, fn func() error) error {
	timingEnabled := p.renderTimingEnabled()
	if !timingEnabled {
		return fn()
	}
	lineStart := len(p.lines)
	started := time.Now()
	err := fn()
	elapsed := time.Since(started)
	if err == nil {
		for i := lineStart; i < len(p.lines); i++ {
			p.lines[i] = appendRenderTiming(p.lines[i], elapsed)
		}
		p.flushRenderLines(lineStart)
		if renderDebugEnabled() {
			console.Debugf("render stage %s: %s", name, elapsed)
		}
	}
	return err
}

// renderTimingEnabled keeps the render timing enabled representation consistent.
func (p *ProjectRenderer) renderTimingEnabled() bool {
	if p != nil && p.timings {
		return true
	}
	for _, key := range []string{"FORJ_RENDER_TIMINGS", "FORJ_RENDER_DEBUG_TIMINGS"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" && value != "0" {
			return true
		}
	}
	return false
}

// streamRenderTimings centralizes stream render timings behavior so callers follow the same contract.
func (p *ProjectRenderer) streamRenderTimings() bool {
	return runningInsideDevCommand() && p.renderTimingEnabled()
}

// appendRenderLine centralizes append render line behavior so callers follow the same contract.
func (p *ProjectRenderer) appendRenderLine(line string) {
	p.lines = append(p.lines, line)
	p.flushRenderLines(len(p.lines) - 1)
}

// flushRenderLines centralizes flush render lines behavior so callers follow the same contract.
func (p *ProjectRenderer) flushRenderLines(start int) {
	if !p.streamRenderTimings() || start >= len(p.lines) {
		return
	}
	for _, line := range p.lines[start:] {
		fmt.Println(line)
	}
	p.lines = p.lines[:start]
}

// renderDebugEnabled keeps the render debug enabled representation consistent.
func renderDebugEnabled() bool {
	for _, key := range []string{"FORJ_DEBUG", "DEBUG"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" && value != "0" {
			return true
		}
	}
	return false
}

// syncProjectConfigForRender updates generated config conventions without persisting dependency-expanded component selections.
func (p *ProjectRenderer) syncProjectConfigForRender(configuredComponents project.Components) error {
	if p.config == nil {
		return nil
	}
	changed := false
	if !p.config.Render.HasAgentGuidance() {
		guidance, err := atlas.InferAgentGuidance(p.workspace.root)
		if err != nil {
			return err
		}
		p.config.Render.AgentGuidance = guidance
		changed = true
	}
	if p.config.NeedsComponentMigration() {
		changed = true
	}
	if p.removeLegacyQueueDriver {
		changed = true
	}
	defaultApp := project.DefaultApp()
	if len(p.config.Dev.WirePaths) == 0 || len(p.config.Dev.WirePaths) == 1 && p.config.Dev.WirePaths[0] == "wire" {
		p.config.Dev.WirePaths = []string{projectlayout.WireDir(".", defaultApp)}
		changed = true
	}
	if removeLegacyInitialBuildTask(&p.config.Dev.Pre) {
		changed = true
	}
	if p.config.Render.Components.Docker {
		if normalizeDockerComposeUpTask(&p.config.Dev.Pre, p.config.Render.Components) {
			changed = true
		}
		if normalizeDockerComposeDownTask(&p.config.Dev.Down) {
			changed = true
		}
	}
	if removeGrafanaSeedTask(&p.config.Dev.Pre) {
		changed = true
	}
	if p.workspace.migrateGeneratedDevWatchers(p.config) {
		changed = true
	}
	if migrateGeneratedDevFrontendInstallTasks(p.config) {
		changed = true
	}
	if migrateGeneratedDevSPABuildCommands(p.config) {
		changed = true
	}
	for i := range p.config.Dev.Watches {
		normalized := p.config.Dev.Watches[i].Watch
		if isGeneratedLegacyBuildWatcher(p.config.Dev.Watches[i]) {
			normalized = normalizeDevWatchWireGenExclusion(normalized)
			if p.config.Render.StarterKit == project.StarterKitTemplHTMX {
				normalized = normalizeTemplBuildWatchExclusions(normalized)
			}
		}
		if isGeneratedLegacyNPMWatcher(p.config.Dev.Watches[i]) {
			normalized = normalizeFrontendNPMWatchExclusions(normalized)
		}
		if normalized != p.config.Dev.Watches[i].Watch {
			p.config.Dev.Watches[i].Watch = normalized
			changed = true
		}
	}
	if p.config.Render.Components.WebUI && project.StarterKitUsesNPM(p.config.Render.StarterKit) && !p.config.Render.Components.DemoApp {
		task := generatedDevFrontendInstallTask(project.DefaultApp())
		if !hasDevFrontendInstallTask(p.config.Dev.Pre, project.DefaultApp()) {
			p.config.Dev.Pre = append(p.config.Dev.Pre, task)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	effectiveComponents := p.config.Render.Components
	p.config.Render.Components = configuredComponents
	defer func() {
		p.config.Render.Components = effectiveComponents
	}()
	return p.workspace.writeProjectConfig(p.config)
}

// ensureGitignoreEnvironmentRules adds newly generated local environment files without replacing owner-authored ignore rules.
func ensureGitignoreEnvironmentRules(path string) error {
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("inspect .gitignore: expected a regular file")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	source, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(source), "\n")
	seen := map[string]bool{}
	for _, line := range lines {
		seen[strings.TrimSpace(line)] = true
	}
	changed := false
	for _, rule := range []string{".env", ".env.host", ".env.local", ".env.staging", ".env.production", "!.env.example", "!.env.testing"} {
		if seen[rule] {
			continue
		}
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines[len(lines)-1] = rule
			lines = append(lines, "")
		} else {
			lines = append(lines, rule)
		}
		seen[rule] = true
		changed = true
	}
	if !changed {
		return nil
	}
	updated := strings.Join(lines, "\n")
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	return writeFileAtomically(path, []byte(updated), 0o644)
}

// dockerComposeUpDevCommand centralizes docker compose up dev command behavior so callers follow the same contract.
func dockerComposeUpDevCommand(components project.Components) string {
	return "docker-compose up -d"
}

// dockerComposeDownDevCommand selects every profile so a later profile edit cannot strand earlier containers.
func dockerComposeDownDevCommand() string {
	return `docker-compose --profile "*" down`
}

// normalizeDockerComposeUpTask keeps normalize docker compose up task handling consistent across callers.
func normalizeDockerComposeUpTask(tasks *[]project.DevTask, components project.Components) bool {
	changed := false
	want := dockerComposeUpDevCommand(components)
	legacy := map[string]bool{
		"docker-compose up -d":                                true,
		"docker-compose up -d --build":                        true,
		"docker-compose up -d --scale grafana-seed=0":         true,
		"docker-compose up -d --build --scale grafana-seed=0": true,
	}
	for i := range *tasks {
		if (*tasks)[i].Name != "Run Docker Compose" {
			continue
		}
		if legacy[(*tasks)[i].Cmd] && (*tasks)[i].Cmd != want {
			(*tasks)[i].Cmd = want
			changed = true
		}
	}
	return changed
}

// normalizeDockerComposeDownTask upgrades only the conventional generated teardown command.
func normalizeDockerComposeDownTask(tasks *[]project.DevTask) bool {
	changed := false
	want := dockerComposeDownDevCommand()
	for i := range *tasks {
		if strings.TrimSpace((*tasks)[i].Name) != "Docker Compose Down" || strings.TrimSpace((*tasks)[i].Cmd) != "docker-compose down" {
			continue
		}
		(*tasks)[i].Cmd = want
		changed = true
	}
	return changed
}

// removeGrafanaSeedTask centralizes remove grafana seed task behavior so callers follow the same contract.
func removeGrafanaSeedTask(tasks *[]project.DevTask) bool {
	changed := false
	out := (*tasks)[:0]
	for _, task := range *tasks {
		if task.Name == "Seed Grafana Dashboards" {
			changed = true
			continue
		}
		out = append(out, task)
	}
	*tasks = out
	return changed
}

// normalizeDevWatchWireGenExclusion keeps the generated wire exclusion stable across repeated renders.
func normalizeDevWatchWireGenExclusion(watch string) string {
	normalized := strings.ReplaceAll(watch, "-xfile wire/wire_gen\\.go$", "-xfile app/wire/wire_gen\\.go$")
	for strings.Contains(normalized, "app/app/") {
		normalized = strings.ReplaceAll(normalized, "app/app/", "app/")
	}
	return normalized
}

// normalizeFrontendNPMWatchExclusions keeps frontend watchers focused on source files.
func normalizeFrontendNPMWatchExclusions(watch string) string {
	normalized := removeWatchArgs(watch, map[string]map[string]bool{
		"-xdir": {".": true},
	})
	return appendMissingWatchArgs(normalized, []string{"-xdir node_modules", "-xdir dist"})
}

// normalizeTemplBuildWatchExclusions keeps normalize templ build watch exclusions handling consistent across callers.
func normalizeTemplBuildWatchExclusions(watch string) string {
	return appendMissingWatchArgs(watch, []string{"-xfile '.*_templ\\.go$'"})
}

// appendMissingWatchArgs centralizes append missing watch args behavior so callers follow the same contract.
func appendMissingWatchArgs(watch string, args []string) string {
	normalized := strings.TrimSpace(watch)
	for _, arg := range args {
		fields := strings.Fields(arg)
		if len(fields) != 2 {
			continue
		}
		needle := fields[0] + " " + strings.Trim(fields[1], "'\"")
		if strings.Contains(normalized, arg) || strings.Contains(normalized, needle) {
			continue
		}
		if normalized == "" {
			normalized = arg
			continue
		}
		normalized += " " + arg
	}
	return normalized
}

// removeWatchArgs drops legacy wgo flag pairs that block recursive source watching.
func removeWatchArgs(watch string, removals map[string]map[string]bool) string {
	fields := strings.Fields(watch)
	if len(fields) == 0 {
		return ""
	}
	kept := make([]string, 0, len(fields))
	for index := 0; index < len(fields); index++ {
		flag := fields[index]
		values, ok := removals[flag]
		if ok && index+1 < len(fields) {
			value := strings.Trim(fields[index+1], "'\"")
			if values[value] {
				index++
				continue
			}
		}
		kept = append(kept, flag)
	}
	return strings.Join(kept, " ")
}

// removeLegacyInitialBuildTask removes the old single-app bootstrap build now owned by forj dev.
func removeLegacyInitialBuildTask(tasks *[]project.DevTask) bool {
	if tasks == nil {
		return false
	}
	filtered := (*tasks)[:0]
	removed := false
	for _, task := range *tasks {
		if strings.TrimSpace(task.Name) == "Initial build" && strings.TrimSpace(task.Cmd) == "forj build -o ./bin/app" {
			removed = true
			continue
		}
		filtered = append(filtered, task)
	}
	*tasks = filtered
	return removed
}

// renderNamedApps renders every non-default app discovered from conventional project layout.
func (p *ProjectRenderer) renderNamedApps() error {
	apps := projectlayout.DiscoveredNamedApps(p.workspace.discoveryRoot())
	if len(apps) == 1 {
		app := apps[0]
		if err := p.renderApp(app); err != nil {
			return fmt.Errorf("render app %s: %w", app.Name, err)
		}
	} else if len(apps) > 1 {
		errs := make([]error, len(apps))
		var wg sync.WaitGroup
		for i, app := range apps {
			wg.Add(1)
			go func(i int, app project.App) {
				defer wg.Done()
				if err := p.renderApp(app); err != nil {
					errs[i] = fmt.Errorf("render app %s: %w", app.Name, err)
				}
			}(i, app)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}
	if len(apps) > 0 && p.projectRenderComponents().HasDatabase() {
		if err := p.expandDefaultMigrationsForNamedApps(); err != nil {
			return err
		}
	}
	return nil
}

// expandDefaultMigrationsForNamedApps moves single-app migration streams into the explicit default app layout.
func (p *ProjectRenderer) expandDefaultMigrationsForNamedApps() error {
	root := "migrations"
	entries, err := p.workspace.readDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		source := filepath.Join(root, name)
		if entry.IsDir() {
			if shouldSkipMigrationExpansionDir(name) {
				continue
			}
			if err := p.workspace.moveDirectMigrationSQLFiles(source, filepath.Join(root, "app", name)); err != nil {
				return err
			}
			continue
		}
		if !isMigrationSQLFile(name) {
			continue
		}
		if err := p.workspace.moveMigrationFile(source, filepath.Join(root, "app", "default", name)); err != nil {
			return err
		}
	}
	return nil
}

// shouldSkipMigrationExpansionDir preserves metadata and already-expanded app directories.
func shouldSkipMigrationExpansionDir(name string) bool {
	return name == ".goforj" || name == "app" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// moveDirectMigrationSQLFiles moves only legacy direct SQL files within one project workspace and leaves nested app layouts alone.
func (w projectRenderWorkspace) moveDirectMigrationSQLFiles(sourceDir, destDir string) error {
	entries, err := w.readDir(sourceDir)
	if err != nil {
		return err
	}
	moved := false
	for _, entry := range entries {
		if entry.IsDir() || !isMigrationSQLFile(entry.Name()) {
			continue
		}
		if err := w.moveMigrationFile(filepath.Join(sourceDir, entry.Name()), filepath.Join(destDir, entry.Name())); err != nil {
			return err
		}
		moved = true
	}
	if !moved {
		return nil
	}

	remaining, err := w.readDir(sourceDir)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		_, err := w.removeEmptyDir(sourceDir)
		return err
	}
	return nil
}

// moveMigrationFile moves one migration file within a project workspace unless a different destination already exists.
func (w projectRenderWorkspace) moveMigrationFile(source, dest string) error {
	if err := w.ensureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	destinationExists, err := w.exists(dest)
	if err != nil {
		return err
	}
	if destinationExists {
		same, err := w.filesHaveSameContent(source, dest)
		if err != nil {
			return err
		}
		if same {
			_, err = w.removeFileIfExists(source)
			return err
		}
		return fmt.Errorf("migration expansion destination already exists with different content: %s", dest)
	}
	return w.move(source, dest)
}

// filesHaveSameContent prevents migration expansion from overwriting user-edited files inside one project workspace.
func (w projectRenderWorkspace) filesHaveSameContent(left, right string) (bool, error) {
	leftBytes, err := w.readFile(left)
	if err != nil {
		return false, err
	}
	rightBytes, err := w.readFile(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftBytes, rightBytes), nil
}

// isMigrationSQLFile reports whether a file belongs to a generated SQL migration pair.
func isMigrationSQLFile(name string) bool {
	return strings.HasSuffix(name, ".up.sql") || strings.HasSuffix(name, ".down.sql")
}

// renderApp writes the app entrypoint, composition files, and app-local Wire graph.
func (p *ProjectRenderer) renderApp(app project.App) error {
	app = projectlayout.NormalizeApp(app)
	if err := p.writeTemplateMappingsForApp(app, p.appFrameworkMappings(app)); err != nil {
		return err
	}
	if _, err := p.workspace.removeFileIfExists(filepath.Join(projectlayout.AppDir(".", app), "event_commands.go")); err != nil {
		return err
	}
	if err := p.migrateFrontendDistPlaceholder(app); err != nil {
		return err
	}
	if err := p.writeTemplateMappingsOnceForApp(app, p.appOwnedMappings(app)); err != nil {
		return err
	}
	if err := p.ensureFrontendPlaceholderAssets(app); err != nil {
		return err
	}
	if app.Name != project.DefaultAppName {
		return p.scaffoldAppStarterKit(app)
	}
	return nil
}

// migrateFrontendDistPlaceholder updates the old generated "no frontend deployed" page without touching real SPA builds.
func (p *ProjectRenderer) migrateFrontendDistPlaceholder(app project.App) error {
	if p.config == nil || !appRenderComponents(p.config, app).WebUI {
		return nil
	}
	app = projectlayout.NormalizeApp(app)
	path := projectlayout.FrontendDistIndex(".", app)
	content, err := p.workspace.readFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !isGeneratedFrontendDistPlaceholderNeedingRefresh(string(content), p.config.ProjectName) {
		return nil
	}
	if err := p.renderTemplateFile(path, "frontend/dist/index.html.tmpl", p.workspace.templateDataForApp(p.config, app)); err != nil {
		return err
	}
	return p.ensureFrontendPlaceholderAssets(app)
}

// ensureFrontendPlaceholderAssets copies static assets only when the generated fallback page references them.
func (p *ProjectRenderer) ensureFrontendPlaceholderAssets(app project.App) error {
	if p.config == nil || !appRenderComponents(p.config, app).WebUI {
		return nil
	}
	app = projectlayout.NormalizeApp(app)
	index := projectlayout.FrontendDistIndex(".", app)
	content, err := p.workspace.readFile(index)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	contentString := string(content)
	for _, asset := range frontendPlaceholderAssets {
		if !strings.Contains(contentString, asset.name) {
			continue
		}
		if err := p.copyFrontendPlaceholderAsset(filepath.Join(filepath.Dir(index), asset.name), asset.template); err != nil {
			return err
		}
	}
	return nil
}

// isGeneratedFrontendDistPlaceholderNeedingRefresh recognizes generated fallback pages that predate the current placeholder.
func isGeneratedFrontendDistPlaceholderNeedingRefresh(content string, projectName string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == strings.TrimSpace(oldFrontendDistPlaceholder(projectName)) {
		return true
	}
	if strings.Contains(trimmed, `data-goforj-placeholder="temper-v1"`) {
		return true
	}
	// The prior branded placeholder used a green network illustration that is specific enough to distinguish it from user-authored pages.
	if strings.Contains(trimmed, `<span class="brand-tagline">Composable apps for Go</span>`) &&
		strings.Contains(trimmed, `<section class="visual" aria-hidden="true">`) &&
		strings.Contains(trimmed, `<div class="connector-mask">`) {
		return !strings.Contains(trimmed, `data-goforj-placeholder="temper-v2"`)
	}
	// These strings are kept only to refresh generated placeholders from pre-rename projects.
	oldPlaceholderCopy := "This app " + "target is running, but no frontend " + "build has been deployed yet."
	previousAppCopy := "This app is running, but no frontend build has been deployed yet."
	oldSubtitle := "Application " + "target"
	previousSubtitle := "Ready for your " + "frontend"
	previousCopy := "Your app " + "is running. Add your " + "frontend to make this page yours."
	previousPolishedCopy := "Your application is accepting requests and ready to go."
	hasGeneratedPlaceholderCopy := strings.Contains(trimmed, oldPlaceholderCopy) ||
		strings.Contains(trimmed, previousAppCopy) ||
		strings.Contains(trimmed, previousCopy) ||
		strings.Contains(trimmed, "Your app is live. Create the experience that belongs here.") ||
		strings.Contains(trimmed, "Your app is live. Build the interface that belongs here.") ||
		strings.Contains(trimmed, previousPolishedCopy)
	if !hasGeneratedPlaceholderCopy || !strings.Contains(trimmed, "GoForj") {
		return false
	}
	legacyLogoName := "goforj-" + "v7.png"
	return strings.Contains(trimmed, `<span class="mark">G</span>`) ||
		strings.Contains(trimmed, legacyLogoName) ||
		!strings.Contains(trimmed, "brand-tagline") ||
		strings.Contains(trimmed, oldSubtitle) ||
		strings.Contains(trimmed, previousSubtitle) ||
		!strings.Contains(trimmed, "Composable apps for Go") ||
		!strings.Contains(trimmed, `class="app-meta"`) ||
		!strings.Contains(trimmed, `class="core"`) ||
		!strings.Contains(trimmed, `class="cube"`) ||
		!strings.Contains(trimmed, "Read the docs") ||
		!strings.Contains(trimmed, `class="visual"`) ||
		!strings.Contains(trimmed, `class="status"`) ||
		!strings.Contains(trimmed, `rel="icon"`)
}

// oldFrontendDistPlaceholder matches the previous generated placeholder so migrations do not overwrite custom HTML.
func oldFrontendDistPlaceholder(projectName string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Title</title>

    You've not deployed anything for %s yet.
</head>
<body>

</body>
</html>`, projectName)
}

// appFrameworkMappings returns files the CLI can safely refresh on every render.
func (p *ProjectRenderer) appFrameworkMappings(app project.App) []templateMapping {
	components := appRenderComponents(p.config, app)
	appDir := projectlayout.AppDir(".", app)
	wireDir := projectlayout.WireDir(".", app)
	mappings := []templateMapping{
		mapTemplateTo("cmd/app/main.go.tmpl", projectlayout.Entrypoint(".", app)),
		mapTemplateTo("app/root_cmd.go.tmpl", filepath.Join(appDir, "root_cmd.go")),
		mapTemplateTo("wire/app.go.tmpl", filepath.Join(wireDir, "app.go")),
		mapTemplateTo("wire/app_test.go.tmpl", filepath.Join(wireDir, "app_test.go")),
		mapTemplateTo("wire/inject_cmd.go.tmpl", filepath.Join(wireDir, "inject_cmd.go")),
		mapTemplateTo("wire/inject_managers.go.tmpl", filepath.Join(wireDir, "inject_managers.go")),
		mapTemplateTo("wire/wire.go.tmpl", filepath.Join(wireDir, "wire.go")),
	}
	if components.HasDatabase() {
		mappings = append(mappings, mapTemplateTo("wire/inject_db.go.tmpl", filepath.Join(wireDir, "inject_db.go")))
	}
	if components.Auth && components.HasDatabase() {
		mappings = append(mappings, mapTemplateTo("wire/inject_auth.go.tmpl", filepath.Join(wireDir, "inject_auth.go")))
	}
	if components.WebAPI || components.WebUI {
		mappings = append(mappings, mapTemplateTo("wire/inject_http.go.tmpl", filepath.Join(wireDir, "inject_http.go")))
	}
	if components.Scheduler {
		mappings = append(mappings, mapTemplateTo("wire/inject_scheduler.go.tmpl", filepath.Join(wireDir, "inject_scheduler.go")))
	}
	if components.Jobs {
		mappings = append(mappings, mapTemplateTo("wire/inject_jobs.go.tmpl", filepath.Join(wireDir, "inject_jobs.go")))
	}
	return mappings
}

// appOwnedMappings returns customization files that should be created once and then preserved.
func (p *ProjectRenderer) appOwnedMappings(app project.App) []templateMapping {
	components := appRenderComponents(p.config, app)
	appDir := projectlayout.AppDir(".", app)
	wireDir := projectlayout.WireDir(".", app)
	mappings := []templateMapping{
		mapTemplateTo("app/lifecycle.go.tmpl", filepath.Join(appDir, "lifecycle.go")),
		mapTemplateTo("app/commands.go.tmpl", filepath.Join(appDir, "commands.go")),
		mapTemplateTo("wire/inject_services_app.go.tmpl", filepath.Join(wireDir, "inject_services_app.go")),
		mapTemplateTo("wire/inject_cmd_app.go.tmpl", filepath.Join(wireDir, "inject_cmd_app.go")),
	}
	if components.Events {
		mappings = append(mappings, mapTemplateTo("wire/inject_subscribers_app.go.tmpl", filepath.Join(wireDir, "inject_subscribers_app.go")))
	}
	if components.WebUI {
		mappings = append(mappings, mapTemplateTo("frontend/dist/index.html.tmpl", projectlayout.FrontendDistIndex(".", app)))
	}
	if components.WebAPI || components.WebUI {
		mappings = append(mappings,
			mapTemplateTo("app/routes.go.tmpl", filepath.Join(appDir, "routes.go")),
			mapTemplateTo("wire/inject_http_controllers_app.go.tmpl", filepath.Join(wireDir, "inject_http_controllers_app.go")),
		)
	}
	if components.HasDatabase() {
		mappings = append(mappings, mapTemplateTo("wire/inject_repositories_app.go.tmpl", filepath.Join(wireDir, "inject_repositories_app.go")))
	}
	if components.Scheduler {
		mappings = append(mappings,
			mapTemplateTo("app/schedules.go.tmpl", filepath.Join(appDir, "schedules.go")),
			mapTemplateTo("wire/inject_schedules_app.go.tmpl", filepath.Join(wireDir, "inject_schedules_app.go")),
		)
	}
	if components.Jobs {
		mappings = append(mappings, mapTemplateTo("wire/inject_jobs_app.go.tmpl", filepath.Join(wireDir, "inject_jobs_app.go")))
	}
	return mappings
}

// writeProjectConfig persists renderer-owned YAML without exposing a partially written configuration.
func writeProjectConfig(path string, cfg *project.Config) error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return err
	}
	return writeFileAtomically(path, buf.Bytes(), 0o644)
}

// writeFileAtomically replaces a file only after its complete contents have reached a same-directory temporary file.
func writeFileAtomically(path string, data []byte, defaultMode fs.FileMode) error {
	mode := defaultMode.Perm()
	if info, err := os.Stat(path); err == nil {
		if defaultMode.Perm() != 0o600 {
			mode = info.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// createGoMod initializes go.mod without hiding command failures behind the existing-file path.
func (p *ProjectRenderer) createGoMod() error {
	exists, err := p.workspace.exists("go.mod")
	if err != nil {
		return err
	}
	if exists {
		p.stats.recordSkipped("go.mod (exists)")
		return nil
	}

	cmd := exec.Command("go", "mod", "init", p.config.GoModuleName)
	cmd.Dir = p.workspace.path()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := p.workspace.logicalError(cmd.Run()); err != nil {
		return commanddiag.Wrap("initialize go.mod", err, stderr.String(), stdout.String())
	}
	p.stats.recordCreated("go.mod")
	return nil
}

// goModTidy runs `go mod tidy` to ensure dependencies are downloaded.
func (p *ProjectRenderer) goModTidy() error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = p.workspace.path()
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := p.workspace.logicalError(cmd.Run()); err != nil {
		p.logger.Error().
			Str("stdout", stdout.String()).
			Str("stderr", stderr.String()).
			Msg("🔴 go mod tidy failed")
		return commanddiag.Wrap("go mod tidy", err, stderr.String(), stdout.String())
	}

	modCount := countTidyModules(stdout.String(), stderr.String())
	p.lines = append(p.lines, renderCountsLine("go mod tidy", modCount, 0, "modules"))

	return nil
}

// syncCoreLibraries updates core goforj dependencies so generated templates and
// module APIs stay aligned.
func (p *ProjectRenderer) syncCoreLibraries() error {
	return p.syncCoreLibrariesInDir(".")
}

// syncCoreLibrariesInDir updates core goforj dependencies in a specific module without forcing callers to change process cwd.
func (p *ProjectRenderer) syncCoreLibrariesInDir(dir string) error {
	projectDir := p.workspace.path(dir)
	components := project.Components{}
	if p.config != nil {
		components = project.ProjectComponents(p.config)
	}
	modules := coredeps.SyncCoreLibraries(components)
	if p.config != nil && p.config.Render.StarterKit == project.StarterKitTemplHTMX {
		modules = append(modules, "github.com/a-h/templ@"+coredeps.MustVersionFor("github.com/a-h/templ"))
	}
	modules, skipped, err := coreModulesNeedingSync(filepath.Join(projectDir, "go.mod"), modules)
	if err != nil {
		return p.workspace.logicalError(err)
	}
	if len(modules) == 0 {
		p.lines = append(p.lines, renderCountsLine("sync core libs", 0, skipped, "modules"))
		return nil
	}

	args := []string{"mod", "edit"}
	for _, module := range modules {
		args = append(args, "-require="+module)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = projectDir
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := p.workspace.logicalError(cmd.Run()); err != nil {
		return commanddiag.Wrap("go mod edit core libs", err, stderr.String(), stdout.String())
	}

	p.lines = append(p.lines, renderCountsLine("sync core libs", len(modules), skipped, "modules"))
	return nil
}

// coreModulesNeedingSync keeps render fast by avoiding go command work when go.mod already has the desired core dependencies.
func coreModulesNeedingSync(path string, desired []string) ([]string, int, error) {
	state, err := readGoModModuleState(path)
	if err != nil {
		if os.IsNotExist(err) {
			return desired, 0, nil
		}
		return nil, 0, err
	}

	pending := make([]string, 0, len(desired))
	skipped := 0
	for _, spec := range desired {
		module, version, ok := strings.Cut(spec, "@")
		if !ok || module == "" || version == "" {
			pending = append(pending, spec)
			continue
		}
		current, required := state.requires[module]
		if state.replaces[module] && required {
			skipped++
			continue
		}
		if required && current == version {
			skipped++
			continue
		}
		pending = append(pending, spec)
	}
	return pending, skipped, nil
}

type goModModuleState struct {
	requires map[string]string
	replaces map[string]bool
}

// readGoModModuleState reads only the directives needed for dependency sync so render does not pay module graph resolution cost.
func readGoModModuleState(path string) (goModModuleState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goModModuleState{}, err
	}
	state := goModModuleState{
		requires: map[string]string{},
		replaces: map[string]bool{},
	}
	mode := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(stripGoModLineComment(line))
		if trimmed == "" {
			continue
		}
		if mode != "" {
			if trimmed == ")" {
				mode = ""
				continue
			}
			recordGoModDirective(&state, mode, trimmed)
			continue
		}
		switch {
		case trimmed == "require (":
			mode = "require"
		case trimmed == "replace (":
			mode = "replace"
		case strings.HasPrefix(trimmed, "require "):
			recordGoModDirective(&state, "require", str.Of(trimmed).TrimPrefix("require ").Trim().String())
		case strings.HasPrefix(trimmed, "replace "):
			recordGoModDirective(&state, "replace", str.Of(trimmed).TrimPrefix("replace ").Trim().String())
		}
	}
	return state, nil
}

// recordGoModDirective stores minimal require and replace metadata because local replaces intentionally override pinned versions.
func recordGoModDirective(state *goModModuleState, directive string, line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	switch directive {
	case "require":
		if len(fields) >= 2 {
			state.requires[fields[0]] = fields[1]
		}
	case "replace":
		before, _, ok := strings.Cut(line, "=>")
		if !ok {
			return
		}
		fields = strings.Fields(before)
		if len(fields) > 0 {
			state.replaces[fields[0]] = true
		}
	}
}

// stripGoModLineComment lets the lightweight parser ignore inline comments without pulling in a module-file parser dependency.
func stripGoModLineComment(line string) string {
	before, _, ok := strings.Cut(line, "//")
	if !ok {
		return line
	}
	return before
}

// runTemplGenerate centralizes run templ generate behavior so callers follow the same contract.
func (p *ProjectRenderer) runTemplGenerate() error {
	cmd := exec.Command("go", "run", "github.com/a-h/templ/cmd/templ@v0.3.1020", "generate")
	cmd.Dir = p.workspace.path()
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	err = p.workspace.logicalError(err)
	if err != nil {
		return commanddiag.Wrap("go run templ", err, string(output))
	}
	p.lines = append(p.lines, renderCountsLine("templ generate", 1, 0, "commands"))
	return nil
}

// runWireGenerate refreshes Wire output for the default app and every discovered additional app.
func (p *ProjectRenderer) runWireGenerate() error {
	wirePath, err := p.wire.resolve()
	if err != nil {
		return err
	}

	wireDirs := p.wireGenerateDirs()
	if err := firstWireGenerateError(p.runWireGenerateDirs(wirePath, wireDirs)); err != nil {
		// If a stale wire binary was built with an older Go toolchain, reinstall
		// wire with the current toolchain and retry all app-local graphs once.
		if wireGenerateErr, ok := err.(*wireGenerateError); ok && wireGenerateErr.stale {
			path, installErr := p.wire.reinstall()
			if installErr != nil {
				return installErr
			}
			if retryErr := firstWireGenerateError(p.runWireGenerateDirs(path, wireDirs)); retryErr != nil {
				return retryErr
			}
		} else {
			return err
		}
	}

	p.lines = append(p.lines, renderCountsLine("wire generate", len(wireDirs), 0, "commands"))
	return nil
}

// runWireGenerateDirs executes independent App Wire graphs while retaining logical directories in diagnostics.
func (p *ProjectRenderer) runWireGenerateDirs(wirePath string, wireDirs []string) []error {
	errs := make([]error, len(wireDirs))
	var wg sync.WaitGroup
	for i, wireDir := range wireDirs {
		wg.Add(1)
		go func(i int, wireDir string) {
			defer wg.Done()
			errs[i] = p.runWireGenerateDir(wirePath, wireDir)
		}(i, wireDir)
	}
	wg.Wait()
	return errs
}

// runWireGenerateDir wraps one Wire failure with the logical App directory that owns the graph.
func (p *ProjectRenderer) runWireGenerateDir(wirePath string, wireDir string) error {
	out, err := p.runWireCommand(wirePath, wireDir)
	if err == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(out))
	return &wireGenerateError{
		dir:    wireDir,
		output: trimmed,
		err:    err,
		stale:  strings.Contains(trimmed, "package requires newer Go version"),
	}
}

// firstWireGenerateError centralizes first wire generate error behavior so callers follow the same contract.
func firstWireGenerateError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// wireGenerateDirs returns existing Wire directories in the order they should be generated.
func (p *ProjectRenderer) wireGenerateDirs() []string {
	seen := make(map[string]bool)
	add := func(dir string, dirs *[]string) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "." || dir == "" || seen[dir] {
			return
		}
		if info, err := p.workspace.stat(dir); err == nil && info.IsDir() {
			seen[dir] = true
			*dirs = append(*dirs, dir)
		}
	}

	dirs := make([]string, 0)
	add(projectlayout.WireDir(".", project.DefaultApp()), &dirs)
	if p.config != nil {
		for _, configured := range p.config.Dev.WirePaths {
			add(configured, &dirs)
		}
	}
	for _, app := range projectlayout.DiscoveredNamedApps(p.workspace.discoveryRoot()) {
		add(projectlayout.WireDir(".", app), &dirs)
	}
	if len(dirs) == 0 {
		add("wire", &dirs)
	}
	return dirs
}

// runWireCommand executes the Wire binary from one app-local Wire package inside the invocation workspace.
func (p *ProjectRenderer) runWireCommand(wirePath string, dir string) ([]byte, error) {
	cmd := exec.Command(wirePath)
	cmd.Dir = p.workspace.path(dir)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	return output, p.workspace.logicalError(err)
}

// installWire builds the pinned Wire command with the caller's active Go toolchain in an isolated directory.
func installWire() (string, error) {
	toolDir, err := os.MkdirTemp("", "forj-wire-*")
	if err != nil {
		return "", fmt.Errorf("create wire tool dir: %w", err)
	}
	install := exec.Command("go", "install", wireInstallTarget)
	install.Env = os.Environ()
	install.Env = append(install.Env, "GOBIN="+toolDir)
	if out, err := install.CombinedOutput(); err != nil {
		return "", commanddiag.Wrap("wire install", err, string(out))
	}
	wirePath, err := exec.LookPath(filepath.Join(toolDir, "wire"))
	if err != nil {
		return "", fmt.Errorf("wire install: locate binary after install: %w", err)
	}
	return wirePath, nil
}

// runGenerateAll regenerates only the packages authorized by the durable project component contract.
func (p *ProjectRenderer) runGenerateAll() error {
	result, err := generate.GenerateProjectFiles(p.workspace.path(), generate.GenerationSelectionFromComponents(p.projectRenderComponents()))
	if err != nil {
		return p.workspace.logicalError(err)
	}
	p.lines = append(p.lines, renderCountsLine("forj generate", result.TotalFiles, 0, "files"))
	return nil
}

// scaffoldDemoFrontend centralizes scaffold demo frontend behavior so callers follow the same contract.
func (p *ProjectRenderer) scaffoldDemoFrontend() error {
	frontendDir := projectlayout.FrontendDir(".", project.DefaultApp())
	if err := p.copyRawPathToDest("demo/frontend", frontendDir); err != nil {
		return err
	}
	exists, err := p.workspace.exists(frontendDir, "dist", "index.html")
	if err != nil {
		return err
	}
	if !exists {
		return p.ensureFrontendDistPlaceholder()
	}
	return nil
}

// scaffoldDefaultStarterKit centralizes scaffold default starter kit behavior so callers follow the same contract.
func (p *ProjectRenderer) scaffoldDefaultStarterKit() error {
	return p.scaffoldStarterKitForApp(project.DefaultApp(), p.config.Render.StarterKit, true)
}

// scaffoldAppStarterKit creates an app-local frontend scaffold only on first app creation.
func (p *ProjectRenderer) scaffoldAppStarterKit(app project.App) error {
	starterKit := appRenderStarterKit(p.config, app)
	if starterKit == project.StarterKitNone {
		return nil
	}
	exists, err := p.workspace.exists(projectlayout.FrontendDir(".", app), "package.json")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return p.scaffoldStarterKitForApp(app, starterKit, false)
}

// scaffoldStarterKitForApp overlays the selected starter kit without deleting user-owned frontend files.
func (p *ProjectRenderer) scaffoldStarterKitForApp(app project.App, starterKit project.StarterKit, overwriteGeneratedTemplates bool) error {
	starterKit = project.NormalizeStarterKit(starterKit)
	if starterKit == project.StarterKitNone {
		return nil
	}
	frontendDir := projectlayout.FrontendDir(".", app)
	componentLibrary := appRenderComponentLibrary(p.config, app)
	sourceRoot, err := starterKitFrontendSource(starterKit)
	if err != nil {
		return err
	}
	if err := p.copyRawPathToDestFiltered(sourceRoot, frontendDir, starterKitFrontendSkip(starterKit, componentLibrary)); err != nil {
		return err
	}
	if !componentLibrary {
		if err := p.removeUnmodifiedStarterKitShowcaseFiles(sourceRoot, frontendDir); err != nil {
			return err
		}
	}
	if err := p.renderStarterKitFeatureFiles(sourceRoot, frontendDir, componentLibrary); err != nil {
		return err
	}
	if !componentLibrary {
		if variantDist := starterKitComponentLibraryOffDist(starterKit); variantDist != "" {
			if err := p.copyRawPathToDest(variantDist, filepath.Join(frontendDir, "dist")); err != nil {
				return err
			}
		}
	}
	if starterKit == project.StarterKitTemplHTMX {
		if err := p.scaffoldTemplHTMXStarterKitForApp(app, overwriteGeneratedTemplates); err != nil {
			return err
		}
	}
	exists, err := p.workspace.exists(frontendDir, "dist", "index.html")
	if err != nil {
		return err
	}
	if !exists {
		if p.config != nil {
			if err := p.renderTemplateFile(projectlayout.FrontendDistIndex(".", app), "frontend/dist/index.html.tmpl", p.workspace.templateDataForApp(p.config, app)); err != nil {
				return err
			}
			return p.ensureFrontendPlaceholderAssets(app)
		}
		return p.writeFrontendDistPlaceholder(projectlayout.FrontendDistIndex(".", app), defaultFrontendDistPlaceholderContent())
	}
	return nil
}

// removeUnmodifiedStarterKitShowcaseFiles removes known showcase files while preserving owner additions and edits.
func (p *ProjectRenderer) removeUnmodifiedStarterKitShowcaseFiles(sourceRoot, frontendDir string) error {
	for _, relRoot := range []string{"src/components/showcase", "src/views/components"} {
		sourcePath := filepath.Join(sourceRoot, relRoot)
		if _, err := fs.ReadDir(templatesFS, sourcePath); err != nil {
			continue
		}
		if err := fs.WalkDir(templatesFS, sourcePath, func(entry string, item fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if item.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(sourceRoot, entry)
			if err != nil {
				return err
			}
			destination := filepath.Join(frontendDir, rel)
			current, err := p.workspace.readFile(destination)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			original, err := templatesFS.ReadFile(entry)
			if err != nil {
				return err
			}
			if !bytes.Equal(current, original) {
				return nil
			}
			_, err = p.workspace.removeFileIfExists(destination)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// starterKitFrontendSkip omits dependency trees and disabled showcase-only source directories.
func starterKitFrontendSkip(starterKit project.StarterKit, componentLibrary bool) func(string, fs.DirEntry) bool {
	return func(rel string, entry fs.DirEntry) bool {
		if skipFrontendDependencyDirectory(rel, entry) {
			return true
		}
		if componentLibrary || !entry.IsDir() {
			return false
		}
		clean := filepath.ToSlash(rel)
		if clean == "dist" && starterKit != project.StarterKitTemplHTMX {
			return true
		}
		return clean == "src/components/showcase" || clean == "src/views/components"
	}
}

// starterKitComponentLibraryOffDist returns the prebuilt browser bundle for a client-rendered minimal starter.
func starterKitComponentLibraryOffDist(starterKit project.StarterKit) string {
	switch project.NormalizeStarterKit(starterKit) {
	case project.StarterKitVue:
		return "starter-kits/vue/component-library-off-dist"
	case project.StarterKitReact:
		return "starter-kits/react/component-library-off-dist"
	default:
		return ""
	}
}

// renderStarterKitFeatureFiles resolves small feature blocks without duplicating complete frontend starter kits.
func (p *ProjectRenderer) renderStarterKitFeatureFiles(sourceRoot, frontendDir string, componentLibrary bool) error {
	for _, rel := range []string{"src/App.tsx", "src/router.ts", "src/lib/navigation.ts", "src/views/DashboardView.vue"} {
		source := filepath.Join(sourceRoot, rel)
		content, err := templatesFS.ReadFile(source)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !bytes.Contains(content, []byte("goforj:component-library:")) {
			continue
		}
		filtered, err := filterStarterKitFeature(content, componentLibrary)
		if err != nil {
			return fmt.Errorf("render starter-kit feature file %s: %w", source, err)
		}
		if err := p.workspace.writeFile(filepath.Join(frontendDir, rel), filtered, 0o644); err != nil {
			return err
		}
		p.stats.recordCreated(filepath.Join(frontendDir, rel))
	}
	return nil
}

// filterStarterKitFeature retains the enabled variant and strips renderer-only feature markers.
func filterStarterKitFeature(content []byte, componentLibrary bool) ([]byte, error) {
	const markerPrefix = "goforj:component-library:"
	lines := strings.Split(string(content), "\n")
	var output []string
	include := true
	inside := false
	for _, line := range lines {
		if strings.Contains(line, markerPrefix+"on:start") {
			if inside {
				return nil, fmt.Errorf("nested component-library feature marker")
			}
			inside = true
			include = componentLibrary
			continue
		}
		if strings.Contains(line, markerPrefix+"off:start") {
			if inside {
				return nil, fmt.Errorf("nested component-library feature marker")
			}
			inside = true
			include = !componentLibrary
			continue
		}
		if strings.Contains(line, markerPrefix+"end") {
			if !inside {
				return nil, fmt.Errorf("component-library end marker has no start")
			}
			inside = false
			include = true
			continue
		}
		if include {
			output = append(output, line)
		}
	}
	if inside {
		return nil, fmt.Errorf("component-library feature marker is not closed")
	}
	return []byte(strings.Join(output, "\n")), nil
}

// scaffoldTemplHTMXStarterKitForApp keeps server-rendered templates aligned when the frontend starter is refreshed.
func (p *ProjectRenderer) scaffoldTemplHTMXStarterKitForApp(app project.App, overwriteGeneratedTemplates bool) error {
	mappings := []templateMapping{
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/controller.go.tmpl", "internal/starterui/controller.go"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/controller_test.go.tmpl", "internal/starterui/controller_test.go"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/viewmodels.go.tmpl", "internal/starterui/viewmodels.go"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/auth_views.templ.tmpl", "internal/starterui/auth_views.templ"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/dashboard.templ.tmpl", "internal/starterui/dashboard.templ"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/icons.templ.tmpl", "internal/starterui/icons.templ"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/layout.templ.tmpl", "internal/starterui/layout.templ"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/settings_views.templ.tmpl", "internal/starterui/settings_views.templ"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/ui.templ.tmpl", "internal/starterui/ui.templ"),
		mapTemplateTo("starter-kits/templ-htmx/internal/starterui/views.templ.tmpl", "internal/starterui/views.templ"),
	}
	if appRenderComponentLibrary(p.config, app) {
		mappings = append(mappings,
			mapTemplateTo("starter-kits/templ-htmx/internal/starterui/components_data.templ.tmpl", "internal/starterui/components_data.templ"),
			mapTemplateTo("starter-kits/templ-htmx/internal/starterui/components_forms.templ.tmpl", "internal/starterui/components_forms.templ"),
			mapTemplateTo("starter-kits/templ-htmx/internal/starterui/components_navigation.templ.tmpl", "internal/starterui/components_navigation.templ"),
			mapTemplateTo("starter-kits/templ-htmx/internal/starterui/components_overlays.templ.tmpl", "internal/starterui/components_overlays.templ"),
			mapTemplateTo("starter-kits/templ-htmx/internal/starterui/components_views.templ.tmpl", "internal/starterui/components_views.templ"),
		)
	}
	if overwriteGeneratedTemplates {
		return p.writeTemplateMappingsForApp(app, mappings)
	}
	return p.writeTemplateMappingsOnceForApp(app, mappings)
}

// starterKitFrontendSource centralizes starter kit frontend source behavior so callers follow the same contract.
func starterKitFrontendSource(starterKit project.StarterKit) (string, error) {
	switch project.NormalizeStarterKit(starterKit) {
	case project.StarterKitVue:
		return "starter-kits/vue/frontend", nil
	case project.StarterKitReact:
		return "starter-kits/react/frontend", nil
	case project.StarterKitTemplHTMX:
		return "starter-kits/templ-htmx/frontend", nil
	default:
		return "", fmt.Errorf("unknown starter kit: %s", starterKit)
	}
}

// skipFrontendDependencyDirectory keeps local dependency trees out of copied starter-kit assets.
func skipFrontendDependencyDirectory(rel string, entry fs.DirEntry) bool {
	return entry.IsDir() && filepath.Base(rel) == "node_modules"
}

// ensureFrontendDistPlaceholder centralizes ensure frontend dist placeholder behavior so callers follow the same contract.
func (p *ProjectRenderer) ensureFrontendDistPlaceholder() error {
	content := defaultFrontendDistPlaceholderContent()
	paths := make([]string, 0)
	for _, app := range projectlayout.ConventionalApps(p.workspace.discoveryRoot()) {
		if !appRenderComponents(p.config, app).WebUI {
			continue
		}
		paths = append(paths, projectlayout.FrontendDistIndex(".", app))
	}
	for _, index := range paths {
		exists, err := p.workspace.exists(index)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := p.writeFrontendDistPlaceholder(index, content); err != nil {
			return err
		}
	}
	return nil
}

const (
	frontendPlaceholderLogoName          = "goforj-logo.png"
	frontendPlaceholderLogoTemplate      = "starter-kits/vue/frontend/public/goforj-logo.png"
	frontendPlaceholderDarkMarkName      = "goforj-mark-dark.svg"
	frontendPlaceholderDarkMarkTemplate  = "internal/lighthouse/ui/src/assets/goforj-mark-dark.svg"
	frontendPlaceholderLightMarkName     = "goforj-mark-light.svg"
	frontendPlaceholderLightMarkTemplate = "internal/lighthouse/ui/src/assets/goforj-mark-light.svg"
)

var frontendPlaceholderAssets = []struct {
	name     string
	template string
}{
	{name: frontendPlaceholderLogoName, template: frontendPlaceholderLogoTemplate},
	{name: frontendPlaceholderDarkMarkName, template: frontendPlaceholderDarkMarkTemplate},
	{name: frontendPlaceholderLightMarkName, template: frontendPlaceholderLightMarkTemplate},
}

// defaultFrontendDistPlaceholderContent keeps no-SPA fallback pages consistent across apps.
func defaultFrontendDistPlaceholderContent() string {
	return "<!doctype html><html><head><meta charset=\"UTF-8\"><title>Ready to build</title><link rel=\"icon\" href=\"./goforj-logo.png\" type=\"image/png\"><link rel=\"apple-touch-icon\" href=\"./goforj-logo.png\"></head><body><img src=\"./goforj-logo.png\" alt=\"GoForj logo\"></body></html>\n"
}

// writeFrontendDistPlaceholder writes a fallback SPA page and records it in render stats.
func (p *ProjectRenderer) writeFrontendDistPlaceholder(index string, content string) error {
	if err := p.workspace.ensureDir(filepath.Dir(index)); err != nil {
		return err
	}
	if err := p.workspace.writeFile(index, []byte(content), 0644); err != nil {
		return err
	}
	if p.stats != nil {
		p.stats.recordCreated(index)
	}
	for _, asset := range frontendPlaceholderAssets {
		if !strings.Contains(content, asset.name) {
			continue
		}
		if err := p.copyFrontendPlaceholderAsset(filepath.Join(filepath.Dir(index), asset.name), asset.template); err != nil {
			return err
		}
	}
	return nil
}

// copyFrontendPlaceholderAsset keeps fallback pages self-contained without overwriting identical assets.
func (p *ProjectRenderer) copyFrontendPlaceholderAsset(dest string, templatePath string) error {
	content, err := templatesFS.ReadFile(templatePath)
	if err != nil {
		return err
	}
	if existing, err := p.workspace.readFile(dest); err == nil {
		if bytes.Equal(existing, content) {
			if p.stats != nil {
				p.stats.recordSkipped(dest)
			}
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := p.workspace.ensureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	if err := p.workspace.writeFile(dest, content, 0644); err != nil {
		return err
	}
	if p.stats != nil {
		p.stats.recordCreated(dest)
	}
	return nil
}

// renderTemplateFile renders ordinary scaffold files while transactional environment files use the atomic variant.
func (p *ProjectRenderer) renderTemplateFile(destPath, tmpl string, data any) error {
	return p.renderTemplateFileWithAtomicWrite(destPath, tmpl, data, false, 0o644)
}

// renderTemplateIfMissing preserves owner files while surfacing filesystem failures that are not simple absence.
func (p *ProjectRenderer) renderTemplateIfMissing(destPath string, tmpl string, data any) error {
	exists, err := p.workspace.exists(destPath)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return p.renderTemplateFile(destPath, tmpl, data)
}

// renderTemplateFileAtomically renders a template through a same-directory replacement file.
func (p *ProjectRenderer) renderTemplateFileAtomically(destPath, tmpl string, data any) error {
	return p.renderTemplateFileWithAtomicWrite(destPath, tmpl, data, true, 0o644)
}

// renderPrivateTemplateFileAtomically creates new local environment files without granting group or world access.
func (p *ProjectRenderer) renderPrivateTemplateFileAtomically(destPath, tmpl string, data any) error {
	return p.renderTemplateFileWithAtomicWrite(destPath, tmpl, data, true, 0o600)
}

// parseProjectTemplate composes catalog fragments only for the generated Compose surface.
func parseProjectTemplate(tmplPath string, source []byte) (*template.Template, error) {
	t := template.New("")
	if tmplPath != "docker-compose.yml.tmpl" {
		return t.Parse(string(source))
	}

	definitions := devservices.Catalog()
	for _, definition := range definitions {
		partial, err := templatesFS.ReadFile(definition.Template)
		if err != nil {
			return nil, fmt.Errorf("read developer service template %s: %w", definition.Template, err)
		}
		if _, err := t.Parse(string(partial)); err != nil {
			return nil, fmt.Errorf("parse developer service template %s: %w", definition.Template, err)
		}
	}
	if _, err := t.Parse(developerServiceAggregateTemplate(definitions)); err != nil {
		return nil, fmt.Errorf("parse developer service catalog template: %w", err)
	}
	for _, definition := range definitions {
		for _, section := range []string{"volumes", "services"} {
			name := developerServiceTemplateName(definition, section)
			if t.Lookup(name) == nil {
				return nil, fmt.Errorf("developer service template %s does not define %q", definition.Template, name)
			}
		}
	}
	return t.Parse(string(source))
}

// developerServiceAggregateTemplate projects catalog order into the two Compose map sections.
func developerServiceAggregateTemplate(definitions []devservices.Definition) string {
	var source strings.Builder
	for _, section := range []string{"volumes", "services"} {
		fmt.Fprintf(&source, "{{ define %q }}", "developer-service-catalog-"+section)
		for _, definition := range definitions {
			fmt.Fprintf(&source, "{{ template %q . }}", developerServiceTemplateName(definition, section))
		}
		source.WriteString("{{ end }}")
	}
	return source.String()
}

// developerServiceTemplateName keeps partial identities deterministic from stable catalog keys.
func developerServiceTemplateName(definition devservices.Definition, section string) string {
	return "developer-service-" + string(definition.Key) + "-" + section
}

// renderTemplateFileWithAtomicWrite shares rendering while reserving atomic replacement for transactional files.
func (p *ProjectRenderer) renderTemplateFileWithAtomicWrite(destPath, tmpl string, data any, atomic bool, mode fs.FileMode) error {
	tmplBytes, err := templatesFS.ReadFile(tmpl)
	if err != nil {
		return err
	}
	t, err := parseProjectTemplate(tmpl, tmplBytes)
	if err != nil {
		return fmt.Errorf("parse template %s: %w", tmpl, err)
	}

	templateInput, err := p.prepareTemplateData(data)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, templateInput); err != nil {
		return err
	}

	newContent := buf.Bytes()
	formatted, err := maybeFormatGoSource(destPath, newContent)
	if err != nil {
		return err
	}
	newContent = formatted
	if existingContent, err := p.workspace.readFile(destPath); err == nil {
		if bytes.Equal(existingContent, newContent) {
			p.stats.recordSkipped(destPath)
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := p.workspace.ensureDir(filepath.Dir(destPath)); err != nil {
		return err
	}
	if atomic {
		if err := p.workspace.writeFileAtomically(destPath, newContent, mode); err != nil {
			return err
		}
	} else {
		if err := p.workspace.writeFile(destPath, newContent, 0644); err != nil {
			return err
		}
	}
	p.stats.recordCreated(destPath)
	return nil
}

// prepareTemplateData adds transient environment and service decisions without placing them in durable project configuration.
func (p *ProjectRenderer) prepareTemplateData(data any) (any, error) {
	value := data
	switch config := data.(type) {
	case *project.Config:
		value = p.workspace.templateDataForApp(config, project.DefaultApp())
	case project.Config:
		value = p.workspace.templateDataForApp(&config, project.DefaultApp())
	}
	if config, ok := value.(templateRenderConfig); ok {
		config.ProjectComponents = project.ProjectComponents(config.Config)
		applyDatabaseRenderCapabilities(&config.ProjectComponents, p.resources.plan)
		resources, err := resourceRenderValuesForPlanWithConsumers(p.resources.plan, config.ProjectComponents, p.resources.serviceIntent, p.resources.serviceConsumers)
		if err != nil {
			return nil, err
		}
		config.Resources = resources
		return config, nil
	}
	return value, nil
}

// projectRenderComponents derives shared capabilities and includes environment-owned database build support.
func (p *ProjectRenderer) projectRenderComponents() project.Components {
	components := project.ProjectComponents(p.config)
	applyDatabaseRenderCapabilities(&components, p.resources.plan)
	return components
}

// templateDataForApp keeps additional-app package paths and capability projections isolated within one project workspace.
func (w projectRenderWorkspace) templateDataForApp(config *project.Config, app project.App) templateRenderConfig {
	if app.Name == "" {
		app = project.DefaultApp()
	}
	appImportPath := filepath.ToSlash(projectlayout.AppDir(".", app))
	wireImportPath := filepath.ToSlash(projectlayout.WireDir(".", app))
	components := appRenderComponents(config, app)
	starterKit := appRenderStarterKit(config, app)
	helpFormat := appRenderHelpFormat(config, app)
	runtimeApps := w.runtimeAppMetadataForRender(config)
	return templateRenderConfig{
		Config:                      config,
		Components:                  components,
		ProjectComponents:           project.ProjectComponents(config),
		StarterKit:                  starterKit,
		ComponentLibrary:            appRenderComponentLibrary(config, app),
		HelpFormat:                  helpFormat,
		HelpFormatterFunc:           helpFormatterFunc(helpFormat),
		HelpCommandFunc:             helpCommandFunc(helpFormat),
		App:                         app,
		AppPackageName:              project.AppPackageName(app.Name),
		AppImportPath:               appImportPath,
		WireImportPath:              wireImportPath,
		AppIsDefault:                app.Name == project.DefaultAppName,
		HasNamedApps:                app.Name != project.DefaultAppName || len(runtimeApps) > 1,
		RuntimeApps:                 runtimeApps,
		LegacyEventPipelineField:    w.legacyEventPipelineField(app),
		LegacyEventPipelineProvider: w.legacyEventPipelineProvider(app),
	}
}

// helpFormatterFunc returns the generated konghelp package function used by Kong.
func helpFormatterFunc(helpFormat project.HelpFormat) string {
	switch project.NormalizeHelpFormat(helpFormat) {
	case project.HelpFormatGuided:
		return "GuidedFormatter"
	case project.HelpFormatExternalCLI:
		return "ExternalCLIFormatter"
	default:
		return "FrameworkFormatter"
	}
}

// helpCommandFunc returns the generated konghelp package function used for standalone preboot command help.
func helpCommandFunc(helpFormat project.HelpFormat) string {
	switch project.NormalizeHelpFormat(helpFormat) {
	case project.HelpFormatGuided:
		return "PrintGuidedCommandHelp"
	case project.HelpFormatExternalCLI:
		return "PrintExternalCLICommandHelp"
	default:
		return "PrintCommandHelp"
	}
}

// appRenderHelpFormat resolves the app-specific help formatter selection.
func appRenderHelpFormat(config *project.Config, app project.App) project.HelpFormat {
	if config == nil {
		return project.DefaultHelpFormat()
	}
	helpFormat := config.Render.HelpFormat
	if app.Name != "" && app.Name != project.DefaultAppName {
		if appConfig, ok := config.Apps[app.Name]; ok {
			helpFormat = appConfig.HelpFormat
		}
	}
	return project.NormalizeHelpFormat(helpFormat)
}

// appRenderComponents resolves the app-slice component participation for an app render.
func appRenderComponents(config *project.Config, app project.App) project.Components {
	if config == nil {
		return project.Components{}
	}
	components := config.Render.Components.WithResolvedDependencies()
	if app.Name == "" || app.Name == project.DefaultAppName {
		return components
	}
	appConfig, ok := config.Apps[app.Name]
	if ok {
		components = project.NormalizeConfiguredAppComponents(config, appConfig.Components)
	}
	return components
}

// appRenderStarterKit resolves the app-specific starter kit selection.
func appRenderStarterKit(config *project.Config, app project.App) project.StarterKit {
	if config == nil {
		return project.StarterKitNone
	}
	components := appRenderComponents(config, app)
	starterKit := config.Render.StarterKit
	if app.Name != "" && app.Name != project.DefaultAppName {
		if appConfig, ok := config.Apps[app.Name]; ok {
			starterKit = appConfig.StarterKit
		}
	}
	starterKit = project.NormalizeStarterKit(starterKit)
	if !components.WebUI {
		return project.StarterKitNone
	}
	return starterKit
}

// appRenderComponentLibrary resolves the default-on showcase choice for one App.
func appRenderComponentLibrary(config *project.Config, app project.App) bool {
	if config == nil {
		return true
	}
	options := config.Render.StarterKitOptions
	if app.Name != "" && app.Name != project.DefaultAppName {
		if appConfig, ok := config.Apps[app.Name]; ok {
			options = appConfig.StarterKitOptions
		}
	}
	return options.ComponentLibraryEnabled()
}

// runtimeAppMetadataForRender creates the compiled App table from one project's discovered and configured Apps.
func (w projectRenderWorkspace) runtimeAppMetadataForRender(config *project.Config) []runtimeAppMetadata {
	apps := projectlayout.RuntimeApps(w.discoveryRoot(), config)
	out := make([]runtimeAppMetadata, 0, len(apps))
	for i, app := range apps {
		out = append(out, runtimeAppMetadata{
			Name:        app.Name,
			Index:       i,
			EnvPrefix:   project.AppEnvironmentPrefix(app.Name),
			HTTPPort:    3000 + i,
			RuntimeBase: 10000 + i*10,
			Components:  appRenderComponents(config, app),
		})
	}
	return out
}

// runtimeAppMetadataForConfiguredApp includes persisted app config before new conventional files are discoverable in one project workspace.
func (w projectRenderWorkspace) runtimeAppMetadataForConfiguredApp(config *project.Config, app project.App) runtimeAppMetadata {
	metadata := runtimeAppMetadataForAppFromApps(app, projectlayout.RuntimeApps(w.discoveryRoot(), config, app))
	metadata.Components = appRenderComponents(config, app)
	return metadata
}

// runtimeAppMetadataForAppFromApps centralizes runtime app metadata for app from apps behavior so callers follow the same contract.
func runtimeAppMetadataForAppFromApps(app project.App, apps []project.App) runtimeAppMetadata {
	app = projectlayout.NormalizeApp(app)
	seen := map[string]project.App{}
	for _, candidate := range apps {
		candidate = projectlayout.NormalizeApp(candidate)
		if candidate.Name == "" || !project.IsSafeAppName(candidate.Name) || project.IsReservedAppName(candidate.Name) {
			continue
		}
		seen[candidate.Name] = candidate
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		if name != project.DefaultAppName {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	ordered := []string{project.DefaultAppName}
	ordered = append(ordered, names...)
	for index, name := range ordered {
		if name != app.Name {
			continue
		}
		return runtimeAppMetadata{
			Name:        app.Name,
			Index:       index,
			EnvPrefix:   project.AppEnvironmentPrefix(app.Name),
			HTTPPort:    3000 + index,
			RuntimeBase: 10000 + index*10,
		}
	}
	return runtimeAppMetadata{Name: app.Name, EnvPrefix: project.AppEnvironmentPrefix(app.Name), HTTPPort: 3000, RuntimeBase: 10000}
}

// writeTemplates renders ordinary templates to their conventional suffix-free destinations.
func (p *ProjectRenderer) writeTemplates(tmpls []string) error {
	for _, path := range tmpls {
		dest := strings.TrimSuffix(path, ".tmpl")
		if err := p.renderTemplateFile(dest, path, p.config); err != nil {
			return err
		}
	}
	return nil
}

// writeEnvironmentTemplates renders environment files atomically so config cleanup cannot outrun partial output.
func (p *ProjectRenderer) writeEnvironmentTemplates(tmpls []string) error {
	for _, path := range tmpls {
		dest := strings.TrimSuffix(path, ".tmpl")
		if err := p.renderPrivateTemplateFileAtomically(dest, path, p.config); err != nil {
			return err
		}
	}
	return nil
}

// writeTemplateMappings writes templates using source->destination mappings.
// mapTemplate(...) derives the destination by removing the trailing .tmpl suffix.
func (p *ProjectRenderer) writeTemplateMappings(mappings []templateMapping) error {
	for _, mapping := range mappings {
		if err := p.renderTemplateFile(mapping.dest, mapping.tmpl, p.config); err != nil {
			return err
		}
	}
	return nil
}

// writeTemplateMappingsForApp renders mapped templates with app-specific package and import data.
func (p *ProjectRenderer) writeTemplateMappingsForApp(app project.App, mappings []templateMapping) error {
	data := p.workspace.templateDataForApp(p.config, projectlayout.NormalizeApp(app))
	for _, mapping := range mappings {
		if err := p.renderTemplateFile(mapping.dest, mapping.tmpl, data); err != nil {
			return err
		}
	}
	return nil
}

// writeTemplatesUnder renders every .tmpl under srcRoot into destRoot, preserving tree layout.
func (p *ProjectRenderer) writeTemplatesUnder(srcRoot, destRoot string, include func(string) bool) error {
	return fs.WalkDir(templatesFS, srcRoot, func(entry string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry, ".tmpl") {
			return nil
		}
		if include != nil && !include(entry) {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, entry)
		if err != nil {
			return err
		}
		dest := filepath.Join(destRoot, strings.TrimSuffix(rel, ".tmpl"))
		return p.renderTemplateFile(dest, entry, p.config)
	})
}

// writeRawFiles writes raw files to the destination directory.
func (p *ProjectRenderer) writeRawFiles(paths []string) error {
	for _, path := range paths {
		if err := p.copyRawPath(path); err != nil {
			return err
		}
	}
	return nil
}

// copyRawPath centralizes copy raw path behavior so callers follow the same contract.
func (p *ProjectRenderer) copyRawPath(path string) error {
	if _, err := fs.ReadDir(templatesFS, path); err == nil {
		return fs.WalkDir(templatesFS, path, func(entry string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			return p.copyRawFile(entry)
		})
	}
	return p.copyRawFile(path)
}

// copyRawPathToDest centralizes copy raw path to dest behavior so callers follow the same contract.
func (p *ProjectRenderer) copyRawPathToDest(path, destRoot string) error {
	return p.copyRawPathToDestFiltered(path, destRoot, nil)
}

// copyRawPathToDestFiltered centralizes copy raw path to dest filtered behavior so callers follow the same contract.
func (p *ProjectRenderer) copyRawPathToDestFiltered(path, destRoot string, skip func(rel string, d fs.DirEntry) bool) error {
	if _, err := fs.ReadDir(templatesFS, path); err == nil {
		return fs.WalkDir(templatesFS, path, func(entry string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(path, entry)
			if err != nil {
				return err
			}
			if skip != nil && skip(rel, d) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			return p.copyRawFileToDest(entry, filepath.Join(destRoot, rel))
		})
	}
	base := filepath.Base(path)
	return p.copyRawFileToDest(path, filepath.Join(destRoot, base))
}

// copyRawFile centralizes copy raw file behavior so callers follow the same contract.
func (p *ProjectRenderer) copyRawFile(path string) error {
	return p.copyRawFileToDest(path, path)
}

// copyRawFileToDest centralizes copy raw file to dest behavior so callers follow the same contract.
func (p *ProjectRenderer) copyRawFileToDest(path, dest string) error {
	content, err := templatesFS.ReadFile(path)
	if err != nil {
		return err
	}
	if err := p.workspace.ensureDir(filepath.Dir(dest)); err != nil {
		return err
	}
	if err := p.workspace.writeFile(dest, content, 0644); err != nil {
		return err
	}
	p.stats.recordCreated(dest)
	return nil
}

// writeTemplatesOnce writes templates to the destination directory only if they do not already exist.
func (p *ProjectRenderer) writeTemplatesOnce(tmpls []string) error {
	for _, path := range tmpls {
		dest := strings.TrimSuffix(path, ".tmpl")

		exists, err := p.workspace.exists(dest)
		if err != nil {
			return err
		}
		if exists {
			p.stats.recordSkipped(dest)
			continue
		}

		if err := p.renderTemplateFile(dest, path, p.config); err != nil {
			return err
		}
	}
	return nil
}

// writeTemplateMappingsOnce writes mapped templates only if destination does not yet exist.
func (p *ProjectRenderer) writeTemplateMappingsOnce(mappings []templateMapping) error {
	for _, mapping := range mappings {
		exists, err := p.workspace.exists(mapping.dest)
		if err != nil {
			return err
		}
		if exists {
			p.stats.recordSkipped(mapping.dest)
			continue
		}
		if err := p.renderTemplateFile(mapping.dest, mapping.tmpl, p.config); err != nil {
			return err
		}
	}
	return nil
}

// writeTemplateMappingsOnceForApp preserves app-owned files after their first render.
func (p *ProjectRenderer) writeTemplateMappingsOnceForApp(app project.App, mappings []templateMapping) error {
	data := p.workspace.templateDataForApp(p.config, projectlayout.NormalizeApp(app))
	for _, mapping := range mappings {
		exists, err := p.workspace.exists(mapping.dest)
		if err != nil {
			return err
		}
		if exists {
			p.stats.recordSkipped(mapping.dest)
			continue
		}
		if err := p.renderTemplateFile(mapping.dest, mapping.tmpl, data); err != nil {
			return err
		}
	}
	return nil
}

// countTidyModules centralizes count tidy modules behavior so callers follow the same contract.
func countTidyModules(stdout, stderr string) int {
	combined := strings.Split(strings.TrimSpace(stdout+"\n"+stderr), "\n")
	count := 0
	for _, line := range combined {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "go:") || strings.HasPrefix(line, "downloading") {
			count++
		}
	}
	return count
}

// printStepSummary centralizes print step summary behavior so callers follow the same contract.
func (p *ProjectRenderer) printStepSummary(title string, before renderCounts, elapsed time.Duration) {
	after := p.stats.counts()
	created := after.created - before.created
	skipped := after.skipped - before.skipped
	if p.renderTimingEnabled() {
		p.appendRenderLine(renderCountsLineWithTiming(title, created, skipped, "files", elapsed))
		return
	}
	p.appendRenderLine(renderCountsLine(title, created, skipped, "files"))
}

// renderBox keeps the render box representation consistent.
func renderBox(title string, lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	maxLine := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxLine {
			maxLine = w
		}
	}
	if title != "" && lipgloss.Width(title) > maxLine {
		maxLine = lipgloss.Width(title)
	}
	inner := maxLine + 2
	top := ""
	if title == "" {
		top = boxBorder.Render("┌" + strings.Repeat("─", inner) + "┐")
	} else {
		topTitle := " " + title + " "
		fill := inner - lipgloss.Width(topTitle)
		if fill < 0 {
			fill = 0
		}
		top = boxBorder.Render("┌" + topTitle + strings.Repeat("─", fill) + "┐")
	}
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w < maxLine {
			line += strings.Repeat(" ", maxLine-w)
		}
		body = append(body, boxBorder.Render("│")+" "+line+" "+boxBorder.Render("│"))
	}
	bottom := boxBorder.Render("└" + strings.Repeat("─", inner) + "┘")
	box := strings.Join(append([]string{top}, append(body, bottom)...), "\n")
	return lipgloss.NewStyle().MarginLeft(1).Render(box)
}

// printRenderDetails centralizes print render details behavior so callers follow the same contract.
func (p *ProjectRenderer) printRenderDetails() {
	if len(p.lines) == 0 {
		return
	}
	if runningInsideDevCommand() {
		for _, line := range p.lines {
			fmt.Println(line)
		}
		return
	}
	title := fmt.Sprintf("%s Project rendering complete", markCreate)
	fmt.Printf("%s\n", renderBox(title, p.lines))
}

// printOverallSummary centralizes print overall summary behavior so callers follow the same contract.
func (p *ProjectRenderer) printOverallSummary() {
	total := p.stats.counts()
	if runningInsideDevCommand() {
		fmt.Printf("%s Render complete (created: %d, skipped: %d)\n", markCreate, total.created, total.skipped)
		return
	}
	title := fmt.Sprintf("%s Project render complete (created: %d, skipped: %d)", markCreate, total.created, total.skipped)
	lines := []string{}
	if total.skipped > 0 {
		lines = append(lines, fmt.Sprintf("%s existing files preserved: %d", markSkip, total.skipped))
	}
	next := p.nextSteps()
	if len(next) > 0 {
		lines = append(lines, nextStyle.Render("next steps:"))
		for _, step := range next {
			lines = append(lines, fmt.Sprintf("%s %s", markAction, step))
		}
	}
	fmt.Printf("\n%s\n", renderBox(title, lines))
}

// nextSteps centralizes next steps behavior so callers follow the same contract.
func (p *ProjectRenderer) nextSteps() []string {
	var steps []string

	steps = append(steps, fmt.Sprintf("Set environment defaults in %s, %s, and %s", commandStyle.Render(".env"), commandStyle.Render(".env.host"), commandStyle.Render(".env.local")))
	steps = append(steps, fmt.Sprintf("Start the dev loop: %s", commandStyle.Render("forj dev")))

	if p.config != nil {
		if p.config.Render.Components.WebUI {
			cmd := "cd " + filepath.ToSlash(projectlayout.FrontendDir(".", project.DefaultApp())) + " && " + generatedFrontendNPMInstallCommand
			steps = append(steps, fmt.Sprintf("Install frontend deps if you plan to edit the UI: %s", commandStyle.Render(cmd)))
		}
		if p.config.Render.StarterKit != project.StarterKitNone && p.config.Render.Components.Auth && p.config.Render.Components.HasDatabase() {
			createUserCmd := "bin/app auth:create-user --username <username> --email <email> --password <password>"
			starterKitName := "starter app"
			if definition, ok := project.StarterKitDefinitionByKey(p.config.Render.StarterKit); ok {
				starterKitName = definition.Label + " app"
			}
			steps = append(steps, fmt.Sprintf("Sign into the %s locally with %s / %s", starterKitName, commandStyle.Render("admin"), commandStyle.Render("admin")))
			steps = append(steps, fmt.Sprintf("Create another auth user: %s", commandStyle.Render(createUserCmd)))
		}
		if p.config.Render.Components.Mail && p.config.Render.Components.Docker {
			steps = append(steps, fmt.Sprintf("Open Mailpit inbox at %s", commandStyle.Render("http://localhost:8025")))
		}
		if p.config.Render.Components.HasDatabase() {
			steps = append(steps, fmt.Sprintf("Review initial migrations under %s before first run", commandStyle.Render("migrations")))
		}
		if p.config.Render.Components.Observability {
			steps = append(steps, fmt.Sprintf("Start local services: %s", commandStyle.Render("docker-compose up -d")))
			steps = append(steps, fmt.Sprintf("Inspect VictoriaMetrics at %s", commandStyle.Render("http://localhost:8428")))
		}
		if p.config.Render.Components.Grafana {
			steps = append(steps, fmt.Sprintf("Open Grafana at %s with %s / %s", commandStyle.Render("http://localhost:13001"), commandStyle.Render("admin"), commandStyle.Render("admin")))
		}
	}

	return steps
}

// runningInsideDevCommand centralizes running inside dev command behavior so callers follow the same contract.
func runningInsideDevCommand() bool {
	return strings.TrimSpace(os.Getenv("FORJ_COMMAND_ORIGIN")) == "dev_command"
}
