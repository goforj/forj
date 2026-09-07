//go:build integration

package forj

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goforj/goforj/internal/testkit"
	"github.com/goforj/goforj/project"
	"github.com/goforj/httpx/v2"
	"github.com/imroc/req/v3"
)

type authRenderedIntegrationCase struct {
	name       string
	moduleName string
	driver     string
	components project.Components
}

func TestGeneratedAuthRenderedIntegration(t *testing.T) {
	if _, err := exec.LookPath("wire"); err != nil {
		t.Skip("wire is required for rendered app integration tests")
	}

	for _, tc := range []authRenderedIntegrationCase{
		{
			name:       "sqlite",
			moduleName: "example.com/authsqlite",
			driver:     "sqlite",
			components: project.Components{
				WebAPI:         true,
				Auth:           true,
				OAuth:          true,
				Docker:         true,
				Scheduler:      true,
				DatabaseSQLite: true,
			},
		},
		{
			name:       "sqlite-auth-only",
			moduleName: "example.com/authsqlitecore",
			driver:     "sqlite",
			components: project.Components{
				WebAPI:         true,
				Auth:           true,
				Docker:         true,
				Scheduler:      true,
				DatabaseSQLite: true,
			},
		},
		{
			name:       "mysql",
			moduleName: "example.com/authmysqlfull",
			driver:     "mysql",
			components: project.Components{
				WebAPI:        true,
				Auth:          true,
				OAuth:         true,
				Docker:        true,
				Scheduler:     true,
				DatabaseMySQL: true,
			},
		},
		{
			name:       "postgres",
			moduleName: "example.com/authpostgresfull",
			driver:     "postgres",
			components: project.Components{
				WebAPI:           true,
				Auth:             true,
				OAuth:            true,
				Docker:           true,
				Scheduler:        true,
				DatabasePostgres: true,
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := renderAuthIntegrationApp(t, tc)
			assertRenderedAuthSchedulerCleanup(t, projectDir)
			assertRenderedOAuthComponent(t, projectDir, tc.driver, tc.components.OAuth)
			assertRenderedMailComponent(t, projectDir, tc.components.Mail || tc.components.Auth)
			setupRenderedAuthEnv(t, projectDir)
			stack := startRenderedAuthDependencies(t, projectDir)
			defer stack.Stop()
			authTestEnv := configureRenderedAuthDatabase(t, projectDir, tc.driver, stack)
			runRenderedAuthPackageTests(t, projectDir, tc.driver, authTestEnv)
			handle, baseURL := startRenderedAuthApp(t, projectDir)
			defer stopProcAsync(t, "auth-api", handle, time.Second)
			runRenderedAuthAppAssertions(t, baseURL)
		})
	}
}

// assertRenderedMailComponent centralizes assert rendered mail component behavior so callers follow the same contract.
func assertRenderedMailComponent(t *testing.T, projectDir string, enabled bool) {
	t.Helper()

	managerPath := filepath.Join(projectDir, "internal", "mail", "manager_gen.go")
	_, managerErr := os.Stat(managerPath)
	if enabled && managerErr != nil {
		t.Fatalf("expected %s to be rendered: %v", managerPath, managerErr)
	}
	if !enabled && !os.IsNotExist(managerErr) {
		t.Fatalf("expected %s to be absent when mail is disabled", managerPath)
	}

	injectPath := filepath.Join(projectDir, "app", "wire", "inject_managers.go")
	_, injectErr := os.Stat(injectPath)
	if enabled && injectErr != nil {
		t.Fatalf("expected %s to be rendered: %v", injectPath, injectErr)
	}
	if !enabled && injectErr != nil {
		t.Fatalf("expected %s to be rendered for shared managers: %v", injectPath, injectErr)
	}
	injectSource, err := os.ReadFile(injectPath)
	if err != nil {
		t.Fatalf("read %s: %v", injectPath, err)
	}
	hasMailProvider := strings.Contains(string(injectSource), "provideMailManager")
	if enabled && !hasMailProvider {
		t.Fatalf("expected %s to include mail manager provider", injectPath)
	}
	if !enabled && hasMailProvider {
		t.Fatalf("expected %s not to include mail manager provider when mail is disabled", injectPath)
	}
}

