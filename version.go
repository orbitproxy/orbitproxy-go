package orbitproxy

import (
	_ "embed"
	"regexp"
	"runtime/debug"
	"strings"
)

//go:embed VERSION
var rawReleaseVersion string

// clientSemverRE 匹配可选 v 前缀的 SemVer。dev / devel / 空值一律非法。
var clientSemverRE = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func isClientSemver(version string) bool {
	return clientSemverRE.MatchString(version)
}

const modulePath = "github.com/orbitproxy/orbitproxy-go"

// ReleaseVersion 来自 VERSION 文件（与 git tag vX.Y.Z 对齐）。
var ReleaseVersion = normalizeReleaseVersion(rawReleaseVersion)

// injectedVersion 可由消费端二进制 ldflags 覆盖。sidecar 用自己的 Version，不要打这里。
var injectedVersion string

func normalizeReleaseVersion(raw string) string {
	version := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
	if version == "" {
		return ""
	}
	if version[0] == 'v' || version[0] == 'V' {
		return version
	}
	return "v" + version
}

// Version 返回本 SDK 的 semver：ldflags > 已 tag 的 module > VERSION 文件。
// 不会返回 dev / devel。
func Version() string {
	if isClientSemver(injectedVersion) {
		return injectedVersion
	}
	if v := versionFromBuildInfo(); isClientSemver(v) {
		return v
	}
	return ReleaseVersion
}

func versionFromBuildInfo() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	if v := versionFromModule(&bi.Main); v != "" {
		return v
	}
	for _, m := range bi.Deps {
		if v := versionFromModule(m); v != "" {
			return v
		}
	}
	return ""
}

func versionFromModule(m *debug.Module) string {
	if m == nil || m.Path != modulePath {
		return ""
	}
	if m.Replace != nil {
		if v := strings.TrimSpace(m.Replace.Version); isClientSemver(v) {
			return v
		}
		return ""
	}
	if v := strings.TrimSpace(m.Version); isClientSemver(v) {
		return v
	}
	return ""
}
