package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
	"github.com/goforj/console"
	"github.com/goforj/env/v2"
	"github.com/goforj/goforj/internal/build"
	"github.com/goforj/goforj/internal/cmd"
	"github.com/goforj/goforj/internal/envcontract"
	"github.com/goforj/goforj/internal/konghelp"
	"github.com/goforj/goforj/internal/launcher"
	"github.com/goforj/goforj/project"
	"github.com/goforj/goforj/version"
	"github.com/goforj/goforj/wire"
	"golang.org/x/term"
)

var cliDefaultedEnv = map[string]bool{}
var cliNativeCommandNames []string

// main initializes the framework CLI and delegates unknown App commands when appropriate.
func main() {
	configureCLIConsole(console.Config{})
	if build.HandleProfileTool(os.Args[1:]) {
		return
	}
	launcher.Capture()
	args := os.Args[1:]
	inGeneratedApp := isGeneratedAppDir()

	// Default environment
	setCLIDefaultEnv("APP_ENV", "local")
	setCLIDefaultEnv("APP_NAME", "GoForj")

	// Initialize application
	app, err := wire.InitializeApplication()
	if err != nil {
		console.Fatalf("initializing application: %v", err)
	}
	app.Logger().Debug().Msg("App initialized")

	// Setup Kong parser
	parser, err := kong.New(
		app.RootCmd(),
		kong.Name("goforj"),
		kong.Description("GoForj CLI\n  The composable stack for building with Go."),
		kong.Help(konghelp.FrameworkFormatter),
		kong.Vars{
			"version": version.String(),
		},
	)
	if err != nil {
		console.Fatalf("setting up CLI parser: %v", err)
	}
	cliNativeCommandNames = nativeCommandNames(parser.Model.Node)

	appContext := ""
	if appName, remaining, ok := resolveAppPrefix(args, inGeneratedApp); ok {
		if shouldRunFrameworkCommandWithAppEnv(remaining) {
			applySourceAppEnv(appName)
			if err := loadAppScopedEnv(); err != nil {
				console.Fatalf("loading app environment: %v", err)
			}
			appContext = appName
			args = remaining
		} else if shouldRunAppThroughSource(appName, remaining, inGeneratedApp) {
			applySourceAppEnv(appName)
			appContext = appName
			args = remaining
		} else if runAppBinary(appName, remaining) {
			return
		} else {
			applySourceAppEnv(appName)
			appContext = appName
			args = remaining
		}
	}
	args = normalizeLegacyLongAliases(args)
	args = insertBuildPassthroughBoundary(args)
	app.RootCmd().RootCmd.RunCmd.Env = delegatedAppEnv()

	if isRootHelp(args) {
		if appContext != "" {
			if err := runAppCommandThroughSource(app.RootCmd(), appRootHelpArgs()); err != nil {
				if code, ok := build.ChildExitCode(err); ok {
					os.Exit(code)
				}
				console.Fatalf("%v", err)
			}
		} else {
			var apps []string
			if inGeneratedApp {
				apps = conventionalAppHelpApps(inGeneratedApp)
				if err := ensureGeneratedAppHelpBinaries(apps, runAppHelpBuild); err != nil {
					console.Fatalf("preparing App help: %v", err)
				}
			}
			printRootHelp(parser)
			if inGeneratedApp {
				printAppUsageHelp(apps)
				if err := printGeneratedAppHelp(apps); err != nil {
					if code, ok := build.ChildExitCode(err); ok {
						os.Exit(code)
					}
					console.Fatalf("%v", err)
				}
			}
		}
		return
	}

	// Parse CLI args
	ctx, err := parser.Parse(args)
	if err != nil {
		if shouldDelegateToAppCommand(args, err, inGeneratedApp) {
			if err := runAppCommandThroughSource(app.RootCmd(), args); err != nil {
				if code, ok := build.ChildExitCode(err); ok {
					os.Exit(code)
				}
				console.Fatalf("%v", err)
			}
			return
		}
		console.Fatalf("%v", err)
	}
	app.RootCmd().RootCmd.BuildCmd.Args = buildPassthroughArgs(app.RootCmd().RootCmd.BuildCmd.Args)
	if err := initializeParsedEnvironment(args, app.RootCmd(), inGeneratedApp); err != nil {
		console.Fatalf("initializing local environment: %v", err)
	}

	// Execute command
	err = ctx.Run()
	if err != nil {
		if code, ok := build.ChildExitCode(err); ok {
			os.Exit(code)
		}
		console.Fatalf("%v", err)
	}
}

