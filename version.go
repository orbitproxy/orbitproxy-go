package orbitproxy

import (
	"runtime/debug"
	"strings"
)

const (
	modulePath = "github.com/orbitproxy/orbitproxy-go"
	// fallbackVersion is used only when build info is unavailable.
	fallbackVersion = "devel"
)

// Version returns this SDK's module version from the build info of the
// running binary (same value Go records in go.mod for dependents).
//
// Typical values:
//   - "v0.1.0" when depended on via a tagged module
//   - "(devel)" when built from a local checkout / replace
//
// CLI wrappers should pass StartOptions.SoftVersion / RegisterOptions.Version
// from their own ldflags instead of relying on this.
func Version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return fallbackVersion
	}

	if v := versionFromModule(&bi.Main); v != "" {
		return v
	}
	for _, m := range bi.Deps {
		if v := versionFromModule(m); v != "" {
			return v
		}
	}
	return fallbackVersion
}

func versionFromModule(m *debug.Module) string {
	if m == nil || m.Path != modulePath {
		return ""
	}
	if m.Replace != nil {
		if v := strings.TrimSpace(m.Replace.Version); v != "" {
			return v
		}
		// Local replace often has an empty Version; still identify as devel.
		return fallbackVersion
	}
	if v := strings.TrimSpace(m.Version); v != "" {
		return v
	}
	return ""
}
