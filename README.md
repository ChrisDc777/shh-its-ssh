# shh-its-ssh

A personal portfolio you visit over **SSH**. No website, no browser — just a
terminal UI that streams to anyone who connects.

```
ssh <your-host>
```

> Replace `<your-host>` with your deployed address (e.g. your Railway-generated
> domain). If your platform exposes the service on a non-standard port, add
> `-p <port>`.

Built with [Charm](https://charm.sh)'s [`wish`](https://github.com/charmbracelet/wish)
(SSH server), [`bubbletea`](https://github.com/charmbracelet/bubbletea) (TUI), and
[`lipgloss`](https://github.com/charmbracelet/lipgloss) (styling).

## Features

- A short animated boot sequence and a connection-aware greeting (it knows your SSH username, client, and the time of day).
- A live **guestbook** wall with real-time presence — see who else is connected right now and leave a mark everyone sees instantly.
- **Reflections** render as scrollable, styled Markdown (via [glamour](https://github.com/charmbracelet/glamour)).
- Animated ASCII name with a twinkling starfield on the landing page.
- Keyboard-driven navigation between **Creations**, **Reflections**, **Contacts**, and the **Guestbook**.
- Switchable color themes (teal, amber, synthwave, mono) — press `t` anytime.
- Clickable links (OSC&nbsp;8 hyperlinks) in supporting terminals.
- Per-session color-profile detection (truecolor / 256 / ANSI / mono).
- Responsive layout that adapts to narrow terminals.
- A couple of hidden surprises for the curious. 👀

### Controls

| Key | Action |
| --- | --- |
| `←` `→` / `tab` / `h` `l` | Move between sections (home) |
| `↑` `↓` / `k` `j` | Move within a list / scroll an article |
| `enter` | Open the selected section / item |
| `t` | Cycle the color theme |
| `esc` | Go back |
| `q` / `ctrl+c` | Quit |

## Non-interactive use

The portfolio also answers like a CLI, so it works from scripts and pipes:

```sh
ssh <host> whoami      # short bio
ssh <host> social      # contact links
ssh <host> resume      # plaintext resume
ssh <host> help        # all commands
ssh <host> | less      # plaintext portfolio (no terminal needed)
```

And you can download a few generated files over `scp`/`sftp`:

```sh
scp <host>:chris.vcf .     # a vCard for your contacts app
scp <host>:resume.txt .    # the resume as a text file
scp <host>:card.txt .      # an ASCII business card
sftp <host>                # browse and grab any of them
```

(Files are served read-only from memory — there's nothing to upload.)

## Run locally

Requires Go 1.24+.

```sh
go run .
# then, in another terminal:
ssh -p 23234 localhost
```

The server listens on `0.0.0.0:23234` by default. Set `PORT` to override.
On first start it generates an SSH host key under `.ssh/` (this directory is
git-ignored and must never be committed).

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `23234` | TCP port to listen on. |
| `SSH_HOST_KEY` | _(unset)_ | PEM-encoded ed25519 private key used as the server's host key. Set this to a stable value so the host identity survives redeploys. |
| `DATA_DIR` | _(unset)_ | Directory for persisting the guestbook + visit count. Unset → in-memory only (resets on restart). Point it at a mounted volume to make them durable. |

Session limits (`idleTimeout`, `maxTimeout`), per-IP connection rate limiting,
and a per-session guestbook post cooldown (all in code) keep this public,
no-auth server resource-friendly and civil on small hosts — no platform
configuration required.

### Persisting the guestbook (optional)

The guestbook and visitor count live in memory by default and reset on restart.
To keep them, mount a persistent volume and set `DATA_DIR` to its path (e.g. on
Railway: add a Volume, then set `DATA_DIR=/data`). State is written atomically to
`$DATA_DIR/guestbook.json`. If the directory isn't writable, the app logs a notice
and falls back to in-memory — it never fails to boot over storage.

### Stable host key (recommended for production)

By default a new host key is generated on each fresh deploy, which makes
returning visitors see SSH's *"REMOTE HOST IDENTIFICATION HAS CHANGED"* warning.
To pin a stable identity, generate a key once and store it as a secret:

```sh
ssh-keygen -t ed25519 -f hostkey -N ""
# put the contents of ./hostkey into an env var named SSH_HOST_KEY on your host
```

## Deploy

The included [`Dockerfile`](./Dockerfile) builds a small static image and runs
as a non-root user. Any platform that can run a container and expose a raw TCP
port works (Railway, Fly.io, Render, a VPS, …). Point the platform at port
`23234` (or whatever `PORT` you set).

## License

[MIT](./LICENSE)
