package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goforj/str/v2"

	"github.com/goforj/console"
	"github.com/goforj/goforj/internal/apiindex"
	"github.com/goforj/goforj/internal/appassets"
	"github.com/goforj/goforj/internal/generate"
	"github.com/goforj/goforj/internal/logger"
	"github.com/goforj/goforj/project"
	"golang.org/x/term"
)

// Step names one observable pipeline action whose runner receives the validated project root.
type Step struct {
	Name string
	Run  func(root string) (string, error)
}

// Pipeline coordinates source generation, indexing, and the caller's final build or launch step.
type Pipeline struct {
	stackEnvironment map[string]string
	logger           *logger.AppLogger
	apiIndex         apiindex.Preparer
	prepareAssets    func(root string, assets []appassets.Asset) (string, error)
	prepareWire      func(root string) (string, error)
}

const buildProgressMarker = "__FORJ_BUILD_PROGRESS__"

type buildProgressReporter interface {
	// Step defines the step behavior required from implementations.
	Step(index int, total int, step string)
	// State defines the state behavior required from implementations.
	State(state string)
}

type buildProgressNoop struct{}

// Step centralizes step behavior so callers follow the same contract.
func (buildProgressNoop) Step(int, int, string) {}

// State centralizes state behavior so callers follow the same contract.
func (buildProgressNoop) State(string) {}

type buildProgressMarkerReporter struct{}

// Step centralizes step behavior so callers follow the same contract.
func (buildProgressMarkerReporter) Step(index int, total int, step string) {
	fmt.Fprintf(os.Stderr, "%s step %d/%d %s\n", buildProgressMarker, index, total, strings.TrimSpace(step))
}

// State centralizes state behavior so callers follow the same contract.
func (buildProgressMarkerReporter) State(state string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", buildProgressMarker, strings.TrimSpace(state))
}

type buildProgressTTYReporter struct {
	mu          sync.Mutex
	loader      *console.Loader
	started     bool
	clearOnDone bool
}

// Step starts the shared console loader on its first update and changes its durable label thereafter.
func (r *buildProgressTTYReporter) Step(index int, total int, step string) {
	r.loader.Update(fmt.Sprintf("%d/%d %s", index, total, strings.TrimSpace(step)))
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()
	// The reporter owns a dedicated console, so no other transient display can contend with it.
	_ = r.loader.Start()
}

// State turns the live loader into the terminal outcome expected by the pipeline mode.
func (r *buildProgressTTYReporter) State(state string) {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return
	}
	r.started = false
	r.mu.Unlock()
	if strings.EqualFold(strings.TrimSpace(state), "done") {
		if r.clearOnDone {
			r.loader.Stop()
			return
		}
		r.loader.Success("")
		return
	}
	r.loader.Fail("")
}

// RunOptions controls diagnostics, source selection, and progress behavior for one pipeline invocation.
type RunOptions struct {
	Timings  bool
	SkipWire bool
	// PreparationOnly stops after generation, templ, and Wire have stabilized project inputs.
	PreparationOnly bool
	// SkipPreparation starts from API indexing when a dev build wave already stabilized project inputs.
	SkipPreparation bool
	// APIIndexStrict fails the pipeline when web indexing reports warnings or errors.
	APIIndexStrict bool
	// BuildTags keeps API indexing on the same conditional source surface as the final Go build.
	BuildTags                []string
	TransientProgress        bool
	ClearProgressBeforeFinal bool
}

// NewPipeline creates a build pipeline whose index candidates share the final step's success boundary.
func NewPipeline(appLogger *logger.AppLogger, apiIndex apiindex.Preparer) Pipeline {
	pipeline := Pipeline{
		logger:        appLogger,
		apiIndex:      apiIndex,
		prepareAssets: prepareAppAssets,
	}
	pipeline.prepareWire = pipeline.runWireGenerate
	return pipeline
}