// assertRenderedOAuthComponent centralizes assert rendered oauth component behavior so callers follow the same contract.
func assertRenderedOAuthComponent(t *testing.T, projectDir, driver string, enabled bool) {
	t.Helper()

	requiredFiles := []string{
		filepath.Join(projectDir, "internal", "auth", "identity.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_state.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_provider.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_provider_apple.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_provider_github.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_provider_google.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_provider_microsoft.go"),
		filepath.Join(projectDir, "internal", "auth", "oauth_integration_test.go"),
		filepath.Join(projectDir, "migrations", fmt.Sprintf("2026_04_09_000006_auth_identities.%s.up.sql", driver)),
		filepath.Join(projectDir, "migrations", fmt.Sprintf("2026_04_09_000006_auth_identities.%s.down.sql", driver)),
		filepath.Join(projectDir, "migrations", fmt.Sprintf("2026_04_09_000007_auth_oauth_states.%s.up.sql", driver)),
		filepath.Join(projectDir, "migrations", fmt.Sprintf("2026_04_09_000007_auth_oauth_states.%s.down.sql", driver)),
	}
	for _, path := range requiredFiles {
		_, statErr := os.Stat(path)
		if enabled && statErr != nil {
			t.Fatalf("expected %s to be rendered: %v", path, statErr)
		}
		if !enabled && !os.IsNotExist(statErr) {
			t.Fatalf("expected %s to be absent when oauth is disabled", path)
		}
	}

	controllerPath := filepath.Join(projectDir, "internal", "auth", "controller.go")
	controllerSrc, err := os.ReadFile(controllerPath)
	if err != nil {
		t.Fatalf("read %s: %v", controllerPath, err)
	}
	hasOAuthRoute := strings.Contains(string(controllerSrc), "/auth/oauth/:provider/")
	if enabled && !hasOAuthRoute {
		t.Fatalf("expected oauth routes in %s", controllerPath)
	}
	if !enabled && hasOAuthRoute {
		t.Fatalf("expected oauth routes to be absent from %s", controllerPath)
	}

	injectPath := filepath.Join(projectDir, "app", "wire", "inject_auth.go")
	injectSrc, err := os.ReadFile(injectPath)
	if err != nil {
		t.Fatalf("read %s: %v", injectPath, err)
	}
	hasOAuthProviders := strings.Contains(string(injectSrc), "auth.NewOAuthProviders")
	hasOAuthStates := strings.Contains(string(injectSrc), "auth.NewOAuthStateRepo")
	hasOAuthIdentities := strings.Contains(string(injectSrc), "auth.NewAuthIdentityRepo")
	if enabled && (!hasOAuthProviders || !hasOAuthStates || !hasOAuthIdentities) {
		t.Fatalf("expected oauth authSet wiring in %s", injectPath)
	}
	if !enabled && (hasOAuthProviders || hasOAuthStates || hasOAuthIdentities) {
		t.Fatalf("expected oauth authSet wiring to be absent from %s", injectPath)
	}

	envPath := filepath.Join(projectDir, ".env")
	envSrc, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read %s: %v", envPath, err)
	}
	if strings.Contains(string(envSrc), "AUTH_OAUTH_") {
		t.Fatalf("expected OAuth configuration to stay out of %s", envPath)
	}

	readmePath := filepath.Join(projectDir, "internal", "auth", "README.md")
	readmeSrc, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	readme := string(readmeSrc)
	providerConfiguration := []string{
		"AUTH_OAUTH_GITHUB_CLIENT_ID",
		"AUTH_OAUTH_GITHUB_CLIENT_SECRET",
		"AUTH_OAUTH_GOOGLE_CLIENT_ID",
		"AUTH_OAUTH_GOOGLE_CLIENT_SECRET",
		"AUTH_OAUTH_MICROSOFT_CLIENT_ID",
		"AUTH_OAUTH_MICROSOFT_CLIENT_SECRET",
		"AUTH_OAUTH_APPLE_CLIENT_ID",
		"AUTH_OAUTH_APPLE_TEAM_ID",
		"AUTH_OAUTH_APPLE_KEY_ID",
		"AUTH_OAUTH_APPLE_PRIVATE_KEY",
	}
	for _, key := range providerConfiguration {
		documented := strings.Contains(readme, "`"+key+"`")
		if enabled && !documented {
			t.Fatalf("expected %s to document %s", readmePath, key)
		}
		if !enabled && documented {
			t.Fatalf("expected %s to omit OAuth configuration when the component is disabled", readmePath)
		}
	}
}