// initializeParsedEnvironment creates local state only after Kong has validated the requested command and flags.
func initializeParsedEnvironment(args []string, root *cmd.RootCmd, inGeneratedApp bool) error {
	if readOnlyCLIRequest(args) || strings.TrimSpace(os.Getenv("FORJ_COMMAND_ORIGIN")) == build.AppHelpCommandOrigin {
		return nil
	}
	command := firstCLICommand(args)
	switch command {
	case "build", "generate", "dev", "run":
		// These are the natural project entry points that require a usable local environment.
	default:
		return nil
	}
	projectRoot := "."
	switch command {
	case "build":
		projectRoot = root.RootCmd.BuildCmd.Root
	case "run":
		projectRoot = root.RootCmd.RunCmd.Root
	default:
		if !inGeneratedApp {
			return nil
		}
	}
	if !isGeneratedAppRoot(projectRoot) {
		return nil
	}
	return initializeEnvironmentAt(projectRoot)
}

// initializeEnvironmentAt creates one private local file and reports the onboarding action without exposing values.
func initializeEnvironmentAt(root string) error {
	created, err := envcontract.Initialize(root)
	if err != nil {
		return err
	}
	if created {
		console.Successf("Created .env with fresh local secrets")
	}
	return nil
}

// firstCLICommand finds the parsed command token after inherited boolean flags.
func firstCLICommand(args []string) string {
	for _, argument := range args {
		argument = strings.TrimSpace(argument)
		if argument == "" || strings.HasPrefix(argument, "-") {
			continue
		}
		return argument
	}
	return ""
}

// readOnlyCLIRequest recognizes help and version requests anywhere before a passthrough boundary.
func readOnlyCLIRequest(args []string) bool {
	for _, argument := range args {
		switch strings.TrimSpace(argument) {
		case "--":
			return false
		case "--help", "-h", "help", "--version":
			return true
		}
	}
	return false
}

// configureCLIConsole enables terminal-owned progress without changing redirected or unsupported terminal output.
func configureCLIConsole(config console.Config) {
	enabled := true
	config.TerminalProgressEnabled = &enabled
	console.SetDefault(console.New(config))
}

// shouldRunFrameworkCommandWithAppEnv keeps framework-owned backup commands in the framework process.
func shouldRunFrameworkCommandWithAppEnv(args []string) bool {
	return len(args) > 0 && strings.HasPrefix(strings.TrimSpace(args[0]), "backup:")
}

// loadAppScopedEnv loads project env files and promotes the selected App's prefixed values.
func loadAppScopedEnv() error {
	if err := env.Reload(); err != nil {
		return err
	}
	prefix := project.AppEnvironmentPrefix(os.Getenv("FORJ_APP"))
	if prefix == "" {
		return nil
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, prefix+"_") {
			continue
		}
		baseKey := strings.TrimPrefix(key, prefix+"_")
		if baseKey == "" || baseKey == "FORJ_APP" {
			continue
		}
		if err := os.Setenv(baseKey, value); err != nil {
			return fmt.Errorf("set app environment %s from %s: %w", baseKey, key, err)
		}
	}
	return nil
}

