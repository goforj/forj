package forj

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/goforj/str/v2"

	"github.com/goforj/goforj/internal/devservices"
	"github.com/goforj/goforj/internal/envfile"
	"github.com/goforj/goforj/internal/projectlayout"
	"github.com/goforj/goforj/internal/resourceenv"
	"github.com/goforj/goforj/project"
)

// normalizeQueueDriver validates legacy queue values against the generator's canonical inventory.
func normalizeQueueDriver(value string) string {
	normalized := str.Of(value).Trim().ToLower().String()
	definition, ok := project.ResourceDefinitionByKey(project.ResourceQueue)
	if !ok {
		return ""
	}
	if _, ok := definition.Driver(normalized); !ok {
		return ""
	}
	return normalized
}

// resourceRenderValues is the template-facing projection of a validated transient resource plan.
type resourceRenderValues struct {
	DatabaseDriver                 string
	DatabaseSupportedDrivers       string
	DatabaseAvailableDrivers       string
	DatabaseMySQL                  bool
	DatabasePostgres               bool
	DatabaseSQLite                 bool
	DatabaseExternal               bool
	CacheDriver                    string
	CacheSupportedDrivers          string
	CacheAvailableDrivers          string
	QueueDriver                    string
	QueueSupportedDrivers          string
	QueueAvailableDrivers          string
	EventsDriver                   string
	EventsSupportedDrivers         string
	EventsAvailableDrivers         string
	StorageDriver                  string
	StorageSupportedDrivers        string
	StorageAvailableDrivers        string
	StoragePublicDriver            string
	StorageFaviconsDriver          string
	MailDriver                     string
	MailSupportedDrivers           string
	MailAvailableDrivers           string
	CacheSettingsDriver            string
	CacheSessionsDriver            string
	RedisActive                    bool
	RedisLocal                     bool
	RedisLocalRequestedUnused      bool
	RedisExternal                  bool
	CacheMemcachedLocal            bool
	CacheDynamoDBLocal             bool
	CacheNATSLocal                 bool
	QueueNATSLocal                 bool
	QueueSQSLocal                  bool
	QueueRabbitMQLocal             bool
	EventsNATSLocal                bool
	EventsKafkaLocal               bool
	EventsGCPPubSubLocal           bool
	DevelopmentServiceProfileLines []string
	ComposeProfiles                string
}

// prepareResourceRenderState resolves one validated resource snapshot for every renderer entry point.
func (p *ProjectRenderer) prepareResourceRenderState(input ComponentRenderInput, projectComponents project.Components) error {
	p.resources = resourceRenderState{
		serviceConsumers: cloneEffectiveResourceConsumers(input.serviceConsumers),
	}
	if len(input.resourcePlan.Selections) > 0 {
		plan, err := normalizeExplicitResourcePlan(input.resourcePlan, projectComponents)
		if err != nil {
			return err
		}
		p.resources.plan = plan
		p.resources.explicitPlan = true
		p.resources.serviceIntent = input.localServiceIntent
	} else {
		// Legacy YAML remains a one-way migration source until environment initialization succeeds.
		queueDriver := legacyQueueDriverDefault(p.config.Render.LegacyQueueDriver())
		plan, err := compatibilityResourcePlan(projectComponents, queueDriver)
		if err != nil {
			return err
		}
		p.resources.plan = plan
	}

	var err error
	p.resources.plan, err = withProjectDatabaseCapabilities(p.resources.plan, p.config.Render.Components, projectComponents)
	if err != nil {
		return err
	}
	if input.renderAll {
		if err := p.prepareResourceEnvironment(); err != nil {
			return err
		}
	}
	if _, err := project.ResolveServicePlanWithConsumers(
		p.resources.plan,
		p.projectRenderComponents(),
		p.resources.serviceIntent,
		p.resources.serviceConsumers,
	); err != nil {
		return fmt.Errorf("resolve effective App services: %w", err)
	}
	return nil
}