// assertRenderedAuthSchedulerCleanup centralizes assert rendered auth scheduler cleanup behavior so callers follow the same contract.
func assertRenderedAuthSchedulerCleanup(t *testing.T, projectDir string) {
	t.Helper()

	schedulerRegistryPath := filepath.Join(projectDir, "app", "schedules.go")
	schedulerRegistrySrc, err := os.ReadFile(schedulerRegistryPath)
	if err != nil {
		t.Fatalf("read %s: %v", schedulerRegistryPath, err)
	}
	for _, token := range []string{
		`DailyAt("04:11")`,
		`Name("auth:sessions:cleanup")`,
		`Do(s.InspectTask("auth:sessions:cleanup", r.authService.Cleanup))`,
	} {
		if !strings.Contains(string(schedulerRegistrySrc), token) {
			t.Fatalf("expected %q in %s", token, schedulerRegistryPath)
		}
	}

	schedulerPath := filepath.Join(projectDir, "internal", "schedules", "scheduler.go")
	schedulerSrc, err := os.ReadFile(schedulerPath)
	if err != nil {
		t.Fatalf("read %s: %v", schedulerPath, err)
	}
	for _, token := range []string{
		`registry ScheduleRegistry`,
		`WithTaskContextDecorator(func(ctx context.Context) context.Context {`,
		`return runtime.WithSource(ctx, runtime.SourceScheduler)`,
	} {
		if !strings.Contains(string(schedulerSrc), token) {
			t.Fatalf("expected %q in %s", token, schedulerPath)
		}
	}
}

// renderAuthIntegrationApp keeps the render auth integration app representation consistent.
func renderAuthIntegrationApp(t *testing.T, tc authRenderedIntegrationCase) string {
	t.Helper()

	projectDir := t.TempDir()
	renderEnv := map[string]string{}
	if driver := tc.components.DatabaseDriver(); driver != "" {
		renderEnv["DB_DRIVER"] = driver
		renderEnv["DB_SUPPORTED_DRIVERS"] = driver
	}
	// Generate the mail manager with the same driver the test will use at runtime.
	renderEnv["MAIL_DRIVER"] = "smtp"
	renderEnv["MAIL_SUPPORTED_DRIVERS"] = "smtp"
	testkit.RenderProjectWithForj(t, projectDir, testkit.RenderProjectRequest{
		Config: project.Config{
			ProjectName:  "AuthApp",
			GoModuleName: tc.moduleName,
			UpdatedAt:    "2026-04-09 00:00:00 UTC",
			Render: project.RenderConfig{
				Components: tc.components,
				ModuleReplaces: func() map[string]string {
					if !tc.components.Mail {
						return nil
					}
					mailRoot := testkit.LocalSiblingRepoPath(t, "mail")
					return map[string]string{
						"github.com/goforj/mail":         mailRoot,
						"github.com/goforj/mail/mailses": filepath.Join(mailRoot, "mailses"),
					}
				}(),
			},
		},
		EnvOverrides: renderEnv,
	})

	return projectDir
}

// runRenderedAuthPackageTests centralizes run rendered auth package tests behavior so callers follow the same contract.
func runRenderedAuthPackageTests(t *testing.T, projectDir, driver string, envOverrides map[string]string) {
	t.Helper()
	args := []string{"go", "test", "./internal/auth", "-tags=integration," + driver, "-count=1", "-run", "^$"}
	label := "go test ./internal/auth (compile check)"
	testEnv := map[string]string{
		"DB_DRIVER":            driver,
		"DB_SUPPORTED_DRIVERS": driver,
	}
	for key, value := range envOverrides {
		testEnv[key] = value
	}
	runRenderedAuthCommand(
		t,
		projectDir,
		label,
		args,
		testkit.IntegrationGoProcessEnv(t, testEnv),
	)
}

