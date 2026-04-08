package version

import (
	"time"
)

var (
	build    Build
	hasBuilt = false
)

// Build holds details about this build of the Ship binary
type Build struct {
	Version      string    `json:"version,omitempty"`
	GitSHA       string    `json:"git,omitempty"`
	BuildTime    time.Time `json:"buildTime,omitempty"`
	TimeFallback string    `json:"buildTimeFallback,omitempty"`
}

// Init sets up the version info from build args
func Init() {
	build.Version = version
	if len(gitSHA) >= 7 {
		build.GitSHA = gitSHA[:7]
	}
	var err error
	build.BuildTime, err = time.Parse(time.RFC3339, buildTime)
	if err != nil {
		build.TimeFallback = buildTime
	}
	hasBuilt = true
}

// GetBuild gets the build
func GetBuild() Build {
	if !hasBuilt {
		Init()
	}
	return build
}

// Version gets the version
func Version() string {
	if !hasBuilt {
		Init()
	}
	return build.Version
}

// GitSHA gets the gitsha
func GitSHA() string {
	if !hasBuilt {
		Init()
	}
	return build.GitSHA
}

// BuildTime gets the build time
func BuildTime() time.Time {
	if !hasBuilt {
		Init()
	}
	return build.BuildTime
}

// ManagerImage gets the manager image, defaulting to stigenai/schemahero-manager
// (stigen fork) if not set. The stigen fork image lives in a private Docker Hub
// repo; child per-Database controller StatefulSets rely on this default when the
// HelmRelease does not pass --manager-image explicitly.
func ManagerImage() string {
	if managerImage != "" {
		return managerImage
	}
	return "stigenai/schemahero-manager"
}

// PluginRegistry gets the plugin registry override, empty if not set
func PluginRegistry() string {
	return pluginRegistry
}