// publishPendingResourceEnvironment commits validated owner changes before another renderer step can update the same file.
func (p *ProjectRenderer) publishPendingResourceEnvironment() error {
	if !p.resources.pendingEnvironmentWrite {
		return nil
	}
	err := p.writeEnvironmentFile(p.workspace.path(".env"), p.resources.pendingEnvironment, 0o600)
	if err := p.workspace.logicalError(err); err != nil {
		return fmt.Errorf("write resource environment: %w", err)
	}
	p.resources.pendingEnvironment = nil
	p.resources.pendingEnvironmentWrite = false
	return nil
}

// prepareResourceEnvironment reconciles an existing owner file without publishing changes before validation completes.
func (p *ProjectRenderer) prepareResourceEnvironment() error {
	const ownerPath = ".env"
	const examplePath = ".env.example"
	path := ownerPath
	ownerExists := true
	if err := p.workspace.rejectEnvironmentSpecialFile(path); err != nil {
		return err
	}
	source, err := p.workspace.readFile(path)
	if os.IsNotExist(err) {
		path = examplePath
		ownerExists = false
		if err := p.workspace.rejectEnvironmentSpecialFile(path); err != nil {
			return err
		}
		source, err = p.workspace.readFile(path)
		if os.IsNotExist(err) {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("read resource environment: %w", err)
	}
	if !ownerExists && p.resources.explicitPlan {
		// A committed fallback reproduces an existing build, but it cannot replace a new wizard decision.
		return nil
	}
	if ownerExists {
		var removedDisabledAppCacheDefaults bool
		source, removedDisabledAppCacheDefaults = removeDisabledAppCacheDriverDefaults(
			source,
			p.config,
			projectlayout.RuntimeApps(p.workspace.discoveryRoot(), p.config),
		)
		p.resources.pendingEnvironmentWrite = removedDisabledAppCacheDefaults
	}
	_, profilesSet := envfile.Lookup(strings.Split(string(source), "\n"), "COMPOSE_PROFILES")
	legacyLocalRedis := !profilesSet && composeRedisServiceWithoutProfile(p.workspace.path("docker-compose.yml"))
	if ownerExists {
		for _, migration := range []struct {
			service string
			profile string
		}{
			{service: "mysql", profile: "mysql"},
			{service: "postgres", profile: "postgres"},
			{service: "mailpit", profile: "mailpit"},
			{service: "victoriametrics", profile: "victoriametrics"},
			{service: "grafana", profile: "grafana"},
		} {
			if !composeServiceWithoutProfile(p.workspace.path("docker-compose.yml"), migration.service) {
				continue
			}
			var profileChanged bool
			source, profileChanged = seedExactComposeProfile(source, migration.profile)
			p.resources.pendingEnvironmentWrite = p.resources.pendingEnvironmentWrite || profileChanged
		}
	}
	if legacyLocalRedis {
		p.resources.serviceIntent = p.resources.serviceIntent.WithMode(project.ServiceRedis, project.LocalServiceModeLocal)
		if ownerExists {
			var profileChanged bool
			source, profileChanged = seedExactComposeProfile(source, "redis")
			p.resources.pendingEnvironmentWrite = p.resources.pendingEnvironmentWrite || profileChanged
		}
	} else {
		p.resources.serviceIntent = resourceenv.ResolveServiceIntent(source, p.resources.serviceIntent)
	}
	projectComponents := p.projectRenderComponents()
	if !projectComponents.Cache {
		var removedCacheEnvironment bool
		source, removedCacheEnvironment = resourceenv.RemoveGeneratedAssignments(
			source,
			projectComponents,
			projectlayout.RuntimeApps(p.workspace.discoveryRoot(), p.config),
		)
		p.resources.pendingEnvironmentWrite = p.resources.pendingEnvironmentWrite || removedCacheEnvironment
	}
	reconciled, err := resourceenv.Reconcile(
		source,
		p.resources.plan,
		projectComponents,
		p.resources.explicitPlan,
	)
	if err != nil {
		return err
	}
	p.resources.plan = reconciled.EffectivePlan
	consumers, err := resourceenv.ResolveConsumers(reconciled.Source, reconciled.EffectivePlan, projectComponents, p.config)
	if err != nil {
		return fmt.Errorf("discover effective resource consumers: %w", err)
	}
	p.resources.serviceConsumers = cloneEffectiveResourceConsumers(consumers)
	if ownerExists {
		p.resources.pendingEnvironment = reconciled.Source
		p.resources.pendingEnvironmentWrite = p.resources.pendingEnvironmentWrite || reconciled.Changed
	}
	return nil
}

// compatibilityResourcePlan preserves pre-wizard render defaults when no explicit transient plan is supplied.
func compatibilityResourcePlan(components project.Components, legacyQueueDriver string) (project.ResourcePlan, error) {
	plan, err := project.DefaultResourcePlan(components)
	if err != nil {
		return project.ResourcePlan{}, err
	}
	// Events and Queue retain their portable pairs so a first enablement can run locally and adopt Redis without rebuilding.
	for _, key := range []project.ResourceKey{
		project.ResourceDatabase,
		project.ResourceCache,
		project.ResourceStorage,
		project.ResourceMail,
	} {
		selection, ok := plan.Selection(key)
		if !ok {
			continue
		}
		if components.DemoApp && key == project.ResourceDatabase {
			continue
		}
		selection.Supported = []string{selection.Active}
		plan = plan.WithSelection(key, selection)
	}
	if components.Jobs {
		queue, _ := plan.Selection(project.ResourceQueue)
		queue.Active = legacyQueueDriverDefault(legacyQueueDriver)
		if queue.Active != "workerpool" {
			queue.Supported = []string{queue.Active}
		}
		plan = plan.WithSelection(project.ResourceQueue, queue)
	}
	return plan.Normalized(components)
}

// normalizeExplicitResourcePlan validates a wizard or command plan before it can initialize files.
func normalizeExplicitResourcePlan(plan project.ResourcePlan, components project.Components) (project.ResourcePlan, error) {
	normalized, err := plan.Normalized(components)
	if err != nil {
		return project.ResourcePlan{}, fmt.Errorf("resource plan: %w", err)
	}
	return normalized, nil
}

// withProjectDatabaseCapabilities keeps the default App driver active while building every engine required by the project envelope.
func withProjectDatabaseCapabilities(plan project.ResourcePlan, defaultComponents project.Components, projectComponents project.Components) (project.ResourcePlan, error) {
	if !projectComponents.HasDatabase() {
		return plan, nil
	}
	selection, ok := plan.Selection(project.ResourceDatabase)
	if !ok {
		return project.ResourcePlan{}, fmt.Errorf("project database capabilities require a database resource selection")
	}
	if projectComponents.DemoApp {
		// Demo's migration contract keeps MySQL active while SQLite remains compiled as its portable fallback.
		selection.Active = "mysql"
	} else if driver := defaultComponents.DatabaseDriver(); driver != "" {
		selection.Active = driver
	}
	for _, candidate := range []struct {
		enabled bool
		driver  string
	}{
		{enabled: projectComponents.DatabaseSQLite, driver: "sqlite"},
		{enabled: projectComponents.DatabaseMySQL, driver: "mysql"},
		{enabled: projectComponents.DatabasePostgres, driver: "postgres"},
	} {
		if candidate.enabled && !stringSliceContainsFold(selection.Supported, candidate.driver) {
			selection.Supported = append(selection.Supported, candidate.driver)
		}
	}
	return plan.WithSelection(project.ResourceDatabase, selection).Normalized(projectComponents)
}

// seedExactComposeProfile appends one profile token without replacing unrelated owner intent.
func seedExactComposeProfile(source []byte, profile string) ([]byte, bool) {
	lines := strings.Split(string(source), "\n")
	profiles, profilesSet := envfile.Lookup(lines, "COMPOSE_PROFILES")
	if profilesSet && exactCSVToken(profiles, profile) {
		return source, false
	}
	profiles = appendCSVToken(profiles, profile)
	lines = envfile.SetFinal(lines, "COMPOSE_PROFILES", profiles)
	updated := strings.Join(lines, "\n")
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	return []byte(updated), true
}

// composeRedisServiceWithoutProfile detects the pre-profile generated Redis service during one-way migration.
func composeRedisServiceWithoutProfile(path string) bool {
	return composeServiceWithoutProfile(path, "redis")
}

// composeServiceWithoutProfile detects a generated service that predates its catalog lifecycle profile.
func composeServiceWithoutProfile(path string, serviceName string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	inServices := false
	inService := false
	for _, line := range lines {
		if line == "services:" {
			inServices = true
			continue
		}
		if !inServices {
			continue
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(strings.TrimSpace(line), ":") {
			if inService {
				return true
			}
			inService = strings.TrimSpace(line) == strings.TrimSpace(serviceName)+":"
			continue
		}
		if inService && strings.HasPrefix(strings.TrimSpace(line), "profiles:") {
			return false
		}
	}
	return inService
}

// resourceRenderValuesForPlanWithConsumers includes environment-resolved named and App-scoped service activity.
func resourceRenderValuesForPlanWithConsumers(plan project.ResourcePlan, components project.Components, intent project.LocalServiceIntent, consumers []project.EffectiveResourceConsumer) (resourceRenderValues, error) {
	servicePlan, err := project.ResolveServicePlanWithConsumers(plan, components, intent, consumers)
	if err != nil {
		return resourceRenderValues{}, fmt.Errorf("resolve resource services: %w", err)
	}
	values := resourceRenderValues{
		DevelopmentServiceProfileLines: developmentServiceProfileLines(),
	}
	availableDrivers := make(map[project.ResourceKey]string)
	for _, definition := range project.ResourceCatalog() {
		drivers := append([]project.DriverDefinition(nil), definition.Drivers...)
		sort.SliceStable(drivers, func(left, right int) bool { return drivers[left].Order < drivers[right].Order })
		names := make([]string, 0, len(drivers))
		for _, driver := range drivers {
			names = append(names, driver.Name)
		}
		availableDrivers[definition.Key] = strings.Join(names, ",")
	}
	set := func(key project.ResourceKey, active *string, supported *string, available *string) {
		selection, ok := plan.Selection(key)
		if !ok {
			return
		}
		*active = selection.Active
		*supported = strings.Join(selection.Supported, ",")
		*available = availableDrivers[key]
	}
	set(project.ResourceDatabase, &values.DatabaseDriver, &values.DatabaseSupportedDrivers, &values.DatabaseAvailableDrivers)
	set(project.ResourceCache, &values.CacheDriver, &values.CacheSupportedDrivers, &values.CacheAvailableDrivers)
	set(project.ResourceQueue, &values.QueueDriver, &values.QueueSupportedDrivers, &values.QueueAvailableDrivers)
	set(project.ResourceEvents, &values.EventsDriver, &values.EventsSupportedDrivers, &values.EventsAvailableDrivers)
	set(project.ResourceStorage, &values.StorageDriver, &values.StorageSupportedDrivers, &values.StorageAvailableDrivers)
	set(project.ResourceMail, &values.MailDriver, &values.MailSupportedDrivers, &values.MailAvailableDrivers)
	values.DatabaseMySQL = values.DatabaseDriver == "mysql"
	values.DatabasePostgres = values.DatabaseDriver == "postgres"
	values.DatabaseSQLite = values.DatabaseDriver == "sqlite"
	values.DatabaseExternal = (values.DatabaseMySQL || values.DatabasePostgres) && !components.Docker
	for _, named := range plan.GeneratedNamedSelections(components) {
		switch named.EnvironmentKey {
		case "CACHE_SETTINGS_DRIVER":
			values.CacheSettingsDriver = named.Active
		case "CACHE_SESSIONS_DRIVER":
			values.CacheSessionsDriver = named.Active
		case "STORAGE_PUBLIC_DRIVER":
			values.StoragePublicDriver = named.Active
		case "STORAGE_FAVICONS_DRIVER":
			values.StorageFaviconsDriver = named.Active
		}
	}
	for _, requirement := range servicePlan.RequirementsFor(project.ServiceRedis) {
		switch requirement.State {
		case project.ServiceStateActiveLocal:
			values.RedisLocal = true
		case project.ServiceStateLocalRequestedUnused:
			values.RedisLocal = true
			values.RedisLocalRequestedUnused = true
		case project.ServiceStateExternalRequired:
			if requirement.EndpointAffinity == "" {
				values.RedisExternal = true
			}
		}
		if len(requirement.ActiveConsumers) > 0 {
			values.RedisActive = true
		}
	}
	values.CacheMemcachedLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceCacheMemcached)
	values.CacheDynamoDBLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceCacheDynamoDB)
	values.CacheNATSLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceCacheNATS)
	values.QueueNATSLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceQueueNATS)
	values.QueueSQSLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceQueueSQS)
	values.QueueRabbitMQLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceQueueRabbitMQ)
	values.EventsNATSLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceEventsNATS)
	values.EventsKafkaLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceEventsKafka)
	values.EventsGCPPubSubLocal = servicePlanHasActiveLocalRequirement(servicePlan, project.ServiceEventsGCPPubSub)
	values.ComposeProfiles = activeDevelopmentServiceProfiles(servicePlan, components)
	return values, nil
}