// startRenderedAuthApp centralizes start rendered auth app behavior so callers follow the same contract.
func startRenderedAuthApp(t *testing.T, projectDir string) (*procHandle, string) {
	t.Helper()

	buildRenderedDefaultAppTo(t, projectDir, filepath.Join(projectDir, "bin", "app"), nil, "go build")
	runRenderedAuthCommand(
		t,
		projectDir,
		"migrate",
		[]string{"./bin/app", "migrate"},
		testkit.IntegrationProcessEnv(t, nil),
	)

	addr := findFreeAddr(t)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	baseURL := "http://127.0.0.1:" + port
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		ctxRun, cancelRun := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctxRun, "./bin/app", "http:serve", "--port", port)
		cmd.Dir = projectDir
		cmd.Env = testkit.IntegrationProcessEnv(t, nil)
		handle := &procHandle{name: "api", cmd: cmd, cancel: cancelRun}
		cmd.Stdout = &handle.stdout
		cmd.Stderr = &handle.stderr
		if err := handle.Start(); err != nil {
			cancelRun()
			t.Fatalf("start auth app: %v", err)
		}

		err := waitForAuthProbeEndpointReady(handle, baseURL+"/-/health", http.StatusOK, 15*time.Second)
		if err == nil {
			return handle, baseURL
		}
		lastErr = err
		stopProcAsync(t, "auth-api-retry", handle, time.Second)
		if !isTransientMySQLStartupError(err) || attempt == 3 {
			t.Fatalf("process exited before probe became ready: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("start auth app: %v", lastErr)
	return nil, ""
}

// runRenderedAuthAppAssertions centralizes run rendered auth app assertions behavior so callers follow the same contract.
func runRenderedAuthAppAssertions(t *testing.T, baseURL string) {
	t.Helper()

	client := newRenderedAuthHTTPClient(t, baseURL)
	secondClient := newRenderedAuthHTTPClient(t, baseURL)
	probeClient := newRenderedAuthHTTPClient(t, baseURL)

	client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)
	client.assertStatus(http.MethodGet, "/api/v1/auth/me", nil, http.StatusUnauthorized)

	client.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "admin",
		Password: "wrong",
	}, http.StatusUnauthorized)
	client.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "missing",
		Password: "wrong",
	}, http.StatusUnauthorized)
	client.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "missing",
		Password: "wrong",
	}, http.StatusUnauthorized)
	client.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "missing",
		Password: "wrong",
	}, http.StatusTooManyRequests)

	loginResp := client.login(authLoginRequest{
		Login:    "admin",
		Password: "admin",
	})
	if loginResp.User.Username != "admin" {
		t.Fatalf("login username = %q, want %q", loginResp.User.Username, "admin")
	}

	meResp := client.me()
	if meResp.User.Username != "admin" {
		t.Fatalf("me username = %q, want %q", meResp.User.Username, "admin")
	}
	sessionsResp := client.sessions()
	if len(sessionsResp.Sessions) != 1 {
		t.Fatalf("initial sessions length = %d, want %d", len(sessionsResp.Sessions), 1)
	}
	if !sessionsResp.Sessions[0].Current {
		t.Fatal("expected initial session to be marked current")
	}
	if strings.TrimSpace(sessionsResp.Sessions[0].DeviceLabel) == "" {
		t.Fatal("expected initial session device label")
	}
	secondLoginResp := secondClient.login(authLoginRequest{
		Login:    "admin",
		Password: "admin",
	})
	if secondLoginResp.User.Username != "admin" {
		t.Fatalf("second login username = %q, want %q", secondLoginResp.User.Username, "admin")
	}

	helloResp := client.getText("/api/v1/hello")
	if strings.TrimSpace(helloResp) != "Hello, World!" {
		t.Fatalf("hello body = %q, want %q", strings.TrimSpace(helloResp), "Hello, World!")
	}

	beforeAccess := authCookieValue(t, client.jar, baseURL, "auth_access")
	if beforeAccess == "" {
		t.Fatal("expected auth_access cookie after login")
	}

	waitForRenderedAuthAccessRotation(t, client, baseURL, beforeAccess, 3*time.Second)

	sessionsResp = client.sessions()
	if len(sessionsResp.Sessions) != 2 {
		t.Fatalf("sessions length after second login = %d, want %d", len(sessionsResp.Sessions), 2)
	}
	var revokeID string
	currentCount := 0
	for _, session := range sessionsResp.Sessions {
		if session.Current {
			currentCount++
			continue
		}
		revokeID = session.ID
	}
	if currentCount != 1 {
		t.Fatalf("current session count = %d, want %d", currentCount, 1)
	}
	if revokeID == "" {
		t.Fatal("expected a non-current session to revoke")
	}

	client.revokeSession(revokeID)
	client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusOK)
	secondClient.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)

	sessionsResp = client.sessions()
	if len(sessionsResp.Sessions) != 1 {
		t.Fatalf("sessions length after revoke = %d, want %d", len(sessionsResp.Sessions), 1)
	}
	if !sessionsResp.Sessions[0].Current {
		t.Fatal("expected remaining session to be current")
	}

	client.changePassword("admin", "Better-admin!")
	client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusOK)
	secondClient.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)
	probeClient.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "admin",
		Password: "admin",
	}, http.StatusUnauthorized)
	secondClient.login(authLoginRequest{
		Login:    "admin",
		Password: "Better-admin!",
	})
	resetToken := secondClient.requestPasswordReset("admin")
	secondClient.confirmPasswordReset(resetToken, "Best-admin!")
	secondClient.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "admin",
		Password: "Better-admin!",
	}, http.StatusUnauthorized)
	client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)
	secondClient.login(authLoginRequest{
		Login:    "admin",
		Password: "Best-admin!",
	})

	secondClient.logoutAll()
	client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)
	secondClient.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)

	secondClient.login(authLoginRequest{
		Login:    "admin",
		Password: "Best-admin!",
	})
	verificationToken := secondClient.requestEmailVerification()
	secondClient.confirmEmailVerification(verificationToken)
	if verifiedUser := secondClient.me().User; verifiedUser.EmailVerifiedAt == "" {
		t.Fatal("expected email_verified_at after email verification")
	}
	secondClient.logout()
	client.logout()
	client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusUnauthorized)

	lockClient := newRenderedAuthHTTPClient(t, baseURL)
	lockClient.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "admin",
		Password: "wrong",
	}, http.StatusUnauthorized)
	lockClient.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "admin",
		Password: "wrong",
	}, http.StatusLocked)
	lockClient.assertStatus(http.MethodPost, "/api/v1/auth/login", authLoginRequest{
		Login:    "admin",
		Password: "Best-admin!",
	}, http.StatusLocked)
}

