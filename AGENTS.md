# AGENTS.md

Context for AI agents and contributors working on this repo. Keep it current when
you change architecture.

## What this is

An SSH-accessible terminal portfolio. Visitors run `ssh <host>` and get an
interactive TUI instead of a website. It's a single Go binary built on
[Charm](https://charm.sh): `wish` (SSH server) → `bubbletea` (TUI runtime) →
`lipgloss` (styling).

Everything lives in **`main.go`** (one file on purpose — it's small). Tests are
in **`main_test.go`**.

## Runtime shape

`main()` starts a `wish` server. A session is routed by its shape (middleware
stack, outermost first):

1. `logging` — logs each connection.
2. `scp` — if the command is an scp transfer, serve it read-only from the
   in-memory asset FS; otherwise fall through.
3. `cliMiddleware` (`cli.go`) — if there's a command (`ssh host whoami`), run it
   and exit; if there's no PTY (a pipe), print the plaintext portfolio and exit;
   otherwise fall through.
4. `teaMiddleware` (`presence.go`) — the interactive TUI, reached only by a PTY
   session with no command. It builds the per-session model, runs the Bubble Tea
   program (mirroring wish's bubbletea middleware: window-size plumbing + graceful
   quit), and registers the program with the hub for the connection's lifetime.

Separately, an **SFTP subsystem** (`withSFTP`, `cli.go`) serves the same asset FS
read-only, so modern `scp` (which speaks SFTP by default), `scp -O` (legacy SCP
via the middleware), and `sftp` all work. There is no `activeterm` — the routing
above subsumes it.

Each interactive session gets its own `model` and `lipgloss.Renderer` (color
profile derived from the client's `$TERM`). The host key is loaded from
`.ssh/term_info_ed25519`; if absent, `wish` generates one. `$SSH_HOST_KEY` (PEM)
overrides it so the identity is stable across redeploys. `$PORT` overrides the
listen port. Idle/max session timeouts bound resource use on small hosts.

## Live presence + guestbook (`presence.go`)

`hub` is the shared state across all sessions: the set of connected `*tea.Program`s
(presence count, in-memory only), the all-time visit count, and the guestbook
`entries` (capped). Presence is "right now" and never persisted; the visit count
and entries are persisted (see below).

`teaMiddleware` calls `hub.join(p)` before running the program and `hub.leave(p)`
after. Any change (join/leave/post) calls `hub.broadcast`, which sends a `hubMsg`
snapshot to every program. Sends run in their own goroutines because a program
that hasn't started `Run()` yet would block on its unbuffered message channel. A
freshly-connected session also pulls an initial snapshot in `Init` (`hubSnapshotCmd`).

**Persistence** (`store.go`): on join/post the visit count + entries are written
atomically to `$DATA_DIR/guestbook.json` and loaded in `newHub`. With `DATA_DIR`
unset or not writable the store is a no-op (in-memory only) — the app always boots.

**Abuse protection** (`ratelimit.go`): `rateLimitMiddleware` rejects an IP that
opens more than `connsPerMin` connections/minute; `hub.post` throttles posts per
IP (`postsPerMin`); the model enforces a per-session `postCooldown`. `limiter` is
a generic per-key sliding window reused for both.

The model handles `hubMsg` by storing the snapshot in `m.presence`; the home
screen shows a live count and `pageGuestbook` renders the wall + a text input
(`m.input`) that calls `hub.post` on Enter. **If you add shared live state, route
it through the hub and broadcast the same way.**

## Article reader (Reflections)

Each `article` has an optional Markdown `body`. Opening one (`openArticle`)
renders the Markdown with **glamour** and loads it into a **bubbles `viewport`**
sized to the terminal, so `pageArticle` scrolls (arrows / `j` `k` / pgup/pgdn).
When `body` is empty, `articleMarkdown` generates a stub from the title/summary —
**fill in `body` (Markdown) in the `articles` slice to publish real essays.**

## Non-interactive surfaces (`cli.go`)

Shared content (bio, `contacts`, `fullName`) lives here so the TUI and the
non-interactive surfaces stay in sync. `cli.go` provides: the CLI command
dispatch (`runCommand`), the plaintext portfolio (`plainPortfolio`), the
generated downloadable assets (`vCard`, `resumeText`, `cardText`, exposed via
`buildAssetFS` as an in-memory `fs.FS`), and the SFTP handler.

## The Bubble Tea model

Standard Elm architecture: `Init` / `Update(msg) -> (model, cmd)` / `View() -> string`.

`page` is an enum that selects which `view*()` renders and how keys are handled in
`Update`. Pages: `pageBoot` (intro), `pageHome` (landing), `pageReflections`,
`pageArticle`, `pageContacts`, `pageCreations`, plus the hidden `pageSnake` and
`pageSecret`.

### Animation: the tick + epoch pattern

`tick(epoch)` schedules a `tickMsg{epoch}` every 100ms. Only the boot and home
pages animate, so `Update` reschedules the tick **only** while on those pages.

`tickEpoch` is a monotonic token identifying the *current* animation loop. Any
`tickMsg` whose epoch doesn't match `m.tickEpoch` is dropped. `goHome()` and
`startSnake()` bump the epoch when they (re)start a loop. This guarantees exactly
one live loop even under rapid navigation — without it, returning to an animated
page could spawn duplicate, speed-doubling loops. **If you add another animated
page, gate its tick the same way.**

The Snake game has its own `snakeTick`/`snakeTickMsg` (slower) but shares the same
`tickEpoch` so it can never run alongside the home animation.

## Easter eggs (keep them undocumented in user-facing copy)

On the home page, `Update` appends each keystroke to a small rolling `keyLog` and
checks for hidden sequences via `endsWith`:

- **Konami code** (`konamiCode`) → opens `pageSecret`, sets `foundKonami`.
- Typing **`snake`** (`snakeCode`) → `startSnake()`, sets `foundSnake`.

`pageSecret` shows a secret-hunt meter (`secretsFound` / `totalSecrets`). Update
`totalSecrets` if you add another egg.

## Theming

`theme` is an accent/fg/dim palette; `themes[0]` is the default. `makeStyles(r, t)`
builds the per-session `styles` from a renderer + theme. `t` cycles palettes from
any page (handled at the top of the `KeyMsg` case, so it works everywhere) except
while typing in the guestbook. Runtime theme switching rebuilds `styles` from the
stored `m.renderer`. (The Konami code is a separate easter egg — it opens the
secret-hunt screen; it no longer gates themes.)

## Connection-aware greeting

Computed once at connect in the `bubbletea` middleware and stored on the model:
`greetWord(time.Now())` (server-local time of day), `greetingName(s)` (the SSH
username, e.g. `ssh nova@host`), `clientHint(s, pty)` (client + `$TERM`). Shown in
the boot sequence. Note the greeting uses the **server's** clock — SSH doesn't
reliably expose the visitor's timezone.

## Conventions

- `main.go` is the TUI + server; `cli.go` is the non-interactive surface
  (commands, plaintext, scp/sftp assets). Keep both `gofmt`-clean (CI enforces).
- No new dependencies without reason; prefer the Charm stack already vendored.
- Add a test in `main_test.go` for new logic. Tests render views with an Ascii
  (plain-text) renderer and assert on substrings — no SSH needed.

## Build / test / run

```sh
go test ./...        # unit tests
go build -o portfolio .
go run .              # listens on :23234; ssh -p 23234 localhost
```

CI (`.github/workflows/ci.yml`) runs `gofmt -l`, `go vet`, `go test`, `go build`.

## Deploy

Container (`Dockerfile`) → any host that exposes a raw TCP port (Railway today).
See `README.md`. Set `SSH_HOST_KEY` in the host's secrets for a stable identity.
