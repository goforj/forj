package build

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goforj/str/v2"

	"github.com/goforj/goforj/internal/apiindex"
	"github.com/goforj/goforj/internal/compileprofile"
	"github.com/goforj/goforj/internal/logger"
	"github.com/goforj/goforj/internal/stacks"
)

const (
	// DevBuildPhaseEnvironment carries the private phase selected by the GoForj development coordinator.
	DevBuildPhaseEnvironment = "FORJ_DEV_BUILD_PHASE"
	// DevBuildPhasePrepare stabilizes generated source without indexing or compiling an App.
	DevBuildPhasePrepare = "prepare"
	// DevBuildPhaseCompile indexes and compiles an App from an already prepared project snapshot.
	DevBuildPhaseCompile = "compile"
	// AppHelpCommandOrigin identifies a build whose successful progress should clear before root help renders.
	AppHelpCommandOrigin = "app_help"
)

// Cmd runs the forj build pipeline.
type Cmd struct {
	logger   *logger.AppLogger
	pipeline Pipeline
	Timings  bool `help:"Print per-step timings for generate, api index, and go build"`
	SkipWire bool `help:"Skip running wire before build" hidden:""`
	// APIIndexStrict fails the build when API indexing reports warnings or errors.
	APIIndexStrict bool `name:"api-index-strict" help:"Fail when API indexing reports warnings or errors"`
	// Stack selects shareable resource defaults without activating the stack locally.
	Stack         string `help:"Bake a saved stack into the binary as overridable defaults"`
	stackDefaults map[string]string
	EnvDefaults   string `help:"Compile unset-only environment defaults as comma-separated KEY=value pairs"`
	EnvOverrides  string `help:"Compile forced environment overrides as comma-separated KEY=value pairs"`

	// Profile flags.
	Profile bool `help:"Profile compile time for this build"`
	Top     int  `help:"Limit profile results" default:"12"`

	Root            string   `help:"Project root to build" default:"."`
	Args            []string `arg:"" optional:"" passthrough:"" help:"Arguments passed through to go build"`
	compileProfile  compileprofile.Report
	lastBuildStatus string
	goGetFunc       func([]string) error
}

// NewCmd creates the build command with the API indexer that shares its final compilation boundary.
func NewCmd(logger *logger.AppLogger, apiIndex apiindex.Preparer) *Cmd {
	return &Cmd{
		logger:   logger,
		pipeline: NewPipeline(logger, apiIndex),
	}
}

// Signature returns CLI metadata for the complete build pipeline.
func (*Cmd) Signature() string {
	return `name:"build" help:"Build an App and all configured frontend assets" group:"build"`
}

// Run refreshes project source and frontend assets, then publishes API artifacts only after compilation succeeds.
func (c *Cmd) Run() error {
	root, err := resolveProjectRoot(c.Root)
	if err != nil {
		return err
	}
	c.stackDefaults = nil
	c.pipeline.stackEnvironment = nil
	if c.Stack != "" {
		c.stackDefaults, err = stacks.BuildDefaults(root, c.Stack)
		if err != nil {
			return err
		}
		source, readErr := os.ReadFile(filepath.Join(root, "internal", "cmd", "env_defaults.go"))
		if readErr != nil || !strings.Contains(string(source), "var CompiledStackDefaultsBase64 string") {
			return fmt.Errorf("this project needs refreshed Stack runtime support; run forj render before building --stack")
		}
		c.pipeline.stackEnvironment = c.stackDefaults
	}
	if err := c.validateCompiledEnv(root); err != nil {
		return err
	}
	buildTags, err := apiindex.BuildTagsFromArgs(c.Args)
	if err != nil {
		return err
	}
	options := RunOptions{Timings: c.Timings, SkipWire: c.SkipWire, APIIndexStrict: c.APIIndexStrict, BuildTags: buildTags, TransientProgress: appHelpBuildTransientProgress()}
	if strings.TrimSpace(os.Getenv("FORJ_COMMAND_ORIGIN")) == "dev_command" {
		switch strings.TrimSpace(os.Getenv(DevBuildPhaseEnvironment)) {
		case DevBuildPhasePrepare:
			options.PreparationOnly = true
		case DevBuildPhaseCompile:
			options.SkipPreparation = true
		}
	}
	if err := c.pipeline.Run(root, "build", Step{
		Name: "go build",
		Run:  c.buildBinary,
	}, options); err != nil {
		return err
	}
	if options.PreparationOnly {
		return nil
	}
	if c.Profile {
		return c.printProfile()
	}
	return nil
}

