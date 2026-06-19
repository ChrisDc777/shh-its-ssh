package main

import (
	"io"
	"io/fs"
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

func TestRunCommand(t *testing.T) {
	if out, code := runCommand([]string{"help"}); code != 0 || !strings.Contains(out, "scp") {
		t.Errorf("help should list scp downloads, code=%d:\n%s", code, out)
	}
	if out, _ := runCommand([]string{"social"}); !strings.Contains(out, "github.com/ChrisDc777") {
		t.Errorf("social should list links:\n%s", out)
	}
	if out, _ := runCommand([]string{"resume"}); !strings.Contains(out, fullName) {
		t.Errorf("resume should include the name:\n%s", out)
	}
	if out, code := runCommand([]string{"bogus"}); code != 2 || !strings.Contains(out, "unknown command") {
		t.Errorf("unknown command should exit non-zero with help, code=%d:\n%s", code, out)
	}
}

func TestVCardAndAssets(t *testing.T) {
	vc := vCard()
	for _, want := range []string{"BEGIN:VCARD", "VERSION:3.0", "FN:" + fullName, "github.com/ChrisDc777", "END:VCARD"} {
		if !strings.Contains(vc, want) {
			t.Errorf("vCard missing %q:\n%s", want, vc)
		}
	}
	assetFS := buildAssetFS()
	for _, name := range []string{"chris.vcf", "resume.txt", "card.txt"} {
		b, err := fs.ReadFile(assetFS, name)
		if err != nil || len(b) == 0 {
			t.Errorf("asset %q not readable: err=%v len=%d", name, err, len(b))
		}
	}
}

func TestPlainPortfolio(t *testing.T) {
	out := plainPortfolio()
	if !strings.Contains(out, "software engineer") || !strings.Contains(out, "github.com/ChrisDc777") {
		t.Errorf("plain portfolio should include bio and links:\n%s", out)
	}
}

func TestHubPostAndCap(t *testing.T) {
	h := newHub()
	h.post("nova", "  hello world  ")
	h.post("ada", "") // empty is ignored
	st := h.snapshot()
	if len(st.entries) != 1 || st.entries[0].name != "nova" || st.entries[0].text != "hello world" {
		t.Fatalf("post/trim failed: %+v", st.entries)
	}
	for i := 0; i < maxGuestEntries+10; i++ {
		h.post("x", "msg")
	}
	if got := len(h.snapshot().entries); got != maxGuestEntries {
		t.Fatalf("guestbook should cap at %d, got %d", maxGuestEntries, got)
	}
}

func TestGuestbookInputPosts(t *testing.T) {
	m := testModel()
	m.hub = newHub()
	m.guestName = "nova"
	m, _ = m.goGuestbook()
	if m.page != pageGuestbook {
		t.Fatalf("expected guestbook page, got %v", m.page)
	}
	for _, r := range "hi" {
		nm, _ := m.Update(keyMsg(string(r)))
		m = nm.(model)
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if m.input != "" {
		t.Errorf("input should clear after posting, got %q", m.input)
	}
	if st := m.hub.snapshot(); len(st.entries) != 1 || st.entries[0].text != "hi" {
		t.Fatalf("guestbook post not recorded: %+v", st.entries)
	}
}

func TestPresenceLine(t *testing.T) {
	if !strings.Contains(presenceLine(1), "only one") {
		t.Error("count 1 should read 'only one'")
	}
	if !strings.Contains(presenceLine(4), "3 others") {
		t.Errorf("count 4 should read '3 others', got %q", presenceLine(4))
	}
}

func TestHubMsgUpdatesPresence(t *testing.T) {
	m := testModel()
	nm, _ := m.Update(hubMsg{count: 3, entries: []guestEntry{{"ada", "hi"}}})
	m = nm.(model)
	if m.presence.count != 3 || len(m.presence.entries) != 1 {
		t.Fatalf("hubMsg should update presence: %+v", m.presence)
	}
}

func TestViewGuestbookAndHomePresence(t *testing.T) {
	m := testModel()
	m.presence = hubState{count: 2, entries: []guestEntry{{"ada", "first mark"}}}

	gb := m.viewGuestbook()
	for _, want := range []string{"Guestbook", "first mark", "ada", "other person"} {
		if !strings.Contains(gb, want) {
			t.Errorf("guestbook view missing %q:\n%s", want, gb)
		}
	}

	m.page = pageHome
	if !strings.Contains(m.viewHome(), "exploring now") {
		t.Error("home should show the live presence indicator when count > 1")
	}
	m.presence.count = 1
	if strings.Contains(m.viewHome(), "exploring now") {
		t.Error("home should hide the presence indicator when you're alone")
	}
}

func TestHubJoinLeaveCount(t *testing.T) {
	p1 := tea.NewProgram(testModel())
	p2 := tea.NewProgram(testModel())
	defer p1.Kill()
	defer p2.Kill()
	h := newHub()
	h.join(p1)
	h.join(p2)
	if got := h.snapshot().count; got != 2 {
		t.Fatalf("expected 2 present, got %d", got)
	}
	h.leave(p1)
	if got := h.snapshot().count; got != 1 {
		t.Fatalf("expected 1 present after leave, got %d", got)
	}
}