// normalizeLegacyLongAliases preserves existing invocations after Kong changed single-character aliases into short flags.
func normalizeLegacyLongAliases(args []string) []string {
	normalized := args
	copied := false
	for index, argument := range args {
		if argument == "--" {
			break
		}
		replacement := ""
		switch {
		case argument == "--x":
			replacement = "--dev"
		case strings.HasPrefix(argument, "--x="):
			replacement = "--dev=" + strings.TrimPrefix(argument, "--x=")
		default:
			continue
		}
		if !copied {
			normalized = append([]string(nil), args...)
			copied = true
		}
		normalized[index] = replacement
	}
	return normalized
}

// insertBuildPassthroughBoundary prevents Kong from splitting Go's single-dash flags into framework short options.
func insertBuildPassthroughBoundary(args []string) []string {
	buildIndex := buildCommandIndex(args)
	if buildIndex < 0 || buildIndex+1 >= len(args) {
		return args
	}
	for index := buildIndex + 1; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		if argument == "--" {
			return args
		}
		valueCount, frameworkFlag := buildFrameworkFlagValueCount(argument)
		if frameworkFlag {
			index += valueCount
			continue
		}
		normalized := make([]string, 0, len(args)+1)
		normalized = append(normalized, args[:index]...)
		normalized = append(normalized, "--")
		normalized = append(normalized, args[index:]...)
		return normalized
	}
	return args
}

// buildCommandIndex locates build after root flags because Kong accepts inherited flags before a command.
func buildCommandIndex(args []string) int {
	for index := 0; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		if buildRootFlag(argument) {
			continue
		}
		if argument == "build" {
			return index
		}
		return -1
	}
	return -1
}

// buildPassthroughArgs removes the parser sentinel while preserving every raw Go build argument after it.
func buildPassthroughArgs(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

// buildFrameworkFlagValueCount identifies flags Kong must parse before raw Go build arguments begin.
func buildFrameworkFlagValueCount(argument string) (int, bool) {
	name, _, hasInlineValue := strings.Cut(argument, "=")
	switch name {
	case "--timings", "--api-index-strict", "--skip-wire", "--profile", "--help", "-h",
		"--dev", "--x", "--version":
		return 0, true
	case "--env-defaults", "--env-overrides", "--stack", "--top", "--root":
		if hasInlineValue {
			return 0, true
		}
		return 1, true
	default:
		return 0, false
	}
}

// buildRootFlag identifies inherited root flags that may legally precede the build command.
func buildRootFlag(argument string) bool {
	name, _, _ := strings.Cut(argument, "=")
	switch name {
	case "--dev", "--x", "-x", "--version", "--help", "-h":
		return true
	default:
		return false
	}
}

// resolveAppPrefix strips a conventional app prefix while preserving native command precedence.
func resolveAppPrefix(args []string, inGeneratedApp bool) (string, []string, bool) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", args, false
	}
	appName := strings.TrimSpace(args[0])
	if !project.IsSafeAppName(appName) || isNativeCommandName(appName) {
		return "", args, false
	}
	if regularFileExists(filepath.Join(".", "bin", appName)) {
		return appName, args[1:], true
	}
	if inGeneratedApp && isConventionalSourceApp(appName) {
		return appName, args[1:], true
	}
	return "", args, false
}

// shouldRunAppThroughSource keeps source-tree app commands current when app binaries may be stale.
func shouldRunAppThroughSource(appName string, args []string, inGeneratedApp bool) bool {
	if shouldRunAppNativeCommand(args) {
		return true
	}
	return inGeneratedApp && isConventionalSourceApp(appName)
}

// shouldRunAppNativeCommand keeps framework-owned commands app-scoped instead of delegating them to app binaries.
func shouldRunAppNativeCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return isNativeCommandName(args[0])
}