// appHelpBuildTransientProgress keeps one-time help preparation from leaving a completed build line above help.
func appHelpBuildTransientProgress() bool {
	return strings.TrimSpace(os.Getenv("FORJ_COMMAND_ORIGIN")) == AppHelpCommandOrigin
}

// buildBinary compiles and publishes the selected App beneath the pipeline's validated project root.
func (c *Cmd) buildBinary(root string) (string, error) {
	args, err := c.buildArgs(root)
	if err != nil {
		return "", err
	}
	if outIndex := outputArgIndex(args); outIndex >= 0 {
		if err := os.MkdirAll(rootedBuildPath(root, filepath.Dir(outputPath(args[outIndex]))), 0o755); err != nil {
			return "", err
		}
	}
	if c.Profile {
		return c.buildBinaryWithProfile(root, args)
	}
	return c.runPlainGoBuild(root, args)
}

// rootedBuildPath anchors relative build artifacts while preserving absolute caller-selected paths.
func rootedBuildPath(root string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	return filepath.Join(root, path)
}

// buildArgs preserves caller-supplied Go build flags while resolving the default App package and configured environment metadata.
func (c *Cmd) buildArgs(root string) ([]string, error) {
	envDefaultsEncoded := c.encodedEnvDefaults()
	envOverridesEncoded := c.encodedEnvOverrides()
	modulePath := ""
	if envDefaultsEncoded != "" || envOverridesEncoded != "" || len(c.stackDefaults) > 0 {
		modulePath = c.modulePath(root)
	}
	var extraLdflags []string
	if len(c.stackDefaults) > 0 {
		runtimeDefaults, err := stacks.AppDefaults(root, c.stackDefaults, ActiveApp().Name)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(runtimeDefaults)
		if err != nil {
			return nil, err
		}
		if modulePath == "" {
			return nil, fmt.Errorf("could not resolve module path for stack defaults")
		}
		extraLdflags = append(extraLdflags, fmt.Sprintf("-X %s/internal/cmd.CompiledStackDefaultsBase64=%s", modulePath, base64.StdEncoding.EncodeToString(data)))
	}
	if envDefaultsEncoded != "" {
		extraLdflags = append(extraLdflags, c.envDefaultsLdflags(modulePath, envDefaultsEncoded))
	}
	if envOverridesEncoded != "" {
		extraLdflags = append(extraLdflags, c.envOverridesLdflags(modulePath, envOverridesEncoded))
	}
	if len(c.Args) == 0 {
		target := ActiveApp()
		args := []string{"-o", filepath.ToSlash(filepath.Join(".", "bin", target.Name))}
		if len(extraLdflags) > 0 {
			args = append(args, "-ldflags", strings.Join(extraLdflags, " "))
		}
		packagePath, err := resolveDefaultAppPackage(root)
		if err != nil {
			return nil, err
		}
		return append(args, packagePath), nil
	}
	args := append([]string{}, c.Args...)
	if !hasGoBuildPackageArg(args) {
		packagePath, err := resolveDefaultAppPackage(root)
		if err != nil {
			return nil, err
		}
		args = append(args, packagePath)
	}
	if len(extraLdflags) == 0 {
		return args, nil
	}
	return c.withExtraLdflags(args, extraLdflags...), nil
}

// hasGoBuildPackageArg reports whether pass-through go build args already name the package to build.
func hasGoBuildPackageArg(args []string) bool {
	flagsWithValue := map[string]struct{}{
		"-asmflags": {}, "-buildmode": {}, "-compiler": {}, "-gccgoflags": {}, "-gcflags": {},
		"-installsuffix": {}, "-ldflags": {}, "-mod": {}, "-modfile": {},
		"-o": {}, "-overlay": {}, "-p": {}, "-pkgdir": {}, "-tags": {}, "-toolexec": {},
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == "--" {
			return i+1 < len(args)
		}
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "=") {
				continue
			}
			flagName := arg
			if strings.HasPrefix(flagName, "--") {
				flagName = "-" + strings.TrimPrefix(flagName, "--")
			}
			// Some go build flags consume the next arg, so that value is not a package path.
			if _, ok := flagsWithValue[flagName]; ok && i+1 < len(args) {
				i++
			}
			continue
		}
		return true
	}
	return false
}

// outputArgIndex centralizes output arg index behavior so callers follow the same contract.
func outputArgIndex(args []string) int {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-o" {
			return i + 1
		}
	}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-o=") {
			return i
		}
	}
	return -1
}

