package generate

import (
	"bytes"
	"fmt"
	"go/format"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/goforj/str/v2"
)

// dbTemplateData keeps manifest, import, and accessor decisions together so emitted database source stays internally consistent.
type dbTemplateData struct {
	CompiledDrivers []string
	HasNames        bool
	NeedsGormImport bool
	Names           []dbAccessorName
	Drivers         []dbDriverSpec
}

// dbAccessorName pairs normalized configuration names with Go-safe method names for generated accessors.
type dbAccessorName struct {
	Method string
	Name   string
}

// dbDriverSpec keeps each driver's imports, aliases, and constructor in one place so generated support cannot drift.
type dbDriverSpec struct {
	Alias       string
	ImportPath  string
	SwitchCases []string
	Constructor string
	PrepareDSN  bool
}

// dbDriverPlan keeps generated implementations and the authoritative compiled manifest in one validated result.
type dbDriverPlan struct {
	drivers  []dbDriverSpec
	compiled []string
}

var dbRootKeys = []string{
	"DEFAULT",
	"DRIVER",
	"DSN",
	"HOST",
	"DATABASE",
	"SQLITE_DATABASE",
	"USERNAME",
	"PASSWORD",
	"PORT",
	"QUERY_LOGGING",
	"SLOW_QUERY_THRESHOLD",
	"MAX_IDLE_CONNECTIONS",
	"MAX_OPEN_CONNECTIONS",
	"CONN_MAX_LIFETIME_MINUTES",
	"ROOT_PASSWORD",
}

// dbLocalDrivers are available independently of the optional driver manifest.
var dbLocalDrivers = []string{"sqlite"}

// GenerateDBFiles writes database accessors whose selectable drivers are fixed by the generation snapshot.
func GenerateDBFiles(projectDir string) (int, error) {
	return generateDBFiles(ambientGenerationInput(projectDir))
}

// generateDBFiles uses one captured environment for validation, rendering, and named-resource discovery.
func generateDBFiles(input generationInput) (int, error) {
	names := discoverDBConnectionNames(input)
	driverPlan, err := discoverDBDrivers(input, names)
	if err != nil {
		return 0, err
	}
	source, err := renderDBAccessors(names, driverPlan)
	if err != nil {
		return 0, err
	}
	formatted, err := format.Source(source)
	if err != nil {
		return 0, fmt.Errorf("failed to format generated db accessors: %w", err)
	}
	changed, err := writeGeneratedSource(filepath.Join(input.projectDir, "internal", "database", "connections_gen.go"), formatted)
	if err != nil {
		return 0, err
	}
	if changed {
		return 1, nil
	}
	return 0, nil
}

