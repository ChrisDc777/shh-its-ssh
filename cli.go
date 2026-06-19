package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing/fstest"

	"github.com/charmbracelet/ssh"
	"github.com/pkg/sftp"
)

// ── Shared content ────────────────────────────────────────────────────────────
// Used by both the TUI (main.go) and the non-interactive surfaces below, so the
// bio/links live in exactly one place.

const fullName = "Chris Dcosta"

const (
	bioLead  = "is a software engineer building intelligent systems on the internet, developing scalable products and experimenting with AI."
	bioWork  = "He works across full-stack and backend systems, building APIs, cloud applications, and AI-powered tools."
	bioEdu   = "Previously, he studied Computer Science Engineering at Symbiosis Institute of Technology, where he built projects in machine learning, computer vision, and AI-driven data systems."
	bioFocus = "His work sits at the intersection of software engineering, artificial intelligence, and real-world problem solving."
)

type contact struct {
	short, name, display, url string
}

var contacts = []contact{
	{"IG", "Instagram", "instagram.com/chrisdcosta777", "https://instagram.com/chrisdcosta777"},
	{"LI", "LinkedIn", "linkedin.com/in/chrisdcosta777", "https://linkedin.com/in/chrisdcosta777"},
	{"GH", "GitHub", "github.com/ChrisDc777", "https://github.com/ChrisDc777"},
}

const repoURL = "https://github.com/ChrisDc777/shh-its-ssh"

// ── Downloadable assets (served read-only over scp) ───────────────────────────

func vCard() string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\n")
	b.WriteString("VERSION:3.0\r\n")
	b.WriteString("FN:" + fullName + "\r\n")
	b.WriteString("N:Dcosta;Chris;;;\r\n")
	b.WriteString("TITLE:Software Engineer\r\n")
	for _, c := range contacts {
		b.WriteString("URL:" + c.url + "\r\n")
	}
	b.WriteString("NOTE:Software engineer building intelligent systems. You found this over SSH.\r\n")
	b.WriteString("END:VCARD\r\n")
	return b.String()
}

func resumeText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — Software Engineer\n%s\n\n", fullName, strings.Repeat("=", 40))
	fmt.Fprintf(&b, "Chris %s\n%s\n\n", bioLead, bioWork)
	fmt.Fprintf(&b, "%s\n%s\n\n", bioEdu, bioFocus)
	b.WriteString("Links\n")
	for _, c := range contacts {
		fmt.Fprintf(&b, "  %-10s %s\n", c.name, c.url)
	}
	b.WriteString("\n(Fetched over SSH.)\n")
	return b.String()
}

func cardText() string {
	lines := []string{fullName, "Software Engineer", ""}
	for _, c := range contacts {
		lines = append(lines, c.display)
	}
	return boxed(lines, 2)
}

// boxed draws a Unicode box around the given lines, padded horizontally.
func boxed(lines []string, pad int) string {
	w := 0
	for _, l := range lines {
		if n := len([]rune(l)); n > w {
			w = n
		}
	}
	inner := w + pad*2
	var b strings.Builder
	b.WriteString("┌" + strings.Repeat("─", inner) + "┐\n")
	for _, l := range lines {
		trail := inner - pad - len([]rune(l))
		b.WriteString("│" + strings.Repeat(" ", pad) + l + strings.Repeat(" ", trail) + "│\n")
	}
	b.WriteString("└" + strings.Repeat("─", inner) + "┘\n")
	return b.String()
}

// buildAssetFS returns the in-memory file tree exposed over scp.
func buildAssetFS() fs.FS {
	return fstest.MapFS{
		"chris.vcf":  &fstest.MapFile{Data: []byte(vCard()), Mode: 0o644},
		"resume.txt": &fstest.MapFile{Data: []byte(resumeText()), Mode: 0o644},
		"card.txt":   &fstest.MapFile{Data: []byte(cardText()), Mode: 0o644},
	}
}

// ── Non-interactive text surfaces (`ssh host <cmd>` and pipes) ─────────────────

func aboutText() string {
	return fmt.Sprintf("%s\n\nChris %s\n%s\n\n%s\n%s\n", fullName, bioLead, bioWork, bioEdu, bioFocus)
}

func linksText() string {
	var b strings.Builder
	for _, c := range contacts {
		fmt.Fprintf(&b, "%-10s %s\n", c.name, c.url)
	}
	return b.String()
}

