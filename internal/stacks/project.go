package stacks

import (
	"fmt"
	"strings"

	"github.com/goforj/goforj/internal/envfile"
	"github.com/goforj/goforj/internal/generate"
	"github.com/goforj/goforj/project"
	"github.com/goforj/str/v2"
)

// Resource identifies an editable root or named resource within an App.
type Resource struct {
	Key, SupportedKey, Label string
	Definition               project.ResourceDefinition
}

// resources discovers enabled roots and concrete named overrides from project-owned configuration.
func resources(root string, config *project.Config, values map[string]string) []Resource {
	byKey := map[string]Resource{}
	components := project.ProjectComponents(config)
	names := generate.ResourceNames(root, values)
	for _, definition := range project.ResourceCatalog() {
		if !definition.AppliesTo(components) {
			continue
		}
		key := definition.EnvironmentKey("DRIVER")
		byKey[key] = Resource{Key: key, SupportedKey: definition.EnvironmentKey("SUPPORTED_DRIVERS"), Label: definition.Label, Definition: definition}
		for _, name := range names[definition.Key] {
			if name == "default" || name == "root" {
				continue
			}
			key := definition.EnvironmentPrefix + "_" + str.Of(name).Snake().ToUpper().String() + "_DRIVER"
			byKey[key] = Resource{Key: key, SupportedKey: definition.EnvironmentKey("SUPPORTED_DRIVERS"), Label: key, Definition: definition}
		}
		for app, appConfig := range config.Apps {
			if app == project.DefaultAppName || !definition.AppliesTo(appConfig.Components.WithResolvedDependencies()) {
				continue
			}
			prefix := project.AppEnvironmentPrefix(app) + "_"
			key := prefix + definition.EnvironmentKey("DRIVER")
			byKey[key] = Resource{Key: key, SupportedKey: definition.EnvironmentKey("SUPPORTED_DRIVERS"), Label: key, Definition: definition}
			for _, name := range names[definition.Key] {
				if name == "default" || name == "root" {
					continue
				}
				key := prefix + definition.EnvironmentPrefix + "_" + str.Of(name).Snake().ToUpper().String() + "_DRIVER"
				byKey[key] = Resource{Key: key, SupportedKey: definition.EnvironmentKey("SUPPORTED_DRIVERS"), Label: key, Definition: definition}
			}
		}
		for _, named := range definition.NamedResources {
			if named.RequiredComponent != "" && !components.Enabled(named.RequiredComponent) {
				continue
			}
			byKey[named.EnvironmentKey] = Resource{Key: named.EnvironmentKey, SupportedKey: definition.EnvironmentKey("SUPPORTED_DRIVERS"), Label: named.Label, Definition: definition}
		}
	}
	for _, key := range keys(values) {
		if !strings.HasSuffix(key, "_DRIVER") {
			continue
		}
		for _, definition := range project.ResourceCatalog() {
			base := resourceKey(config, key)
			if !definition.AppliesTo(components) || !strings.HasPrefix(base, definition.EnvironmentPrefix+"_") {
				continue
			}
			byKey[key] = Resource{Key: key, SupportedKey: definition.EnvironmentKey("SUPPORTED_DRIVERS"), Label: key, Definition: definition}
		}
	}
	out := make([]Resource, 0, len(byKey))
	for _, key := range keys(byKey) {
		out = append(out, byKey[key])
	}
	return out
}

// resourceKey removes only configured App prefixes, preventing unrelated application keys from being claimed.
func resourceKey(config *project.Config, key string) string {
	for name := range config.Apps {
		if name == project.DefaultAppName {
			continue
		}
		prefix := project.AppEnvironmentPrefix(name) + "_"
		if strings.HasPrefix(key, prefix) {
			candidate := strings.TrimPrefix(key, prefix)
			for _, resource := range []string{"DB_", "CACHE_", "QUEUE_", "EVENTS_", "STORAGE_", "MAIL_", "REDIS_"} {
				if strings.HasPrefix(candidate, resource) {
					return candidate
				}
			}
		}
	}
	return key
}

// managedKey limits stack ownership to resource settings and Compose selection.
func managedKey(config *project.Config, key string) bool {
	if key == "COMPOSE_PROFILES" {
		return true
	}
	base := resourceKey(config, key)
	for _, prefix := range []string{"DB_", "CACHE_", "QUEUE_", "EVENTS_", "STORAGE_", "MAIL_", "REDIS_"} {
		if strings.HasPrefix(base, prefix) {
			return true
		}
	}
	return false
}

