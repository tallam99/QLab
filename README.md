# QLab

Lab equipment scheduling PWA with a live queue and a **multi-bench scheduling
engine** that continuously re-flows the queue when experiments run over, finish
early, or get cancelled. Built initially for a ~15-person biology lab sharing
ventilation hoods.

> **Status:** early development. The Go service, a one-command local stack
> (Docker Compose + Postgres), and the CI/CD pipeline (GitHub Actions → Cloud Run
> + Firebase Hosting) are in place; the data model, API, and engine are next. See
> the roadmap in [`docs/PLAN.md`](docs/PLAN.md).

## Documentation

All project docs live in [`docs/`](docs/):

| Doc | What it is |
|-----|------------|
| [`docs/PLAN.md`](docs/PLAN.md) | The engineering roadmap — phased build plan with exit criteria. |
| [`docs/ALGORITHM.md`](docs/ALGORITHM.md) | The scheduling-engine spec — **read before touching scheduling logic.** |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | The system map — components, surfaces, environments. |
| [`docs/runbook.md`](docs/runbook.md) | How to run and debug the local stack. |
| [`docs/deploy.md`](docs/deploy.md) | CI/CD + cloud deploy setup (Cloud Run + Firebase Hosting, both environments). |
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

The local stack (Go API + Postgres in Docker Compose) is driven by `mage`. From a
clean checkout:

```bash
mage startStack                      # build + start API + Postgres (creates .env.json on first run)
curl localhost:8090/healthz          # -> {"status":"ok"}   (liveness)
curl localhost:8090/readyz           # -> {"status":"ok"}   (readiness — 503 until deps init, then 200)
mage test                            # all test tiers (Go unit tests + security/secret-scan checks)
mage serviceLogs                     # follow service logs
mage resetStack                      # wipe the DB volume and start fresh
mage stopStack                       # stop (keeps the data volume)
```

Run `mage` with no args for the full target list. See
[`docs/runbook.md`](docs/runbook.md) for details.

## Development environment

QLab is developed on **Windows + WSL2 (Ubuntu)**. GUI apps stay on Windows; all
terminal, IDE, and toolchain work runs inside Linux — matching production (Linux
containers) and sidestepping Windows-only tooling bugs (see the troubleshooting log
below).

| Runs on Windows | Runs in WSL2 / Ubuntu |
|-----------------|------------------------|
| Web browser (OAuth flows, the running PWA), any GUI app | Repo, terminal, VS Code (*Remote – WSL*), Claude Code, and every CLI: Go, Node, `firebase`, `buf`, `mage`, `docker`, `gcloud` |

**Key choices:**

- **Homebrew is the package manager of choice** for CLI tooling (`brew install go buf
  mage`, and future tools like `sqlc`/`goose`). Documented exceptions, where brew on
  Linux can't or shouldn't own it: **Docker** (apt — the Engine daemon needs root +
  systemd, which an unprivileged brew prefix can't provide), **Node** (`nvm`, for
  per-project versions), and **`gcloud`** (Google's home-dir installer).
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
- **Node** via `nvm` (Node 24, current LTS); **`firebase`** via the keep-alive
  workaround in the troubleshooting log.
- **`gcloud`** installed natively in WSL (not the `/mnt/c` passthrough) — but auth
  and all cloud use are **user-only** (see `CLAUDE.md`).

### One-time WSL setup

Run these inside a fresh WSL2 Ubuntu shell, in order. (`systemd` must be on —
`/etc/wsl.conf` → `[boot] systemd=true`; it is the default on recent Ubuntu/WSL.)

```bash
# 1. Homebrew — the package manager of choice. Bootstrap needs sudo once.
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
echo 'eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"' >> ~/.bashrc
eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
sudo apt-get install -y build-essential        # compiler brew expects for some formulae

# 2. Docker Engine — native, auto-starting. apt, NOT brew: the daemon needs root +
#    systemd, which an unprivileged brew prefix can't run.
sudo apt-get update && sudo apt-get install -y docker.io docker-compose-v2
sudo systemctl enable --now docker
sudo usermod -aG docker "$USER"                # then `wsl --shutdown` from Windows to apply group

# 3. Go toolchain + buf + mage — via brew (no sudo, no tarball).
brew install go buf mage
echo 'export PATH="$PATH:$HOME/go/bin"' >> ~/.bashrc   # so `go install`-ed tools (sqlc, goose, …) are on PATH

# 4. Node via nvm (per-project versions), then the firebase CLI.
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/master/install.sh | bash
#    reload the shell, then:
nvm install 24 && nvm alias default 24         # Node 24 (current LTS)
npm i -g firebase-tools
#    Node 19+ needs the firebase keep-alive workaround — see the troubleshooting log below.

# 5. gcloud — native WSL install (home dir, no sudo). With --disable-prompts the
#    installer does NOT edit ~/.bashrc, so add the PATH line yourself.
curl -fsSL https://sdk.cloud.google.com | bash -s -- --disable-prompts --install-dir="$HOME"
echo '[ -f "$HOME/google-cloud-sdk/path.bash.inc" ] && . "$HOME/google-cloud-sdk/path.bash.inc"' >> ~/.bashrc
#    Auth and all cloud use are user-only (see CLAUDE.md) — install only, never authenticate.

# 6. Repo on the WSL filesystem, LF line endings.
git config --global core.autocrlf false
git clone https://github.com/tallam99/QLab.git ~/repos/qlab

# 7. VS Code — install the WSL extension into the Windows VS Code, then open the repo.
code --install-extension ms-vscode-remote.remote-wsl
code ~/repos/qlab        # first run installs ~/.vscode-server; window badge should read "WSL: Ubuntu"
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
