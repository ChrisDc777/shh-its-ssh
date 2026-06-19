package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/muesli/termenv"
)

const (
	maxGuestEntries = 50
	maxGuestLen     = 120
)

type guestEntry struct{ name, text string }

type hubState struct {
	count   int
	entries []guestEntry
}

// hubMsg delivers a fresh presence/guestbook snapshot to a session's program.
type hubMsg hubState

// hub is the shared, in-memory live state across every connected session: who's
// online and the guestbook wall. It's intentionally ephemeral — it resets on
// restart, which is fine for a "right now" presence feature.
type hub struct {
	mu       sync.Mutex
	programs map[*tea.Program]struct{}
	entries  []guestEntry
}

func newHub() *hub {
	return &hub{programs: map[*tea.Program]struct{}{}}
}

func (h *hub) stateLocked() hubState {
	return hubState{count: len(h.programs), entries: append([]guestEntry(nil), h.entries...)}
}

func (h *hub) snapshot() hubState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stateLocked()
}

// broadcast pushes the current state to every program. Sends run in their own
// goroutines: a program that hasn't started Run() yet would otherwise block the
// caller on its unbuffered message channel.
func (h *hub) broadcast() {
	h.mu.Lock()
	st := h.stateLocked()
	progs := make([]*tea.Program, 0, len(h.programs))
	for p := range h.programs {
		progs = append(progs, p)
	}
	h.mu.Unlock()
	for _, p := range progs {
		go p.Send(hubMsg(st))
	}
}

func (h *hub) join(p *tea.Program) {
	h.mu.Lock()
	h.programs[p] = struct{}{}
	h.mu.Unlock()
	h.broadcast()
}

func (h *hub) leave(p *tea.Program) {
	h.mu.Lock()
	delete(h.programs, p)
	h.mu.Unlock()
	h.broadcast()
}

func (h *hub) post(name, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len([]rune(text)) > maxGuestLen {
		text = string([]rune(text)[:maxGuestLen])
	}
	h.mu.Lock()
	h.entries = append(h.entries, guestEntry{name: name, text: text})
	if len(h.entries) > maxGuestEntries {
		h.entries = h.entries[len(h.entries)-maxGuestEntries:]
	}
	h.mu.Unlock()
	h.broadcast()
}

func presenceLine(n int) string {
	switch {
	case n <= 1:
		return "you're the only one here right now"
	case n == 2:
		return "1 other person is here right now"
	default:
		return fmt.Sprintf("%d others are here right now", n-1)
	}
}

// ── Session → TUI wiring ──────────────────────────────────────────────────────

// newModel builds the per-session model: terminal size, the connection-aware
// greeting, a forced color profile, and a hub handle for live presence.
func newModel(s ssh.Session, pty ssh.Pty, h *hub) model {
	m := initialModel()
	m.width, m.height = pty.Window.Width, pty.Window.Height
	m.guestName = greetingName(s)
	m.greetWord = greetWord(time.Now())
	m.clientHint = clientHint(s, pty)
	m.hub = h

	var profile termenv.Profile
	switch {
	case strings.Contains(pty.Term, "truecolor") || strings.Contains(pty.Term, "24bit"):
		profile = termenv.TrueColor
	case strings.Contains(pty.Term, "256color"):
		profile = termenv.ANSI256
	case strings.Contains(pty.Term, "color") || strings.Contains(pty.Term, "ansi"):
		profile = termenv.ANSI
	default:
		profile = termenv.Ascii
	}
	r := lipgloss.NewRenderer(s)
	r.SetColorProfile(profile)
	m.renderer = r
	m.styles = makeStyles(r, themes[m.themeIndex])
	return m
}

// teaMiddleware runs the Bubble Tea TUI for a PTY session and registers it with
// the hub for the lifetime of the connection. It mirrors wish's bubbletea
// middleware (window-size plumbing, graceful quit) but adds the join/leave so
// presence and the guestbook can broadcast to every live program.
func teaMiddleware(h *hub) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			pty, winCh, ok := s.Pty()
			if !ok {
				next(s)
				return
			}
			p := tea.NewProgram(newModel(s, pty, h),
				tea.WithInput(s), tea.WithOutput(s), tea.WithAltScreen())

			ctx, cancel := context.WithCancel(s.Context())
			go func() {
				for {
					select {
					case <-ctx.Done():
						p.Quit()
						return
					case w := <-winCh:
						p.Send(tea.WindowSizeMsg{Width: w.Width, Height: w.Height})
					}
				}
			}()

			h.join(p)
			if _, err := p.Run(); err != nil {
				fmt.Println("tui error:", err)
			}
			h.leave(p)
			p.Kill()
			cancel()
		}
	}
}
