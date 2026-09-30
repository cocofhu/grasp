package runtime

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// LiveMarkerScanner is an optional provider capability for Live variants:
// find preview markers left in a parked session's worktree and install the
// local pre-commit guard that keeps them out of commits.
type LiveMarkerScanner interface {
	// LiveMarkerSIDs returns the Live session ids whose wrapper markers are in
	// the node's workspace. ok=false when no session is parked.
	LiveMarkerSIDs(ctx context.Context, runID, nodeID string) (sids []string, ok bool, err error)
	// InstallLiveGuard writes the pre-commit guard into every git repo under
	// the workspace (best-effort, idempotent).
	InstallLiveGuard(ctx context.Context, runID, nodeID string)
}

var liveMarkerPattern = regexp.MustCompile(models.LiveMarkerAttr + `="([A-Za-z0-9_-]+)"`)

// liveScanExcludes are build output and dependency dirs a marker in source
// never needs to be looked for in.
const liveScanExcludes = "--exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist --exclude-dir=build " +
	"--exclude-dir=.next --exclude-dir=.nuxt --exclude-dir=.svelte-kit --exclude-dir=.output " +
	"--exclude-dir=coverage --exclude-dir=.turbo --exclude-dir=.cache"

// liveGuardHook rejects commits that add a Live marker. Marked so a rerun
// recognises its own hook and never overwrites someone else's.
const liveGuardHook = `#!/bin/sh
# grasp-live-guard
if git diff --cached -U0 | grep -E '^\+.*data-grasp-(live|variant)=' >/dev/null 2>&1; then
  echo "grasp: 提交里有 Live 变体预览标记(data-grasp-live / data-grasp-variant),请先采用或放弃变体" >&2
  exit 1
fi
`

func (c *acpProvider) parkedWorkspace(runID, nodeID string) (*reactSession, string, bool) {
	c.mu.Lock()
	sess := c.sessions[runID+"|"+nodeID]
	c.mu.Unlock()
	if sess == nil || sess.sb == nil {
		return nil, "", false
	}
	ws := sess.sb.WorkspaceDir
	if ws == "" {
		ws = "/root/workspace"
	}
	return sess, ws, true
}

func (c *acpProvider) LiveMarkerSIDs(ctx context.Context, runID, nodeID string) ([]string, bool, error) {
	sess, ws, ok := c.parkedWorkspace(runID, nodeID)
	if !ok {
		return nil, false, nil
	}
	script := "grep -rIohE " + liveScanExcludes + " " +
		shellArg(models.LiveMarkerAttr+`="[A-Za-z0-9_-]+"`) + " " + shellArg(ws) + " 2>/dev/null | sort -u | head -n 64; true"
	out, err := sess.sb.ExecScript(ctx, 30*time.Second, "bash", script)
	if err != nil {
		return nil, true, err
	}
	return parseLiveMarkerSIDs(out), true, nil
}

func parseLiveMarkerSIDs(out string) []string {
	seen := map[string]bool{}
	var sids []string
	for _, m := range liveMarkerPattern.FindAllStringSubmatch(out, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			sids = append(sids, m[1])
		}
	}
	sort.Strings(sids)
	return sids
}

func (c *acpProvider) InstallLiveGuard(ctx context.Context, runID, nodeID string) {
	sess, ws, ok := c.parkedWorkspace(runID, nodeID)
	if !ok {
		return
	}
	_, _ = sess.sb.ExecScript(ctx, 20*time.Second, "bash", liveGuardScript(ws))
}

func liveGuardScript(ws string) string {
	var b strings.Builder
	b.WriteString("HOOK=" + shellArg(liveGuardHook) + "\n")
	b.WriteString("for d in " + shellArg(ws) + " " + shellArg(ws) + `/*; do
  [ -d "$d/.git" ] || continue
  h="$d/.git/hooks/pre-commit"
  if [ -e "$h" ] && ! grep -q grasp-live-guard "$h" 2>/dev/null; then continue; fi
  mkdir -p "$d/.git/hooks" && printf '%s' "$HOOK" > "$h" && chmod +x "$h"
done
true
`)
	return b.String()
}