// conventionalAppHelpApps discovers the apps that should be visible from `forj --help`.
func conventionalAppHelpApps(inGeneratedApp bool) []string {
	if !inGeneratedApp {
		return nil
	}

	seen := map[string]struct{}{}
	apps := []string{}
	appendConventionalAppHelpApp(&apps, seen, project.DefaultAppName)

	appendConventionalAppHelpAppsFromDir(&apps, seen, "cmd")
	appendConventionalBinaryHelpApps(&apps, seen, filepath.Join(".", "bin"))

	sort.Strings(apps)
	return apps
}

// appendConventionalAppHelpAppsFromDir treats cmd/<app>/main.go as a source-owned app.
func appendConventionalAppHelpAppsFromDir(apps *[]string, seen map[string]struct{}, root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		appName := entry.Name()
		if !regularFileExists(filepath.Join(root, appName, "main.go")) {
			continue
		}
		appendConventionalAppHelpApp(apps, seen, appName)
	}
}

// appendConventionalBinaryHelpApps keeps binary-only apps visible after a build.
func appendConventionalBinaryHelpApps(apps *[]string, seen map[string]struct{}, root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		appendConventionalAppHelpApp(apps, seen, entry.Name())
	}
}

// appendConventionalAppHelpApp applies the same safety and native-command precedence rules as app dispatch.
func appendConventionalAppHelpApp(apps *[]string, seen map[string]struct{}, appName string) {
	appName = strings.TrimSpace(appName)
	if appName == "" || !project.IsSafeAppName(appName) || project.IsReservedAppName(appName) {
		return
	}
	if appName != project.DefaultAppName && isNativeCommandName(appName) {
		return
	}
	if _, ok := seen[appName]; ok {
		return
	}
	seen[appName] = struct{}{}
	*apps = append(*apps, appName)
}

// ensureGeneratedAppHelpBinaries builds source-owned Apps only when their help binary is absent.
func ensureGeneratedAppHelpBinaries(apps []string, buildApp func(string) error) error {
	for _, appName := range apps {
		binPath := filepath.Join(".", "bin", appName)
		if regularFileExists(binPath) || !isConventionalSourceApp(appName) {
			continue
		}
		invocation := appHelpBuildInvocation(appName)
		console.Infof("App binary not found; running %s", invocation)
		if err := buildApp(appName); err != nil {
			return fmt.Errorf("%s: %w", invocation, err)
		}
		if !regularFileExists(binPath) {
			return fmt.Errorf("%s completed without producing %s", invocation, filepath.ToSlash(binPath))
		}
	}
	return nil
}

// runAppHelpBuild invokes the normal App-scoped build path in an isolated process.
func runAppHelpBuild(appName string) error {
	command := exec.Command(os.Args[0], appHelpBuildArgs(appName)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = withAppHelpBuildOrigin(appCommandEnv(appName))
	return command.Run()
}

// withAppHelpBuildOrigin marks only the bootstrap subprocess while replacing any inherited command origin.
func withAppHelpBuildOrigin(env []string) []string {
	const key = "FORJ_COMMAND_ORIGIN"
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, prefix+build.AppHelpCommandOrigin)
}

// appHelpBuildArgs returns the public build invocation for a missing App binary.
func appHelpBuildArgs(appName string) []string {
	if strings.TrimSpace(appName) == project.DefaultAppName {
		return []string{"build"}
	}
	return []string{strings.TrimSpace(appName), "build"}
}

// appHelpBuildInvocation formats the command shown before a first-help build.
func appHelpBuildInvocation(appName string) string {
	return "forj " + strings.Join(appHelpBuildArgs(appName), " ")
}

// isConventionalSourceApp reports whether cmd/<app>/main.go defines an app.
func isConventionalSourceApp(appName string) bool {
	appName = strings.TrimSpace(appName)
	if !project.IsSafeAppName(appName) {
		return false
	}
	return regularFileExists(filepath.Join(".", "cmd", appName, "main.go"))
}