func helpText() string {
	return strings.Join([]string{
		fullName + " — this portfolio is an SSH app.",
		"",
		"Commands:",
		"  ssh <host>            open the interactive portfolio (needs a terminal)",
		"  ssh <host> whoami     print a short bio",
		"  ssh <host> social     print contact links",
		"  ssh <host> resume     print a plaintext resume",
		"  ssh <host> repo       link to this project's source",
		"  ssh <host> help       show this help",
		"",
		"Downloads (scp or sftp):",
		"  scp <host>:chris.vcf .    a vCard for your contacts app",
		"  scp <host>:resume.txt .   the resume as a text file",
		"  scp <host>:card.txt .     an ASCII business card",
		"  sftp <host>               browse and grab any of the above",
		"",
	}, "\n")
}

// plainPortfolio is served to non-interactive sessions (e.g. `ssh host | less`).
func plainPortfolio() string {
	return aboutText() + "\n" + linksText() + "\nTip: connect from a terminal for the interactive version,\n     or run `ssh <host> help` for commands and downloads.\n"
}

// runCommand handles `ssh host <args...>` and returns output plus an exit code.
func runCommand(args []string) (string, int) {
	switch strings.ToLower(args[0]) {
	case "help", "-h", "--help":
		return helpText(), 0
	case "whoami", "about", "bio":
		return aboutText(), 0
	case "social", "socials", "contact", "contacts", "links":
		return linksText(), 0
	case "resume", "cv":
		return resumeText(), 0
	case "repo", "source", "code":
		return repoURL + "\n", 0
	default:
		return "unknown command: " + args[0] + "\n\n" + helpText(), 2
	}
}

// cliMiddleware routes non-interactive sessions: a command runs once and exits;
// a session without a PTY (a pipe) gets the plaintext portfolio. Only an
// interactive PTY session with no command falls through to the TUI.
func cliMiddleware(next ssh.Handler) ssh.Handler {
	return func(s ssh.Session) {
		if cmd := s.Command(); len(cmd) > 0 {
			out, code := runCommand(cmd)
			_, _ = io.WriteString(s, out)
			_ = s.Exit(code)
			return
		}
		if _, _, active := s.Pty(); !active {
			_, _ = io.WriteString(s, plainPortfolio())
			_ = s.Exit(0)
			return
		}
		next(s)
	}
}

// ── SFTP subsystem ────────────────────────────────────────────────────────────
// Modern OpenSSH `scp` speaks SFTP by default (legacy SCP needs `scp -O`), and
// `sftp` always does. This serves the same in-memory asset FS read-only so all
// of `scp host:f .`, `scp -O host:f .`, and `sftp host` work.

type sftpFS struct{ fsys fs.FS }

func sftpPath(p string) string {
	if p = strings.TrimPrefix(p, "/"); p == "" {
		return "."
	}
	return p
}

func (h sftpFS) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	b, err := fs.ReadFile(h.fsys, sftpPath(req.Filepath))
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func (h sftpFS) Filewrite(*sftp.Request) (io.WriterAt, error) {
	return nil, sftp.ErrSSHFxPermissionDenied
}
func (h sftpFS) Filecmd(*sftp.Request) error { return sftp.ErrSSHFxPermissionDenied }

type listerAt []fs.FileInfo

func (l listerAt) ListAt(out []fs.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	if n < len(out) {
		return n, io.EOF
	}
	return n, nil
}

func (h sftpFS) Filelist(req *sftp.Request) (sftp.ListerAt, error) {
	switch req.Method {
	case "List":
		entries, err := fs.ReadDir(h.fsys, sftpPath(req.Filepath))
		if err != nil {
			return nil, err
		}
		infos := make([]fs.FileInfo, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				return nil, err
			}
			infos = append(infos, info)
		}
		return listerAt(infos), nil
	case "Stat":
		info, err := fs.Stat(h.fsys, sftpPath(req.Filepath))
		if err != nil {
			return nil, err
		}
		return listerAt{info}, nil
	default:
		return nil, sftp.ErrSSHFxOpUnsupported
	}
}

// withSFTP registers a read-only SFTP subsystem serving fsys.
func withSFTP(fsys fs.FS) ssh.Option {
	return func(srv *ssh.Server) error {
		if srv.SubsystemHandlers == nil {
			srv.SubsystemHandlers = map[string]ssh.SubsystemHandler{}
		}
		h := sftpFS{fsys}
		handlers := sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h}
		srv.SubsystemHandlers["sftp"] = func(s ssh.Session) {
			server := sftp.NewRequestServer(s, handlers)
			defer server.Close()
			_ = server.Serve()
		}
		return nil
	}
}
