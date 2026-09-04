package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/orbitproxy/orbitproxy-go/internal/userenv"
)

// Command error codes (aligned with execbridge / wire exec_preflight).
const (
	CodeCommandNotFound      = "command_not_found"
	CodeCommandNotExecutable = "command_not_executable"
	CodeSpawnFailed          = "spawn_failed"
	CodePackageNotInstalled  = "package_not_installed"
	CodeEnvFileMissing       = "env_file_missing"
)

// CommandConfig is the static exec command check input (no process start).
type CommandConfig struct {
	Command string
	Args    []string
	WorkDir string
}

// CommandResult is the outcome of CheckCommand.
type CommandResult struct {
	OK           bool   `json:"ok"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	ResolvedPath string `json:"resolved_path,omitempty"`
}

// CheckCommand verifies the command is on PATH / executable and workdir exists.
// For npx, it also requires the npm package to already be installed locally
// (global or workDir node_modules). It does not start the MCP server and
// will not let npx download from the registry.
func CheckCommand(cfg CommandConfig) *CommandResult {
	if cfg.Command == "" {
		return &CommandResult{
			ErrorCode:    CodeCommandNotFound,
			ErrorMessage: "command is empty",
		}
	}

	resolvedPath, err := userenv.LookPath(cfg.Command)
	if err != nil {
		if pkg := npxPackageSpec(cfg.Command, cfg.Args); pkg != "" {
			return &CommandResult{
				ErrorCode:    CodePackageNotInstalled,
				ErrorMessage: fmt.Sprintf("MCP package not installed locally: %s", pkg),
			}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return &CommandResult{
				ErrorCode:    CodeCommandNotFound,
				ErrorMessage: "command not found in PATH: " + cfg.Command,
			}
		}
		return &CommandResult{
			ErrorCode:    CodeCommandNotFound,
			ErrorMessage: err.Error(),
		}
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return &CommandResult{
			ErrorCode:    CodeCommandNotExecutable,
			ErrorMessage: "cannot stat command: " + err.Error(),
		}
	}
	if info.IsDir() {
		return &CommandResult{
			ErrorCode:    CodeCommandNotExecutable,
			ErrorMessage: "command path is a directory: " + resolvedPath,
		}
	}
	if info.Mode().Perm()&0o111 == 0 {
		return &CommandResult{
			ErrorCode:    CodeCommandNotExecutable,
			ErrorMessage: "command not executable: " + resolvedPath,
		}
	}

	if cfg.WorkDir != "" {
		dirInfo, err := os.Stat(cfg.WorkDir)
		if err != nil {
			return &CommandResult{
				ErrorCode:    CodeSpawnFailed,
				ErrorMessage: "work directory does not exist: " + cfg.WorkDir,
				ResolvedPath: resolvedPath,
			}
		}
		if !dirInfo.IsDir() {
			return &CommandResult{
				ErrorCode:    CodeSpawnFailed,
				ErrorMessage: "work directory path is not a directory: " + cfg.WorkDir,
				ResolvedPath: resolvedPath,
			}
		}
	}

	if pkg := npxPackageSpec(cfg.Command, cfg.Args); pkg != "" {
		if !npmPackageInstalled(pkg, resolvedPath, cfg.WorkDir) {
			return &CommandResult{
				ErrorCode:    CodePackageNotInstalled,
				ErrorMessage: fmt.Sprintf("MCP package not installed locally: %s", pkg),
				ResolvedPath: resolvedPath,
			}
		}
	}
	if pkg := uvxPackageSpec(cfg.Command, cfg.Args); pkg != "" {
		if !uvToolInstalled(pkg) {
			return &CommandResult{
				ErrorCode:    CodePackageNotInstalled,
				ErrorMessage: fmt.Sprintf("MCP package not installed locally: %s", pkg),
				ResolvedPath: resolvedPath,
			}
		}
	}

	return &CommandResult{
		OK:           true,
		ResolvedPath: resolvedPath,
	}
}

func npxPackageSpec(command string, args []string) string {
	base := strings.ToLower(filepath.Base(command))
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".exe")
	if base != "npx" {
		return ""
	}
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func packageDirName(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return spec
	}
	if strings.HasPrefix(spec, "@") {
		slash := strings.Index(spec, "/")
		if slash < 0 {
			return spec
		}
		rest := spec[slash+1:]
		if at := strings.LastIndex(rest, "@"); at > 0 {
			return spec[:slash+1+at]
		}
		return spec
	}
	if at := strings.LastIndex(spec, "@"); at > 0 {
		return spec[:at]
	}
	return spec
}

func npmPackageInstalled(spec, npxPath, workDir string) bool {
	name := packageDirName(spec)
	if name == "" {
		return false
	}
	for _, dir := range npmPackageCandidateDirs(npxPath, workDir) {
		if _, err := os.Stat(filepath.Join(dir, name, "package.json")); err == nil {
			return true
		}
	}
	return false
}

func npmPackageCandidateDirs(npxPath, workDir string) []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		for _, existing := range dirs {
			if existing == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	for _, dir := range userenv.ExtraNpmRoots() {
		add(dir)
	}
	add(userenv.NpmGlobalRoot())
	if npxPath != "" {
		binDir := filepath.Dir(npxPath)
		add(filepath.Join(filepath.Dir(binDir), "lib", "node_modules"))
	}
	if workDir != "" {
		add(filepath.Join(workDir, "node_modules"))
	}
	if cwd, err := os.Getwd(); err == nil && cwd != workDir {
		add(filepath.Join(cwd, "node_modules"))
	}
	home := userenv.Home()
	if home != "" {
		add(filepath.Join(home, ".npm-global", "lib", "node_modules"))
		add(filepath.Join(home, ".local", "lib", "node_modules"))
		if matches, err := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "lib", "node_modules")); err == nil {
			for _, dir := range matches {
				add(dir)
			}
		}
	}
	return dirs
}

func commandBase(command string) string {
	base := strings.ToLower(filepath.Base(command))
	base = strings.TrimSuffix(base, ".cmd")
	base = strings.TrimSuffix(base, ".exe")
	return base
}

func uvxPackageSpec(command string, args []string) string {
	if commandBase(command) != "uvx" {
		return ""
	}
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func uvToolName(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return ""
	}
	if i := strings.Index(spec, "=="); i > 0 {
		return spec[:i]
	}
	if i := strings.LastIndex(spec, "@"); i > 0 {
		return spec[:i]
	}
	return spec
}

func uvToolInstalled(spec string) bool {
	name := uvToolName(spec)
	if name == "" {
		return false
	}
	for _, dir := range uvToolCandidateDirs() {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func uvToolCandidateDirs() []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dirs = append(dirs, dir)
	}
	if v := strings.TrimSpace(os.Getenv("UV_TOOL_DIR")); v != "" {
		add(v)
	}
	add(queryUvToolDir())
	if home := userenv.Home(); home != "" {
		add(filepath.Join(home, ".local", "share", "uv", "tools"))
	}
	return dirs
}

func queryUvToolDir() string {
	uv, err := userenv.LookPath("uv")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, "tool", "dir")
	env := os.Environ()
	if path := userenv.PATH(); path != "" {
		env = replaceEnvValue(env, "PATH", path)
	}
	if home := userenv.Home(); home != "" {
		env = replaceEnvValue(env, "HOME", home)
	}
	cmd.Env = env
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func replaceEnvValue(env []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			if !replaced {
				out = append(out, prefix+val)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, prefix+val)
	}
	return out
}