// isNativeCommandName reports whether name belongs to the framework CLI grammar.
func isNativeCommandName(name string) bool {
	name = strings.TrimSpace(name)
	for _, native := range cliNativeCommandNames {
		if native == name {
			return true
		}
	}
	return false
}

// runAppBinary delegates `forj <app> ...` to ./bin/<app> when that app exists.
func runAppBinary(appName string, args []string) bool {
	if err := runAppBinaryCommand(appName, args); err != nil {
		if errors.Is(err, errAppBinaryNotFound) {
			return false
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		console.Fatalf("%v", err)
	}
	return true
}

// errAppBinaryNotFound lets app-prefix dispatch fall back to source-mode handling.
var errAppBinaryNotFound = errors.New("app binary not found")

// runAppBinaryCommand delegates to ./bin/<app> while leaving exit handling to the caller.
func runAppBinaryCommand(appName string, args []string) error {
	appName = strings.TrimSpace(appName)
	binPath := filepath.Join(".", "bin", appName)
	if !regularFileExists(binPath) {
		return errAppBinaryNotFound
	}
	command := exec.Command(binPath, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = appCommandEnv(appName)
	if err := command.Run(); err != nil {
		return err
	}
	return nil
}

// applySourceAppEnv makes native source-mode commands operate against the selected app.
func applySourceAppEnv(appName string) {
	appName = strings.TrimSpace(appName)
	_ = os.Setenv("FORJ_COMMAND_PREFIX", "forj "+appName)
	_ = os.Setenv("FORJ_APP", appName)
}

// appCommandEnv marks delegated app commands, with the explicit CLI app taking precedence.
func appCommandEnv(appName string) []string {
	return withAppEnv(os.Environ(), strings.TrimSpace(appName))
}

// withAppEnv overlays app identity onto an environment slice.
func withAppEnv(env []string, appName string) []string {
	updates := map[string]string{
		"FORJ_COMMAND_PREFIX": "forj " + appName,
		"FORJ_APP":            appName,
	}
	out := make([]string, 0, len(env)+len(updates))
	seen := map[string]struct{}{}
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if value, replace := updates[key]; replace {
				out = append(out, key+"="+value)
				seen[key] = struct{}{}
				continue
			}
		}
		out = append(out, entry)
	}
	for key, value := range updates {
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, key+"="+value)
	}
	return out
}

// setCLIDefaultEnv sets a framework default and records that it should not leak into delegated App commands.
func setCLIDefaultEnv(key, value string) {
	if _, ok := os.LookupEnv(key); ok {
		return
	}
	_ = os.Setenv(key, value)
	cliDefaultedEnv[key] = true
}

// isRootHelp reports whether args request the root help screen.
func isRootHelp(args []string) bool {
	return len(args) == 0 || len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")
}

// appRootHelpArgs returns the generated App args used for root help delegation.
func appRootHelpArgs() []string {
	return []string{"--help"}
}

// printAppUsageHelp explains app prefixing once when a project has multiple apps.
func printAppUsageHelp(apps []string) {
	if len(apps) <= 1 {
		return
	}
	fmt.Println()
	fmt.Println(console.Colorize(console.ColorBoldWhite, "app usage"))
	renderAppUsageRow("forj <app> <command>", "Run a command for a specific app")
	renderAppUsageRow("forj <app> build", "Build a specific app binary")
	renderAppUsageRow("forj dev", "Build and run all apps in development")
}

// renderAppUsageRow prints one compact root help example row.
func renderAppUsageRow(command string, help string) {
	const width = 23
	spacing := strings.Repeat(" ", width-len(command)+2)
	fmt.Printf("  %s%s%s\n",
		console.Colorize(console.ColorBoldGreen, command),
		spacing,
		console.Colorize(console.ColorGray, help),
	)
}

