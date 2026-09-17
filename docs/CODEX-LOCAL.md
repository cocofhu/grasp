# Codex CLI with a local login

Select **CLI account → Codex CLI** when creating an Agent or completing onboarding.
The API key step may be skipped when the Grasp server is configured to reuse a local login.

Run `codex login` on the Grasp server host first. Set the server environment variable
`GRASP_CODEX_AUTH_FILE` to the absolute path of that account's `auth.json`
(normally `$CODEX_HOME/auth.json`, or the user's `.codex/auth.json`). This is an explicit
operator opt-in; merely installing Grasp does not discover or export local credentials.

Set `GRASP_SANDBOX_IMAGE_CODEX` to a sandbox image containing Codex CLI and the updated
bridge. Leave `GRASP_SANDBOX_IMAGE` empty if selecting a separate image per backend;
the global image override takes priority over per-backend images.

The local Windows launcher in `.devdata/start.ps1` enables this integration for this
checkout. `grasp-codex:local` contains Codex CLI 0.154.0. The normal universal Dockerfile
also includes Codex in its default agent installation list.

Execution remains inside the Grasp sandbox. Only the login file is copied into the
temporary Codex config home; host sessions, history and global configuration are not
copied. The file is not saved in Agent metadata, returned by the UI, baked into images,
or written to logs. The sandbox copy is writable so token refresh can work, but changes
are not written back to the host. Re-login on the host and create a new sandbox when
authentication expires. Sandbox credentials live until that sandbox/config home is
destroyed by the existing lifecycle. Only trusted local users should access a Grasp
instance configured to use the host account.

Agent MCP entries are translated to Codex `config.toml`; platform and Agent rules are
combined into `AGENTS.md`. `ACP_BRIDGE_MODEL` selects a model explicitly; otherwise the
CLI chooses its default. An optional Agent `config.toml` supplies Codex configuration.
Image attachments are not supported by this one-shot adapter yet.

The bridge handles current `thread.started`, `item.*`, `turn.completed` and
`turn.failed` JSONL events and resumes the thread for subsequent chat turns.

The opt-in `TestLiveCodexEditAndResume` test must run inside a disposable container:
it consumes real model usage, creates a Python file, resumes the same conversation to
edit it, and verifies file contents and token reporting. Normal tests skip it.