// Run executes the configured steps from the project root and publishes API artifacts after the final step succeeds.
func (p Pipeline) Run(root string, kind string, final Step, opts RunOptions) (err error) {
	if opts.PreparationOnly && opts.SkipPreparation {
		return fmt.Errorf("pipeline cannot prepare and skip preparation in the same run")
	}
	if err := apiindex.ValidateGOFLAGS(os.Getenv("GOFLAGS")); err != nil {
		return err
	}
	absRoot, err := resolveProjectRoot(root)
	if err != nil {
		return err
	}

	debug := debugEnabled()
	progress := newBuildProgressReporter(debug, opts)
	progressState := "done"
	defer func() {
		progress.State(progressState)
	}()
	var pendingAPIIndex apiindex.Candidate
	defer func() {
		if pendingAPIIndex != nil {
			if discardErr := pendingAPIIndex.Discard(); discardErr != nil {
				progressState = "failed"
				err = errors.Join(err, discardErr)
			}
		}
	}()
	steps := make([]Step, 0, 4)
	var assets []appassets.Asset
	if !opts.SkipPreparation {
		steps = append(steps, Step{Name: "generate", Run: p.generateProjectFiles})
		if buildUsesTemplHTMX(absRoot) {
			steps = append(steps, Step{Name: "templ", Run: p.runTemplGenerate})
		}
		if !opts.PreparationOnly {
			var assetErr error
			assets, assetErr = configuredAppAssets(absRoot)
			if assetErr != nil {
				return assetErr
			}
		}
		if !opts.SkipWire && (opts.PreparationOnly || len(assets) == 0) {
			steps = append(steps, Step{Name: "wire", Run: p.prepareWire})
		}
	}
	if !opts.PreparationOnly {
		if len(assets) > 0 {
			stepName := "assets + api-index"
			if !opts.SkipWire {
				stepName = "assets + wire + api-index"
			}
			steps = append(steps, Step{Name: stepName, Run: func(root string) (string, error) {
				status, candidate, err := p.prepareAssetsWireAndAPIIndex(root, assets, !opts.SkipWire, opts.APIIndexStrict, opts.BuildTags...)
				pendingAPIIndex = candidate
				return status, err
			}})
		} else {
			steps = append(steps, Step{Name: "build:api-index", Run: func(root string) (string, error) {
				preparation, err := p.prepareAPIIndex(root, opts.APIIndexStrict, opts.BuildTags...)
				pendingAPIIndex = preparation.Candidate
				return preparation.Status, err
			}})
		}
		steps = append(steps, final)
	}
	for index, step := range steps {
		if debug {
			p.logger.Info().Str("kind", kind).Str("step", step.Name).Msg("Running pipeline step")
		}
		progress.Step(index+1, len(steps), step.Name)
		if !opts.PreparationOnly && index == len(steps)-1 && opts.ClearProgressBeforeFinal {
			progress.State("done")
		}
		startedAt := time.Now()
		var status string
		if !opts.PreparationOnly && index == len(steps)-1 {
			status, err = runFinalAndPublishAPIIndex(absRoot, step, pendingAPIIndex)
		} else {
			status, err = step.Run(absRoot)
		}
		if err != nil {
			progressState = "failed"
			return err
		}
		if opts.Timings {
			printStepTiming(kind, step.Name, time.Since(startedAt), status)
		}
	}
	if debug {
		p.logger.Info().Str("kind", kind).Int("steps", len(steps)).Msg("Pipeline completed")
	}
	return nil
}

type assetPreparationResult struct {
	status string
	err    error
}

type apiIndexPreparationResult struct {
	preparation apiindex.Preparation
	err         error
}

// prepareAssetsWireAndAPIIndex overlaps assets with Go preparation while keeping Wire's writes ahead of API-index reads.
func (p Pipeline) prepareAssetsWireAndAPIIndex(root string, assets []appassets.Asset, runWire bool, strict bool, buildTags ...string) (string, apiindex.Candidate, error) {
	assetResults := make(chan assetPreparationResult, 1)
	go func() {
		status, err := p.prepareAssets(root, assets)
		assetResults <- assetPreparationResult{status: status, err: err}
	}()
	wireStatus := ""
	var wireErr error
	if runWire {
		wireStatus, wireErr = p.prepareWire(root)
	}
	apiIndexResult := apiIndexPreparationResult{}
	if wireErr == nil {
		apiIndexResult.preparation, apiIndexResult.err = p.prepareAPIIndex(root, strict, buildTags...)
	}
	assetsResult := <-assetResults
	statuses := make([]string, 0, 3)
	if strings.TrimSpace(assetsResult.status) != "" {
		statuses = append(statuses, "assets "+assetsResult.status)
	}
	if strings.TrimSpace(wireStatus) != "" {
		statuses = append(statuses, "wire "+wireStatus)
	}
	if strings.TrimSpace(apiIndexResult.preparation.Status) != "" {
		statuses = append(statuses, "api-index "+apiIndexResult.preparation.Status)
	}
	return strings.Join(statuses, ", "), apiIndexResult.preparation.Candidate, errors.Join(assetsResult.err, wireErr, apiIndexResult.err)
}

// resolveProjectRoot validates the explicit command root once so every pipeline step shares one stable absolute path.
func resolveProjectRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root %q: %w", root, err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", fmt.Errorf("open project root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("open project root %q: not a directory", root)
	}
	return absRoot, nil
}