// printGeneratedAppHelp prints generated app help screens in app order after collecting them concurrently.
func printGeneratedAppHelp(apps []string) error {
	results := generatedAppHelpResults(collectGeneratedAppHelp(apps))
	if output, ok := compactGeneratedAppHelp(results); ok {
		fmt.Print(output)
		return nil
	}
	for _, result := range results {
		fmt.Println()
		fmt.Print(result.output)
	}
	return nil
}

// generatedAppHelpResults keeps root help aggregation limited to GoForj-generated app help.
func generatedAppHelpResults(results []appHelpResult) []appHelpResult {
	filtered := make([]appHelpResult, 0, len(results))
	for _, result := range results {
		if result.err != nil {
			continue
		}
		if _, ok := parseGeneratedAppHelp(result.app, result.output); !ok {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

type appHelpResult struct {
	app    string
	output string
	err    error
}

type appHelpCommand struct {
	section string
	name    string
	help    string
}

type parsedAppHelp struct {
	app      string
	title    string
	baseName string
	commands []appHelpCommand
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

const (
	appHelpTimeout   = 1500 * time.Millisecond
	appHelpWaitDelay = 100 * time.Millisecond
)

// compactGeneratedAppHelp folds identical generated app command surfaces into one shared help block.
func compactGeneratedAppHelp(results []appHelpResult) (string, bool) {
	if len(results) <= 1 {
		return "", false
	}
	parsed := make([]parsedAppHelp, 0, len(results))
	for _, result := range results {
		help, ok := parseGeneratedAppHelp(result.app, result.output)
		if !ok {
			return "", false
		}
		parsed = append(parsed, help)
	}

	shared := sharedAppHelpCommands(parsed)
	if len(shared) == 0 {
		return "", false
	}

	baseName := parsed[0].baseName
	var out strings.Builder
	renderAppHelpBlock(&out, baseName+" · available in all apps", shared)
	for _, help := range parsed {
		delta := appHelpDelta(help.commands, shared)
		if len(delta) == 0 {
			continue
		}
		renderAppHelpBlock(&out, baseName+" · "+help.app, delta)
	}
	return out.String(), true
}

// parseGeneratedAppHelp extracts command rows from the generated app root help format.
func parseGeneratedAppHelp(appName string, output string) (parsedAppHelp, bool) {
	lines := strings.Split(stripANSI(output), "\n")
	help := parsedAppHelp{app: strings.TrimSpace(appName)}
	currentSection := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if help.title == "" {
			help.title = strings.TrimSpace(line)
			help.baseName = generatedHelpBaseName(help.title)
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			command, ok := parseGeneratedAppHelpCommand(currentSection, line)
			if ok {
				help.commands = append(help.commands, command)
			}
			continue
		}
		currentSection = strings.TrimSpace(line)
	}
	return help, help.title != "" && help.baseName != "" && len(help.commands) > 0
}

// parseGeneratedAppHelpCommand parses one aligned command row from generated app help.
func parseGeneratedAppHelpCommand(section string, line string) (appHelpCommand, bool) {
	section = strings.TrimSpace(section)
	if section == "" {
		return appHelpCommand{}, false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return appHelpCommand{}, false
	}
	name, help, found := strings.Cut(trimmed, " ")
	if !found || name == "" {
		return appHelpCommand{}, false
	}
	return appHelpCommand{section: section, name: name, help: strings.TrimSpace(help)}, true
}

// generatedHelpBaseName returns the app name before the optional app qualifier.
func generatedHelpBaseName(title string) string {
	title = strings.TrimSpace(title)
	if before, _, ok := strings.Cut(title, " · "); ok {
		return strings.TrimSpace(before)
	}
	if before, _, ok := strings.Cut(title, " | "); ok {
		return strings.TrimSpace(before)
	}
	return title
}

// sharedAppHelpCommands returns commands that are exactly present in every parsed app.
func sharedAppHelpCommands(parsed []parsedAppHelp) []appHelpCommand {
	counts := map[string]int{}
	commands := map[string]appHelpCommand{}
	for _, help := range parsed {
		seen := map[string]struct{}{}
		for _, command := range help.commands {
			key := appHelpCommandKey(command)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			counts[key]++
			commands[key] = command
		}
	}
	shared := make([]appHelpCommand, 0)
	for key, count := range counts {
		if count == len(parsed) {
			shared = append(shared, commands[key])
		}
	}
	sortAppHelpCommands(shared)
	return shared
}

// appHelpDelta removes shared commands from one app's command list.
func appHelpDelta(commands []appHelpCommand, shared []appHelpCommand) []appHelpCommand {
	sharedKeys := map[string]struct{}{}
	for _, command := range shared {
		sharedKeys[appHelpCommandKey(command)] = struct{}{}
	}
	delta := make([]appHelpCommand, 0)
	for _, command := range commands {
		if _, ok := sharedKeys[appHelpCommandKey(command)]; ok {
			continue
		}
		delta = append(delta, command)
	}
	sortAppHelpCommands(delta)
	return delta
}

// renderAppHelpBlock writes one compact app help block with the same simple grouping as generated help.
func renderAppHelpBlock(out *strings.Builder, title string, commands []appHelpCommand) {
	if len(commands) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, console.Colorize(console.ColorBoldWhite, title))
	fmt.Fprintln(out)
	sections := map[string][]appHelpCommand{}
	maxLen := 0
	for _, command := range commands {
		sections[command.section] = append(sections[command.section], command)
		if len(command.name) > maxLen {
			maxLen = len(command.name)
		}
	}
	for _, section := range sortedStringKeys(sections) {
		fmt.Fprintln(out, console.Colorize(console.ColorBoldWhite, section))
		for _, command := range sections[section] {
			spacing := strings.Repeat(" ", maxLen-len(command.name)+2)
			fmt.Fprintf(out, "  %s%s%s\n",
				console.Colorize(console.ColorBoldGreen, command.name),
				spacing,
				console.Colorize(console.ColorGray, command.help),
			)
		}
	}
}

// sortAppHelpCommands keeps compact help deterministic by section then command name.
func sortAppHelpCommands(commands []appHelpCommand) {
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].section != commands[j].section {
			return commands[i].section < commands[j].section
		}
		return commands[i].name < commands[j].name
	})
}