// waitForRenderedAuthAccessRotation observes the expiry boundary directly instead of padding every driver case with a fixed sleep.
func waitForRenderedAuthAccessRotation(t *testing.T, client *renderedAuthHTTPClient, baseURL string, beforeAccess string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		client.assertStatus(http.MethodGet, "/api/v1/hello", nil, http.StatusOK)
		afterAccess := authCookieValue(t, client.jar, baseURL, "auth_access")
		if afterAccess != "" && afterAccess != beforeAccess {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected auth_access cookie to rotate after expiry/refresh")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// runRenderedAuthCommand centralizes run rendered auth command behavior so callers follow the same contract.
func runRenderedAuthCommand(t *testing.T, projectDir, name string, args []string, env []string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = projectDir
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, out.String())
	}
}

// setupRenderedAuthEnv centralizes setup rendered auth env behavior so callers follow the same contract.
func setupRenderedAuthEnv(t *testing.T, projectDir string) {
	t.Helper()
	for _, kv := range []struct {
		key   string
		value string
	}{
		{"AUTH_ACCESS_TOKEN_TTL", "1s"},
		{"AUTH_SESSION_IDLE_TTL", "30m"},
		{"AUTH_SESSION_TTL", "30m"},
		{"AUTH_COOKIE_SECURE", "false"},
		{"AUTH_EMAIL_VERIFICATION_RETURN_TOKEN", "true"},
		{"AUTH_LOGIN_LOCKOUT_ATTEMPTS", "2"},
		{"AUTH_LOGIN_LOCKOUT_DURATION", "30m"},
		{"AUTH_LOGIN_RATE_LIMIT_ATTEMPTS", "3"},
		{"AUTH_LOGIN_RATE_LIMIT_DURATION", "30m"},
		{"AUTH_PASSWORD_RESET_RETURN_TOKEN", "true"},
		{"AUTH_BOOTSTRAP_USERNAME", "admin"},
		{"AUTH_BOOTSTRAP_PASSWORD", "admin"},
		{"MAIL_DRIVER", "smtp"},
		{"MAIL_SUPPORTED_DRIVERS", "smtp"},
		{"MAIL_FROM_ADDRESS", "no-reply@example.com"},
		{"MAIL_FROM_NAME", "AuthApp"},
	} {
		if err := testkit.ReplaceOrAppendEnvValues([]string{filepath.Join(projectDir, ".env")}, map[string]string{kv.key: kv.value}); err != nil {
			t.Fatalf("set %s: %v", kv.key, err)
		}
	}
}

// startRenderedAuthDependencies centralizes start rendered auth dependencies behavior so callers follow the same contract.
func startRenderedAuthDependencies(t *testing.T, projectDir string) *testkit.RenderedComposeStack {
	t.Helper()

	stack, err := testkit.StartRenderedComposeServices(projectDir, nil)
	if err != nil {
		t.Fatalf("start rendered auth compose services: %v", err)
	}
	if mailpit, ok := stack.Service("mailpit"); ok {
		host := strings.TrimSpace(mailpit.Host)
		if host == "" {
			host = "127.0.0.1"
		}
		if err := testkit.ReplaceOrAppendEnvValues(
			[]string{filepath.Join(projectDir, ".env")},
			map[string]string{
				"MAIL_SMTP_HOST": host,
				"MAIL_SMTP_PORT": mailpit.Port,
			},
		); err != nil {
			stack.Stop()
			t.Fatalf("set rendered auth mailpit env: %v", err)
		}
	}
	return stack
}

