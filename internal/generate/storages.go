package generate

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/goforj/str/v2"
)

// storageAccessorTemplateData carries the named disk methods emitted for one project snapshot.
type storageAccessorTemplateData struct {
	Names []storageAccessorName
}

// storageAccessorName binds an environment disk name to its generated Go method.
type storageAccessorName struct {
	Method string
	Disk   string
}

// storageConfigTemplateData keeps compiled backends and named disks aligned while rendering the manager.
type storageConfigTemplateData struct {
	CompiledDrivers []string
	Drivers         []storageDriverSpec
	Names           []storageAccessorName
}

// storageDriverSpec captures the import and configuration metadata needed to emit one storage backend branch.
type storageDriverSpec struct {
	ConstName  string
	ImportPath string
	ConfigType string
	Setup      []string
	Fields     []storageConfigField
}

// storageConfigField binds a backend configuration field to its generated value expression.
type storageConfigField struct {
	Name  string
	Value string
}

var storageDriverSpecs = map[string]storageDriverSpec{
	"dropbox": {
		ConstName:  "driverDropbox",
		ImportPath: "github.com/goforj/storage/driver/dropboxstorage",
		ConfigType: "dropboxstorage.Config",
		Fields: []storageConfigField{
			{Name: "Token", Value: `scope.Get("TOKEN", "")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"ftp": {
		ConstName:  "driverFTP",
		ImportPath: "github.com/goforj/storage/driver/ftpstorage",
		ConfigType: "ftpstorage.Config",
		Fields: []storageConfigField{
			{Name: "Host", Value: `scope.Get("HOST", "")`},
			{Name: "Port", Value: `scope.GetInt("PORT", "21")`},
			{Name: "User", Value: `scope.Get("USER", "")`},
			{Name: "Password", Value: `scope.Get("PASSWORD", "")`},
			{Name: "TLS", Value: `scope.GetBool("TLS", "false")`},
			{Name: "InsecureSkipVerify", Value: `scope.GetBool("INSECURE_SKIP_VERIFY", "false")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"gcs": {
		ConstName:  "driverGCS",
		ImportPath: "github.com/goforj/storage/driver/gcsstorage",
		ConfigType: "gcsstorage.Config",
		Fields: []storageConfigField{
			{Name: "Bucket", Value: `scope.Get("BUCKET", "")`},
			{Name: "CredentialsJSON", Value: `scope.Get("CREDENTIALS_JSON", "")`},
			{Name: "Endpoint", Value: `scope.Get("ENDPOINT", "")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"local": {
		ConstName:  "driverLocal",
		ImportPath: "github.com/goforj/storage/driver/localstorage",
		ConfigType: "localstorage.Config",
		Setup: []string{
			`root := scope.Get("ROOT", localRoot)`,
			`if err := os.MkdirAll(root, 0o755); err != nil {`,
			`	return nil, err`,
			`}`,
		},
		Fields: []storageConfigField{
			{Name: "Root", Value: `root`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"memory": {
		ConstName:  "driverMemory",
		ImportPath: "github.com/goforj/storage/driver/memorystorage",
		ConfigType: "memorystorage.Config",
		Fields: []storageConfigField{
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"rclone": {
		ConstName:  "driverRclone",
		ImportPath: "github.com/goforj/storage/driver/rclonestorage",
		ConfigType: "rclonestorage.Config",
		Fields: []storageConfigField{
			{Name: "Remote", Value: `scope.Get("REMOTE", "")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
			{Name: "RcloneConfigPath", Value: `scope.Get("RCLONE_CONFIG_PATH", "")`},
			{Name: "RcloneConfigData", Value: `scope.Get("RCLONE_CONFIG_DATA", "")`},
		},
	},
	"redis": {
		ConstName:  "driverRedis",
		ImportPath: "github.com/goforj/storage/driver/redisstorage",
		ConfigType: "redisstorage.Config",
		Setup: []string{
			`addr := str.Of(scope.Get("ADDR", "")).Trim().String()`,
			`if addr == "" {`,
			`	addr = fmt.Sprintf("%s:%s", env.Get("REDIS_HOST", "redis"), env.Get("REDIS_PORT", "6379"))`,
			`}`,
		},
		Fields: []storageConfigField{
			{Name: "Addr", Value: "addr"},
			{Name: "Username", Value: `scope.Get("USERNAME", "")`},
			{Name: "Password", Value: `scope.Get("PASSWORD", env.Get("REDIS_PASSWORD", ""))`},
			{Name: "DB", Value: `scope.GetInt("DB", "0")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"s3": {
		ConstName:  "driverS3",
		ImportPath: "github.com/goforj/storage/driver/s3storage",
		ConfigType: "s3storage.Config",
		Fields: []storageConfigField{
			{Name: "Bucket", Value: `scope.Get("BUCKET", "")`},
			{Name: "Endpoint", Value: `scope.Get("ENDPOINT", "")`},
			{Name: "Region", Value: `scope.Get("REGION", "us-east-1")`},
			{Name: "AccessKeyID", Value: `scope.Get("ACCESS_KEY_ID", "")`},
			{Name: "SecretAccessKey", Value: `scope.Get("SECRET_ACCESS_KEY", "")`},
			{Name: "UsePathStyle", Value: `scope.GetBool("USE_PATH_STYLE", "false")`},
			{Name: "UnsignedPayload", Value: `scope.GetBool("UNSIGNED_PAYLOAD", "false")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
	"sftp": {
		ConstName:  "driverSFTP",
		ImportPath: "github.com/goforj/storage/driver/sftpstorage",
		ConfigType: "sftpstorage.Config",
		Fields: []storageConfigField{
			{Name: "Host", Value: `scope.Get("HOST", "")`},
			{Name: "Port", Value: `scope.GetInt("PORT", "22")`},
			{Name: "User", Value: `scope.Get("USER", "root")`},
			{Name: "Password", Value: `scope.Get("PASSWORD", "")`},
			{Name: "KeyPath", Value: `scope.Get("KEY_PATH", "")`},
			{Name: "KnownHostsPath", Value: `scope.Get("KNOWN_HOSTS_PATH", "")`},
			{Name: "InsecureIgnoreHostKey", Value: `scope.GetBool("INSECURE_IGNORE_HOST_KEY", "false")`},
			{Name: "Prefix", Value: `scope.Get("PREFIX", "")`},
		},
	},
}

var storageRootKeys = []string{
	"DRIVER",
	"ROOT",
	"PREFIX",
	"ADDR",
	"USERNAME",
	"PASSWORD",
	"DB",
	"HOST",
	"PORT",
	"USER",
	"TLS",
	"INSECURE_SKIP_VERIFY",
	"KEY_PATH",
	"KNOWN_HOSTS_PATH",
	"INSECURE_IGNORE_HOST_KEY",
	"BUCKET",
	"ENDPOINT",
	"REGION",
	"ACCESS_KEY_ID",
	"SECRET_ACCESS_KEY",
	"USE_PATH_STYLE",
	"UNSIGNED_PAYLOAD",
	"CREDENTIALS_JSON",
	"TOKEN",
	"REMOTE",
	"RCLONE_CONFIG_PATH",
	"RCLONE_CONFIG_DATA",
}

var storageCommonKeys = makeSet(
	"DRIVER",
	"PREFIX",
)

var storageDriverKeys = map[string]map[string]struct{}{
	"local":   makeSet("ROOT"),
	"memory":  makeSet(),
	"redis":   makeSet("ADDR", "USERNAME", "PASSWORD", "DB"),
	"ftp":     makeSet("HOST", "PORT", "USER", "PASSWORD", "TLS", "INSECURE_SKIP_VERIFY"),
	"sftp":    makeSet("HOST", "PORT", "USER", "PASSWORD", "KEY_PATH", "KNOWN_HOSTS_PATH", "INSECURE_IGNORE_HOST_KEY"),
	"s3":      makeSet("BUCKET", "ENDPOINT", "REGION", "ACCESS_KEY_ID", "SECRET_ACCESS_KEY", "USE_PATH_STYLE", "UNSIGNED_PAYLOAD"),
	"gcs":     makeSet("BUCKET", "CREDENTIALS_JSON", "ENDPOINT"),
	"dropbox": makeSet("TOKEN"),
	"rclone":  makeSet("REMOTE", "RCLONE_CONFIG_PATH", "RCLONE_CONFIG_DATA"),
}

// storageLocalDrivers excludes memory because its optional driver module is not in the generated dependency baseline.
var storageLocalDrivers = []string{"local"}

// GenerateStorageFiles writes disk accessors whose selectable backends are fixed by the generation snapshot.
func GenerateStorageFiles(projectDir string) (int, error) {
	return generateStorageFiles(ambientGenerationInput(projectDir))
}

// generateStorageFiles uses one captured environment for validation, rendering, and named-resource discovery.
func generateStorageFiles(input generationInput) (int, error) {
	if err := validatePrimitiveEnv(input, primitiveEnvContract{
		Prefix:        "STORAGE",
		DefaultDriver: "local",
		LocalDrivers:  storageLocalDrivers,
		RootKeys:      storageRootKeys,
		CommonKeys:    storageCommonKeys,
		DriverKeys:    storageDriverKeys,
		ChildNames: func(environment generationEnvironment) []string {
			return exactScopedChildNames(environment, "STORAGE", storageRootKeys)
		},
		AllowInactiveRootKeys: true,
		EagerNamedResources:   true,
	}); err != nil {
		return 0, err
	}
	manager, err := renderStorageConfig(input)
	if err != nil {
		return 0, err
	}
	formattedManager, err := format.Source(manager)
	if err != nil {
		return 0, fmt.Errorf("failed to format generated storage manager: %w", err)
	}
	accessors, err := renderStorageAccessors(discoverStorageDiskNames(input))
	if err != nil {
		return 0, err
	}
	formattedAccessors, err := format.Source(accessors)
	if err != nil {
		return 0, fmt.Errorf("failed to format generated storage accessors: %w", err)
	}
	written := 0
	changed, err := writeGeneratedSource(filepath.Join(input.projectDir, "internal", "storages", "manager_gen.go"), formattedManager)
	if err != nil {
		return written, err
	}
	if changed {
		written++
	}
	changed, err = writeGeneratedSource(filepath.Join(input.projectDir, "internal", "storages", "accessors_gen.go"), formattedAccessors)
	if err != nil {
		return written, err
	}
	if changed {
		written++
	}
	for _, name := range storageLegacyGeneratedFiles {
		changed, err = removeGeneratedFileIfExists(filepath.Join(input.projectDir, "internal", "storages", name))
		if err != nil {
			return written, err
		}
		if changed {
			written++
		}
	}
	return written, nil
}

// discoverStorageDiskNames normalizes App and resource-first scopes into generated accessor names.
func discoverStorageDiskNames(input generationInput) []string {
	names := discoverStorageChildren(input)
	for i := range names {
		names[i] = str.Of(names[i]).Trim().ToLower().String()
	}
	return names
}

// discoverStorageChildren includes disks declared only through a configured App overlay.
func discoverStorageChildren(input generationInput) []string {
	return discoverPrimitiveChildNames(input, "STORAGE", storageRootKeys)
}

// exactScopedChildNames finds names only when their trailing key matches a complete resource key.
func exactScopedChildNames(environment generationEnvironment, prefix string, rootKeys []string) []string {
	prefix = str.Of(prefix).ToUpper().Trim().String()
	if prefix == "" {
		return nil
	}

	rootKeyParts := make(map[string][]string, len(rootKeys))
	orderedRootKeys := make([]string, 0, len(rootKeys))
	for _, key := range rootKeys {
		normalized := str.Of(key).ToUpper().Trim().String()
		if normalized == "" {
			continue
		}
		rootKeyParts[normalized] = strings.Split(normalized, "_")
		orderedRootKeys = append(orderedRootKeys, normalized)
	}
	sort.SliceStable(orderedRootKeys, func(left, right int) bool {
		return len(rootKeyParts[orderedRootKeys[left]]) > len(rootKeyParts[orderedRootKeys[right]])
	})

	seen := map[string]struct{}{}
	names := make([]string, 0)
	envPrefix := prefix + "_"

	for _, entry := range environment.Entries() {
		key := entry.key
		if !strings.HasPrefix(key, envPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(key, envPrefix)
		if suffix == "" {
			continue
		}
		if _, root := rootKeyParts[strings.ToUpper(suffix)]; root {
			continue
		}
		parts := strings.Split(strings.ToUpper(suffix), "_")
		for _, root := range orderedRootKeys {
			rootParts := rootKeyParts[root]
			if len(parts) <= len(rootParts) || !slices.Equal(parts[len(parts)-len(rootParts):], rootParts) {
				continue
			}
			child := strings.Join(parts[:len(parts)-len(rootParts)], "_")
			if child == "" {
				continue
			}
			if _, exists := seen[child]; !exists {
				seen[child] = struct{}{}
				names = append(names, child)
			}
			// The longest matching key owns the assignment even when another setting already declared its child.
			break
		}
	}

	sort.Strings(names)
	return names
}

// renderStorageAccessors keeps generated methods aligned with the named disks discovered for this build.
func renderStorageAccessors(names []string) ([]byte, error) {
	data := storageAccessorTemplateData{
		Names: make([]storageAccessorName, 0, len(names)),
	}
	for _, name := range names {
		data.Names = append(data.Names, storageAccessorName{
			Method: str.Of(name).Pascal().String(),
			Disk:   name,
		})
	}
	var b bytes.Buffer
	tmpl, err := template.New("storage-accessors").Parse(storageAccessorsSourceTemplate)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// renderStorageConfig keeps local backends in every generated build.
func renderStorageConfig(input generationInput) ([]byte, error) {
	names := discoverStorageDiskNames(input)
	driverSet := map[string]struct{}{}
	defaultDriver := effectivePrimitiveDriver(input.environment.Get("STORAGE_DRIVER", "local"), "local")
	driverSet[defaultDriver] = struct{}{}
	for _, child := range discoverStorageChildren(input) {
		driver := effectivePrimitiveDriver(input.environment.Get("STORAGE_"+child+"_DRIVER", ""), "local")
		driverSet[driver] = struct{}{}
	}
	for _, appPrefix := range generationAppEnvPrefixesForResource(input, "STORAGE") {
		resourcePrefix := appPrefix + "_STORAGE"
		for _, child := range exactScopedChildNames(input.environment, resourcePrefix, storageRootKeys) {
			driver := effectiveAppPrimitiveChildDriver(input.environment, appPrimitiveDriverScope{
				resourcePrefix: resourcePrefix,
				contract: primitiveEnvContract{
					Prefix:        "STORAGE",
					DefaultDriver: "local",
				},
				rootDriver:    defaultDriver,
				appRootDriver: defaultDriver,
			}, child)
			driverSet[driver] = struct{}{}
		}
	}
	for _, active := range appPrefixedActiveDrivers(input, "STORAGE", "local", false) {
		driverSet[active.driver] = struct{}{}
	}
	drivers, err := supportedDrivers(input.environment, "STORAGE", storageDriverKeys, sortStrings(driverSet), storageLocalDrivers)
	if err != nil {
		return nil, err
	}
	data := storageConfigTemplateData{
		CompiledDrivers: drivers,
		Drivers:         make([]storageDriverSpec, 0, len(drivers)),
		Names:           make([]storageAccessorName, 0, len(names)),
	}
	for _, name := range names {
		data.Names = append(data.Names, storageAccessorName{
			Method: str.Of(name).Pascal().String(),
			Disk:   name,
		})
	}
	for _, driver := range drivers {
		if spec, ok := storageDriverSpecs[driver]; ok {
			data.Drivers = append(data.Drivers, spec)
		}
	}
	var b bytes.Buffer
	tmpl, err := template.New("storage-config").Parse(storageConfigSourceTemplate)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeGeneratedSource avoids rewriting unchanged artifacts so generation remains idempotent for callers and tooling.
func writeGeneratedSource(path string, content []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

const storageAccessorsSourceTemplate = `// Code generated by forj generate --storage. DO NOT EDIT.
// Run: forj generate --storage
//
// These storage manager accessors are derived from the current .env file
// and environment variables available when generation ran.
// Named accessors are generated from STORAGE_<NAME>_<KEY> environment variables.
package storages

import "github.com/goforj/storage"

// Default returns the default storage disk instance derived from STORAGE_* configuration.
func (m *Manager) Default() storage.Storage {
	return m.defaultDisk
}

{{ range .Names }}
// {{ .Method }} returns the "{{ .Disk }}" storage disk.
func (m *Manager) {{ .Method }}() storage.Storage {
	return m.{{ .Disk }}
}
{{ end }}

// Instances returns the generated storage disk instances derived from STORAGE_* configuration.
func (m *Manager) Instances() []Instance {
	instances := []Instance{
		{Name: "default", Driver: m.defaultDriver, Disk: m.defaultDisk, IsDefault: true},
	}
{{- range .Names }}
	if m.{{ .Disk }} != nil {
		instances = append(instances, Instance{Name: "{{ .Disk }}", Driver: m.{{ .Disk }}Driver, Disk: m.{{ .Disk }}})
	}
{{- end }}
	return instances
}`

const storageConfigSourceTemplate = `// Code generated by forj generate --storage. DO NOT EDIT.
// Run: forj generate --storage
package storages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/goforj/env/v2"
	"github.com/goforj/storage"
{{- range .Drivers }}
	"{{ .ImportPath }}"
{{- end }}
	"github.com/goforj/str/v2"
)

const defaultDiskName storage.DiskName = "default"

const (
	driverDropbox = "dropbox"
	driverFTP     = "ftp"
	driverGCS     = "gcs"
	driverLocal   = "local"
	driverMemory  = "memory"
	driverRclone  = "rclone"
	driverRedis   = "redis"
	driverS3      = "s3"
	driverSFTP    = "sftp"
)

var compiledStorageDrivers = []string{
{{- range .CompiledDrivers }}
	"{{ . }}",
{{- end }}
}

var storageRootKeys = []string{
	"DRIVER",
	"ROOT",
	"PREFIX",
	"ADDR",
	"USERNAME",
	"PASSWORD",
	"DB",
	"HOST",
	"PORT",
	"USER",
	"TLS",
	"INSECURE_SKIP_VERIFY",
	"KEY_PATH",
	"KNOWN_HOSTS_PATH",
	"INSECURE_IGNORE_HOST_KEY",
	"BUCKET",
	"ENDPOINT",
	"REGION",
	"ACCESS_KEY_ID",
	"SECRET_ACCESS_KEY",
	"USE_PATH_STYLE",
	"UNSIGNED_PAYLOAD",
	"CREDENTIALS_JSON",
	"TOKEN",
	"REMOTE",
	"RCLONE_CONFIG_PATH",
	"RCLONE_CONFIG_DATA",
}

// Manager owns the storage disks generated from the project's build contract.
type Manager struct {
	defaultDisk storage.Storage
	observer Observer
	defaultDriver string
	warnings []OptionalDiskWarning
	ownedDisks []ownedStorageDisk
	closeOnce sync.Once
	closeErr error
{{- range .Names }}
	{{ .Disk }} storage.Storage
	{{ .Disk }}Driver string
{{- end }}
}

// ownedStorageDisk retains the construction name needed for deterministic cleanup diagnostics.
type ownedStorageDisk struct {
	name string
	disk storage.Storage
}

// storageCloser captures the optional lifecycle exposed by resource-backed storage drivers.
type storageCloser interface {
	Close() error
}

// storageCloserIdentity distinguishes shared pointer-backed closers without assuming every implementation is comparable.
type storageCloserIdentity struct {
	typeName string
	pointer  uintptr
}

// Instance gives tooling a uniform view of each initialized storage disk.
type Instance struct {
	Name      string
	Driver    string
	Disk      storage.Storage
	IsDefault bool
}

// ReadinessCheck pairs a stable disk name with its health probe.
type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

// OptionalDiskWarning retains startup diagnostics when a non-default disk is unavailable.
type OptionalDiskWarning struct {
	Name   string
	Driver string
	Error  string
}

// Observer decouples generated storage instrumentation from its metrics and tracing consumers.
type Observer interface {
	// OnStorageOp gives generated storage wrappers one stable hook for operation telemetry.
	OnStorageOp(ctx context.Context, event StorageOpEvent)
}

// StorageOpEvent provides bounded operation details without exposing driver-specific instrumentation.
type StorageOpEvent struct {
	Operation string
	Disk      string
	Path      string
	Driver    string
	Err       error
	Duration  time.Duration
}

// ObserverFunc lets a callback participate in the generated storage observer contract.
type ObserverFunc func(ctx context.Context, event StorageOpEvent)

// OnStorageOp adapts a callback so generated managers can compose it with interface-based observers.
func (f ObserverFunc) OnStorageOp(ctx context.Context, event StorageOpEvent) {
	if f == nil {
		return
	}
	f(ctx, event)
}

// observerChain retains multiple storage observers without exposing composition to callers.
type observerChain []Observer

// OnStorageOp preserves registration order when a storage operation is fanned out to multiple observers.
func (c observerChain) OnStorageOp(ctx context.Context, event StorageOpEvent) {
	for _, observer := range c {
		if observer == nil {
			continue
		}
		observer.OnStorageOp(ctx, event)
	}
}

// NewManager builds storage disks from the environment contract captured by this generated artifact.
func NewManager() (*Manager, error) {
	return newManagerFromEnv()
}

// Close releases every initialized disk once in reverse construction order.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.closeErr = closeOwnedStorageDisks(m.ownedDisks)
	})
	return m.closeErr
}

// WithObserver adds observability without replacing observers already attached by framework wiring.
func (m *Manager) WithObserver(observer Observer) *Manager {
	if observer == nil {
		return m
	}
	if m.observer == nil {
		m.observer = observer
	} else {
		switch existing := m.observer.(type) {
		case observerChain:
			m.observer = append(existing, observer)
		default:
			m.observer = observerChain{existing, observer}
		}
	}
	combined := m.observer
	m.defaultDisk = wrapObservedStorage(m.defaultDisk, "default", m.defaultDriver, combined)
{{- range .Names }}
	if m.{{ .Disk }} != nil {
		m.{{ .Disk }} = wrapObservedStorage(m.{{ .Disk }}, "{{ .Disk }}", m.{{ .Disk }}Driver, combined)
	}
{{- end }}
	return m
}

// Warnings preserves diagnostics for optional disks whose unavailable infrastructure did not prevent startup.
func (m *Manager) Warnings() []OptionalDiskWarning {
	if len(m.warnings) == 0 {
		return nil
	}
	out := make([]OptionalDiskWarning, len(m.warnings))
	copy(out, m.warnings)
	return out
}

// ReadinessChecks exposes one probe per initialized disk so health excludes optional disks skipped at startup.
func (m *Manager) ReadinessChecks() []ReadinessCheck {
	checks := []ReadinessCheck{
		{
			Name: "storage_default",
			Check: func(ctx context.Context) error {
				return storageReadinessCheck(ctx, m.defaultDisk)
			},
		},
	}
{{- range .Names }}
	if m.{{ .Disk }} != nil {
		checks = append(checks, ReadinessCheck{
			Name: "storage_{{ .Disk }}",
			Check: func(ctx context.Context) error {
				return storageReadinessCheck(ctx, m.{{ .Disk }})
			},
		})
	}
{{- end }}
	return checks
}

// LoadConfigFromEnv materializes the generated disk contract for callers that build storage independently.
func LoadConfigFromEnv() (storage.Config, error) {
	storageScope := env.WithPrefix("STORAGE")
	disks, err := loadDisksFromEnv(storageScope)
	if err != nil {
		return storage.Config{}, err
	}
	return storage.Config{
		Default: defaultDiskName,
		Disks:   disks,
	}, nil
}

// loadDisksFromEnv keeps default and named disk parsing under one validation boundary.
func loadDisksFromEnv(storageScope env.Scope) (map[storage.DiskName]storage.DriverConfig, error) {
	disks := map[storage.DiskName]storage.DriverConfig{}

	defaultCfg, err := buildDiskConfig(defaultDiskName, storageScope)
	if err != nil {
		return nil, err
	}
	disks[defaultDiskName] = defaultCfg

	for _, child := range storageScope.ChildNames(storageRootKeys) {
		name := storage.DiskName(str.Of(child).Trim().ToLower().String())
		cfg, err := buildDiskConfig(name, storageScope.Child(child))
		if err != nil {
			return nil, err
		}
		disks[name] = cfg
	}

	return disks, nil
}
// newManagerFromEnv eagerly initializes the default disk while allowing generated optional disks to degrade visibly.
func newManagerFromEnv() (*Manager, error) {
	return newManagerFromEnvWithBuilder(storage.Build)
}

// newManagerFromEnvWithBuilder keeps partial-construction cleanup testable without replacing the production builder.
func newManagerFromEnvWithBuilder(build func(storage.DriverConfig) (storage.Storage, error)) (*Manager, error) {
	storageScope := env.WithPrefix("STORAGE")
	defaultCfg, err := buildDiskConfig(defaultDiskName, storageScope)
	if err != nil {
		return nil, err
	}
	defaultDisk, err := build(defaultCfg)
	if err != nil {
		return nil, fmt.Errorf("storage: initialize disk %q: %w", defaultDiskName, err)
	}
	manager := &Manager{
		defaultDisk: defaultDisk,
		defaultDriver: storageDriverNameFromScope(storageScope),
		ownedDisks: []ownedStorageDisk{
			{name: string(defaultDiskName), disk: defaultDisk},
		},
	}
{{- range .Names }}
	disk{{ .Method }}, warning{{ .Method }}, err := optionalDiskFromScope(storageScope, storage.DiskName("{{ .Disk }}"), build)
	if err != nil {
		return nil, joinStorageInitializationCleanup(err, manager.Close())
	}
	manager.{{ .Disk }} = disk{{ .Method }}
	if disk{{ .Method }} != nil {
		manager.{{ .Disk }}Driver = storageDriverNameFromScope(storageScope.Child(str.Of("{{ .Disk }}").Snake().ToUpper().String()))
		manager.ownedDisks = append(manager.ownedDisks, ownedStorageDisk{name: "{{ .Disk }}", disk: disk{{ .Method }}})
	} else if warning{{ .Method }} != nil {
		manager.warnings = append(manager.warnings, *warning{{ .Method }})
	}
{{- end }}
	return manager, nil
}

// optionalDiskFromScope keeps expected infrastructure outages nonfatal for non-default disks while retaining diagnostics.
func optionalDiskFromScope(storageScope env.Scope, name storage.DiskName, build func(storage.DriverConfig) (storage.Storage, error)) (storage.Storage, *OptionalDiskWarning, error) {
	childScope := storageScope.Child(str.Of(string(name)).Snake().ToUpper().String())
	cfg, err := buildDiskConfig(name, childScope)
	if err != nil {
		return nil, nil, err
	}
	disk, err := build(cfg)
	if err == nil {
		return disk, nil, nil
	}
	if isOptionalStorageDiskError(err) {
		driver := storageDriverNameFromScope(childScope)
		return nil, &OptionalDiskWarning{
			Name:   string(name),
			Driver: driver,
			Error:  err.Error(),
		}, nil
	}
	return nil, nil, err
}

// joinStorageInitializationCleanup retains both the construction failure and any cleanup failures it triggered.
func joinStorageInitializationCleanup(initializationErr error, cleanupErr error) error {
	if cleanupErr == nil {
		return initializationErr
	}
	return errors.Join(initializationErr, cleanupErr)
}

// closeOwnedStorageDisks closes unique resource handles in reverse construction order and retains every failure.
func closeOwnedStorageDisks(disks []ownedStorageDisk) error {
	seen := map[storageCloserIdentity]struct{}{}
	var closeErrs []error
	for i := len(disks) - 1; i >= 0; i-- {
		closer, ok := disks[i].disk.(storageCloser)
		if !ok {
			continue
		}
		if identity, identifiable := storageCloseIdentity(closer); identifiable {
			if _, duplicate := seen[identity]; duplicate {
				continue
			}
			seen[identity] = struct{}{}
		}
		if err := closer.Close(); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("storage: close disk %q: %w", disks[i].name, err))
		}
	}
	return errors.Join(closeErrs...)
}

// storageCloseIdentity returns stable identity only for pointer-backed closers that can represent a shared resource.
func storageCloseIdentity(closer storageCloser) (storageCloserIdentity, bool) {
	value := reflect.ValueOf(closer)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return storageCloserIdentity{}, false
	}
	return storageCloserIdentity{typeName: value.Type().String(), pointer: value.Pointer()}, true
}

// isOptionalStorageDiskError limits startup leniency to missing or unreachable optional backends.
func isOptionalStorageDiskError(err error) bool {
	if err == nil {
		return false
	}
	if strings.HasPrefix(err.Error(), "storage: disk \"") && strings.HasSuffix(err.Error(), "\" not found") {
		return true
	}
	return strings.Contains(err.Error(), "connect: connection refused") ||
		strings.Contains(err.Error(), "no such host") ||
		strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "network is unreachable")
}

// storageDriverNameFromScope normalizes driver labels shared by diagnostics and operation observers.
func storageDriverNameFromScope(scope env.Scope) string {
	driver := str.Of(scope.Get("DRIVER", driverLocal)).Trim().ToLower().String()
	if driver == "" {
		return driverLocal
	}
	return driver
}

// buildDiskConfig rejects backends outside the generated manifest before endpoint configuration is constructed.
// The local driver is always compiled; optional drivers come from STORAGE_SUPPORTED_DRIVERS,
// falling back to active root and named Storage scopes when that list is unset.
func buildDiskConfig(name storage.DiskName, scope env.Scope) (storage.DriverConfig, error) {
	driver := str.Of(scope.Get("DRIVER", driverLocal)).Trim().ToLower().String()
	if driver == "" {
		driver = driverLocal
	}
	if !storageDriverCompiled(driver) {
		return nil, fmt.Errorf("storage: active driver %q is not built in; compiled choices: %s; run forj generate --storage after updating STORAGE_SUPPORTED_DRIVERS", driver, strings.Join(compiledStorageDrivers, ", "))
	}

	localRoot := filepath.Join("storage", "app", "private")
	if name != defaultDiskName {
		localRoot = filepath.Join("storage", "app", string(name))
	}

	switch driver {
{{- range .Drivers }}
	case {{ .ConstName }}:
{{- range .Setup }}
		{{ . }}
{{- end }}
		return {{ .ConfigType }}{
{{- range .Fields }}
			{{ .Name }}: {{ .Value }},
{{- end }}
		}, nil
{{- end }}
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q", driver)
	}
}

// storageDriverCompiled reports whether driver is selectable in this generated artifact.
func storageDriverCompiled(driver string) bool {
	for _, compiled := range compiledStorageDrivers {
		if driver == compiled {
			return true
		}
	}
	return false
}

// storageReadinessCheck prefers explicit driver health contracts and falls back to a lightweight listing operation.
func storageReadinessCheck(ctx context.Context, disk storage.Storage) error {
	if ready, ok := any(disk).(interface{ Ready(context.Context) error }); ok {
		return ready.Ready(ctx)
	}
	if ready, ok := any(disk).(interface{ Ready() error }); ok {
		return ready.Ready()
	}
	_, err := disk.WithContext(ctx).List("")
	return err
}

// observedStorage decorates any storage driver with context-aware operation telemetry.
type observedStorage struct {
	inner    storage.Storage
	name     string
	driver   string
	observer Observer
	ctx      context.Context
	lifecycle *observedStorageLifecycle
}

// observedStorageLifecycle shares one close result across every context-derived observer wrapper.
type observedStorageLifecycle struct {
	closer storageCloser
	once   sync.Once
	err    error
}

// wrapObservedStorage reuses an existing wrapper so adding observers does not stack duplicate instrumentation.
func wrapObservedStorage(inner storage.Storage, name string, driver string, observer Observer) storage.Storage {
	if observer == nil {
		return inner
	}
	if wrapped, ok := inner.(*observedStorage); ok {
		wrapped.name = name
		wrapped.driver = driver
		wrapped.observer = observer
		return wrapped
	}
	wrapped := &observedStorage{
		inner:    inner,
		name:     name,
		driver:   driver,
		observer: observer,
	}
	if closer, ok := inner.(storageCloser); ok {
		wrapped.lifecycle = &observedStorageLifecycle{closer: closer}
	}
	return wrapped
}

// observe emits one uniform event after a delegated storage operation completes.
func (s *observedStorage) observe(ctx context.Context, op string, path string, start time.Time, err error) {
	s.observer.OnStorageOp(ctx, StorageOpEvent{
		Operation: op,
		Disk:      s.name,
		Path:      path,
		Driver:    s.driver,
		Err:       err,
		Duration:  time.Since(start),
	})
}

// WithContext clones the wrapper so request contexts cannot leak between storage callers.
func (s *observedStorage) WithContext(ctx context.Context) storage.Storage {
	clone := *s
	if ctx == nil {
		ctx = context.Background()
	}
	clone.ctx = ctx
	return &clone
}

// Close releases the underlying driver once across the root wrapper and every context-derived clone.
func (s *observedStorage) Close() error {
	if s.lifecycle == nil || s.lifecycle.closer == nil {
		return nil
	}
	s.lifecycle.once.Do(func() {
		s.lifecycle.err = s.lifecycle.closer.Close()
	})
	return s.lifecycle.err
}

// context supplies a background context for callers that use the context-free storage API.
func (s *observedStorage) context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Get records read telemetry around the underlying driver without changing its behavior.
func (s *observedStorage) Get(p string) ([]byte, error) {
	start := time.Now()
	ctx := s.context()
	body, err := s.inner.WithContext(ctx).Get(p)
	s.observe(ctx, "get", p, start, err)
	return body, err
}

// Put records write telemetry around the underlying driver without changing its behavior.
func (s *observedStorage) Put(p string, contents []byte) error {
	start := time.Now()
	ctx := s.context()
	err := s.inner.WithContext(ctx).Put(p, contents)
	s.observe(ctx, "put", p, start, err)
	return err
}

// MakeDir records directory-creation telemetry around the underlying driver.
func (s *observedStorage) MakeDir(p string) error {
	start := time.Now()
	ctx := s.context()
	err := s.inner.WithContext(ctx).MakeDir(p)
	s.observe(ctx, "make_dir", p, start, err)
	return err
}

// Delete records deletion telemetry around the underlying driver without changing its behavior.
func (s *observedStorage) Delete(p string) error {
	start := time.Now()
	ctx := s.context()
	err := s.inner.WithContext(ctx).Delete(p)
	s.observe(ctx, "delete", p, start, err)
	return err
}

// Stat records metadata-read telemetry around the underlying driver.
func (s *observedStorage) Stat(p string) (storage.Entry, error) {
	start := time.Now()
	ctx := s.context()
	entry, err := s.inner.WithContext(ctx).Stat(p)
	s.observe(ctx, "stat", p, start, err)
	return entry, err
}

// Exists records existence-check telemetry around the underlying driver.
func (s *observedStorage) Exists(p string) (bool, error) {
	start := time.Now()
	ctx := s.context()
	exists, err := s.inner.WithContext(ctx).Exists(p)
	s.observe(ctx, "exists", p, start, err)
	return exists, err
}

// List records directory-listing telemetry around the underlying driver.
func (s *observedStorage) List(p string) ([]storage.Entry, error) {
	start := time.Now()
	ctx := s.context()
	entries, err := s.inner.WithContext(ctx).List(p)
	s.observe(ctx, "list", p, start, err)
	return entries, err
}

// Walk records traversal telemetry around the underlying driver.
func (s *observedStorage) Walk(p string, fn func(storage.Entry) error) error {
	start := time.Now()
	ctx := s.context()
	err := s.inner.WithContext(ctx).Walk(p, fn)
	s.observe(ctx, "walk", p, start, err)
	return err
}

// Copy records both paths in telemetry so cross-location operations remain diagnosable.
func (s *observedStorage) Copy(src, dst string) error {
	start := time.Now()
	ctx := s.context()
	err := s.inner.WithContext(ctx).Copy(src, dst)
	s.observe(ctx, "copy", src+" -> "+dst, start, err)
	return err
}

// Move records both paths in telemetry so cross-location operations remain diagnosable.
func (s *observedStorage) Move(src, dst string) error {
	start := time.Now()
	ctx := s.context()
	err := s.inner.WithContext(ctx).Move(src, dst)
	s.observe(ctx, "move", src+" -> "+dst, start, err)
	return err
}

// URL records URL-generation telemetry around the underlying driver.
func (s *observedStorage) URL(p string) (string, error) {
	start := time.Now()
	ctx := s.context()
	url, err := s.inner.WithContext(ctx).URL(p)
	s.observe(ctx, "url", p, start, err)
	return url, err
}

// ListPage preserves paged-storage capability checks while recording the result uniformly.
func (s *observedStorage) ListPage(p string, offset, limit int) (storage.ListPageResult, error) {
	start := time.Now()
	ctx := s.context()
	paged, ok := s.inner.WithContext(ctx).(storage.PagedStorage)
	if !ok {
		err := storage.ErrUnsupported
		s.observe(ctx, "list_page", p, start, err)
		return storage.ListPageResult{}, err
	}
	result, err := paged.ListPage(p, offset, limit)
	s.observe(ctx, "list_page", p, start, err)
	return result, err
}`
