package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DevWatch represents a command to be run in development mode.
type DevWatch struct {
	Name     string            `yaml:"name" json:"name"`
	Watch    string            `yaml:"-" json:"-"`
	Legacy   bool              `yaml:"-" json:"-"`
	Include  []string          `yaml:"-" json:"-"`
	Ignore   []string          `yaml:"ignore,omitempty" json:"ignore,omitempty"`
	Roots    []string          `yaml:"roots,omitempty" json:"roots,omitempty"`
	WorkDir  string            `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	Files    DevWatchMatchers  `yaml:"files,omitempty" json:"files,omitempty"`
	Dirs     DevWatchMatchers  `yaml:"dirs,omitempty" json:"dirs,omitempty"`
	Exec     string            `yaml:"exec" json:"exec"`
	Env      map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Debounce string            `yaml:"debounce,omitempty" json:"debounce,omitempty"`
	Poll     string            `yaml:"poll,omitempty" json:"poll,omitempty"`
	Postpone bool              `yaml:"postpone,omitempty" json:"postpone,omitempty"`
	Restart  bool              `yaml:"restart,omitempty" json:"restart,omitempty"`
	Exit     bool              `yaml:"exit,omitempty" json:"exit,omitempty"`
	Stdin    bool              `yaml:"stdin,omitempty" json:"stdin,omitempty"`
	// Extra preserves watcher controls introduced by newer GoForj versions during config migration.
	Extra map[string]any `yaml:",inline" json:"-"`
}

// DevTask represents a task to be run in development mode.
type DevTask struct {
	Name string `yaml:"name" json:"name"`
	Cmd  string `yaml:"cmd" json:"cmd"`
}

// DevConfig represents development lifecycle configuration.
type DevConfig struct {
	Pre               []DevTask         `yaml:"pre" json:"pre"`
	Down              []DevTask         `yaml:"down" json:"down"`
	Run               map[string]string `yaml:"run,omitempty" json:"run,omitempty"`
	AutoMigrate       bool              `yaml:"auto_migrate" json:"auto_migrate"`
	DownOnExit        bool              `yaml:"down_on_exit" json:"down_on_exit"`
	SoundOnWatchError bool              `yaml:"sound_on_watch_error" json:"sound_on_watch_error"`
	WirePaths         []string          `yaml:"wire_paths" json:"wire_paths"`
	Watches           []DevWatch        `yaml:"watches,omitempty" json:"watches,omitempty"`
	Apps              map[string]DevApp `yaml:"apps,omitempty" json:"apps,omitempty"`
	// Extra preserves lifecycle settings introduced by newer GoForj versions during config migration.
	Extra          map[string]any `yaml:",inline" json:"-"`
	appsConfigured bool
}

// DefaultAppName is the conventional name of every Project's default app.
const DefaultAppName = "app"

// App describes one executable app in the project.
type App struct {
	Name       string `yaml:"name" json:"name"`
	Entrypoint string `yaml:"entrypoint" json:"entrypoint"`
	AppDir     string `yaml:"app_dir" json:"app_dir"`
	WireDir    string `yaml:"wire_dir" json:"wire_dir"`
}

// DefaultApp returns the conventional default app.
func DefaultApp() App {
	return AppForName(DefaultAppName)
}

// AppForName returns the conventional paths for an app name.
func AppForName(name string) App {
	if name == "" || name == DefaultAppName {
		return App{
			Name:       DefaultAppName,
			Entrypoint: "cmd/app/main.go",
			AppDir:     "app",
			WireDir:    "app/wire",
		}
	}
	return App{
		Name:       name,
		Entrypoint: filepath.Join("cmd", name, "main.go"),
		AppDir:     filepath.Join("app", name),
		WireDir:    filepath.Join("app", name, "wire"),
	}
}

// DefaultNamedApp returns the conventional paths for an app name.
//
// Deprecated: use AppForName.
func DefaultNamedApp(name string) App {
	return AppForName(name)
}

// IsSafeAppName reports whether name is a lowercase app slug safe for app-owned paths.
func IsSafeAppName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	previousWasSeparator := false
	for i, r := range name {
		if r >= 'a' && r <= 'z' {
			previousWasSeparator = false
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			previousWasSeparator = false
			continue
		}
		if r == '-' && !previousWasSeparator {
			previousWasSeparator = true
			continue
		}
		return false
	}
	return true
}

// AppPackageName converts an app slug into a valid Go package name for app-owned composition.
func AppPackageName(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	pkg := builder.String()
	if pkg == "" {
		return DefaultAppName
	}
	if pkg[0] >= '0' && pkg[0] <= '9' {
		pkg = DefaultAppName + pkg
	}
	if pkg != DefaultAppName && !strings.HasSuffix(pkg, DefaultAppName) {
		pkg += DefaultAppName
	}
	return pkg
}

// RenderConfig represents render-time defaults and selections.
type RenderConfig struct {
	Components           Components         `yaml:"components" json:"components"`
	StarterKit           StarterKit         `yaml:"starter_kit" json:"starter_kit"`
	StarterKitOptions    *StarterKitOptions `yaml:"starter_kit_options,omitempty" json:"starter_kit_options,omitempty"`
	HelpFormat           HelpFormat         `yaml:"help_format,omitempty" json:"help_format,omitempty"`
	AgentGuidance        AgentGuidance      `yaml:"agent_guidance,omitempty" json:"agent_guidance,omitempty"`
	GoForjVersion        string             `yaml:"goforj_version" json:"goforj_version"`
	agentGuidanceSet     bool
	legacyQueueDriverSet bool
	legacyQueueDriver    string
	// ModuleReplaces applies optional local go.mod replace directives before dependency sync.
	ModuleReplaces map[string]string `yaml:"module_replaces,omitempty" json:"module_replaces,omitempty"`
	// Extra preserves render settings introduced by newer GoForj versions during config migration.
	Extra map[string]any `yaml:",inline" json:"-"`
}

// HasLegacyQueueDriver reports whether the obsolete render key was present, including an explicitly empty value.
func (c RenderConfig) HasLegacyQueueDriver() bool {
	return c.legacyQueueDriverSet
}

// LegacyQueueDriver returns the obsolete value solely for one-way environment migration.
func (c RenderConfig) LegacyQueueDriver() string {
	return c.legacyQueueDriver
}

// UnmarshalYAML accepts obsolete render fields long enough to migrate their behavior without persisting them again.
func (c *RenderConfig) UnmarshalYAML(value *yaml.Node) error {
	type renderConfigFields RenderConfig
	var fields renderConfigFields
	if err := value.Decode(&fields); err != nil {
		return fmt.Errorf("decode render config: %w", err)
	}
	*c = RenderConfig(fields)
	c.agentGuidanceSet = yamlMappingValue(value, "agent_guidance") != nil
	if c.agentGuidanceSet && !c.AgentGuidance.Valid() {
		return fmt.Errorf("decode render config: unsupported agent_guidance %q; expected %q or %q", c.AgentGuidance, AgentGuidanceBaseline, AgentGuidanceNone)
	}
	delete(c.Extra, "component_contract")
	delete(c.Extra, "queue_driver")
	if len(c.Extra) == 0 {
		c.Extra = nil
	}
	for index := 0; index+1 < len(value.Content); index += 2 {
		if value.Content[index].Value == "queue_driver" {
			c.legacyQueueDriverSet = true
			if err := value.Content[index+1].Decode(&c.legacyQueueDriver); err != nil {
				return fmt.Errorf("decode legacy queue driver: %w", err)
			}
			break
		}
	}
	return nil
}

// HasAgentGuidance reports whether a Project explicitly persisted the guidance contract.
func (c RenderConfig) HasAgentGuidance() bool {
	return c.agentGuidanceSet || c.AgentGuidance.Valid()
}

// AppConfig records optional per-app participation in project-level capabilities.
type AppConfig struct {
	Components        Components         `yaml:"components" json:"components"`
	StarterKit        StarterKit         `yaml:"starter_kit" json:"starter_kit"`
	StarterKitOptions *StarterKitOptions `yaml:"starter_kit_options,omitempty" json:"starter_kit_options,omitempty"`
	HelpFormat        HelpFormat         `yaml:"help_format,omitempty" json:"help_format,omitempty"`
	// Extra preserves App settings introduced by newer GoForj versions during config migration.
	Extra map[string]any `yaml:",inline" json:"-"`
}

// ProjectConfig represents the configuration for a project.
type ProjectConfig struct {
	ProjectName  string               `yaml:"project_name" json:"project_name"`
	GoModuleName string               `yaml:"module_name" json:"module_name"`
	UpdatedAt    string               `yaml:"updated_at" json:"updated_at"`
	Dev          DevConfig            `yaml:"dev" json:"dev"`
	Render       RenderConfig         `yaml:"render" json:"render"`
	Apps         map[string]AppConfig `yaml:"apps,omitempty" json:"apps,omitempty"`
	// Extra preserves project settings introduced by newer GoForj versions during config migration.
	Extra map[string]any `yaml:",inline" json:"-"`

	needsComponentMigration bool

	// temporary
	AppKey           string `yaml:"-" json:"-"`
	AppDiagToken     string `yaml:"-" json:"-"`
	LighthouseSecret string `yaml:"-" json:"-"`
	JWTSecretKey     string `yaml:"-" json:"-"`
}

// Config is the preferred name for project configuration.
type Config = ProjectConfig

// Components represents the components of the project.
type Components struct {
	CLI              bool `yaml:"cli" json:"cli"`
	DemoApp          bool `yaml:"demo_app" json:"demo_app"`
	Mail             bool `yaml:"mail" json:"mail"`
	Auth             bool `yaml:"auth" json:"auth"`
	OAuth            bool `yaml:"oauth" json:"oauth"`
	WebAPI           bool `yaml:"web_api" json:"web_api"`
	WebUI            bool `yaml:"web_ui" json:"web_ui"`
	Metrics          bool `yaml:"metrics" json:"metrics"`
	Observability    bool `yaml:"observability" json:"observability"`
	Grafana          bool `yaml:"grafana" json:"grafana"`
	Docker           bool `yaml:"docker" json:"docker"`
	DatabaseMySQL    bool `yaml:"database_mysql" json:"database_mysql"`
	DatabasePostgres bool `yaml:"database_postgres" json:"database_postgres"`
	DatabaseSQLite   bool `yaml:"database_sqlite" json:"database_sqlite"`
	Scheduler        bool `yaml:"scheduler" json:"scheduler"`
	Cache            bool `yaml:"cache" json:"cache"`
	Events           bool `yaml:"events" json:"events"`
	Storage          bool `yaml:"storage" json:"storage"`
	Jobs             bool `yaml:"jobs" json:"jobs"`
}

// Enabled reports whether a component is enabled.
func (c Components) Enabled(key ComponentKey) bool {
	switch key {
	case ComponentCLI:
		return c.CLI
	case ComponentDemoApp:
		return c.DemoApp
	case ComponentMail:
		return c.Mail
	case ComponentAuth:
		return c.Auth
	case ComponentOAuth:
		return c.OAuth
	case ComponentWebAPI:
		return c.WebAPI
	case ComponentWebUI:
		return c.WebUI
	case ComponentMetrics:
		return c.Metrics
	case ComponentObservability:
		return c.Observability
	case ComponentGrafana:
		return c.Grafana
	case ComponentDocker:
		return c.Docker
	case ComponentDatabaseMySQL:
		return c.DatabaseMySQL
	case ComponentDatabasePostgres:
		return c.DatabasePostgres
	case ComponentDatabaseSQLite:
		return c.DatabaseSQLite
	case ComponentScheduler:
		return c.Scheduler
	case ComponentCache:
		return c.Cache
	case ComponentEvents:
		return c.Events
	case ComponentStorage:
		return c.Storage
	case ComponentJobs:
		return c.Jobs
	default:
		return false
	}
}

// SetEnabled toggles a component by catalog key.
func (c *Components) SetEnabled(key ComponentKey, enabled bool) {
	switch key {
	case ComponentCLI:
		c.CLI = enabled
	case ComponentDemoApp:
		c.DemoApp = enabled
	case ComponentMail:
		c.Mail = enabled
	case ComponentAuth:
		c.Auth = enabled
	case ComponentOAuth:
		c.OAuth = enabled
	case ComponentWebAPI:
		c.WebAPI = enabled
	case ComponentWebUI:
		c.WebUI = enabled
	case ComponentMetrics:
		c.Metrics = enabled
	case ComponentObservability:
		c.Observability = enabled
	case ComponentGrafana:
		c.Grafana = enabled
	case ComponentDocker:
		c.Docker = enabled
	case ComponentDatabaseMySQL:
		c.DatabaseMySQL = enabled
	case ComponentDatabasePostgres:
		c.DatabasePostgres = enabled
	case ComponentDatabaseSQLite:
		c.DatabaseSQLite = enabled
	case ComponentScheduler:
		c.Scheduler = enabled
	case ComponentCache:
		c.Cache = enabled
	case ComponentEvents:
		c.Events = enabled
	case ComponentStorage:
		c.Storage = enabled
	case ComponentJobs:
		c.Jobs = enabled
	}
}

// ResolveDependencies applies dependency rules in-place without mutating the original config source.
func (c *Components) ResolveDependencies() {
	if c.DemoApp {
		c.Cache = true
		c.Events = true
		c.Storage = true
		c.Jobs = true
	}
	changed := true
	for changed {
		changed = false
		for _, definition := range ComponentCatalog() {
			if !c.Enabled(definition.Key) {
				continue
			}
			for _, required := range definition.Requires {
				if c.Enabled(required) {
					continue
				}
				c.SetEnabled(required, true)
				changed = true
			}
		}
	}
}

// WithResolvedDependencies returns a copy with dependency rules applied.
func (c Components) WithResolvedDependencies() Components {
	resolved := c
	resolved.ResolveDependencies()
	return resolved
}

// HasDatabase reports whether any database component is enabled.
func (c Components) HasDatabase() bool {
	return c.DatabaseMySQL || c.DatabasePostgres || c.DatabaseSQLite
}

// HasRuntime reports whether the app can host at least one runtime through its run command.
func (c Components) HasRuntime() bool {
	return c.WebAPI || c.WebUI || c.Scheduler || c.Jobs
}

// ValidateRenderContract reports invalid component combinations that cannot be rendered coherently.
func (c Components) ValidateRenderContract() error {
	c = c.WithResolvedDependencies()
	if c.Auth && !c.HasDatabase() {
		return fmt.Errorf("auth component requires a database")
	}
	if c.OAuth && !c.HasDatabase() {
		return fmt.Errorf("oauth component requires a database")
	}
	return nil
}

// DatabaseDriver returns the selected database driver name.
func (c Components) DatabaseDriver() string {
	if c.DatabasePostgres {
		return "postgres"
	}
	if c.DatabaseMySQL {
		return "mysql"
	}
	if c.DatabaseSQLite {
		return "sqlite"
	}
	return ""
}

// DatabaseServiceName returns the docker service name for the selected database.
func (c Components) DatabaseServiceName() string {
	if c.DatabasePostgres {
		return "postgres"
	}
	if c.DatabaseMySQL {
		return "mysql"
	}
	if c.DatabaseSQLite {
		return "sqlite"
	}
	return ""
}

// LoadProjectConfig loads the project configuration from the .goforj.yml file.
func LoadProjectConfig() (*Config, error) {
	return LoadProjectConfigAt(".")
}

// LoadProjectConfigAt loads project configuration without requiring callers to change process working directory.
func LoadProjectConfigAt(root string) (*Config, error) {
	config := &ProjectConfig{}
	configFile := filepath.Join(root, ".goforj.yml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(config); err != nil {
		return nil, err
	}
	if len(config.Dev.WirePaths) == 0 {
		config.Dev.WirePaths = []string{DefaultApp().WireDir}
	}

	return config, nil
}

// IsReservedAppName reports whether name is owned by the app composition layout.
func IsReservedAppName(name string) bool {
	return name == "wire"
}

// IsNativeFrameworkCommandName reports whether name is owned by the framework CLI.
func IsNativeFrameworkCommandName(name string) bool {
	switch strings.TrimSpace(name) {
	case "stack",
		"build",
		"backup:create",
		"backup:list",
		"backup:plan",
		"backup:prune",
		"backup:restore",
		"backup:status",
		"backup:verify",
		"dev",
		"down",
		"generate",
		"help",
		"new",
		"render",
		"run",
		"version",
		"x":
		return true
	default:
		return false
	}
}