// configureRenderedAuthDatabase centralizes configure rendered auth database behavior so callers follow the same contract.
func configureRenderedAuthDatabase(t *testing.T, projectDir, driver string, stack *testkit.RenderedComposeStack) map[string]string {
	t.Helper()

	authTestEnv := map[string]string{
		"DB_DRIVER":            driver,
		"DB_SUPPORTED_DRIVERS": driver,
	}
	setEnv := func(key, value string) {
		if err := testkit.ReplaceOrAppendEnvValues(
			[]string{filepath.Join(projectDir, ".env")},
			map[string]string{key: value},
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	switch driver {
	case "sqlite":
		setEnv("DB_DRIVER", "sqlite")
		setEnv("DB_SUPPORTED_DRIVERS", "sqlite")
		dbPath := filepath.Join(projectDir, "storage", "auth-integration.db")
		setEnv("DB_DATABASE", dbPath)
		authTestEnv["DB_DATABASE"] = dbPath
		return authTestEnv
	case "mysql":
		started, ok := stack.Service("mysql")
		if !ok {
			t.Fatal("rendered auth app missing mysql compose service")
		}
		setRenderedAuthDatabaseEnv(t, setEnv, driver, started.Host, started.Port, "db", "user", "password")
		resetRenderedMySQLAuthDatabase(t, started)
		authTestEnv["AUTH_INTEGRATION_USE_CONFIGURED_DB"] = "true"
		authTestEnv["DB_HOST"] = started.Host
		authTestEnv["DB_PORT"] = started.Port
		authTestEnv["DB_DATABASE"] = "db"
		authTestEnv["DB_USERNAME"] = "user"
		authTestEnv["DB_PASSWORD"] = "password"
		return authTestEnv
	case "postgres":
		started, ok := stack.Service("postgres")
		if !ok {
			t.Fatal("rendered auth app missing postgres compose service")
		}
		setRenderedAuthDatabaseEnv(t, setEnv, driver, started.Host, started.Port, "app", "postgres", "postgres")
		resetRenderedPostgresAuthDatabase(t, started)
		authTestEnv["AUTH_INTEGRATION_USE_CONFIGURED_DB"] = "true"
		authTestEnv["DB_HOST"] = started.Host
		authTestEnv["DB_PORT"] = started.Port
		authTestEnv["DB_DATABASE"] = "app"
		authTestEnv["DB_USERNAME"] = "postgres"
		authTestEnv["DB_PASSWORD"] = "postgres"
		return authTestEnv
	default:
		t.Fatalf("unsupported rendered auth driver %q", driver)
		return nil
	}
}

// setRenderedAuthDatabaseEnv centralizes set rendered auth database env behavior so callers follow the same contract.
func setRenderedAuthDatabaseEnv(t *testing.T, setEnv func(string, string), driver, host, port, database, username, password string) {
	t.Helper()

	setEnv("DB_DRIVER", driver)
	setEnv("DB_SUPPORTED_DRIVERS", driver)
	setEnv("DB_HOST", host)
	setEnv("DB_PORT", port)
	setEnv("DB_DATABASE", database)
	setEnv("DB_USERNAME", username)
	setEnv("DB_PASSWORD", password)
}

// resetRenderedMySQLAuthDatabase centralizes reset rendered my sqlauth database behavior so callers follow the same contract.
func resetRenderedMySQLAuthDatabase(t *testing.T, started *testkit.StartedContainer) {
	t.Helper()

	if err := testkit.WaitForContainerExecSuccess(
		started.Container,
		[]string{
			"sh", "-lc",
			`mariadb -h 127.0.0.1 -u"$MARIADB_USER" -p"$MARIADB_PASSWORD" "$MARIADB_DATABASE" -e 'DROP TABLE IF EXISTS auth_login_attempts; DROP TABLE IF EXISTS auth_password_resets; DROP TABLE IF EXISTS auth_email_verifications; DROP TABLE IF EXISTS auth_sessions; DROP TABLE IF EXISTS users; DROP TABLE IF EXISTS migrations;'`,
		},
		20*time.Second,
	); err != nil {
		t.Fatalf("reset mysql auth database: %v", err)
	}
}

// resetRenderedPostgresAuthDatabase centralizes reset rendered postgres auth database behavior so callers follow the same contract.
func resetRenderedPostgresAuthDatabase(t *testing.T, started *testkit.StartedContainer) {
	t.Helper()

	if err := testkit.WaitForContainerExecSuccess(
		started.Container,
		[]string{
			"sh", "-lc",
			`psql -U postgres -d postgres -v ON_ERROR_STOP=1 -tc "SELECT 1 FROM pg_database WHERE datname = 'app'" | grep -q 1 || psql -U postgres -d postgres -v ON_ERROR_STOP=1 -c 'CREATE DATABASE app'; psql -U postgres -d app -v ON_ERROR_STOP=1 -c 'DROP TABLE IF EXISTS auth_login_attempts, auth_password_resets, auth_email_verifications, auth_sessions, users, migrations CASCADE;'`,
		},
		20*time.Second,
	); err != nil {
		t.Fatalf("reset postgres auth database: %v", err)
	}
}

type authLoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type authUser struct {
	Username        string `json:"username"`
	EmailVerifiedAt string `json:"email_verified_at"`
}

type authUserResponse struct {
	OK    bool     `json:"ok"`
	User  authUser `json:"user"`
	Error string   `json:"error,omitempty"`
}

type authSession struct {
	ID          string `json:"id"`
	Current     bool   `json:"current"`
	DeviceLabel string `json:"device_label"`
	UserAgent   string `json:"user_agent"`
	IPAddress   string `json:"ip_address"`
}

type authSessionsResponse struct {
	OK       bool          `json:"ok"`
	Sessions []authSession `json:"sessions"`
	Error    string        `json:"error,omitempty"`
}

type authOKResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type authPasswordResetRequestResponse struct {
	OK         bool   `json:"ok"`
	ResetToken string `json:"reset_token,omitempty"`
	Error      string `json:"error,omitempty"`
}

type authEmailVerificationRequestResponse struct {
	OK                bool   `json:"ok"`
	VerificationToken string `json:"verification_token,omitempty"`
	Error             string `json:"error,omitempty"`
}

type renderedAuthHTTPClient struct {
	t      *testing.T
	client *httpx.Client
	jar    http.CookieJar
}

// newRenderedAuthHTTPClient centralizes new rendered auth httpclient behavior so callers follow the same contract.
func newRenderedAuthHTTPClient(t *testing.T, baseURL string) *renderedAuthHTTPClient {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &renderedAuthHTTPClient{
		t: t,
		client: httpx.New(
			httpx.BaseURL(baseURL),
			httpx.CookieJar(jar),
			httpx.Timeout(2*time.Second),
		),
		jar: jar,
	}
}

// login centralizes login behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) login(input authLoginRequest) authUserResponse {
	c.t.Helper()

	resp, err := httpx.Post[authLoginRequest, authUserResponse](c.client, "/api/v1/auth/login", input)
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/login failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/login returned ok=false: %#v", resp)
	}
	return resp
}

