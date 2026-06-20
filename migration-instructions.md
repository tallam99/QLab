# Migration: Windows-native → WSL2 (Ubuntu) — one-time

**Purpose:** move all development into WSL2 Ubuntu. Afterward, Claude Code, the
terminal, VS Code, and the whole toolchain run **inside Linux**; only GUIs
(browser, etc.) stay on Windows. See README → "Development environment" for the
target model and rationale.

This is the handoff checklist. It exists because the migration **ends the Windows
Claude Code session and starts a new one inside WSL** — the new session won't have
the prior conversation's context, so it should read this file (and `CLAUDE.md`,
`docs/PLAN.md`) first.

## Context for the resuming session

- Repo: `github.com/tallam99/QLab`. Active branch: **`tallam/init-plans-docs`**.
- Phase **0** (foundations) — see `docs/PLAN.md`. `docs/ALGORITHM.md` §12 is signed off.
- **Boundary:** the **user** solely operates all cloud CLIs/consoles (`gcloud`,
  `neonctl`, Firebase staging/prod) — Claude never authenticates to or touches cloud
  infra, even read-only. Claude operates **local** infra (docker, postgres,
  migrations, tests) autonomously. See `CLAUDE.md`.
- **Already set up in WSL home (persists — do NOT redo):** `nvm` + Node 24 (default);
  the `firebase` CLI with the keep-alive workaround (`~/.firebase-no-keepalive.js` +
  the `firebase()` wrapper in `~/.bashrc`); firebase is logged in (`projects:list`
  shows `qlab-staging` + `qlab-production`).

## Who does what

- **User** runs the sudo-gated steps (Docker install/enable, `usermod`) and launches
  Claude Code in WSL.
- **Claude** can do the no-sudo steps (repo copy, `go install` of buf/mage,
  verification). The Go tarball extraction to `/usr/local` needs sudo — user runs
  that one line, or grants it.

## Steps

### 1. Get the repo into WSL

The branch `tallam/init-plans-docs` is **pushed to GitHub** before migration, so
clone fresh for a clean LF checkout:

```bash
git config --global core.autocrlf false
git clone https://github.com/tallam99/QLab.git ~/repos/qlab
cd ~/repos/qlab && git checkout tallam/init-plans-docs
```

(Fallback if something local wasn't pushed: `cp -a "/mnt/c/Users/thfif/repos/qlab"
~/repos/qlab`, then `cd ~/repos/qlab && git config core.autocrlf false && git add
--renormalize .`.)

### 2. Docker Engine — native + auto-starting  (USER, sudo)

```bash
sudo apt-get update && sudo apt-get install -y docker.io docker-compose-v2
sudo systemctl enable --now docker
sudo usermod -aG docker "$USER"
```
Then from Windows: `wsl --shutdown`, reopen Ubuntu. Verify (no sudo):
```bash
docker run --rm hello-world
docker compose version
```

### 3. Go + buf + mage

```bash
GO_VER=<latest stable from https://go.dev/dl/>      # e.g. go1.2x.y
curl -fsSL "https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz" | sudo tar -C /usr/local -xz
# append to ~/.bashrc:  export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"
# reload the shell, then:
go install github.com/bufbuild/buf/cmd/buf@latest
go install github.com/magefile/mage@latest
go version && buf --version && mage --version
```

### 4. Verify Node + firebase (already configured)

```bash
node -v          # v24.x (current LTS)
which firebase   # must be the nvm path, NOT /mnt/c/...
firebase --version
firebase projects:list   # via the ~/.bashrc wrapper; should list qlab-staging + qlab-production
```

### 5. Google Cloud SDK (gcloud) — native WSL install  (Claude or user; no sudo)

All terminal tooling lives in WSL, so install gcloud in Linux rather than relying on
the `/mnt/c` Windows passthrough. Home-dir install, no sudo:

```bash
curl -fsSL https://sdk.cloud.google.com > /tmp/gcloud-install.sh
bash /tmp/gcloud-install.sh --disable-prompts --install-dir="$HOME"
# adds ~/google-cloud-sdk/bin to PATH via ~/.bashrc; reload the shell, then:
gcloud --version
```

**Auth and all cloud use are USER-only** (boundary): the user runs `gcloud auth
login` and any project/billing/API/Neon commands. Claude installs the SDK but never
authenticates to or runs commands against cloud infra.

### 6. Claude Code in WSL  (USER)

```bash
npm i -g @anthropic-ai/claude-code
cd ~/repos/qlab
claude           # launch; resume Phase 0 / start Phase 1 from here
```

### 7. VS Code

Install the **Remote – WSL** extension (in Windows VS Code), then from the WSL
shell: `code ~/repos/qlab`. The window renders on Windows; the filesystem,
terminal, extensions, and language servers all run in Linux.

## After migration

- **gcloud** stays user-driven (Windows, or your own WSL install) — Claude won't touch it.
- **Docker Desktop** is optional now — keep it for other projects or uninstall.
- Once verified, the Windows clone at `C:\Users\thfif\repos\qlab` can be removed.
- Update README → "Development environment" if any step differed in practice.
- This file can be deleted once the migration is complete.
