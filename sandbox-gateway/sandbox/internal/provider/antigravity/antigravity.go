// Package antigravity implements a dedicated one-shot codec for the Antigravity
// CLI (`agy -p <prompt> --dangerously-skip-permissions`). The CLI streams plain
// assistant text on stdout (no structured event stream), so each stdout line is
// forwarded as an assistant message chunk. Session continuity uses
// `--conversation <id>`, but the conversation id is not printed on stdout — it
// is recovered post-run from a --log-file the engine allocates (LogFileCodec).
package antigravity

import (
	"path/filepath"
	"regexp"
	"strings"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// Config parameterizes the Antigravity codec.
type Config struct {
	AgentName  provider.Name
	Bin        string
	Runtime    string
	ConfigRoot string
	// PrintTimeout is passed via --print-timeout to defeat agy's 5m default cap
	// (Go duration string, e.g. "24h"). Empty => "24h".
	PrintTimeout string
	AuthEnvFn    func(env []string) []string
	Catalog      []provider.Model
}

// New builds a one-shot provider for the Antigravity CLI.
func New(c Config) provider.Provider {
	return oneshot.NewProvider(&codec{CLI: oneshot.NewCLI(oneshot.CLIConfig{
		AgentName: c.AgentName, Bin: c.Bin, Runtime: c.Runtime, ConfigRoot: c.ConfigRoot,
		AuthEnvFn: c.AuthEnvFn, Catalog: c.Catalog,
	}), c: c})
}

type codec struct {
	oneshot.CLI
	c Config
}

func (d *codec) ReportsUsage() bool { return false }

// Args is the fallback (no log file); the engine prefers ArgsWithLog.
func (d *codec) Args(opts provider.OpenOptions, prompt, resumeID string) []string {
	return d.build(opts, prompt, resumeID, "")
}

// ArgsWithLog builds the argv with a --log-file so the engine can recover the
// conversation id after the process exits.
func (d *codec) ArgsWithLog(opts provider.OpenOptions, prompt, resumeID, logPath string) []string {
	return d.build(opts, prompt, resumeID, logPath)
}

func (d *codec) build(opts provider.OpenOptions, prompt, resumeID, logPath string) []string {
	timeout := d.c.PrintTimeout
	if timeout == "" {
		timeout = "24h"
	}
	args := []string{d.c.Bin, "-p", prompt, "--dangerously-skip-permissions"}
	if opts.Model != "" && opts.Model != "auto" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, "--print-timeout", timeout)
	if logPath != "" {
		args = append(args, "--log-file", logPath)
	}
	if resumeID != "" {
		args = append(args, "--conversation", resumeID)
	}
	if opts.Cwd != "" {
		args = append(args, "--add-dir", filepath.Clean(opts.Cwd))
	}
	args = append(args, opts.CustomArgs...)
	return args
}

// ParseLine forwards each non-empty stdout line as an assistant message chunk.
func (d *codec) ParseLine(line []byte) oneshot.ParseResult {
	text := string(line)
	if strings.TrimSpace(text) == "" {
		return oneshot.ParseResult{}
	}
	return oneshot.ParseResult{Msgs: []oneshot.Msg{{Kind: oneshot.KindText, Text: text}}}
}

// conversationIDRe matches the log line the CLI writes when it dispatches the
// user's message — the reliable source of the conversation UUID for fresh and
// resumed turns alike.
var conversationIDRe = regexp.MustCompile(
	`conversation=([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`,
)

// printTimeoutRe matches the marker the CLI logs when its print-mode wall-clock
// budget elapses while it exits 0 with a truncated reply.
var printTimeoutRe = regexp.MustCompile(`Print mode: timed out after \d+ polls`)

// providerErrorRe extracts terminal upstream/model errors the CLI logs but does
// not necessarily print to stdout or reflect in its exit code.
var providerErrorRe = regexp.MustCompile(`agent executor error:\s*(.+)`)

// ParseLogFile recovers the conversation id (resume pointer) and promotes
// silent log-only failures (print-timeout, provider errors) that the CLI would
// otherwise leave looking like a completed run.
func (d *codec) ParseLogFile(data []byte) oneshot.ParseResult {
	var r oneshot.ParseResult
	if m := conversationIDRe.FindAllSubmatch(data, -1); len(m) > 0 {
		// The id repeats through a turn; the last match resolves to the same
		// conversation and is what --conversation should pin next turn.
		r.SessionID = string(m[len(m)-1][1])
	}
	if printTimeoutRe.Match(data) {
		r.StopReason = "failed"
		r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: "agy --print-timeout elapsed waiting for the agent response"})
		return r
	}
	if m := providerErrorRe.FindAllSubmatch(data, -1); len(m) > 0 {
		r.StopReason = "failed"
		r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: "agy provider error: " + strings.TrimSpace(string(m[len(m)-1][1]))})
	}
	return r
}
