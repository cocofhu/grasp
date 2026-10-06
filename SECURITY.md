# Security policy

Report vulnerabilities privately through GitHub
[Private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on [github.com/cocofhu/approving](https://github.com/cocofhu/approving).

Do not disclose suspected vulnerabilities in public issues, pull requests,
discussions, logs, or screenshots.

## Supported versions

Only the latest published release line on GitHub Releases is supported for
security fixes until a longer support matrix is published.

## Response targets

- Acknowledgement: within 3 business days
- Status updates: at least every 7 days while the report remains open
- Coordinated disclosure: agree on a public date after a fix or mitigation is
  available, or after a reasonable remediation window

## Temporary human_gate approval links

Inbox operators can mint a one-shot external approval URL for a single pending
`human_gate` instance (`/public/gate-approvals#t=…`).

- Token entropy is ≥256-bit CSPRNG. The server stores **only** SHA-256 hex;
  verification uses constant-time compare. Plaintext tokens are never written
  to disk, logs, or audit payloads.
- The share URL places the token **only in the fragment**. Public preview/decide
  APIs accept the token via `X-Gate-Share-Token` / JSON body — never path or
  query (so it is not copied into Referer or common access logs).
- Scope is one Run + one `human_gate` node + the current pending iteration.
  Holders cannot open projects, other runs/nodes, or authenticated `/api/*`.
- Default TTL is 24h (1h / 8h / 24h / 72h / 7d). One active link per instance.
  Minting also stores a link-level `permissionPreset` (`full` default, or
  `react_only`). Preview.actions and public decide/reply/cancel/live share the
  same Allow(preset, action) check: `react_only` may reply/cancel but every
  decide and every Live write (generate / accept / discard / steer, including
  discard-all) is rejected with `403 permission_denied` **before** ConsumeCAS, so the
  one-shot token is not marked used and the gate does not advance. The preset is
  required when minting. Regenerating immediately revokes
  the previous URL and reuses the same TTL tier **and** permission preset from
  the new mint time (change permission only via Create). Revoke, expiry,
  successful decide, login-side resume, run cancel/complete, or a new gate
  iteration all invalidate unused links immediately. ReAct-only links that
  never successfully decide still expire via TTL / revoke / login-side finish.
- Public responses set `Cache-Control: no-store`, `Referrer-Policy: no-referrer`,
  and a strict CSP. The `/public/gate-approvals` prefix does **not** emit
  `Access-Control-Allow-Origin`. POST decide requires Origin/Referer + custom
  header + one-time nonce, and is per-IP rate limited.
- Share URLs mint from this request's `Host` (plus TLS /
  `X-Forwarded-Proto` for scheme). Client `X-Forwarded-Host` is never trusted
  for minting or Public CSRF Origin/Referer checks. Reverse proxies must
  preserve the browser's original Host (for example nginx
  `proxy_set_header Host $host`); when TLS terminates upstream, forward
  `X-Forwarded-Proto` correctly. `server.public_advertise` remains for QQ /
  preview deep links and is **not** the gate-share mint source. Third-party
  Origin still fails. External preview only returns this `human_gate`'s
  primary products — it does not scan other nodes in the Run.
- Preview nonces live in the GateShare database (same DB as share links), keyed
  by token hash, last few unused nonces, 15-minute TTL, no plaintext token.
  Multi-replica preview→decide share the same bucket. The public IP limiter
  remains in-process; GET preview and POST decide use separate per-IP buckets so
  polling cannot starve confirm. The store keeps the last few nonces per link so
  multiple tabs can submit after refresh.
- Public app-preview remote desktop uses a **separate** short-lived ticket
  (not the long-lived share token): clients exchange via
  `POST /public/gate-approvals/preview-ticket` with `X-Gate-Share-Token`, then
  connect `GET /public/gate-approvals/preview-vnc/ws?ticket=…` or load a
  same-origin `/public/gate-approvals/preview-api/:ticket/…` iframe. Tickets are
  ~2 minutes, keyed by token hash + run/node/port; share-token lifetime rules
  still apply and revocation/decide kicks live sessions. Preview DTO ports are
  desensitized (no `runId`/`nodeId`/internal paths). Logged-in
  `/preview-vnc/:runId/:nodeId/:port/ws` remains Session-gated and unchanged.
- Audit records create / regen / revoke / use with `callerKind=external` on
  use, optional self-reported name, masked IP (last octet/group), and
  browser/OS UA only — still without the plaintext token.

## Safe harbor

We will not pursue legal action against researchers who:

- make a good-faith effort to avoid privacy violations, service disruption, and
  data destruction;
- do not access or modify data that is not their own beyond what is needed to
  demonstrate the issue;
- report findings promptly through the private channel above and give us a
  reasonable chance to remediate before public disclosure.

## Sandbox data-plane trust boundary (CDP / noVNC)

Unauthenticated Chromium CDP (`:9222`, socat) and noVNC/websockify (`:6080`,
`x11vnc -nopw`) must not be reachable from users or untrusted networks.

- **Users** reach the desktop only through Grasp WebSocket proxies:
  `/sandbox-vnc/:sandboxId/ws` and `/preview-vnc/:runId/:nodeId/:port/ws`.
  When platform Auth is injected (always on outside local-demo), these require
  a valid session cookie. Auth checks **Session validity only** — it does **not**
  check sandbox or run ownership; any logged-in user who knows the URL can
  connect. The UI does not expose or copy direct `cdp` / `novnc` addresses.
- **session / ide / ssh** may still be published. They use their own passwords
  or SSH keys (`ROOT_PASSWORD` / `SSH_KEY` / IDE password). Direct CDP/noVNC
  is not a substitute.
- **Grasp** (and other in-cluster / Docker-network peers) may dial CDP and
  noVNC on the container IP or ClusterIP DNS. Grasp running *outside* the
  cluster or Docker network is **not** a supported topology for CDP/VNC control.
- **Residual risk**: pods on the same cluster / Docker network can still reach
  `:9222` / `:6080`. This change does not add NetworkPolicy.
- **Docker inventory**: already-running containers keep their existing `-p`
  mappings until TTL expiry or Reinstall. Old `host:9222` / `host:6080`
  bookmarks becoming unreachable is an **expected breaking change**.
- **Kubernetes inventory**: existing `*-lb` Services that still list 9222/6080
  stay exposed until gateway **startup reconcile** (`ReconcileOnStartup`),
  `Start`, or `Reinstall` updates `Spec.Ports`. Until that heal completes, old
  bookmarks and untrusted scans may still succeed. After converge, Endpoints
  report ClusterIP DNS for CDP/noVNC; LB Ingress IPs are public ports only.
- Do not rely on traditional VNC 8-character passwords; isolation is by
  publish surface + platform Session, not RFB auth.
