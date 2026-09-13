package generate

import (
	"slices"

	"github.com/goforj/goforj/project"
)

// BaselineDrivers shares the generators' always-available providers with configuration tooling.
func BaselineDrivers(resource project.ResourceKey) []string {
	var drivers []string
	switch resource {
	case project.ResourceDatabase:
		drivers = dbLocalDrivers
	case project.ResourceCache:
		drivers = cacheLocalDrivers
	case project.ResourceQueue:
		drivers = queueLocalDrivers
	case project.ResourceEvents:
		drivers = eventLocalDrivers
	case project.ResourceStorage:
		drivers = storageLocalDrivers
	case project.ResourceMail:
		drivers = mailLocalDrivers
	}
	return slices.Clone(drivers)
}
