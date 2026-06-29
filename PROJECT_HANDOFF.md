# PROJECT_HANDOFF.md

Handoff for **shh-its-ssh** — a personal portfolio you visit over SSH. For deep
architecture notes see [`AGENTS.md`](./AGENTS.md); for user-facing docs see
[`README.md`](./README.md). This file is the high-level map for whoever (human or
AI) picks the project up next.

## 1. Overview & goals

Instead of a website, this is an interactive terminal UI streamed over SSH:
visitors run `ssh <host>` and get a Bubble Tea app. Built on the Charm stack
(`wish` SSH server → `bubbletea` TUI → `lipgloss` styling).

Goal: a distinctive, memorable portfolio that shows off terminal/SSH craft and
does things a static web page **can't** (live presence, a shared guestbook,
`scp`/`sftp` downloads). It complements the owner's web portfolio
(chrisdcosta.com) rather than duplicating it.

Deployed on Railway (free tier) behind a TCP proxy. Single Go binary, single
container.

## 2. Current development phase

**Core experience is feature-complete (v1).** All planned creative features have
shipped and are merged to `main` with green CI. Remaining work is **content,
distribution, and ops/tooling** — not core engineering. There is **no in-progress
or uncommitted code** at handoff.

## 3. Completed features

| Area | What | PR |
| --- | --- | --- |
| Hardening | Removed committed host key, slim non-root Dockerfile, session timeouts, responsive layout, CI, README, MIT license | #1 |
| Easter eggs | Hidden Konami secret screen + a playable Snake game | #6 |
| Atmosphere | Boot intro, connection-aware greeting, color themes, secret-hunt meter | #7 |
| Non-interactive | `ssh host <cmd>` CLI, plaintext pipe output, `scp`/`sftp` downloads (vCard/resume/card) | #8 |
| Live | Real-time presence + shared guestbook wall | #9 |
| Reading | Reflections render as scrollable Markdown via glamour | #10 |
| Fix | Dockerfile copies all Go sources (was `COPY main.go` → broke Railway builds) | #12 |
| Theming | `t` cycles palettes always (no longer Konami-gated) | #13 |
| Hardening | Per-IP connection rate limit + post throttle/cooldown; persistent guestbook + visit counter | #14 |

## 4. In-progress work

None. Working tree is clean; no open feature branches.

## 5. Remaining phases / roadmap (all tracked as issues)

