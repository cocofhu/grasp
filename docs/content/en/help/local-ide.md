# Open sandboxes from a local IDE

With the built-in SSH gateway enabled, you can open any sandbox (E2B, Docker or Kubernetes) from VS Code, Cursor (Remote-SSH) or a terminal on your own machine. You only need the IDE and the OpenSSH client that ships with your OS.

```
VS Code / Cursor ──SSH──> Grasp SSH gateway (port 2222 by default) ──> sandbox sshd
```

The SSH user name selects the sandbox: `sbx-<sandbox id>`. The gateway authenticates the public key you registered in Grasp, then logs into the sandbox as root. Keys stay in Grasp and are never written into sandboxes; deleting a key closes every connection that used it.

## Admins: enable the gateway

| Setting | Environment variable | Meaning |
| --- | --- | --- |
| `ssh_gateway.enabled` | `GRASP_SSH_GATEWAY_ENABLED` | `true` turns it on; off by default |
| `ssh_gateway.listen` | `GRASP_SSH_GATEWAY_LISTEN` | Listen address, default `:2222` |
| `ssh_gateway.public_host` | `GRASP_SSH_GATEWAY_PUBLIC_HOST` | Host name users connect to; empty uses the host name the browser opened Grasp with |
| `ssh_gateway.public_port` | `GRASP_SSH_GATEWAY_PUBLIC_PORT` | Port users connect to; set it when a port mapping or load balancer changes it |
| `ssh_gateway.host_key_path` | `GRASP_SSH_GATEWAY_HOST_KEY_PATH` | Gateway host private key, generated on first start |
| `ssh_gateway.max_conns` / `max_conns_per_sandbox` | Same names in upper case | Global / per-sandbox connection limits, default 200 / 10 |
| `ssh_gateway.admins_only` | `GRASP_SSH_GATEWAY_ADMINS_ONLY` | Allow only admin accounts |

When deploying:

- **Expose the TCP port.** The Docker Compose examples use host networking, so open the port in the firewall. A Kubernetes Ingress only carries HTTP; add a `LoadBalancer` or `NodePort` Service such as the commented `grasp-ssh` at the end of `sandbox-gateway/deploy/k8s/stack.example.yaml`. Networks that only allow port 443 may block it.
- **Persist the host key.** Every sandbox is served under this one key, so rebuilt or resumed sandboxes never trigger a "host key changed" warning. Keep it on a persistent volume (the examples use `/data`) and share it between replicas, or clients will warn after every redeploy.
- **Settings are read at startup**; restart the server after changing them.

## Security notes

- Access is authorized per account: the owner of a Solo session may enter that session's sandbox, and admins may enter any sandbox; sandboxes without an owner (workflows, tests) are admin-only. Missing and forbidden sandboxes give the same message. Open connections close within a minute when the session is deleted or reassigned, the key is deleted (immediately) or the account is removed. With `admins_only` only admins may connect.
- The gateway terminates and re-opens the SSH connection, so it can see session contents. The platform already holds root on every sandbox, so the trust boundary does not change.
- Only public-key authentication is accepted. A source address is refused for a while after 20 failed attempts within 5 minutes; connections still authenticating are capped separately (64 in total, 8 per source) and never use the authenticated slots.
- Remote forwarding (`-R`), agent forwarding and X11 are not supported. Local forwarding (`-L`), SFTP and the channels VS Code and Cursor need all work.
- Restarting the server drops open connections; IDEs reconnect on their own.

## Users: first connection

1. Create a key if you do not have one: `ssh-keygen -t ed25519`.
2. Open **SSH keys** from the account menu at the bottom left and paste the contents of `~/.ssh/id_ed25519.pub`. ed25519, ECDSA and RSA of at least 2048 bits are supported.
3. In a Solo session, click the **info icon** at the top right and choose **Open in VS Code** under **Local IDE**. The drop-down switches to Cursor, CodeBuddy, Trae (`CN` opens the China edition) or JetBrains Gateway, and remembers your choice.
4. The browser asks to open the app, and the IDE asks once whether to open the remote folder; accept both. The IDE connects to `sbx-12@grasp.example.com:2222` and opens `/root/workspace` directly, with no `~/.ssh/config` change.

| IDE | How it opens |
| --- | --- |
| VS Code | Needs the Remote - SSH extension |
| Cursor | Install Cursor's Remote SSH extension when prompted the first time |
| CodeBuddy / Trae | Remote-SSH is built in; the international and China editions are separate apps with separate menu entries |
| JetBrains Gateway | No one-click link: Gateway links only attach to an IDE already installed on the host. The entry shows the connection details; in Gateway choose SSH → New Connection, enter the user, host and port, and use `/root/workspace` as the project folder. The first connection downloads the IDE backend into the sandbox |

**SSH connection details** in the drop-down lists the user, host, port and an SSH command such as `ssh -p 2222 sbx-12@grasp.example.com` that runs in any terminal. For a short name such as `ssh grasp`, add the optional snippet from the panel to `~/.ssh/config`:

```
Host grasp
  HostName grasp.example.com
  Port 2222
  ServerAliveInterval 30
```

One machine can connect to several sandboxes at once, one window each.

## Connections and sandbox state

- **A paused E2B sandbox** wakes when you connect, which can take tens of seconds. VS Code times out after 15 seconds by default; if it fails, raise `remote.SSH.connectTimeout` to 60.
- **A session you stopped or closed** is not woken by SSH. The terminal shows why, for example that the session was stopped manually. Resume it in Grasp first.
- **Connected sandboxes are not reclaimed for idling.** While any IDE connection is open, an E2B sandbox is neither saved and paused nor memory-trimmed; the idle countdown restarts when the last connection closes. The info panel shows "Local IDE connected; countdown paused".
- The info panel also shows the environment type, the idle reclaim countdown, the last model activity and the last backup, and offers stop, wake, resume and **Keep running**.

## Resource use

VS Code and Cursor run a remote server inside the sandbox that uses about 0.5–1.5 GB of memory (JetBrains about 2–4 GB), which can crowd the Agent on small templates. After a Docker/Kubernetes sandbox stops, `~/.vscode-server` and `~/.cursor-server` are not in the workspace archive, so the next connection downloads them again.