// servicePlanHasActiveLocalRequirement reports whether any endpoint affinity for a capability uses its catalog provider.
func servicePlanHasActiveLocalRequirement(servicePlan project.ServicePlan, service project.ServiceKey) bool {
	for _, requirement := range servicePlan.RequirementsFor(service) {
		if requirement.State == project.ServiceStateActiveLocal {
			return true
		}
	}
	return false
}

// developmentServiceProfileLines wraps catalog discovery without making generated dotenv comments hard to scan.
func developmentServiceProfileLines() []string {
	const maximumLineLength = 88
	lines := []string{}
	current := ""
	for _, definition := range devservices.Catalog() {
		candidate := definition.Profile
		if current != "" {
			candidate = current + "," + candidate
		}
		if current != "" && len(candidate) > maximumLineLength {
			lines = append(lines, current)
			current = definition.Profile
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// activeDevelopmentServiceProfiles projects compatibility defaults and explicit local-provider intent into the owner environment.
func activeDevelopmentServiceProfiles(servicePlan project.ServicePlan, components project.Components) string {
	profiles := []string{}
	components = components.WithResolvedDependencies()
	for _, definition := range devservices.Catalog() {
		enabled := false
		for _, component := range definition.DefaultFor {
			if components.Enabled(component) {
				enabled = true
				break
			}
		}
		for _, provider := range definition.Providers {
			if enabled {
				break
			}
			for _, requirement := range servicePlan.RequirementsFor(provider) {
				if requirement.State == project.ServiceStateActiveLocal || requirement.State == project.ServiceStateLocalRequestedUnused {
					enabled = true
					break
				}
			}
			if enabled {
				break
			}
		}
		if enabled {
			profiles = append(profiles, definition.Profile)
		}
	}
	for _, database := range []struct {
		service project.ServiceKey
		profile string
	}{
		{service: project.ServiceMySQL, profile: "mysql"},
		{service: project.ServicePostgres, profile: "postgres"},
	} {
		if servicePlanHasActiveLocalRequirement(servicePlan, database.service) {
			profiles = append(profiles, database.profile)
		}
	}
	return strings.Join(profiles, ",")
}

// DriverEnvironmentPlaceholders renders only opt-in driver hints selected by the effective build contract.
func (v resourceRenderValues) DriverEnvironmentPlaceholders() string {
	selections := map[project.ResourceKey]project.DriverSelection{
		project.ResourceDatabase: {Active: v.DatabaseDriver, Supported: splitDriverList(v.DatabaseSupportedDrivers)},
		project.ResourceCache:    {Active: v.CacheDriver, Supported: splitDriverList(v.CacheSupportedDrivers)},
		project.ResourceQueue:    {Active: v.QueueDriver, Supported: splitDriverList(v.QueueSupportedDrivers)},
		project.ResourceEvents:   {Active: v.EventsDriver, Supported: splitDriverList(v.EventsSupportedDrivers)},
		project.ResourceStorage:  {Active: v.StorageDriver, Supported: splitDriverList(v.StorageSupportedDrivers)},
		project.ResourceMail:     {Active: v.MailDriver, Supported: splitDriverList(v.MailSupportedDrivers)},
	}
	return renderDriverEnvironmentPlaceholders(selections)
}

// renderDriverEnvironmentPlaceholders formats catalog hints in deterministic resource and driver order.
func renderDriverEnvironmentPlaceholders(selections map[project.ResourceKey]project.DriverSelection) string {
	lines := []string{}
	seenKeys := map[string]bool{}
	for _, definition := range project.ResourceCatalog() {
		selection, ok := selections[definition.Key]
		if !ok {
			continue
		}
		for _, driver := range orderedSelectedDriverDefinitions(definition, selection) {
			placeholders := uniqueDriverEnvironmentPlaceholders(driver.Environment, seenKeys)
			if len(placeholders) == 0 {
				continue
			}
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, "# "+definition.Label+" · "+driver.Label)
			for _, placeholder := range placeholders {
				if description := strings.TrimSpace(placeholder.Description); description != "" {
					lines = append(lines, "# "+description)
				}
				lines = append(lines, "# "+placeholder.Key+"="+placeholder.Example)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// orderedSelectedDriverDefinitions lets the active driver's examples own shared keys before catalog-ordered transition drivers.
func orderedSelectedDriverDefinitions(definition project.ResourceDefinition, selection project.DriverSelection) []project.DriverDefinition {
	activeName := str.Of(selection.Active).Trim().ToLower().String()
	selected := make(map[string]bool, len(selection.Supported))
	for _, name := range selection.Supported {
		name = str.Of(name).Trim().ToLower().String()
		if name != "" && name != activeName {
			selected[name] = true
		}
	}

	drivers := make([]project.DriverDefinition, 0, len(selected)+1)
	if active, ok := definition.Driver(activeName); ok {
		drivers = append(drivers, active)
	}
	supported := make([]project.DriverDefinition, 0, len(selected))
	for _, driver := range definition.Drivers {
		if selected[driver.Name] {
			supported = append(supported, driver)
		}
	}
	sort.SliceStable(supported, func(left, right int) bool {
		return supported[left].Order < supported[right].Order
	})
	return append(drivers, supported...)
}

// uniqueDriverEnvironmentPlaceholders removes shared scope keys without conflating service identities.
func uniqueDriverEnvironmentPlaceholders(placeholders []project.DriverEnvironmentPlaceholder, seen map[string]bool) []project.DriverEnvironmentPlaceholder {
	unique := make([]project.DriverEnvironmentPlaceholder, 0, len(placeholders))
	for _, placeholder := range placeholders {
		key := strings.TrimSpace(placeholder.Key)
		if key == "" || seen[key] {
			continue
		}
		placeholder.Key = key
		seen[key] = true
		unique = append(unique, placeholder)
	}
	return unique
}

// applyDatabaseRenderCapabilities promotes every built-in database without changing the environment-owned active driver.
func applyDatabaseRenderCapabilities(components *project.Components, plan project.ResourcePlan) {
	selection, ok := plan.Selection(project.ResourceDatabase)
	if !ok {
		return
	}
	for _, driver := range selection.Supported {
		switch driver {
		case "mysql":
			components.DatabaseMySQL = true
		case "postgres":
			components.DatabasePostgres = true
		case "sqlite":
			components.DatabaseSQLite = true
		}
	}
}

// splitDriverList normalizes an owner list without changing its relative order before catalog validation.
func splitDriverList(value string) []string {
	drivers := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		driver := str.Of(part).Trim().ToLower().String()
		if driver == "" || seen[driver] {
			continue
		}
		seen[driver] = true
		drivers = append(drivers, driver)
	}
	return drivers
}

// stringSliceContainsFold compares driver names before the plan normalizer applies canonical casing.
func stringSliceContainsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

// exactCSVToken reports profile membership without substring matches such as redis-debug.
func exactCSVToken(value string, want string) bool {
	for _, token := range strings.Split(value, ",") {
		if strings.TrimSpace(token) == want {
			return true
		}
	}
	return false
}

// appendCSVToken preserves unrelated profile order while adding one missing exact token.
func appendCSVToken(value string, token string) string {
	tokens := []string{}
	for _, current := range strings.Split(value, ",") {
		current = strings.TrimSpace(current)
		if current == "" || current == token {
			continue
		}
		tokens = append(tokens, current)
	}
	tokens = append(tokens, token)
	return strings.Join(tokens, ",")
}