// outputPath centralizes output path behavior so callers follow the same contract.
func outputPath(arg string) string {
	if strings.HasPrefix(arg, "-o=") {
		return strings.TrimPrefix(arg, "-o=")
	}
	return arg
}

// validateCompiledEnv fails before pipeline work because linker injection needs valid assignments and a resolvable module path.
func (c *Cmd) validateCompiledEnv(root string) error {
	if _, err := parseEnvAssignments(c.EnvDefaults, "--env-defaults"); err != nil {
		return err
	}
	if _, err := parseEnvAssignments(c.EnvOverrides, "--env-overrides"); err != nil {
		return err
	}
	if modulePath := c.modulePath(root); modulePath == "" && (strings.TrimSpace(c.EnvDefaults) != "" || strings.TrimSpace(c.EnvOverrides) != "") {
		if strings.TrimSpace(c.EnvDefaults) != "" {
			return fmt.Errorf("could not resolve module path from %s/go.mod for env defaults %q", strings.TrimSpace(c.Root), strings.TrimSpace(c.EnvDefaults))
		}
		return fmt.Errorf("could not resolve module path from %s/go.mod for env overrides %q", strings.TrimSpace(c.Root), strings.TrimSpace(c.EnvOverrides))
	}
	return nil
}

// encodedEnvDefaults keeps encoded env defaults handling consistent across callers.
func (c *Cmd) encodedEnvDefaults() string {
	pairs, err := parseEnvAssignments(c.EnvDefaults, "--env-defaults")
	if err != nil || len(pairs) == 0 {
		return ""
	}
	raw := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		raw = append(raw, pair.key+"="+pair.value)
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(raw, ",")))
}

// encodedEnvOverrides keeps encoded env overrides handling consistent across callers.
func (c *Cmd) encodedEnvOverrides() string {
	pairs, err := parseEnvAssignments(c.EnvOverrides, "--env-overrides")
	if err != nil || len(pairs) == 0 {
		return ""
	}
	raw := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		raw = append(raw, pair.key+"="+pair.value)
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(raw, ",")))
}

// envDefaultsLdflags centralizes env defaults ldflags behavior so callers follow the same contract.
func (c *Cmd) envDefaultsLdflags(modulePath, encoded string) string {
	return fmt.Sprintf("-X %s/internal/cmd.CompiledEnvDefaultsBase64=%s", modulePath, encoded)
}

// envOverridesLdflags centralizes env overrides ldflags behavior so callers follow the same contract.
func (c *Cmd) envOverridesLdflags(modulePath, encoded string) string {
	return fmt.Sprintf("-X %s/internal/cmd.CompiledEnvOverridesBase64=%s", modulePath, encoded)
}

// withExtraLdflags centralizes with extra ldflags behavior so callers follow the same contract.
func (c *Cmd) withExtraLdflags(args []string, extras ...string) []string {
	ldflagsValue := strings.Join(extras, " ")
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-ldflags" {
			args[i+1] = strings.TrimSpace(args[i+1] + " " + ldflagsValue)
			return args
		}
	}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-ldflags=") {
			current := strings.TrimPrefix(args[i], "-ldflags=")
			args[i] = "-ldflags=" + strings.TrimSpace(current+" "+ldflagsValue)
			return args
		}
	}
	return append([]string{"-ldflags", ldflagsValue}, args...)
}

type envDefaultPair struct {
	key   string
	value string
}

// parseEnvAssignments keeps parse env assignments handling consistent across callers.
func parseEnvAssignments(raw string, flagName string) ([]envDefaultPair, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	entries := strings.Split(trimmed, ",")
	pairs := make([]envDefaultPair, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("%s contains an empty entry", flagName)
		}
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("%s entry %q must be KEY=value", flagName, entry)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s entry %q has an empty key", flagName, entry)
		}
		if strings.ContainsAny(key, " \t\r\n") {
			return nil, fmt.Errorf("%s key %q must not contain whitespace", flagName, key)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%s key %q is duplicated", flagName, key)
		}
		seen[key] = struct{}{}
		pairs = append(pairs, envDefaultPair{
			key:   key,
			value: strings.TrimSpace(value),
		})
	}
	return pairs, nil
}

// modulePath reads linker metadata from the selected project instead of the caller's working directory.
func (c *Cmd) modulePath(root string) string {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return str.Of(line).TrimPrefix("module ").Trim().String()
		}
	}
	return ""
}
