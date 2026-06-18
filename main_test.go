package main

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// testModel builds a model with a plain-text (Ascii) renderer so view output
// can be asserted on without ANSI escape codes getting in the way.
func testModel() model {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.Ascii)
	m := initialModel()
	m.renderer = r
	m.styles = makeStyles(r, themes[0])
	m.width, m.height = 120, 40
	m.guestName = "nova"
	m.greetWord = "Good evening"
	m.clientHint = "OpenSSH_9.6 · xterm-256color"
	return m
}

// keyMsg constructs a tea.KeyMsg whose String() matches the names used in
// Update (e.g. "up", "a", "t").
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestEndsWith(t *testing.T) {
	if !endsWith([]string{"x", "s", "n", "a", "k", "e"}, snakeCode) {
		t.Fatal("expected snake suffix to match")
	}
	if endsWith([]string{"s", "n", "a", "k"}, snakeCode) {
		t.Fatal("partial sequence should not match")
	}
	if !endsWith(konamiCode, konamiCode) {
		t.Fatal("konami should match itself")
	}
}

func TestViewBoot(t *testing.T) {
	m := testModel()
	m.page = pageBoot
	m.frame = 40 // far enough that every line is revealed
	out := m.viewBoot()
	for _, want := range []string{"secure channel", "handshake ok", "OpenSSH_9.6", "Good evening", "nova", "terminal"} {
		if !strings.Contains(out, want) {
			t.Errorf("viewBoot output missing %q:\n%s", want, out)
		}
	}
}

func TestViewSecretMeter(t *testing.T) {
	m := testModel()
	m.foundKonami = true
	m.foundSnake = true
	out := m.viewSecret()
	if !strings.Contains(out, "2/2") {
		t.Errorf("secret meter should read 2/2:\n%s", out)
	}
	if !strings.Contains(out, "Palettes unlocked") {
		t.Errorf("secret screen should mention palette unlock:\n%s", out)
	}
}

func TestKonamiUnlocksThemes(t *testing.T) {
	m := testModel()
	m.page = pageHome
	for _, k := range konamiCode {
		nm, _ := m.Update(keyMsg(k))
		m = nm.(model)
	}
	if !m.themesUnlocked || !m.foundKonami || m.page != pageSecret {
		t.Fatalf("konami did not work: unlocked=%v konami=%v page=%v", m.themesUnlocked, m.foundKonami, m.page)
	}
	before := m.themeIndex
	nm, _ := m.Update(keyMsg("t"))
	m = nm.(model)
	if m.themeIndex == before {
		t.Fatal("'t' should cycle the active theme once unlocked")
	}
}

func TestSnakeTriggerAndMechanics(t *testing.T) {
	// Typing "snake" on the home page starts the game.
	m := testModel()
	m.page = pageHome
	for _, k := range snakeCode {
		nm, _ := m.Update(keyMsg(k))
		m = nm.(model)
	}
	if m.page != pageSnake || m.snake == nil || !m.foundSnake {
		t.Fatalf("typing snake did not start the game: page=%v snake=%v found=%v", m.page, m.snake != nil, m.foundSnake)
	}

	// Eating food directly ahead grows the snake and scores.
	s := newSnake(10, 10)
	s.food = point{s.body[0].x + 1, s.body[0].y}
	before := len(s.body)
	s.step()
	if len(s.body) != before+1 || s.score != 1 {
		t.Fatalf("eating food should grow+score: len=%d score=%d", len(s.body), s.score)
	}

	// Running into a wall ends the game.
	s = newSnake(10, 10)
	for i := 0; i < 30 && !s.dead; i++ {
		s.step()
	}
	if !s.dead {
		t.Fatal("snake should die after running into a wall")
	}
}

func TestGreetWord(t *testing.T) {
	cases := map[int]string{2: "midnight", 8: "morning", 14: "afternoon", 19: "evening", 23: "night"}
	for h, want := range cases {
		g := greetWord(time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC))
		if !strings.Contains(strings.ToLower(g), want) {
			t.Errorf("hour %d: got %q, want substring %q", h, g, want)
		}
	}
}
