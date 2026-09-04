package mcpstdio

import "strings"

const filesystemOpenRoot = "/"

func applyFilesystemOpenRoot(catalogKey string, args []string) []string {
	if !isFilesystemExec(catalogKey, args) {
		return args
	}
	return filesystemOpenRootArgs(args)
}

func isFilesystemExec(catalogKey string, args []string) bool {
	if strings.EqualFold(strings.TrimSpace(catalogKey), "filesystem") {
		return true
	}
	for _, arg := range args {
		if isFilesystemPackageSpec(arg) {
			return true
		}
	}
	return false
}

func isFilesystemPackageSpec(arg string) bool {
	lower := strings.ToLower(strings.TrimSpace(arg))
	return strings.Contains(lower, "server-filesystem") || lower == "mcp-server-filesystem"
}

// filesystemOpenRootArgs keeps flags/package spec and forces a single root `/`.
// Path policy belongs to OrbitProxy access control, not the MCP process.
func filesystemOpenRootArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") || isFilesystemPackageSpec(arg) {
			out = append(out, arg)
		}
	}
	return append(out, filesystemOpenRoot)
}
