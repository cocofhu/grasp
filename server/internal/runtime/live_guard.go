package runtime

import (
	"context"
	"errors"
	"fmt"
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

// LiveBaselinePreparer freezes tracked marker examples before the first Live
// generation executes. Failure must stop that turn before the agent edits source.
type LiveBaselinePreparer interface {
	PrepareLiveBaseline(ctx context.Context, runID, nodeID string) error
}

var liveAttributePattern = regexp.MustCompile(`data-grasp-(live|variant)[[:space:]]*=[[:space:]]*(\{[^}]*\}|"[^"]*"|'[^']*'|[^ \t\r\n<>]+)?`)

type liveMarkerBaseline map[string]map[string]int

func (c *acpProvider) PrepareLiveBaseline(ctx context.Context, runID, nodeID string) error {
	sess, ws, ok := c.parkedWorkspace(runID, nodeID)
	if !ok {
		return errors.New("Live 预览会话不可用,请先恢复节点会话后重试")
	}
	sess.liveMu.Lock()
	defer sess.liveMu.Unlock()
	if sess.liveBaseline != nil {
		return nil
	}
	out, err := sess.sb.ExecScript(ctx, 30*time.Second, "bash", liveBaselineScript(ws))
	if err != nil {
		return fmt.Errorf("读取 Live 源码基线失败: %w", err)
	}
	files, err := liveScanFiles(out)
	if err != nil {
		return err
	}
	baseline := liveMarkerBaseline{}
	for path, source := range files {
		counts := map[string]int{}
		for _, line := range liveMarkerLines(source) {
			counts[line]++
		}
		baseline[path] = counts
	}
	sess.liveBaseline = baseline
	return nil
}

var liveMarkerPattern = regexp.MustCompile(models.LiveMarkerAttr + `[[:space:]]*=[[:space:]]*\{?[[:space:]]*["']([A-Za-z0-9_-]+)["']`)
var liveAssignmentPattern = regexp.MustCompile(models.LiveMarkerAttr + `[[:space:]]*=`)
var liveVariantAssignmentPattern = regexp.MustCompile(`data-grasp-variant[[:space:]]*=`)

// liveScanExcludes are build output and dependency dirs a marker in source
// never needs to be looked for in.
const liveScanExcludes = "--exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist --exclude-dir=build " +
	"--exclude-dir=.next --exclude-dir=.nuxt --exclude-dir=.svelte-kit --exclude-dir=.output " +
	"--exclude-dir=coverage --exclude-dir=.turbo --exclude-dir=.cache"

// liveGuardHook rejects commits that add a Live marker. Marked so a rerun
// recognises its own hook and never overwrites someone else's.
const liveGuardHook = `#!/bin/sh
# grasp-live-guard
if git diff --cached -U0 | grep -E '^\+.*data-grasp-(live|variant)[[:space:]]*=' >/dev/null 2>&1; then
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
	out, err := sess.sb.ExecScript(ctx, 30*time.Second, "bash", liveScanScript(ws))
	if err != nil {
		return nil, true, err
	}
	files, err := liveScanFiles(out)
	if err != nil {
		return nil, true, err
	}
	sess.liveMu.Lock()
	baseline := sess.liveBaseline // immutable after successful initialization
	sess.liveMu.Unlock()
	var changed strings.Builder
	for path, source := range files {
		seen := map[string]int{}
		for _, line := range liveMarkerLines(source) {
			seen[line]++
			if seen[line] > baseline[path][line] {
				changed.WriteString(line)
				changed.WriteByte('\n')
			}
		}
	}
	out = changed.String()
	// A dynamic/malformed wrapper or an orphan variant is still preview code.
	// Do not declare the workspace clean when its session cannot be resolved.
	if len(liveAssignmentPattern.FindAllStringIndex(out, -1)) != len(liveMarkerPattern.FindAllStringIndex(out, -1)) {
		return nil, true, errors.New("Live 标记无法解析,请清理源码中的预览包装后重试")
	}
	if liveVariantAssignmentPattern.MatchString(out) && !liveAssignmentPattern.MatchString(out) {
		return nil, true, errors.New("源码中仍有未清理的 Live 变体标记")
	}
	return parseLiveMarkerSIDs(out), true, nil
}

// grep status 1 is a successful scan with no matches; status 2 is a read or
// execution failure. Avoid pipelines and truncation that can mask those errors.
func liveScanScript(ws string) string {
	return "[ -d " + shellArg(ws) + " ] || { echo 'Live workspace is unavailable' >&2; exit 2; }\n" +
		"scan_files=$(mktemp) || exit 2\ntrap 'rm -f \"$scan_files\"' EXIT\n" +
		"grep -rIlZE " + liveScanExcludes + " " +
		shellArg(`data-grasp-(live|variant)[[:space:]]*=`) + " " + shellArg(ws) + " >\"$scan_files\"\n" +
		"scan_status=$?\ncase \"$scan_status\" in 0|1) ;; *) exit \"$scan_status\";; esac\n" +
		// Read complete matching files so formatter-split attribute values still
		// resolve. NUL-delimited paths preserve whitespace and newlines safely.
		"while IFS= read -r -d '' scan_file; do\n  printf '%s\\0' \"$scan_file\"\n  cat -- \"$scan_file\" || exit 2\n  printf '\\0'\ndone <\"$scan_files\"\n"
}

// Snapshot committed source once, rather than dirty/untracked content or a HEAD
// that can advance during later turns. Workspace layouts are one repo at root
// or flat multi-repo clones, matching sandbox provisioning.
func liveBaselineScript(ws string) string {
	return "[ -d " + shellArg(ws) + " ] || exit 2\n" +
		"baseline_files=$(mktemp) || exit 2\ntrap 'rm -f \"$baseline_files\"' EXIT\n" +
		"for repo in " + shellArg(ws) + " " + shellArg(ws) + `/*; do
  [ -e "$repo/.git" ] || continue
  git -C "$repo" rev-parse --git-dir >/dev/null || exit 2
  base=$(git -C "$repo" rev-parse --verify HEAD 2>/dev/null)
  if [ -z "$base" ]; then
    ref=$(git -C "$repo" symbolic-ref -q HEAD) || exit 2
    git -C "$repo" show-ref --verify --quiet "$ref"
    [ "$?" = 1 ] || exit 2
    continue
  fi
  git -C "$repo" grep -IlzE 'data-grasp-(live|variant)[[:space:]]*=' "$base" -- . \
    ':(exclude)**/node_modules/**' ':(exclude)**/dist/**' ':(exclude)**/build/**' \
    ':(exclude)**/.next/**' ':(exclude)**/.nuxt/**' ':(exclude)**/.svelte-kit/**' \
    ':(exclude)**/.output/**' ':(exclude)**/coverage/**' ':(exclude)**/.turbo/**' ':(exclude)**/.cache/**' >"$baseline_files"
  status=$?
  case "$status" in 0|1) ;; *) exit "$status";; esac
  while IFS= read -r -d '' entry; do
    path=${entry#*:}
    printf '%s\0' "$repo/$path"
    git -C "$repo" show "$base:$path" || exit 2
    printf '\0'
  done <"$baseline_files"
done
`
}

func liveScanFiles(out string) (map[string]string, error) {
	files := map[string]string{}
	if out == "" {
		return files, nil
	}
	parts := strings.Split(out, "\x00")
	if len(parts)%2 != 1 || parts[len(parts)-1] != "" {
		return nil, errors.New("Live 源码扫描结果不完整")
	}
	for i := 0; i < len(parts)-1; i += 2 {
		if parts[i] == "" {
			return nil, errors.New("Live 源码扫描缺少文件路径")
		}
		files[parts[i]] = parts[i+1]
	}
	return files, nil
}

// Include whole source lines around assignments, including formatter-split
// values. This ignores unchanged examples while detecting changed values,
// newly duplicated attributes and edits to the surrounding markup.
func liveMarkerLines(source string) []string {
	var spans [][2]int
	for _, match := range liveAttributePattern.FindAllStringIndex(source, -1) {
		start := strings.LastIndex(source[:match[0]], "\n") + 1
		end := len(source)
		if n := strings.Index(source[match[1]:], "\n"); n >= 0 {
			end = match[1] + n
		}
		if n := len(spans); n > 0 && start <= spans[n-1][1] {
			if end > spans[n-1][1] {
				spans[n-1][1] = end
			}
		} else {
			spans = append(spans, [2]int{start, end})
		}
	}
	lines := make([]string, 0, len(spans))
	for _, span := range spans {
		lines = append(lines, source[span[0]:span[1]])
	}
	return lines
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