// sortedStringKeys returns sorted map keys for compact help rendering.
func sortedStringKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// appHelpCommandKey identifies a command row exactly, including its section and description.
func appHelpCommandKey(command appHelpCommand) string {
	return command.section + "\x00" + command.name + "\x00" + command.help
}

// stripANSI removes color escape sequences before parsing generated help output.
func stripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

// collectGeneratedAppHelp shells out per app so help rendering can run in parallel without sharing parser state.
func collectGeneratedAppHelp(apps []string) []appHelpResult {
	results := make([]appHelpResult, len(apps))
	multi := len(apps) > 1
	var wait sync.WaitGroup
	for index, appName := range apps {
		wait.Add(1)
		go func(index int, appName string, multi bool) {
			defer wait.Done()
			output, err := runAppHelpForApp(appName, multi)
			results[index] = appHelpResult{app: appName, output: output, err: err}
		}(index, appName, multi)
	}
	wait.Wait()
	return results
}

// runAppHelpForApp reads help only from an existing app binary so root help remains a read-only operation.
func runAppHelpForApp(appName string, multi bool) (string, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = project.DefaultAppName
	}
	binPath := filepath.Join(".", "bin", appName)
	if !regularFileExists(binPath) {
		return "", errAppBinaryNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), appHelpTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binPath, "--help")
	command.WaitDelay = appHelpWaitDelay
	command.Env = withAppEnv(delegatedAppEnv(), appName)
	if multi {
		command.Env = append(command.Env, "FORJ_MULTI_APP_HELP=1")
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return output.String(), fmt.Errorf("read %s app help: %w", appName, ctx.Err())
		}
		return output.String(), err
	}
	return output.String(), nil
}