// me centralizes me behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) me() authUserResponse {
	c.t.Helper()

	resp, err := httpx.Get[authUserResponse](c.client, "/api/v1/auth/me")
	if err != nil {
		c.t.Fatalf("GET /api/v1/auth/me failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("GET /api/v1/auth/me returned ok=false: %#v", resp)
	}
	return resp
}

// sessions centralizes sessions behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) sessions() authSessionsResponse {
	c.t.Helper()

	resp, err := httpx.Get[authSessionsResponse](c.client, "/api/v1/auth/sessions")
	if err != nil {
		c.t.Fatalf("GET /api/v1/auth/sessions failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("GET /api/v1/auth/sessions returned ok=false: %#v", resp)
	}
	return resp
}

// logout centralizes logout behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) logout() {
	c.t.Helper()

	resp, err := httpx.Post[any, authOKResponse](c.client, "/api/v1/auth/logout", nil)
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/logout failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/logout returned ok=false: %#v", resp)
	}
}

// logoutAll centralizes logout all behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) logoutAll() {
	c.t.Helper()

	resp, err := httpx.Post[any, authOKResponse](c.client, "/api/v1/auth/logout-all", nil)
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/logout-all failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/logout-all returned ok=false: %#v", resp)
	}
}

// changePassword centralizes change password behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) changePassword(currentPassword, newPassword string) {
	c.t.Helper()

	resp, err := httpx.Post[map[string]string, authUserResponse](c.client, "/api/v1/auth/change-password", map[string]string{
		"current_password": currentPassword,
		"new_password":     newPassword,
	})
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/change-password failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/change-password returned ok=false: %#v", resp)
	}
}

// revokeSession centralizes revoke session behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) revokeSession(id string) {
	c.t.Helper()

	resp, err := httpx.Post[any, authOKResponse](c.client, "/api/v1/auth/sessions/"+id+"/revoke", nil)
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/sessions/%s/revoke failed: %v", id, err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/sessions/%s/revoke returned ok=false: %#v", id, resp)
	}
}

