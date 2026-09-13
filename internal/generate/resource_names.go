package generate

import (
	"github.com/goforj/goforj/project"
)

// ResourceNames exposes the generator's named-resource discovery to configuration tooling.
func ResourceNames(root string, values map[string]string) map[project.ResourceKey][]string {
	environment := generationEnvironment{values: values}
	input := generationInput{projectDir: root, environment: environment, appPrefixes: newGenerationEnvironmentFilter(root, environment).sortedAppPrefixes()}
	return map[project.ResourceKey][]string{
		project.ResourceDatabase: discoverDBConnectionNames(input),
		project.ResourceCache:    discoverCacheStoreNames(input),
		project.ResourceQueue:    discoverQueueNames(input),
		project.ResourceEvents:   discoverEventNames(input),
		project.ResourceStorage:  discoverStorageDiskNames(input),
		project.ResourceMail:     discoverMailNames(input),
	}
}
