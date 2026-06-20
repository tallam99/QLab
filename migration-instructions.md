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
- **Already set up in WSL home (persists — do NOT redo):** `nvm` + Node 22; the
  `firebase` CLI with the keep-alive workaround (`~/.firebase-no-keepalive.js` +
  the `firebase()` wrapper in `~/.bashrc`); firebase is logged in.

## Who does what

- **User** runs the sudo-gated steps (Docker install/enable, `usermod`) and launches
  Claude Code in WSL.
- **Claude** can do the no-sudo steps (repo copy, `go install` of buf/mage,
  verification). The Go tarball extraction to `/usr/local` needs sudo — user runs
  that one line, or grants it.

## Steps

### 1. Get the repo into WSL (latest commits are LOCAL/unpushed)

The recent docs commits exist only on the Windows clone. Pick one:

- **A — copy the local clone (no push needed):**
  ```bash
  git config --global core.autocrlf false
  cp -a "/mnt/c/Users/thfif/repos/qlab" ~/repos/qlab
  cd ~/repos/qlab
  git add --renormalize .     # normalize any CRLF from the Windows checkout to LF
  git status                   # confirm branch + clean history
  ```
- **B — push then clone fresh (cleanest LF checkout; needs approval to push):**
  from the Windows session: `git push -u origin tallam/init-plans-docs`, then:
  ```bash
  git config --global core.autocrlf false
  git clone https://github.com/tallam99/QLab.git ~/repos/qlab
  cd ~/repos/qlab && git checkout tallam/init-plans-docs
  ```

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
node -v          # v22.x
which firebase   # must be the nvm path, NOT /mnt/c/...
firebase --version
```

### 5. Claude Code in WSL  (USER)

```bash
npm i -g @anthropic-ai/claude-code
cd ~/repos/qlab
claude           # launch; resume Phase 0 / start Phase 1 from here
```

### 6. VS Code

Install the **Remote – WSL** extension (in Windows VS Code), then from the WSL
shell: `code ~/repos/qlab`. The window renders on Windows; the filesystem,
terminal, extensions, and language servers all run in Linux.

## After migration

- **gcloud** stays user-driven (Windows, or your own WSL install) — Claude won't touch it.
- **Docker Desktop** is optional now — keep it for other projects or uninstall.
- Once verified, the Windows clone at `C:\Users\thfif\repos\qlab` can be removed.
- Update README → "Development environment" if any step differed in practice.
- This file can be deleted once the migration is complete.