- **Content** — Creations gallery ([#11](https://github.com/ChrisDc777/shh-its-ssh/issues/11)), real Reflection essays ([#4](https://github.com/ChrisDc777/shh-its-ssh/issues/4)).
- **Distribution** — custom domain ([#15](https://github.com/ChrisDc777/shh-its-ssh/issues/15)), link from website + GitHub profile ([#16](https://github.com/ChrisDc777/shh-its-ssh/issues/16)), browser fallback ([#17](https://github.com/ChrisDc777/shh-its-ssh/issues/17)).
- **Ops** — stable host key ([#2](https://github.com/ChrisDc777/shh-its-ssh/issues/2)), Railway persistence volume ([#20](https://github.com/ChrisDc777/shh-its-ssh/issues/20)), real host in README ([#5](https://github.com/ChrisDc777/shh-its-ssh/issues/5)).
- **Tooling** — Dependabot + golangci-lint ([#18](https://github.com/ChrisDc777/shh-its-ssh/issues/18)), Docker build in CI ([#19](https://github.com/ChrisDc777/shh-its-ssh/issues/19)).

## 6. Open issues & known bugs

No known functional bugs. Open issues: see [the tracker](https://github.com/ChrisDc777/shh-its-ssh/issues) (#2, #4, #5, #11, #15–#20).

**Known limitations (by design):**
- Presence and (without a volume) the guestbook/visit count are **in-memory** and reset on restart. Persistence needs `DATA_DIR` → a mounted volume ([#20](https://github.com/ChrisDc777/shh-its-ssh/issues/20)).
- The greeting uses the **server's** clock — SSH doesn't expose the visitor's timezone.
- Without `SSH_HOST_KEY`, the host key regenerates each fresh deploy → returning visitors see SSH's "HOST IDENTIFICATION HAS CHANGED" warning ([#2](https://github.com/ChrisDc777/shh-its-ssh/issues/2)).

## 7. Architecture & key design decisions

Full detail in [`AGENTS.md`](./AGENTS.md). The essentials:

- **Session routing** (wish middleware, outermost→inner): `logging → rate-limit → scp → cli → bubbletea`. A session is dispatched by shape — scp transfer, `ssh host <cmd>`/pipe, or interactive PTY (the TUI). A separate **SFTP subsystem** serves the same read-only asset FS so modern `scp` (SFTP) and `sftp` work.
- **One process holds every live session** → this is what makes presence + the guestbook possible (a static site can't). `hub` broadcasts `hubMsg` snapshots to all connected `*tea.Program`s.
- **Custom `teaMiddleware`** (instead of wish's built-in bubbletea middleware) so each session registers with the hub for its lifetime; it still mirrors the window-size/graceful-quit plumbing.
- **Animation = tick + epoch token.** Only boot/home animate; an epoch counter guarantees exactly one live tick loop across navigation (prevents speed-doubling).
- **Static build (`CGO_ENABLED=0`)** → pure-Go deps only. Persistence is a small atomic-write **JSON file** (no SQLite/DB) and the scp assets are an in-memory `fs.FS` — deliberately dependency-light for free-tier hosting.
- **Graceful degradation:** unset/unwritable `DATA_DIR` → in-memory; the app always boots regardless of storage.
- **Hidden vs. discoverable:** Snake + Konami are hidden easter eggs; the theme switcher (`t`) is intentionally discoverable.

## 8. Key files

| File | Purpose |
| --- | --- |
| `main.go` | TUI: model/update/view, pages, boot, themes, Snake, the Markdown article reader, and the SSH server bootstrap (`main`). |
| `cli.go` | Non-interactive surface: CLI commands, plaintext portfolio, generated downloadable assets (vCard/resume/card) + SFTP handler. Also the **shared content** (bio, `contacts`, `fullName`). |
| `presence.go` | `hub` (presence + guestbook), `teaMiddleware`, `newModel`. |
| `store.go` | File-backed persistence (`$DATA_DIR/guestbook.json`) for entries + visit count. |
| `ratelimit.go` | Generic sliding-window `limiter` + connection rate-limit middleware. |
| `main_test.go` | Deterministic unit tests (render views to an Ascii renderer; assert on text). |
| `Dockerfile` | Multi-stage static build, non-root runtime. |
| `.github/workflows/ci.yml` | CI: gofmt, vet, test, build. |
| `AGENTS.md` | Living architecture/contributor doc — **keep current when architecture changes.** |
| `README.md` | User-facing docs (connect, controls, config, deploy). |

## 9. Setup / run

Requires Go 1.24+.

```sh
go test ./...                 # unit tests
go run .                      # listens on :23234
ssh -p 23234 localhost        # in another terminal
ssh -p 23234 localhost help   # CLI mode
```

Container: `docker build -t shh . && docker run -p 23234:23234 shh`.

Deploy: any host that runs a container and exposes a raw TCP port (Railway today).
**The Dockerfile is the source of truth for builds** — Railway uses it, and CI does
*not* currently build it (see [#19](https://github.com/ChrisDc777/shh-its-ssh/issues/19)).

## 10. Environment variables (no secrets here)

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `23234` | TCP listen port (Railway injects this). |
| `SSH_HOST_KEY` | unset | PEM ed25519 private key for a stable host identity across redeploys. Store as a platform secret. |
| `DATA_DIR` | unset | Directory for persisting the guestbook + visit count. Point at a mounted volume; unset → in-memory. |

## 11. Dependencies

Go 1.24+. Direct deps (see `go.mod` for exact versions):
`charmbracelet/{wish,bubbletea,bubbles,lipgloss,glamour,ssh}`, `pkg/sftp`,
`muesli/termenv`. Build is static (`CGO_ENABLED=0`); keep new deps pure-Go.

## 12. Technical debt & future improvements

- **CI doesn't build the Docker image** — a `go build` passes while the Dockerfile breaks. This already caused a multi-deploy outage (the `COPY main.go` bug). Add a `docker build` step ([#19](https://github.com/ChrisDc777/shh-its-ssh/issues/19)).
- **No linter / dependency automation** — add golangci-lint + Dependabot ([#18](https://github.com/ChrisDc777/shh-its-ssh/issues/18)).
- **Rate-limiter map isn't pruned** — bounded by unique IPs seen within the window (fine at portfolio scale); add periodic cleanup if traffic grows.
- **No guestbook moderation** — only length-capped + throttled; consider a profanity/spam filter before heavy promotion.
- **The TUI can't be reliably e2e-tested over SSH** (static low-volume screens don't flush in headless capture); covered instead by deterministic view-rendering unit tests. Keep that pattern.
- `articles` has a single stub entry; real essays pending ([#4](https://github.com/ChrisDc777/shh-its-ssh/issues/4)).

## 13. Suggested first prompt to resume development

> Read `AGENTS.md` and `PROJECT_HANDOFF.md` first. The SSH portfolio is
> feature-complete; pick up the **Creations gallery** ([#11](https://github.com/ChrisDc777/shh-its-ssh/issues/11)). Add a `project`
> struct and a `projects` slice rendered as a selectable list like Reflections,
> with `enter` opening a detail view (reuse the glamour Markdown reader), and a
> couple of placeholder cards the owner can fill in. Add unit tests that render
> the new views to an Ascii `lipgloss` renderer and assert on text. Verify with
> `gofmt`, `go vet`, `go test ./...`, and `go build`. Open one focused PR and
> merge it. Conventions: keep `main.go`/`cli.go`/`presence.go` cohesive, no new
> non-pure-Go deps, and **no AI attribution in commit messages**.
