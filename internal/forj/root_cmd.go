package forj

import (
	"github.com/alecthomas/kong"
	"github.com/goforj/goforj/internal/apiindex"
	"github.com/goforj/goforj/internal/backup"
	"github.com/goforj/goforj/internal/bench"
	"github.com/goforj/goforj/internal/build"
	"github.com/goforj/goforj/internal/forj/atlas"
	"github.com/goforj/goforj/internal/forj/makeapp"
	"github.com/goforj/goforj/internal/generate"
)

// RootCmd owns framework-level commands while app-owned commands are reached through delegation.
type RootCmd struct {
	Version                   kong.VersionFlag                `help:"Show version information" version:"${version}"`
	Dev                       bool                            `name:"dev" aliases:"x" env:"FORJ_DEV" help:"Show developer/maintainer commands in help output" hidden:""`
	BuildCmd                  build.Cmd                       `cmd:""`
	APIIndexCmd               apiindex.Cmd                    `cmd:""`
	GenerateCmd               generate.Cmd                    `cmd:""`
	NewProjectCmd             NewProjectCmd                   `cmd:""`
	AtlasInstallCmd           atlas.InstallCmd                `cmd:""`
	AtlasUpdateCmd            atlas.UpdateCmd                 `cmd:""`
	AtlasDoctorCmd            atlas.DoctorCmd                 `cmd:""`
	AtlasListSkillsCmd        atlas.ListSkillsCmd             `cmd:""`
	AtlasMakeSkillCmd         atlas.MakeSkillCmd              `cmd:""`
	AtlasMCPCmd               atlas.MCPCmd                    `cmd:""`
	AtlasEvalCmd              atlas.EvalCmd                   `cmd:""`
	MakeAppCmd                makeapp.Cmd                     `cmd:""`
	ProjectDescribeCmd        ProjectDescribeCmd              `cmd:""`
	StackCmd                  StackCmd                        `cmd:""`
	ProjectRenameModuleCmd    ProjectRenameModuleCmd          `cmd:""`
	DevCmd                    DevCmd                          `cmd:""`
	DevStatusCmd              DevStatusCmd                    `cmd:""`
	DownCmd                   DownCmd                         `cmd:""`
	BuildBinaryCmd            BuildBinaryCmd                  `cmd:""`
	TestRenderCmd             TestRenderCmd                   `cmd:""`
	TestRendersCmd            TestRendersCmd                  `cmd:""`
	TestIntegrationCmd        TestIntegrationCmd              `cmd:""`
	InspectOverheadMeasureCmd bench.InspectOverheadMeasureCmd `cmd:""`
	LoggerOverheadMeasureCmd  bench.LoggerOverheadMeasureCmd  `cmd:""`
	HTTPLiveProfileCmd        bench.HTTPLiveProfileCmd        `cmd:""`
	HTTPRuntimeProfileCmd     bench.HTTPRuntimeProfileCmd     `cmd:""`
	MetricsOverheadMeasureCmd bench.MetricsOverheadMeasureCmd `cmd:""`
	TestConsoleCmd            TestConsoleCmd                  `cmd:""`
	TestOpenAPICmd            TestOpenAPICmd                  `cmd:""`
	ScenarioListCmd           ScenarioListCmd                 `cmd:""`
	ScenarioGenerateCmd       ScenarioGenerateCmd             `cmd:""`
	ScenarioTestCmd           ScenarioTestCmd                 `cmd:""`
	RenderCmd                 RenderCmd                       `cmd:""`
	EnvInitCmd                EnvInitCmd                      `cmd:""`
	EnvSetCmd                 EnvSetCmd                       `cmd:""`
	EnvCheckCmd               EnvCheckCmd                     `cmd:""`
	RunCmd                    build.RunCmd                    `cmd:""`
	BackupPlanCmd             backup.PlanCmd                  `cmd:""`
	BackupListCmd             backup.ListCmd                  `cmd:""`
	BackupCreateCmd           backup.CreateCmd                `cmd:""`
	BackupVerifyCmd           backup.VerifyCmd                `cmd:""`
	BackupRestoreCmd          backup.RestoreCmd               `cmd:""`
	BackupPruneCmd            backup.PruneCmd                 `cmd:""`
	BackupStatusCmd           backup.StatusCmd                `cmd:""`
}

// NewRootCmd wires only native framework commands so generated app generators do not appear in forj help.
func NewRootCmd(
	authoring *projectAuthoringCommands,
	builds *buildCommands,
	runtime *runtimeCommands,
	atlasCommands *atlasCommands,
	tests *testCommands,
	benchmarks *benchmarkCommands,
	scenarios *scenarioCommands,
	backups *backupCommands,
) *RootCmd {
	return &RootCmd{
		BuildCmd:                  builds.build,
		APIIndexCmd:               builds.apiIndex,
		GenerateCmd:               authoring.generate,
		NewProjectCmd:             authoring.newProject,
		AtlasInstallCmd:           atlasCommands.install,
		AtlasUpdateCmd:            atlasCommands.update,
		AtlasDoctorCmd:            atlasCommands.doctor,
		AtlasListSkillsCmd:        atlasCommands.listSkills,
		AtlasMakeSkillCmd:         atlasCommands.makeSkill,
		AtlasMCPCmd:               atlasCommands.mcp,
		AtlasEvalCmd:              atlasCommands.eval,
		MakeAppCmd:                authoring.makeApp,
		ProjectDescribeCmd:        ProjectDescribeCmd{},
		ProjectRenameModuleCmd:    ProjectRenameModuleCmd{},
		DevCmd:                    runtime.dev,
		DevStatusCmd:              runtime.devStatus,
		DownCmd:                   runtime.down,
		BuildBinaryCmd:            builds.buildBinary,
		TestRenderCmd:             tests.render,
		TestRendersCmd:            tests.renders,
		TestIntegrationCmd:        tests.integration,
		InspectOverheadMeasureCmd: benchmarks.inspectOverhead,
		LoggerOverheadMeasureCmd:  benchmarks.loggerOverhead,
		HTTPLiveProfileCmd:        benchmarks.httpLive,
		HTTPRuntimeProfileCmd:     benchmarks.httpRuntime,
		MetricsOverheadMeasureCmd: benchmarks.metricsOverhead,
		TestConsoleCmd:            tests.console,
		TestOpenAPICmd:            tests.openAPI,
		ScenarioListCmd:           scenarios.list,
		ScenarioGenerateCmd:       scenarios.generate,
		ScenarioTestCmd:           scenarios.test,
		RenderCmd:                 authoring.render,
		EnvInitCmd:                authoring.envInit,
		EnvSetCmd:                 authoring.envSet,
		EnvCheckCmd:               authoring.envCheck,
		RunCmd:                    runtime.run,
		BackupPlanCmd:             backups.plan,
		BackupListCmd:             backups.list,
		BackupCreateCmd:           backups.create,
		BackupVerifyCmd:           backups.verify,
		BackupRestoreCmd:          backups.restore,
		BackupPruneCmd:            backups.prune,
		BackupStatusCmd:           backups.status,
	}
}