// managedValues excludes application identity and unrelated secrets from stack snapshots.
func managedValues(config *project.Config, values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if managedKey(config, key) {
			out[key] = value
		}
	}
	return out
}

// SetDriver selects a driver and retains the compiled support needed to switch back later.
func SetDriver(values map[string]string, resource Resource, driver string) error {
	driver = project.CanonicalResourceDriver(resource.Definition.Key, driver)
	if _, ok := resource.Definition.Driver(driver); !ok {
		return fmt.Errorf("%s has no driver %q", resource.Key, driver)
	}
	old := values[resource.Key]
	values[resource.Key] = driver
	supported := strings.Split(values[resource.SupportedKey], ",")
	for _, name := range []string{old, driver} {
		if name == "" {
			continue
		}
		found := false
		for _, existing := range supported {
			if strings.TrimSpace(existing) == name {
				found = true
			}
		}
		if !found {
			supported = append(supported, name)
		}
	}
	var nonempty []string
	for _, name := range supported {
		if name = strings.TrimSpace(name); name != "" {
			nonempty = append(nonempty, name)
		}
	}
	values[resource.SupportedKey] = strings.Join(nonempty, ",")
	if resource.Definition.Key == project.ResourceDatabase && driver == "sqlite" {
		prefix := strings.TrimSuffix(resource.Key, "DRIVER")
		path := "./_data/stacks/portable/" + strings.ToLower(strings.TrimSuffix(prefix, "_")) + ".db"
		values[prefix+"DSN"] = ""
		values[prefix+"DATABASE"] = path
		values[prefix+"SQLITE_DATABASE"] = path
	}
	return nil
}

// Portable suggests local providers for every discovered resource without enabling disabled components.
func (s *Session) Portable() (map[string]string, error) {
	values := clone(s.Current)
	for _, resource := range s.Resources {
		if err := SetDriver(values, resource, resource.Definition.DefaultDriver); err != nil {
			return nil, err
		}
	}
	values["COMPOSE_PROFILES"] = ""
	return values, nil
}

// Validate rejects unknown drivers and settings outside the project resource boundary.
func (s *Session) Validate(values map[string]string) error {
	for key := range values {
		if !envfile.IsValidKey(key) || !managedKey(s.config, key) {
			return fmt.Errorf("%s is not a stack resource setting", key)
		}
	}
	for key, value := range values {
		base := resourceKey(s.config, key)
		if !strings.HasSuffix(base, "_DRIVER") && !strings.HasSuffix(base, "_SUPPORTED_DRIVERS") {
			continue
		}
		for _, definition := range project.ResourceCatalog() {
			if !strings.HasPrefix(base, definition.EnvironmentPrefix+"_") {
				continue
			}
			for _, driver := range strings.Split(value, ",") {
				if driver == "" {
					continue
				}
				if _, ok := definition.Driver(strings.TrimSpace(driver)); !ok {
					return fmt.Errorf("%s selects unsupported driver", key)
				}
			}
		}
	}
	for _, resource := range resources(s.Root, s.config, values) {
		driver := values[resource.Key]
		if driver == "" {
			continue
		}
		if _, ok := resource.Definition.Driver(driver); !ok {
			return fmt.Errorf("%s selects unsupported driver %q", resource.Key, driver)
		}
		supported := values[resource.SupportedKey]
		if supported == "" {
			continue
		}
		found := false
		for _, name := range strings.Split(supported, ",") {
			name = strings.TrimSpace(name)
			if _, ok := resource.Definition.Driver(name); !ok {
				return fmt.Errorf("%s contains unsupported driver %q", resource.SupportedKey, name)
			}
			if name == driver {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s must include %s for %s", resource.SupportedKey, driver, resource.Key)
		}
	}
	return nil
}

// shareable copies only explicit provider selections; connection settings remain private by default.
func shareable(values map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		if strings.HasSuffix(key, "_DRIVER") || strings.HasSuffix(key, "_SUPPORTED_DRIVERS") || key == "COMPOSE_PROFILES" {
			result[key] = value
		}
	}
	for key, driver := range values {
		if driver != "sqlite" || !strings.HasSuffix(key, "_DRIVER") {
			continue
		}
		prefix := strings.TrimSuffix(key, "DRIVER")
		if !strings.Contains(prefix, "DB_") {
			continue
		}
		for _, suffix := range []string{"DATABASE", "SQLITE_DATABASE"} {
			if value, ok := values[prefix+suffix]; ok {
				result[prefix+suffix] = value
			}
		}
		if value, ok := values[prefix+"DSN"]; ok && value == "" {
			result[prefix+"DSN"] = ""
		}
	}
	return result
}