// requestPasswordReset centralizes request password reset behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) requestPasswordReset(login string) string {
	c.t.Helper()

	resp, err := httpx.Post[map[string]string, authPasswordResetRequestResponse](c.client, "/api/v1/auth/password-reset/request", map[string]string{
		"login": login,
	})
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/password-reset/request failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/password-reset/request returned ok=false: %#v", resp)
	}
	if resp.ResetToken == "" {
		c.t.Fatalf("POST /api/v1/auth/password-reset/request returned empty reset token: %#v", resp)
	}
	return resp.ResetToken
}

// confirmPasswordReset centralizes confirm password reset behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) confirmPasswordReset(token, newPassword string) {
	c.t.Helper()

	resp, err := httpx.Post[map[string]string, authOKResponse](c.client, "/api/v1/auth/password-reset/confirm", map[string]string{
		"token":        token,
		"new_password": newPassword,
	})
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/password-reset/confirm failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/password-reset/confirm returned ok=false: %#v", resp)
	}
}

// requestEmailVerification centralizes request email verification behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) requestEmailVerification() string {
	c.t.Helper()

	resp, err := httpx.Post[any, authEmailVerificationRequestResponse](c.client, "/api/v1/auth/email-verification/request", nil)
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/email-verification/request failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/email-verification/request returned ok=false: %#v", resp)
	}
	if resp.VerificationToken == "" {
		c.t.Fatalf("POST /api/v1/auth/email-verification/request returned empty verification token: %#v", resp)
	}
	return resp.VerificationToken
}

// confirmEmailVerification centralizes confirm email verification behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) confirmEmailVerification(token string) {
	c.t.Helper()

	resp, err := httpx.Post[map[string]string, authUserResponse](c.client, "/api/v1/auth/email-verification/confirm", map[string]string{
		"token": token,
	})
	if err != nil {
		c.t.Fatalf("POST /api/v1/auth/email-verification/confirm failed: %v", err)
	}
	if !resp.OK {
		c.t.Fatalf("POST /api/v1/auth/email-verification/confirm returned ok=false: %#v", resp)
	}
	if resp.User.EmailVerifiedAt == "" {
		c.t.Fatalf("POST /api/v1/auth/email-verification/confirm returned empty email_verified_at: %#v", resp)
	}
}

// getText centralizes get text behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) getText(path string) string {
	c.t.Helper()

	resp, err := httpx.Get[string](c.client, path)
	if err != nil {
		c.t.Fatalf("GET %s failed: %v", path, err)
	}
	return resp
}

// assertStatus centralizes assert status behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) assertStatus(method, path string, body any, want int) string {
	c.t.Helper()

	resp, responseBody := c.do(method, path, body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s status = %d, want %d\nbody:\n%s", method, path, resp.StatusCode, want, responseBody)
	}
	return responseBody
}

// do centralizes do behavior so callers follow the same contract.
func (c *renderedAuthHTTPClient) do(method, path string, body any) (*req.Response, string) {
	c.t.Helper()

	request := c.client.Raw().R()
	if body != nil {
		request.SetBody(body)
	}

	var (
		resp *req.Response
		err  error
	)
	switch method {
	case http.MethodGet:
		resp, err = request.Get(path)
	case http.MethodPost:
		resp, err = request.Post(path)
	default:
		c.t.Fatalf("unsupported method %s", method)
	}
	if err != nil {
		c.t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()

	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		c.t.Fatalf("read response body: %v", err)
	}
	return resp, out.String()
}

// authCookieValue centralizes auth cookie value behavior so callers follow the same contract.
func authCookieValue(t *testing.T, jar http.CookieJar, baseURL, name string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil {
		t.Fatalf("new cookie inspection request: %v", err)
	}
	for _, cookie := range jar.Cookies(req.URL) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

// waitForAuthProbeEndpointReady centralizes wait for auth probe endpoint ready behavior so callers follow the same contract.
func waitForAuthProbeEndpointReady(proc *procHandle, url string, wantStatus int, timeout time.Duration) error {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if proc != nil {
			if err := proc.ExitError(); err != nil {
				return err
			}
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == wantStatus {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if proc != nil {
		return fmt.Errorf("probe %s did not return status %d before timeout\n%s", url, wantStatus, proc.Output())
	}
	return fmt.Errorf("probe %s did not return status %d before timeout", url, wantStatus)
}

// isTransientMySQLStartupError centralizes the is transient my sqlstartup error decision for its callers.
func isTransientMySQLStartupError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "driver: bad connection") ||
		strings.Contains(text, "unexpected EOF")
}
