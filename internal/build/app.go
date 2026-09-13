package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goforj/goforj/project"
)

// ActiveApp resolves the App selected by environment or convention when an operation starts.
func ActiveApp() project.App {
	appName := requestedAppName()
	if project.IsSafeAppName(appName) {
		return conventionalApp(appName)
	}
	return conventionalApp(project.DefaultAppName)
}

// requestedAppName returns an explicit app selected by the command environment.
func requestedAppName() string {
	if app := strings.TrimSpace(os.Getenv("FORJ_APP")); app != "" {
		return app
	}
	return ""
}

// conventionalApp returns the standard layout paths for an app.
func conventionalApp(name string) project.App {
	return project.AppForName(name)
}

// appPackageFromEntrypoint converts cmd/<app>/main.go into a go command package path.
func appPackageFromEntrypoint(entrypoint string) string {
	entrypoint = filepath.ToSlash(filepath.Clean(strings.TrimSpace(entrypoint)))
	if entrypoint == "." || entrypoint == "" {
		return "."
	}
	dir := filepath.ToSlash(filepath.Dir(entrypoint))
	if dir == "." || dir == "" {
		return "."
	}
	return "./" + strings.TrimPrefix(dir, "./")
}

// resolveDefaultAppPackage prefers the selected App and treats only absent package directories as a reason to fall back.
func resolveDefaultAppPackage(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	selected := appPackageFromEntrypoint(ActiveApp().Entrypoint)
	candidates := make([]string, 0, 2)
	if selected != "." {
		candidates = append(candidates, selected)
	}
	if selected != "./cmd/app" {
		candidates = append(candidates, "./cmd/app")
	}
	for _, candidate := range candidates {
		path := filepath.Join(root, strings.TrimPrefix(candidate, "./"))
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("inspect App package %s: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("inspect App package %s: expected a directory", path)
		}
		return candidate, nil
	}
	return ".", nil
}

// stackApp resolves one conventional App target before embedding App-specific defaults.
func (c *Cmd) stackApp(root string) (string, error) {
	if len(c.Args) > 0 {
		flag, _, _ := strings.Cut(c.Args[0], "=")
		if flag == "-C" || flag == "--C" {
			return "", fmt.Errorf("--stack requires --root instead of go build -C so generation and compilation use the same project")
		}
	}
	packages := goBuildPackages(c.Args)
	if len(packages) == 0 {
		path, err := resolveDefaultAppPackage(root)
		if err != nil {
			return "", err
		}
		packages = []string{path}
	}
	if len(packages) != 1 {
		return "", fmt.Errorf("--stack requires a single App package; build each App separately")
	}
	config, err := project.LoadProjectConfigAt(root)
	if err != nil {
		return "", err
	}
	path := packages[0]
	if module := c.modulePath(root); module != "" {
		if path == module {
			path = "."
		} else if strings.HasPrefix(path, module+"/") {
			path = "./" + strings.TrimPrefix(path, module+"/")
		}
	}
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(root, path)
		if err != nil {
			return "", fmt.Errorf("resolve Stack App package: %w", err)
		}
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || path == "cmd/app" {
		return project.DefaultAppName, nil
	}
	for name := range config.Apps {
		if path == filepath.ToSlash(filepath.Dir(project.AppForName(name).Entrypoint)) {
			return name, nil
		}
	}
	return "", fmt.Errorf("--stack requires a configured App package such as ./cmd/app; cannot resolve %q", packages[0])
}
