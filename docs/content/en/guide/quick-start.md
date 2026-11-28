---
title: Start Grasp
navTitle: Quick start
description: Launch the local demo with Docker Compose and verify that Grasp and the sandbox gateway are ready.
section: start
order: 10
keywords: [install, self-host, start.sh, Docker, sign in, doctor]
---

## Prepare a host {#prerequisites}

Use a Linux host with Git, Docker, and Docker Compose. The startup script pulls published images and runs Grasp and its Docker gateway on the host. For a shared or production host, Kubernetes, or E2B, start with [Deployment and configuration](/en/help/configuration/).

## Start the published stack {#start}

The published stack uses host networking. By default, Grasp and the Docker gateway listen on every host interface, on ports 8080 and 8899 respectively. Before starting the demo, use firewall or security group rules to restrict both ports to local access. Anyone who can reach port 8080 can sign in with the demo administrator account.

```bash
git clone https://github.com/cocofhu/grasp.git
cd grasp
./start.sh -d
```

Open [http://localhost:8080](http://localhost:8080) on the host. The local demo login is `admin` / `demo1234`. Set up dedicated users and a credential encryption key for a production deployment.

Check both services from the host.

```bash
curl -fsS http://localhost:8080/api/health
curl -fsS http://localhost:8899/healthz
```

The gateway pulls the sandbox image when you first create a sandbox. To warm the image beforehand, run `./start.sh pull`. Then follow [Your first run](/en/guide/first-run/) to set up a model and an agent.

## Everyday commands {#commands}

| Command | Effect |
| --- | --- |
| `./start.sh logs` | Follow service logs. |
| `./start.sh restart` | Recreate release-stack services and keep `.localdata`. |
| `./start.sh down` | Stop release-stack services and keep `.localdata`. |
| `./start.sh pull` | Refresh the release images and warm the default sandbox image. |
| `./start.sh dev -d` | Run the source development stack with frontend hot reload. |

## Back up app data and attachments {#backup}

Keep the database, app data, gateway state, and original deployment key in one recovery set. This preserves attachment references, sandbox records, and saved credentials together.

| Location | Content |
| --- | --- |
| `.localdata/db` | The SQLite database and its related files. |
| `.localdata/app-data` | Agent configuration and default attachment storage. Include the custom path if you set `GRASP_BLOBS_ROOT`. |
| `.localdata/gateway` | Gateway records. |
| Deployment configuration and original `GRASP_SECRETS_KEY` | Restore deployment settings and decrypt existing credentials. |

Sandbox working files follow their own [save and restore policy](/en/help/sandbox-lifecycle/). Pause writes or take a consistent snapshot when backing up application data. If migrated attachments show `blob:` errors or return 404, check that the database and attachment directory came from the same backup.
