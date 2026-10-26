package modelconnection

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"

	"backend/internal/provider"
)

var cliVersion = regexp.MustCompile(`(?:^|\s)([0-9]+)\.([0-9]+)\.([0-9]+)([^0-9.]|$)`)
var versionChecks sync.Map

// CheckVersion refuses adapters whose CLI cannot enforce their managed routing.
// OpenCode calls this gate only for its isolated official platform mode.
func CheckVersion(ctx context.Context, tool provider.Name, binary string, env []string) error {
	if tool != provider.ClaudeCode && tool != provider.ClaudeCodeACP && tool != provider.OpenCode {
		return nil
	}
	minimum := "Claude Code >=2.1.257"
	supported := supportedClaudeVersion
	if tool == provider.OpenCode {
		minimum = "OpenCode >=1.4.1"
		supported = supportedOpenCodeVersion
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return errors.New("model routing: " + minimum + " is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return errors.New("model routing: cannot inspect CLI version")
	}
	cacheKey := string(tool) + ":" + path + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)
	if _, ok := versionChecks.Load(cacheKey); ok {
		return nil
	}
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(versionCtx, path, "--version")
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil || !supported(string(output)) {
		return errors.New("model routing: " + minimum + " is required to enforce model routing; update the installed CLI")
	}
	versionChecks.Store(cacheKey, struct{}{})
	return nil
}

func supportedOpenCodeVersion(text string) bool {
	match := cliVersion.FindStringSubmatch(text)
	if len(match) != 5 || match[4] == "-" {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	return major > 1 || major == 1 && (minor > 4 || minor == 4 && patch >= 1)
}

func supportedClaudeVersion(text string) bool {
	match := cliVersion.FindStringSubmatch(text)
	if len(match) != 5 || match[4] == "-" {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	return major > 2 || major == 2 && (minor > 1 || minor == 1 && patch >= 257)
}