// runFinalAndPublishAPIIndex keeps the previous active contract until compilation or process startup succeeds.
func runFinalAndPublishAPIIndex(root string, final Step, pending apiindex.Candidate) (string, error) {
	status, err := final.Run(root)
	if err != nil {
		return "", err
	}
	if pending != nil {
		if err := pending.Publish(); err != nil {
			return "", err
		}
	}
	return status, nil
}

// prepareAPIIndex applies the command's diagnostics policy while keeping pipeline status formatting centralized.
func (p Pipeline) prepareAPIIndex(root string, strict bool, buildTags ...string) (apiindex.Preparation, error) {
	preparation, err := p.apiIndex.Prepare(apiindex.Options{Root: root, Strict: strict, BuildTags: append([]string(nil), buildTags...)})
	if err != nil {
		return preparation, fmt.Errorf("%s: %w", preparation.Status, err)
	}
	return preparation, nil
}

// buildProgressEnabled keeps the build progress enabled representation consistent.
func buildProgressEnabled() bool {
	value := strings.TrimSpace(os.Getenv("FORJ_BUILD_PROGRESS"))
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
}

// newBuildProgressReporter selects private marker output, durable output, or an interactive console loader.
func newBuildProgressReporter(debug bool, opts RunOptions) buildProgressReporter {
	if buildProgressEnabled() {
		return buildProgressMarkerReporter{}
	}
	if debug || opts.Timings || !term.IsTerminal(int(os.Stderr.Fd())) {
		return buildProgressNoop{}
	}
	progressConsole := console.New(console.Config{
		Stdout:         os.Stderr,
		Stderr:         os.Stderr,
		LoaderInterval: 120 * time.Millisecond,
	})
	return &buildProgressTTYReporter{
		loader:      progressConsole.Loader(""),
		clearOnDone: opts.TransientProgress,
	}
}

// printStepTiming centralizes print step timing behavior so callers follow the same contract.
func printStepTiming(kind string, stepName string, duration time.Duration, status string) {
	timing := duration.Round(time.Millisecond)
	if strings.TrimSpace(status) != "" {
		fmt.Fprintf(os.Stderr, "forj %s %s: %s (%s)\n", kind, stepName, timing, status)
		return
	}
	fmt.Fprintf(os.Stderr, "forj %s %s: %s\n", kind, stepName, timing)
}

// buildUsesTemplHTMX limits template generation to the selected App's configured starter kit so unrelated Apps do not add build work.
func buildUsesTemplHTMX(root string) bool {
	cfg, err := project.LoadProjectConfigAt(root)
	if err != nil {
		return false
	}
	app := ActiveApp()
	starterKit := cfg.Render.StarterKit
	if app.Name != project.DefaultAppName {
		if appConfig, ok := cfg.Apps[app.Name]; ok {
			starterKit = appConfig.StarterKit
		}
	}
	return project.NormalizeStarterKit(starterKit) == project.StarterKitTemplHTMX
}

// runTemplGenerate keeps template discovery inside the selected project without changing process state.
func (p Pipeline) runTemplGenerate(root string) (string, error) {
	cmd := exec.Command("go", "run", "github.com/a-h/templ/cmd/templ@v0.3.1020", "generate")
	cmd.Dir = root
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			printBuildFailureDetail(detail)
		}
		return "", fmt.Errorf("templ generate: %w", err)
	}
	return "generated", nil
}

// runWireGenerate evaluates every configured dependency-injection root beneath the selected project.
func (p Pipeline) runWireGenerate(root string) (string, error) {
	ran := false
	for _, wirePath := range loadWirePaths(root) {
		info, err := os.Stat(wirePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("wire path %q: %w", wirePath, err)
		}
		if !info.IsDir() {
			continue
		}
		ran = true

		status, err := p.runWireCommand(wirePath, debugEnabled())
		if err != nil {
			return "", err
		}
		if status != "" {
			return status, nil
		}
	}
	if !ran {
		return "no changes", nil
	}
	return "", nil
}

// wireCommandResult separates user-facing success status from failure diagnostics captured from the same process.
type wireCommandResult struct {
	status string
	detail string
}

// runWireCommand retries the one known transient Wire import failure while preserving useful diagnostics.
func (p Pipeline) runWireCommand(wirePath string, debug bool) (string, error) {
	result, err := runWireCommandQuiet(wirePath)
	if err == nil {
		if debug && strings.TrimSpace(result.status) != "" {
			fmt.Fprintln(os.Stderr, strings.TrimSpace(result.status))
		}
		return result.status, nil
	}
	if shouldRetryWire(result.detail) {
		retryResult, retryErr := runWireCommandQuiet(wirePath)
		if retryErr == nil {
			if retryResult.status == "" {
				return "retried", nil
			}
			return retryResult.status + ", retried", nil
		}
		result = retryResult
		err = retryErr
	}
	if result.detail != "" {
		printBuildFailureDetail(result.detail)
		return "", fmt.Errorf("wire (%s): %w", wirePath, err)
	}
	return "", fmt.Errorf("wire (%s): %w", wirePath, err)
}

