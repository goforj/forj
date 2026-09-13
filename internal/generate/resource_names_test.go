package generate

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/goforj/goforj/project"
)

// TestResourceNamesSharesGeneratorDiscovery includes implicit children and App overlays while excluding driver helper keys.
func TestResourceNamesSharesGeneratorDiscovery(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".goforj.yml"), []byte("apps:\n  admin:\n    components: [cli, database_sqlite, cache]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	names := ResourceNames(root, map[string]string{"DB_ANALYTICS_DATABASE": "analytics", "DB_SQLITE_DATABASE": "app.db", "ADMIN_CACHE_SESSIONS_DRIVER": "redis"})
	if !slices.Contains(names[project.ResourceDatabase], "analytics") || slices.Contains(names[project.ResourceDatabase], "sqlite") {
		t.Fatalf("database names: %#v", names)
	}
	if !slices.Contains(names[project.ResourceCache], "sessions") {
		t.Fatalf("cache names: %#v", names)
	}
}
