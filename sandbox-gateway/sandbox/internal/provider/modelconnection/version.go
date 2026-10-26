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

// CheckVersion refuses Claude connection mode when the installed CLI cannot
// enforce all subagent model bindings. Platform mode never calls this gate.
func CheckVersion(ctx context.Context, tool provider.Name, binary string, env []string) error {
	if tool != provider.ClaudeCode && tool != provider.ClaudeCodeACP {
		return nil
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return errors.New("model connection: Claude Code >=2.1.257 is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return errors.New("model connection: cannot inspect Claude Code version")
	}
	cacheKey := path + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)
	if _, ok := versionChecks.Load(cacheKey); ok {
		return nil
	}
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(versionCtx, path, "--version")
	cmd.Env = env
	output, err := cmd.Output()
	if err != nil || !supportedClaudeVersion(string(output)) {
		return errors.New("model connection: Claude Code >=2.1.257 is required to enforce subagent model routing; update the installed CLI")
	}
	versionChecks.Store(cacheKey, struct{}{})
	return nil
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