// printBuildFailureDetail keeps subprocess diagnostics separate from the pipeline's concise error line.
func printBuildFailureDetail(detail string) {
	trimmed := strings.TrimSpace(detail)
	if trimmed == "" {
		return
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, trimmed)
}

// runWireCommandQuiet captures Wire output so the pipeline can decide whether to retry or print it.
func runWireCommandQuiet(wirePath string) (wireCommandResult, error) {
	cmd := exec.Command("wire")
	cmd.Dir = wirePath
	cmd.Env = append(os.Environ(), "WIRE_INCREMENTAL=1")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = strings.TrimSpace(stdout.String())
	}
	status := ""
	if err == nil {
		status = strings.TrimSpace(stdout.String())
	}
	return wireCommandResult{status: status, detail: detail}, err
}

// shouldRetryWire recognizes the transient import failure that a second incremental Wire pass can resolve.
func shouldRetryWire(detail string) bool {
	detail = str.Of(detail).Trim().ToLower().String()
	if detail == "" {
		return false
	}
	return strings.Contains(detail, "type-check failed for") &&
		strings.Contains(detail, "could not import")
}

// generateProjectFiles uses durable component intent when available so stale generated directories cannot reactivate optional primitives.
func (p Pipeline) generateProjectFiles(root string) (string, error) {
	selection := generate.GenerationSelection{
		Storage:       hasDir(filepath.Join(root, "internal", "storages")),
		Cache:         hasDir(filepath.Join(root, "internal", "caches")),
		Mail:          hasDir(filepath.Join(root, "internal", "mail")),
		Queue:         hasDir(filepath.Join(root, "internal", "jobs")) || hasDir(filepath.Join(root, "internal", "queues")),
		Events:        hasDir(filepath.Join(root, "internal", "events")),
		Database:      hasDir(filepath.Join(root, "internal", "database")),
		Observability: hasDir(filepath.Join(root, "containers", "observability", "vmagent")),
	}
	config, err := project.LoadProjectConfigAt(root)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("load project generation config: %w", err)
	}
	if config != nil {
		selection = generate.GenerationSelectionFromComponents(project.ProjectComponents(config))
	}
	selection.Environment = p.stackEnvironment
	result, err := generate.GenerateProjectFiles(root, selection)
	if err != nil {
		return "", fmt.Errorf("generate project files: %w", err)
	}
	if debugEnabled() {
		p.logger.Info().Int("files", result.TotalFiles).Msg("Generated project files")
	}
	if result.ChangedFiles == 0 {
		return "no changes", nil
	}
	return fmt.Sprintf("%d files", result.ChangedFiles), nil
}

// loadWirePaths reads project-configured Wire roots and falls back to the generated app layout.
func loadWirePaths(root string) []string {
	if targetName := requestedAppName(); targetName != "" {
		if !project.IsSafeAppName(targetName) {
			return defaultWirePaths(root)
		}
		target := project.AppForName(targetName)
		wireDir := filepath.Join(root, target.WireDir)
		if hasDir(wireDir) {
			return []string{wireDir}
		}
	}
	config, err := project.LoadProjectConfigAt(root)
	if err != nil {
		return defaultWirePaths(root)
	}
	if len(config.Dev.WirePaths) == 0 {
		return defaultWirePaths(root)
	}
	paths := make([]string, 0, len(config.Dev.WirePaths))
	for _, wirePath := range config.Dev.WirePaths {
		if !filepath.IsAbs(wirePath) {
			wirePath = filepath.Join(root, wirePath)
		}
		paths = append(paths, wirePath)
	}
	return paths
}

// defaultWirePaths prefers app/wire so rendered projects do not depend on the legacy root wire directory.
func defaultWirePaths(root string) []string {
	appWireDir := filepath.Join(root, "app", "wire")
	if hasDir(appWireDir) {
		return []string{appWireDir}
	}
	return []string{filepath.Join(root, "wire")}
}

// hasDir treats missing paths as a normal layout probe instead of an error.
func hasDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// debugEnabled centralizes debug enabled behavior so callers follow the same contract.
func debugEnabled() bool {
	for _, key := range []string{"FORJ_DEBUG", "DEBUG"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" && value != "0" {
			return true
		}
	}
	return false
}