// discoverDBConnectionNames includes connections declared only through a configured App overlay.
func discoverDBConnectionNames(input generationInput) []string {
	names := discoverPrimitiveChildNames(input, "DB", dbRootKeys)
	out := make([]string, 0, len(names))
	for _, name := range names {
		normalized := str.Of(name).Trim().ToLower().String()
		if normalized == "" || normalized == "root" || dbHelperConnectionName(normalized) {
			continue
		}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

// dbHelperConnectionName skips driver-specific helper keys such as DB_SQLITE_DATABASE.
func dbHelperConnectionName(name string) bool {
	switch str.Of(name).ToLower().Trim().String() {
	case "mysql", "postgres", "postgresql", "sqlite", "sqlite3":
		return true
	default:
		return false
	}
}

// renderDBAccessors emits the validated database implementations and compiled manifest together.
func renderDBAccessors(names []string, driverPlan dbDriverPlan) ([]byte, error) {
	data := dbTemplateData{
		CompiledDrivers: driverPlan.compiled,
		HasNames:        false,
		NeedsGormImport: len(names) > 1 || len(driverPlan.drivers) > 0,
		Names:           make([]dbAccessorName, 0, len(names)),
		Drivers:         driverPlan.drivers,
	}
	for _, name := range names {
		if name == "default" {
			continue
		}
		data.HasNames = true
		data.Names = append(data.Names, dbAccessorName{
			Method: str.Of(name).Pascal().String(),
			Name:   name,
		})
	}
	var b bytes.Buffer
	tmpl, err := template.New("db-accessors").Parse(dbAccessorsSourceTemplate)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// discoverDBDrivers validates every active connection against the explicit build contract before source is emitted.
func discoverDBDrivers(input generationInput, names []string) (dbDriverPlan, error) {
	drivers := map[string]dbDriverSpec{}
	compiled := map[string]struct{}{}
	for _, driver := range dbLocalDrivers {
		compiled[driver] = struct{}{}
		recordDBDriver(drivers, driver)
	}
	rootDriver := str.Of(input.environment.Get("DB_DRIVER", "sqlite")).Trim().ToLower().String()
	if rootDriver == "" {
		rootDriver = "sqlite"
	}
	activeDrivers := []generationActiveDriver{
		{
			key:    "DB_DRIVER",
			driver: rootDriver,
		},
	}
	for _, name := range names {
		prefix := "DB_" + str.Of(name).Snake().ToUpper().String()
		driver := str.Of(input.environment.Get(prefix+"_DRIVER", "")).Trim().ToLower().String()
		if driver == "" {
			continue
		}
		activeDrivers = append(activeDrivers, generationActiveDriver{
			key:    "DB_" + str.Of(name).Snake().ToUpper().String() + "_DRIVER",
			driver: driver,
		})
	}
	activeDrivers = append(activeDrivers, appPrefixedActiveDrivers(input, "DB", "sqlite", true)...)
	rawSupported := str.Of(input.environment.Get("DB_SUPPORTED_DRIVERS", "")).Trim().ToLower().String()
	if rawSupported != "" {
		supportedCount := 0
		for _, part := range strings.Split(rawSupported, ",") {
			driver := str.Of(part).Trim().ToLower().String()
			if driver == "" {
				continue
			}
			if !recordDBDriver(drivers, driver) {
				return dbDriverPlan{}, fmt.Errorf("DB_SUPPORTED_DRIVERS includes unsupported driver %q", driver)
			}
			supportedCount++
			compiled[canonicalDBDriver(driver)] = struct{}{}
		}
		if supportedCount == 0 {
			return dbDriverPlan{}, fmt.Errorf("DB_SUPPORTED_DRIVERS must include at least one driver")
		}
		for _, active := range activeDrivers {
			if _, ok := compiled[canonicalDBDriver(active.driver)]; !ok {
				return dbDriverPlan{}, fmt.Errorf("%s selects driver %q not enabled by DB_SUPPORTED_DRIVERS", active.key, active.driver)
			}
		}
	} else {
		for _, active := range activeDrivers {
			if !recordDBDriver(drivers, active.driver) {
				return dbDriverPlan{}, fmt.Errorf("%s selects unsupported driver %q", active.key, active.driver)
			}
			compiled[canonicalDBDriver(active.driver)] = struct{}{}
		}
	}
	out := make([]dbDriverSpec, 0, len(drivers))
	for _, driver := range drivers {
		out = append(out, driver)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return dbDriverPlan{drivers: out, compiled: sortStrings(compiled)}, nil
}

// canonicalDBDriver normalizes accepted compatibility aliases to the generated manifest name.
func canonicalDBDriver(driver string) string {
	switch str.Of(driver).ToLower().Trim().String() {
	case "mariadb":
		return "mysql"
	case "postgresql":
		return "postgres"
	case "sqlite3":
		return "sqlite"
	default:
		return str.Of(driver).ToLower().Trim().String()
	}
}

// recordDBDriver collapses accepted aliases into one specification so generated imports and switch cases stay deduplicated.
func recordDBDriver(drivers map[string]dbDriverSpec, driver string) bool {
	switch driver {
	case "mysql", "mariadb":
		drivers["mysql"] = dbDriverSpec{
			Alias:       "mysql",
			ImportPath:  "gorm.io/driver/mysql",
			SwitchCases: []string{"mysql", "mariadb"},
			Constructor: `mysql.New(mysql.Config{DSN: dsn, DisableWithReturning: true})`,
		}
		return true
	case "postgres", "postgresql":
		drivers["postgres"] = dbDriverSpec{
			Alias:       "postgres",
			ImportPath:  "gorm.io/driver/postgres",
			SwitchCases: []string{"postgres", "postgresql"},
			Constructor: "postgres.Open(dsn)",
		}
		return true
	case "sqlite", "sqlite3":
		drivers["sqlite"] = dbDriverSpec{
			Alias:       "sqlite",
			ImportPath:  "github.com/glebarez/sqlite",
			SwitchCases: []string{"sqlite", "sqlite3"},
			Constructor: "sqlite.Open(dsn)",
			PrepareDSN:  true,
		}
		return true
	}
	return false
}

const dbAccessorsSourceTemplate = `// Code generated by forj generate --db. DO NOT EDIT.
// Run: forj generate --db
//
// This file contains accessors generated from DB_<NAME>_<KEY> environment variables.
// For example, DB_ANALYTICS_DRIVER will generate GetAnalytics().
package database

{{- if .NeedsGormImport }}
import (
	"context"
	"fmt"
	"strings"
	{{- if .Drivers }}
	"github.com/goforj/str/v2"
	{{- range .Drivers }}
	"{{ .ImportPath }}"
	{{- end }}
	{{- end }}
	"gorm.io/gorm"
)
{{ end }}
var compiledDatabaseDrivers = []string{
{{- range .CompiledDrivers }}
	"{{ . }}",
{{- end }}
}

// ReadinessCheck gives health aggregation a stable label and deferred probe without exposing connection internals.
type ReadinessCheck struct {
	Name  string
	Check func(context.Context) error
}

// ReadinessChecks exposes one stable probe per generated connection so health reporting stays independent of accessor names.
func (c *Connections) ReadinessChecks() []ReadinessCheck {
	checks := []ReadinessCheck{
		{
			Name: "db_default",
			Check: func(ctx context.Context) error {
				return c.readinessCheck(ctx, "default")
			},
		},
{{- range .Names }}
		{
			Name: "db_{{ .Name }}",
			Check: func(ctx context.Context) error {
				return c.readinessCheck(ctx, "{{ .Name }}")
			},
		},
{{- end }}
	}
	return checks
}

// readinessCheck uses the shared connection lookup so default and named databases retain identical error behavior.
func (c *Connections) readinessCheck(ctx context.Context, name string) error {
	conn, err := c.Connection(name)
	if err != nil {
		return err
	}
	sqlDB, err := conn.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

{{- if .Drivers }}

// openDialector rejects drivers outside the generated manifest before GORM initializes a connection.
func openDialector(driver, dsn string) (gorm.Dialector, error) {
	driver = str.Of(driver).ToLower().Trim().String()
	if !databaseDriverCompiled(driver) {
		return nil, fmt.Errorf("database: active driver %q is not built in; compiled choices: %s; run forj generate --db after updating DB_SUPPORTED_DRIVERS", driver, strings.Join(compiledDatabaseDrivers, ", "))
	}
	switch driver {
	{{- range .Drivers }}
	case {{ range $idx, $case := .SwitchCases }}{{ if $idx }}, {{ end }}"{{ $case }}"{{ end }}:
		{{- if .PrepareDSN }}
		if err := ensureSQLitePath(dsn); err != nil {
			return nil, err
		}
		{{- end }}
		return {{ .Constructor }}, nil
	{{- end }}
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
}

// databaseDriverCompiled reports whether driver is selectable in this generated artifact.
func databaseDriverCompiled(driver string) bool {
	switch driver {
	case "mariadb":
		driver = "mysql"
	case "postgresql":
		driver = "postgres"
	case "sqlite3":
		driver = "sqlite"
	}
	for _, compiled := range compiledDatabaseDrivers {
		if driver == compiled {
			return true
		}
	}
	return false
}
{{ end }}
{{ range .Names }}
// Get{{ .Method }} returns the "{{ .Name }}" connection.
func (c *Connections) Get{{ .Method }}() (*gorm.DB, error) {
	return c.get("{{ .Name }}")
}

{{ end }}`
