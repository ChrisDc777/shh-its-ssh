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

`main()` starts a `wish` server with this middleware stack (outermost first):

1. `logging` — logs each connection.
2. `activeterm` — rejects sessions without an interactive PTY.
3. `bubbletea` — runs the TUI `model` for the session.

Each session gets its own `model` and its own `lipgloss.Renderer` (color profile
is derived from the client's `$TERM`). The host key is loaded from
`.ssh/term_info_ed25519`; if absent, `wish` generates one. `$SSH_HOST_KEY` (PEM)
overrides it so the identity is stable across redeploys. `$PORT` overrides the
listen port. Idle/max session timeouts bound resource use on small hosts.

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

- **Konami code** (`konamiCode`) → opens `pageSecret`, sets `foundKonami`, and
  unlocks themes.
- Typing **`snake`** (`snakeCode`) → `startSnake()`, sets `foundSnake`.

`pageSecret` shows a secret-hunt meter (`secretsFound` / `totalSecrets`). Update
`totalSecrets` if you add another egg.

## Theming

`theme` is an accent/fg/dim palette; `themes[0]` is the default. `makeStyles(r, t)`
builds the per-session `styles` from a renderer + theme. Themes are locked until
the Konami code unlocks them; then `t` cycles palettes from any page (handled at
the top of the `KeyMsg` case, so it works everywhere). Runtime theme switching
rebuilds `styles` from the stored `m.renderer`.

## Connection-aware greeting

Computed once at connect in the `bubbletea` middleware and stored on the model:
`greetWord(time.Now())` (server-local time of day), `greetingName(s)` (the SSH
username, e.g. `ssh nova@host`), `clientHint(s, pty)` (client + `$TERM`). Shown in
the boot sequence. Note the greeting uses the **server's** clock — SSH doesn't
reliably expose the visitor's timezone.

## Conventions

- One file (`main.go`); keep it `gofmt`-clean (CI enforces).
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
