package version

import (
	"runtime/debug"
	"strings"
	"time"
)

const defaultModule = "pkt.systems/centaurx"

// buildVersion is set via -ldflags "-X pkt.systems/centaurx/internal/version.buildVersion=...".
var buildVersion = ""

// Current returns the best available version string (without dirty suffix).
func Current() string {
	return currentFromBuildInfo(false)
}

// CurrentWithDirty returns the best available version string (including dirty suffix when available).
func CurrentWithDirty() string {
	return currentFromBuildInfo(true)
}

// Module returns the module path from build info when available.
func Module() string {
	info, ok := debug.ReadBuildInfo()
	if ok {
		if path := strings.TrimSpace(info.Main.Path); path != "" {
			return path
		}
	}
	return defaultModule
}

func currentFromBuildInfo(includeDirty bool) string {
	if strings.TrimSpace(buildVersion) != "" {
		return normalizeVersion(buildVersion, includeDirty)
	}
	info, ok := debug.ReadBuildInfo()
	if ok {
		if v := strings.TrimSpace(info.Main.Version); v != "" && v != "(devel)" {
			return normalizeVersion(v, includeDirty)
		}
		if v := pseudoFromBuildInfo(info, includeDirty); v != "" {
			return normalizeVersion(v, includeDirty)
		}
	}
	return "v0.0.0-unknown"
}

func normalizeVersion(v string, includeDirty bool) string {
	value := strings.TrimSpace(v)
	if includeDirty {
		return value
	}
	return strings.TrimSuffix(value, "+dirty")
}

func pseudoFromBuildInfo(info *debug.BuildInfo, includeDirty bool) string {
	if info == nil {
		return ""
	}
	var revision string
	var vcsTime string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			vcsTime = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" || vcsTime == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, vcsTime)
	if err != nil {
		return ""
	}
	rev := revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	ver := "v0.0.0-" + parsed.UTC().Format("20060102150405") + "-" + rev
	if modified && includeDirty {
		ver += "+dirty"
	}
	return ver
}
