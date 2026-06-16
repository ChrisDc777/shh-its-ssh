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

- Animated ASCII name with a twinkling starfield on the landing page.
- Keyboard-driven navigation between **Creations**, **Reflections**, and **Contacts**.
- Clickable links (OSC&nbsp;8 hyperlinks) in supporting terminals.
- Per-session color-profile detection (truecolor / 256 / ANSI / mono).
- Responsive layout that adapts to narrow terminals.

### Controls

| Key | Action |
| --- | --- |
| `←` `→` / `tab` / `h` `l` | Move between sections (home) |
| `↑` `↓` / `k` `j` | Move within a list |
| `enter` | Open the selected section / item |
| `esc` | Go back |
| `q` / `ctrl+c` | Quit |

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

Session limits (`idleTimeout`, `maxTimeout` in `main.go`) keep this public,
no-auth server resource-friendly on small hosts — idle and very long-lived
sessions are reaped automatically. No platform configuration is required.

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