// printRootHelp prints native GoForj help.
func printRootHelp(parser *kong.Kong) {
	ctx, _ := kong.Trace(parser, []string{})
	ctx.PrintUsage(false)
}

// shouldDelegateToAppCommand reports whether an unresolved native command should run as an App command.
func shouldDelegateToAppCommand(args []string, parseErr error, inGeneratedApp bool) bool {
	if !inGeneratedApp || parseErr == nil || len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return false
	}
	message := strings.ToLower(parseErr.Error())
	return strings.Contains(message, "unexpected argument") || strings.Contains(message, "unknown command")
}

// runAppCommandThroughSource runs a generated App command through the same source path as forj run.
func runAppCommandThroughSource(root *cmd.RootCmd, args []string) error {
	return runAppCommandThroughSourceWithEnv(root, args, delegatedAppEnv())
}

// runAppCommandThroughSourceWithEnv runs source-mode app commands with an explicit environment overlay.
func runAppCommandThroughSourceWithEnv(root *cmd.RootCmd, args []string, env []string) error {
	run := &root.RootCmd.RunCmd
	run.Root = "."
	run.Args = append([]string(nil), args...)
	run.Env = env
	run.PreserveTTY = true
	return run.Run()
}

// delegatedAppEnv returns the environment passed to source-run generated App commands.
func delegatedAppEnv() []string {
	env := os.Environ()
	if len(cliDefaultedEnv) != 0 {
		filtered := env[:0]
		for _, entry := range env {
			key, _, ok := strings.Cut(entry, "=")
			if ok && cliDefaultedEnv[key] {
				continue
			}
			filtered = append(filtered, entry)
		}
		env = filtered
	}
	if _, ok := os.LookupEnv("FORJ_COMMAND_PREFIX"); !ok {
		env = append(env, "FORJ_COMMAND_PREFIX=forj")
	}
	if len(cliNativeCommandNames) > 0 {
		env = append(env, "FORJ_NATIVE_COMMAND_NAMES="+strings.Join(cliNativeCommandNames, ","))
	}
	if shouldForceDelegatedAppColor(term.IsTerminal(int(os.Stdout.Fd()))) {
		env = append(env, "CLICOLOR_FORCE=1")
	}
	return env
}

// nativeCommandNames returns every native command name and alias exposed by the framework CLI.
func nativeCommandNames(node *kong.Node) []string {
	seen := map[string]struct{}{}
	collectNativeCommandNames(node, seen)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// collectNativeCommandNames walks the Kong command tree and records command names and aliases.
func collectNativeCommandNames(node *kong.Node, names map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Type == kong.CommandNode {
		if clean := strings.TrimSpace(node.Name); clean != "" {
			names[clean] = struct{}{}
		}
		for _, alias := range node.Aliases {
			if clean := strings.TrimSpace(alias); clean != "" {
				names[clean] = struct{}{}
			}
		}
	}
	for _, child := range node.Children {
		collectNativeCommandNames(child, names)
	}
}

// shouldForceDelegatedAppColor reports whether delegated app commands should preserve terminal color.
func shouldForceDelegatedAppColor(stdoutTTY bool) bool {
	if !stdoutTTY {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if _, ok := os.LookupEnv("CLICOLOR_FORCE"); ok {
		return false
	}
	return true
}

// isGeneratedAppDir reports whether the current directory looks like a generated App root.
func isGeneratedAppDir() bool {
	return isGeneratedAppRoot(".")
}

// isGeneratedAppRoot reports whether a selected command root contains the generated project markers.
func isGeneratedAppRoot(root string) bool {
	return regularFileExists(filepath.Join(root, ".goforj.yml")) && regularFileExists(filepath.Join(root, "go.mod"))
}

// regularFileExists reports whether path exists and is not a directory.
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
