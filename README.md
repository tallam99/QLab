# Quelab

Lab equipment scheduling PWA with a live queue and a **multi-bench scheduling
engine** that continuously re-flows the queue when experiments run over, finish
early, or get cancelled. Built initially for a ~15-person biology lab sharing
ventilation hoods.

> **Status:** early development — Phase 0 (foundations). Most of the system isn't
> built yet; see the roadmap below.

## Documentation

All project docs live in [`docs/`](docs/):

| Doc | What it is |
|-----|------------|
| [`docs/PLAN.md`](docs/PLAN.md) | The engineering roadmap — phased build plan with exit criteria. |
| [`docs/ALGORITHM.md`](docs/ALGORITHM.md) | The scheduling-engine spec — **read before touching scheduling logic.** |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | The system map — components, surfaces, environments. |
| [`docs/runbook.md`](docs/runbook.md) | How to run and debug the local stack. |
| [`docs/decisions/`](docs/decisions/) | Decision log (ADRs) for cross-cutting choices. |
| [`CLAUDE.md`](CLAUDE.md) | Orientation for a fresh Claude Code session. |

## Tech stack (summary)

- **Frontend:** React + TypeScript PWA (Vite), deployed to Firebase Hosting.
- **Backend:** Go Connect-RPC API on Google Cloud Run.
- **Contract:** Protobuf via Connect + buf (shared Go + TypeScript types).
- **Database:** Neon (serverless Postgres).
- **Auth:** Firebase Auth (Login with Google).
- **Notifications:** transactional-outbox email (Resend/SendGrid); modular for SMS/push.

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for detail and
[`docs/PLAN.md`](docs/PLAN.md) for the build order.

## Local development

The local stack (Docker Compose + Postgres, driven by `mage` targets) lands in
Phase 2 — see [`docs/runbook.md`](docs/runbook.md). Until then there is nothing to
run locally.

## Development environment

Quelab is developed on **Windows + WSL2 (Ubuntu)**. GUI apps stay on Windows; all
terminal, IDE, and toolchain work runs inside Linux — matching production (Linux
containers) and sidestepping Windows-only tooling bugs (see the troubleshooting log
below).

| Runs on Windows | Runs in WSL2 / Ubuntu |
|-----------------|------------------------|
| Web browser (OAuth flows, the running PWA), any GUI app | Repo, terminal, VS Code (*Remote – WSL*), Claude Code, and every CLI: Go, Node, `firebase`, `buf`, `mage`, `docker`, `gcloud` |

**Key choices:**

- **Repo lives on the WSL filesystem** (`~/repos/qlab`), *not* `/mnt/c`. Native ext4
  is far faster for Go builds and `node_modules`; `/mnt/c` crosses a translation
  layer that is slow for many small files. Open it with `code .` from the WSL shell
  (the *Remote – WSL* extension); reach it from Windows Explorer at
  `\\wsl.localhost\Ubuntu\home\<user>\repos\qlab` when needed.
- **Line endings are LF** — `git config --global core.autocrlf false` inside WSL, so
  files don't round-trip through CRLF like Windows-native git.
- **Docker runs natively in WSL** (Docker Engine as a **systemd** service, *not*
  Docker Desktop). The daemon auto-starts on every WSL launch — nothing to start by
  hand — and the user is in the `docker` group so `docker` needs no `sudo`. systemd
  is already enabled in the distro (`/etc/wsl.conf` → `[boot] systemd=true`).
- **Node** via `nvm` (Node 22); **`firebase`** via the keep-alive workaround in the
  troubleshooting log.

### One-time WSL setup

```bash
# Docker Engine — native, auto-starting (systemd is already on in this distro)
sudo apt-get update && sudo apt-get install -y docker.io docker-compose-v2
sudo systemctl enable --now docker
sudo usermod -aG docker "$USER"        # then `wsl --shutdown` from Windows to apply group

# Go (current stable from https://go.dev/dl/), then buf + mage via `go install`
GO_VER=<latest>   # e.g. go1.2x.y
curl -fsSL "https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz" | sudo tar -C /usr/local -xz
# add to ~/.bashrc: export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
go install github.com/bufbuild/buf/cmd/buf@latest
go install github.com/magefile/mage@latest

git config --global core.autocrlf false
```

## Environment troubleshooting log

Symptom → cause → fix for environment problems hit during setup, recorded so we
don't re-debug them.

### `firebase login` fails: "Unable to authenticate using the provided code" (`FetchError: Premature close`)

- **Symptom:** `firebase login` (or `--reauth`) opens the browser, you authorize
  and obtain the auth code, then the CLI dies with *"Unable to authenticate using
  the provided code."* The debug log shows `FetchError: Invalid response body
  while trying to fetch https://accounts.google.com/o/oauth2/token: Premature
  close`. Reproduces on Windows-native Node **and** inside WSL, and survives
  switching networks (e.g. a mobile hotspot) — so it is not the machine, the
  network, a proxy, or the clock.
- **Cause:** firebase-tools bundles **`node-fetch` v2.7.0**. With no proxy set it
  passes no HTTP agent, so node-fetch uses Node's *global* agent. **Node 19+
  changed the global agent's default to `keepAlive: true`**; node-fetch v2
  mishandles a kept-alive connection that the server closes (Google's OAuth token
  endpoint does exactly that) and throws "Premature close." `curl` and Node's
  built-in `fetch` (undici) reach the same endpoint fine — the bug is specific to
  node-fetch v2 + keep-alive, which is why this is a recent, confusing failure.
- **Fix:** force the global HTTP agents back to `keepAlive: false` for the
  firebase CLI via a `NODE_OPTIONS` preload (no patching of `node_modules`,
  survives upgrades, applies to every firebase command). Preload module
  `~/.firebase-no-keepalive.js`:

  ```js
  const http = require("http");
  const https = require("https");
  http.globalAgent = new http.Agent({ keepAlive: false });
  https.globalAgent = new https.Agent({ keepAlive: false });
  ```

  plus a scoped wrapper in `~/.bashrc`:

  ```bash
  firebase() { NODE_OPTIONS="--require $HOME/.firebase-no-keepalive.js${NODE_OPTIONS:+ $NODE_OPTIONS}" command firebase "$@"; }
  ```

  This generalizes to any Node 19+ tool that bundles node-fetch v2.

## Cost

The entire stack is designed to run within free tiers — **$0/month** aside from
tooling subscriptions.
