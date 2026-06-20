package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	lm "github.com/charmbracelet/wish/logging"
	"github.com/charmbracelet/wish/scp"
)

const (
	host = "0.0.0.0"
	port = 23234

	// Session limits keep a public, no-auth SSH server friendly on small
	// (e.g. free-tier) hosts: idle sessions are reaped and no session can be
	// held open forever. These are enforced entirely in-process — no provider
	// configuration is required.
	idleTimeout = 10 * time.Minute
	maxTimeout  = 30 * time.Minute

	// connsPerMin caps new connections per source IP per minute.
	connsPerMin = 12
)

// ── Themes ────────────────────────────────────────────────────────────────────
type theme struct {
	name            string
	accent, fg, dim lipgloss.Color
}

// themes[0] is the default. The rest are unlocked via the hidden Konami code and
// cycled with the "t" key (see the Easter eggs section).
var themes = []theme{
	{"teal", "#4ec9b0", "#d4d4d4", "#6a737d"},
	{"amber", "#e3b341", "#e6dcc8", "#8a7f6a"},
	{"synthwave", "#ff6ac1", "#d7c6ff", "#7c6f9b"},
	{"mono", "#c9d1d9", "#c9d1d9", "#586069"},
}

type styles struct {
	name, title, body, dim, hint, selected lipgloss.Style
}

func makeStyles(r *lipgloss.Renderer, t theme) styles {
	return styles{
		name:     r.NewStyle().Foreground(t.accent).Bold(true),
		title:    r.NewStyle().Foreground(t.accent).Underline(true),
		body:     r.NewStyle().Foreground(t.fg),
		dim:      r.NewStyle().Foreground(t.dim),
		hint:     r.NewStyle().Foreground(t.dim),
		selected: r.NewStyle().Foreground(t.accent).SetString("✦ "),
	}
}

// ── ASCII art ─────────────────────────────────────────────────────────────────
// Paste your ASCII name art here (generated from https://patorjk.com/software/taag/)
const asciiName = `
      __       _
 ____/ /  ____(_)__
/ __/ _ \/ __/ (_-<
\__/_//_/_/ /_/___/
`

// Paste your ASCII portrait here (generated from https://ascii-art-generator.org/)
// or use a short placeholder
const asciiPortrait = `

                         .....:...
                    .:::.:...:::::::...
                   ...  ..      ..:..--:..
                 ...            ... ...:::::
                                       ..:....
                    ..  ..:.......       .....
              ..  ...:::::::::::::...      ....
             :--. ...-===-::...::::::....    ...
            .--:::.:------:......::::::::...  ...
            .-.:.:-===--:..:::....::::::---:
             ::-:--==--:::.:.....:..:.....:.
              -:.:--------::.......: ...:::.
               .::----::::::::::.::-....::..
                :--=-::::::::::::-:-:...::
                :---:::::::::::::----::--.
                --::::::::::::.....::--:.
               .-::::::::::.......::--:.
               .-::::::::..:.....:..::
               :--:::.:::.........::.
               ------:......::.:...
      .      . :----::.    ......
......      ....:--::::..  .....
::. ...   .......:::::::........
. ....    ::..::..:::::::::.....          ..
 ....    .:...:::.::::::::::::...          ....
`

// ── Model ─────────────────────────────────────────────────────────────────────
type page int

const (
	pageHome page = iota
	pageReflections
	pageContacts
	pageArticle
	pageCreations
	pageSnake     // hidden Snake game (easter egg)
	pageSecret    // hidden message screen (easter egg)
	pageBoot      // intro/boot sequence shown on connect
	pageGuestbook // live presence + shared guestbook wall
)

type article struct {
	title, summary, link string
	body                 string // Markdown; falls back to a generated stub when empty
}

type model struct {
	page          page
	navIndex      int // home nav: 0=Creations,1=Reflections,2=Contacts
	reflIndex     int // reflections list cursor
	articleOpen   *article
	width, height int
	frame         int
	tickEpoch     int         // identifies the live animation loop; stale ticks are dropped
	keyLog        []string    // recent landing-page keystrokes, for easter-egg detection
	snake         *snakeState // active hidden Snake game, if any

	// Connection-aware greeting, computed once at connect time.
	guestName  string
	greetWord  string
	clientHint string

	// Theming + secret hunt.
	renderer    *lipgloss.Renderer
	themeIndex  int
	foundSnake  bool
	foundKonami bool

	// Live presence + guestbook (shared via hub).
	hub      *hub
	presence hubState
	input    string
	remoteIP string
	lastPost time.Time
	gbNote   string // transient guestbook status line

	// Scrollable Markdown reader for the open article.
	viewport viewport.Model

	styles styles
}

// homeNav lists the landing-page sections in order.
var homeNav = []string{"Creations(soon)", "Reflections", "Contacts", "Guestbook"}

// articles back the Reflections section. Set `body` to Markdown to publish a
// full essay (it renders scrollable via glamour); without one, a stub is shown.
var articles = []article{
	{
		title:   "Reimagining Human Labor in the Age of AI",
		summary: "The true crisis AI reveals isn't job loss but the absence of meaning...",
		// link: "https://example.com/article1",
		// body: "# Reimagining Human Labor…\n\nYour Markdown here.",
	},
	// {
	//     title:   "AI as a Creative Springboard",
	//     summary: "Enhancing, Not Replacing, Human Ingenuity...",
	// },
}

// bootFrames is how long (in 100ms ticks) the intro sequence plays before it
// auto-advances to the landing page.
const bootFrames = 26

func initialModel() model {
	return model{page: pageBoot, navIndex: 2} // default home highlight: Contacts
}

type tickMsg struct {
	epoch int
}

// tick schedules the next animation frame for a given loop identity (epoch).
func tick(epoch int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(_ time.Time) tea.Msg {
		return tickMsg{epoch: epoch}
	})
}

func (m model) Init() tea.Cmd {
	if m.hub != nil {
		return tea.Batch(tick(m.tickEpoch), hubSnapshotCmd(m.hub))
	}
	return tick(m.tickEpoch)
}

// hubSnapshotCmd delivers the hub's current state to this program once, so a
// freshly-connected session sees presence/guestbook immediately.
func hubSnapshotCmd(h *hub) tea.Cmd {
	return func() tea.Msg { return hubMsg(h.snapshot()) }
}

// postCooldown is the minimum gap between guestbook posts from one session.
const postCooldown = 4 * time.Second

// goHome returns to the landing page and restarts the animation loop. A fresh
// epoch guarantees exactly one live tick loop even after rapid navigation.
func (m model) goHome() (model, tea.Cmd) {
	m.page = pageHome
	m.keyLog = nil
	m.snake = nil
	m.input = ""
	m.gbNote = ""
	m.tickEpoch++
	return m, tick(m.tickEpoch)
}

// goGuestbook opens the live guestbook and refreshes its state.
func (m model) goGuestbook() (model, tea.Cmd) {
	m.page = pageGuestbook
	m.input = ""
	m.gbNote = ""
	if m.hub != nil {
		return m, hubSnapshotCmd(m.hub)
	}
	return m, nil
}

// postGuestbook submits the current draft, respecting a per-session cooldown and
// the hub's per-IP throttle; it surfaces a transient note when held back.
func (m model) postGuestbook() model {
	if strings.TrimSpace(m.input) == "" {
		m.input = ""
		return m
	}
	if time.Since(m.lastPost) < postCooldown {
		m.gbNote = "one sec — you're posting fast"
		return m
	}
	if m.hub != nil && !m.hub.post(m.remoteIP, m.guestName, m.input) {
		m.gbNote = "easy there — too many posts, try again shortly"
		return m
	}
	m.lastPost = time.Now()
	m.input = ""
	m.gbNote = ""
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case tickMsg:
		// Drop frames from a superseded loop, and only keep animating while the
		// landing page (the only animated view) is on screen.
		if msg.epoch != m.tickEpoch {
			return m, nil
		}
		m.frame++
		if m.page == pageBoot {
			if m.frame > bootFrames {
				m.page = pageHome
				m.frame = 0
			}
			return m, tick(m.tickEpoch)
		}
		if m.page == pageHome {
			return m, tick(m.tickEpoch)
		}
		return m, nil

	case snakeTickMsg:
		// Advance the hidden Snake game; stop the loop if it's been superseded
		// or the game ended.
		if msg.epoch != m.tickEpoch || m.page != pageSnake || m.snake == nil {
			return m, nil
		}
		m.snake.step()
		if m.snake.dead {
			return m, nil
		}
		return m, snakeTick(m.tickEpoch)

	case hubMsg:
		m.presence = hubState(msg)
		return m, nil

	case tea.KeyMsg:
		// Global: "t" cycles color palettes from any page — except while typing
		// in the guestbook, where it's just text.
		if msg.String() == "t" && m.page != pageGuestbook {
			m.themeIndex = (m.themeIndex + 1) % len(themes)
			if m.renderer != nil {
				m.styles = makeStyles(m.renderer, themes[m.themeIndex])
			}
			return m, nil
		}

		switch m.page {
		case pageBoot:
			// Any key skips the intro.
			if k := msg.String(); k == "q" || k == "ctrl+c" {
				return m, tea.Quit
			}
			return m.goHome()

		case pageHome:
			// Buffer keystrokes and watch for hidden sequences before handling
			// normal navigation (see the Easter eggs section).
			m.keyLog = append(m.keyLog, msg.String())
			if len(m.keyLog) > 12 {
				m.keyLog = m.keyLog[len(m.keyLog)-12:]
			}
			if endsWith(m.keyLog, konamiCode) {
				m.keyLog = nil
				m.foundKonami = true
				m.page = pageSecret
				return m, nil
			}
			if endsWith(m.keyLog, snakeCode) {
				m.keyLog = nil
				m.foundSnake = true
				return m.startSnake()
			}

			switch msg.String() {
			case "left", "h", "shift+tab":
				if m.navIndex > 0 {
					m.navIndex--
				} else {
					m.navIndex = len(homeNav) - 1
				}
			case "right", "l", "tab":
				if m.navIndex < len(homeNav)-1 {
					m.navIndex++
				} else {
					m.navIndex = 0
				}
			case "enter":
				switch m.navIndex {
				case 0:
					m.page = pageCreations
				case 1:
					m.page = pageReflections
				case 2:
					m.page = pageContacts
				case 3:
					return m.goGuestbook()
				}
			case "q", "ctrl+c":
				return m, tea.Quit
			}

		case pageReflections:
			switch msg.String() {
			case "up", "k":
				if m.reflIndex > 0 {
					m.reflIndex--
				}
			case "down", "j":
				if m.reflIndex < len(articles)-1 {
					m.reflIndex++
				}
			case "enter":
				return m.openArticle(articles[m.reflIndex]), nil
			case "esc":
				return m.goHome()
			case "q", "ctrl+c":
				return m, tea.Quit
			}

		case pageSnake:
			if m.snake == nil {
				return m.goHome()
			}
			switch msg.String() {
			case "up", "k":
				m.snake.turn(point{0, -1})
			case "down", "j":
				m.snake.turn(point{0, 1})
			case "left", "h":
				m.snake.turn(point{-1, 0})
			case "right", "l":
				m.snake.turn(point{1, 0})
			case "r":
				if m.snake.dead {
					return m.startSnake()
				}
			case "esc":
				return m.goHome()
			case "q", "ctrl+c":
				return m, tea.Quit
			}

		case pageGuestbook:
			// A simple text input: type a message, enter posts it to the wall.
			switch msg.Type {
			case tea.KeyEnter:
				m = m.postGuestbook()
			case tea.KeyBackspace, tea.KeyDelete:
				if r := []rune(m.input); len(r) > 0 {
					m.input = string(r[:len(r)-1])
				}
				m.gbNote = ""
			case tea.KeySpace:
				if len([]rune(m.input)) < maxGuestLen {
					m.input += " "
				}
				m.gbNote = ""
			case tea.KeyRunes:
				if len([]rune(m.input)) < maxGuestLen {
					m.input += string(msg.Runes)
				}
				m.gbNote = ""
			case tea.KeyEsc:
				if m.input != "" {
					m.input = ""
				} else {
					return m.goHome()
				}
			case tea.KeyCtrlC:
				return m, tea.Quit
			}

		case pageArticle:
			switch msg.String() {
			case "esc":
				m.page = pageReflections
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Everything else (arrows, j/k, pgup/pgdn, space) scrolls.
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}

		case pageContacts, pageCreations, pageSecret:
			switch msg.String() {
			case "esc":
				return m.goHome()
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	// Guard against terminals too small to lay out the UI.
	if m.width > 0 && (m.width < 50 || m.height < 12) {
		return m.styles.dim.Render(fmt.Sprintf(
			"\n  Terminal too small to render this portfolio.\n"+
				"  Please resize to at least 80x24 (current: %dx%d).\n",
			m.width, m.height))
	}

	switch m.page {
	case pageBoot:
		return m.viewBoot()
	case pageHome:
		return m.viewHome()
	case pageReflections:
		return m.viewReflections()
	case pageContacts:
		return m.viewContacts()
	case pageArticle:
		return m.viewArticle()
	case pageCreations:
		return m.viewCreations()
	case pageSnake:
		if m.snake == nil {
			return ""
		}
		return m.snake.render(m.styles)
	case pageSecret:
		return m.viewSecret()
	case pageGuestbook:
		return m.viewGuestbook()
	}
	return ""
}

func (m model) viewGuestbook() string {
	out := m.styles.title.Render("Guestbook") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
	out += m.styles.name.Render("● ") + m.styles.body.Render(presenceLine(m.presence.count))
	if m.presence.visits > 0 {
		out += m.styles.dim.Render(fmt.Sprintf("   ·   %d visits all-time", m.presence.visits))
	}
	out += "\n\n"

	if len(m.presence.entries) == 0 {
		out += m.styles.dim.Render("No marks yet — be the first.") + "\n"
	} else {
		entries := m.presence.entries
		if len(entries) > 8 {
			entries = entries[len(entries)-8:]
		}
		for _, e := range entries {
			out += m.styles.name.Render(e.name) + m.styles.dim.Render(": ") + m.styles.body.Render(e.text) + "\n"
		}
	}

	out += "\n" + m.styles.dim.Render("leave a mark ") + m.styles.body.Render("› "+m.input) + m.styles.name.Render("▌") + "\n"
	if m.gbNote != "" {
		out += m.styles.dim.Render(m.gbNote) + "\n"
	}
	out += "\n" + m.styles.hint.Render("[type · enter to post · esc back]")
	return "\n" + out
}

// ── Views ─────────────────────────────────────────────────────────────────────
func (m model) renderAnimatedName() string {
	lines := strings.Split(strings.Trim(asciiName, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	// Get max width
	nameWidth := 0
	for _, l := range lines {
		if len(l) > nameWidth {
			nameWidth = len(l)
		}
	}

	// Reduce space as requested: margins are tighter
	marginX := 4
	marginY := 2
	canvasH := len(lines) + 2*marginY
	canvasW := nameWidth + 2*marginX
	grid := make([][]string, canvasH)
	for i := range grid {
		grid[i] = make([]string, canvasW)
		for j := range grid[i] {
			grid[i][j] = " "
		}
	}

	sparkles := []string{"✦", "✧", "⋆", "✧", "+", ".", "*"}

	// Shifting density: sinusoidally oscillate the "target" center
	centerX := float64(canvasW)/2.0 + math.Sin(float64(m.frame)*0.05)*float64(canvasW)*0.4
	centerY := float64(canvasH)/2.0 + math.Cos(float64(m.frame)*0.07)*float64(canvasH)*0.4

	// More sparkles
	numSparkles := 7
	for i := 0; i < numSparkles; i++ {
		// Deterministic but diverse seed
		t := m.frame / 5 // Sparkle positions shift slowly
		seed := int64(m.frame/3 + i*777)

		// Use a simple distribution: start with a base random pos
		// and pull it slightly towards the shifting center
		baseX := float64((seed * 43) % int64(canvasW))
		baseY := float64((seed * 37) % int64(canvasH))

		// Attraction to shifting center (0.3 factor for "random but biased" look)
		posX := baseX*0.7 + centerX*0.3
		posY := baseY*0.7 + centerY*0.3

		x := int(posX)
		y := int(posY)

		// Clip to canvas
		if x < 0 {
			x = 0
		} else if x >= canvasW {
			x = canvasW - 1
		}
		if y < 0 {
			y = 0
		} else if y >= canvasH {
			y = canvasH - 1
		}

		// Twinkle effect: only show if "phase" allows (prevents static clumps)
		phase := (int(seed) + m.frame) % 15
		if phase < 10 {
			// Don't place on the name's non-empty characters
			artX := x - marginX
			artY := y - marginY
			isName := false
			if artY >= 0 && artY < len(lines) && artX >= 0 && artX < len(lines[artY]) {
				if lines[artY][artX] != ' ' {
					isName = true
				}
			}

			if !isName {
				char := sparkles[(i+t)%len(sparkles)]
				grid[y][x] = m.styles.body.Render(char)
			}
		}
	}

	// Place the Name on top
	for y, line := range lines {
		for x, char := range line {
			if char != ' ' {
				grid[y+marginY][x+marginX] = m.styles.name.Render(string(char))
			}
		}
	}

	var result strings.Builder
	for y := range grid {
		for x := range grid[y] {
			result.WriteString(grid[y][x])
		}
		result.WriteString("\n")
	}

	return result.String()
}

func (m model) renderLink(text, url string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

func (m model) wrapText(text string, width int) string {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return ""
	}
	var res strings.Builder
	curr := 0
	for _, w := range words {
		if curr+len(w)+1 > width && curr != 0 {
			res.WriteString("\n")
			curr = 0
		}
		if curr > 0 {
			res.WriteString(" ")
			curr++
		}
		res.WriteString(w)
		curr += len(w)
	}
	return res.String()
}

func (m model) viewBoot() string {
	lines := []string{
		m.styles.dim.Render("▸ establishing secure channel…"),
		m.styles.dim.Render("▸ handshake ok · ") + m.styles.body.Render(m.clientHint),
		m.styles.body.Render("▸ "+m.greetWord+", ") + m.styles.name.Render(m.guestName),
		m.styles.dim.Render("▸ welcome to ") + m.styles.name.Render("chris") + m.styles.dim.Render("'s terminal"),
	}
	shown := m.frame / 4
	var b strings.Builder
	b.WriteString("\n\n")
	for i, l := range lines {
		if i <= shown {
			b.WriteString("  " + l + "\n")
		}
	}
	if shown >= len(lines) {
		b.WriteString("\n  " + m.styles.hint.Render("press any key…"))
	}
	return b.String()
}

func (m model) viewHome() string {
	// Limit width of bio text to avoid stretching
	maxWidth := 55
	if m.width > 0 && m.width-4 < maxWidth {
		maxWidth = m.width - 4
	}

	name := m.renderAnimatedName()
	bio1 := m.styles.body.Render(m.wrapText(bioLead, maxWidth))
	bio2 := m.styles.body.Render("\n" + m.wrapText(bioWork, maxWidth))
	bio3 := m.styles.dim.Render(m.wrapText(bioEdu, maxWidth))
	bio4 := m.styles.dim.Render("\n" + m.wrapText(bioFocus, maxWidth))
	bio5 := m.styles.dim.Render("Explore the directories below ↓")

	nav := ""
	for i, item := range homeNav {
		if i == m.navIndex {
			nav += m.styles.selected.String() + m.styles.name.Render(item) + "   "
		} else {
			nav += "  " + m.styles.body.Render(item) + "   "
		}
	}

	cols := []string{name, bio1, bio2, bio3, bio4, bio5, "\n" + nav}
	if m.presence.count > 1 {
		cols = append(cols, m.styles.dim.Render(fmt.Sprintf("● %d exploring now", m.presence.count)))
	}
	right := lipgloss.JoinVertical(lipgloss.Left, cols...)
	hint := m.styles.hint.Render("\n[← → / tab · enter to open · t theme · q to quit]")

	// On narrow terminals, drop the side portrait and stack the content so it
	// doesn't overflow or wrap awkwardly.
	if m.width > 0 && m.width < 100 {
		return lipgloss.JoinVertical(lipgloss.Left, "\n"+right, hint)
	}

	left := m.styles.body.Render(asciiPortrait)
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
	return lipgloss.JoinVertical(lipgloss.Left, "\n"+content, hint)
}

func (m model) viewReflections() string {
	out := m.styles.title.Render("Reflections") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
	out += m.styles.dim.Render("technology") + "\n"
	for i, a := range articles {
		prefix := "    "
		title := m.styles.body.Render(a.title)
		if i == m.reflIndex {
			prefix = m.styles.selected.String()
			title = m.styles.name.Render(a.title)
		}
		out += prefix + title + "\n"
	}
	out += "\n" + m.styles.hint.Render("[↑ ↓ to select · enter to open · esc back]")
	return "\n" + out
}

func (m model) viewContacts() string {
	out := m.styles.title.Render("Contacts") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
	for _, c := range contacts {
		clickableLink := m.renderLink(m.styles.body.Render(c.display), c.url)
		out += m.styles.name.Render(c.short+" ") + "  " + clickableLink + "\n\n"
	}
	out += m.styles.hint.Render("[esc] back")
	return "\n" + out
}

func (m model) viewCreations() string {
	out := m.styles.title.Render("Creations") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
	out += m.styles.body.Render("Projects and experiments are being curated here.") + "\n"
	out += m.styles.dim.Render("Check back soon — or find work-in-progress on GitHub.") + "\n\n"
	clickableLink := m.renderLink(m.styles.body.Render("github.com/ChrisDc777"), "https://github.com/ChrisDc777")
	out += m.styles.name.Render("GH ") + "  " + clickableLink + "\n\n"
	out += m.styles.hint.Render("[esc] back")
	return "\n" + out
}

// openArticle renders an article's Markdown into a scrollable viewport sized to
// the terminal.
func (m model) openArticle(a article) model {
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	h := m.height - 6
	if h < 5 {
		h = 5
	}
	vp := viewport.New(w, h)
	vp.SetContent(renderMarkdown(articleMarkdown(a), w))
	m.viewport = vp
	m.articleOpen = &a
	m.page = pageArticle
	return m
}

// articleMarkdown returns the article body, or a generated stub built from its
// title/summary when no body has been written yet.
func articleMarkdown(a article) string {
	if strings.TrimSpace(a.body) != "" {
		return a.body
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", a.title)
	if a.summary != "" {
		fmt.Fprintf(&b, "> %s\n\n", a.summary)
	}
	b.WriteString("---\n\n")
	b.WriteString("*This piece is being written.* When it's ready it renders right here — ")
	b.WriteString("Markdown styled for the terminal:\n\n")
	b.WriteString("- **bold**, *italic*, and `inline code`\n")
	b.WriteString("- lists, quotes, and headings\n")
	b.WriteString("- links your terminal can open\n\n")
	if a.link != "" {
		fmt.Fprintf(&b, "[Read the original](%s)\n", a.link)
	} else {
		b.WriteString("_Check back soon._\n")
	}
	return b.String()
}

// renderMarkdown turns Markdown into styled terminal output, wrapped to width.
// On any failure it falls back to the raw Markdown.
func renderMarkdown(md string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

func (m model) viewArticle() string {
	if m.articleOpen == nil {
		return ""
	}
	header := m.styles.title.Render("Reflections") + m.styles.dim.Render("  ·  "+m.articleOpen.title) + "\n"
	footer := m.styles.hint.Render("[↑ ↓ / pgup pgdn to scroll · esc back]") +
		m.styles.dim.Render(fmt.Sprintf("   %.0f%%", m.viewport.ScrollPercent()*100))
	return "\n" + header + m.viewport.View() + "\n" + footer
}

const totalSecrets = 2

func (m model) secretsFound() int {
	n := 0
	if m.foundSnake {
		n++
	}
	if m.foundKonami {
		n++
	}
	return n
}

func mark(found bool) string {
	if found {
		return "✓"
	}
	return "·"
}

func (m model) viewSecret() string {
	out := m.styles.title.Render("✦ secret unlocked ✦") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
	out += m.styles.body.Render("You entered the Konami code. Nicely done.") + "\n\n"
	out += m.styles.name.Render(fmt.Sprintf("secrets found: %d/%d", m.secretsFound(), totalSecrets)) + "\n"
	out += m.styles.dim.Render(fmt.Sprintf("  %s konami   %s snake (type it on the home screen)",
		mark(m.foundKonami), mark(m.foundSnake))) + "\n\n"
	out += m.styles.dim.Render("Curiosity is the best debugger.") + "\n\n"
	out += m.styles.hint.Render("[esc] back")
	return "\n" + out
}

// ── Easter eggs ───────────────────────────────────────────────────────────────
// Two hidden surprises live on the landing page, each triggered by typing a
// secret key sequence (nothing is advertised in the UI):
//   - the Konami code (↑ ↑ ↓ ↓ ← → ← → b a) opens a hidden message screen
//   - typing "snake" launches a small playable Snake game

var (
	konamiCode = []string{"up", "up", "down", "down", "left", "right", "left", "right", "b", "a"}
	snakeCode  = []string{"s", "n", "a", "k", "e"}
)

// endsWith reports whether the tail of log matches seq exactly.
func endsWith(log, seq []string) bool {
	if len(log) < len(seq) {
		return false
	}
	tail := log[len(log)-len(seq):]
	for i := range seq {
		if tail[i] != seq[i] {
			return false
		}
	}
	return true
}

type snakeTickMsg struct {
	epoch int
}

func snakeTick(epoch int) tea.Cmd {
	return tea.Tick(110*time.Millisecond, func(_ time.Time) tea.Msg {
		return snakeTickMsg{epoch: epoch}
	})
}

// startSnake initialises a fresh game sized to the terminal and starts its loop
// under a new epoch (which also retires any landing-page animation loop).
func (m model) startSnake() (model, tea.Cmd) {
	m.snake = newSnake(clamp(m.width-8, 16, 40), clamp(m.height-9, 8, 18))
	m.page = pageSnake
	m.tickEpoch++
	return m, snakeTick(m.tickEpoch)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

type point struct{ x, y int }

type snakeState struct {
	w, h    int
	body    []point // body[0] is the head
	dir     point
	pendDir point // direction applied at the next step (buffered input)
	food    point
	score   int
	dead    bool
}

func newSnake(w, h int) *snakeState {
	cx, cy := w/2, h/2
	s := &snakeState{
		w:       w,
		h:       h,
		dir:     point{1, 0},
		pendDir: point{1, 0},
		body:    []point{{cx, cy}, {cx - 1, cy}, {cx - 2, cy}},
	}
	s.placeFood()
	return s
}

func (s *snakeState) placeFood() {
	for {
		f := point{rand.Intn(s.w), rand.Intn(s.h)}
		onSnake := false
		for _, p := range s.body {
			if p == f {
				onSnake = true
				break
			}
		}
		if !onSnake {
			s.food = f
			return
		}
	}
}

// turn buffers a direction change, ignoring 180° reversals into the snake.
func (s *snakeState) turn(d point) {
	if d.x == -s.dir.x && d.y == -s.dir.y {
		return
	}
	s.pendDir = d
}

func (s *snakeState) step() {
	if s.dead {
		return
	}
	s.dir = s.pendDir
	head := s.body[0]
	next := point{head.x + s.dir.x, head.y + s.dir.y}

	// Wall collision.
	if next.x < 0 || next.x >= s.w || next.y < 0 || next.y >= s.h {
		s.dead = true
		return
	}
	// Self collision (the tail tip is about to move out of the way).
	for i, p := range s.body {
		if i == len(s.body)-1 {
			continue
		}
		if p == next {
			s.dead = true
			return
		}
	}

	s.body = append([]point{next}, s.body...)
	if next == s.food {
		s.score++
		s.placeFood()
	} else {
		s.body = s.body[:len(s.body)-1]
	}
}

func (s *snakeState) render(st styles) string {
	grid := make([][]rune, s.h)
	for y := range grid {
		grid[y] = make([]rune, s.w)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}
	grid[s.food.y][s.food.x] = '◆'
	for i, p := range s.body {
		if i == 0 {
			grid[p.y][p.x] = '█'
		} else {
			grid[p.y][p.x] = '▓'
		}
	}

	var b strings.Builder
	b.WriteString(st.title.Render("snake") + "  " + st.dim.Render(fmt.Sprintf("score %d", s.score)) + "\n")
	b.WriteString(st.dim.Render("┌"+strings.Repeat("─", s.w)+"┐") + "\n")
	for y := range grid {
		b.WriteString(st.dim.Render("│") + st.name.Render(string(grid[y])) + st.dim.Render("│") + "\n")
	}
	b.WriteString(st.dim.Render("└"+strings.Repeat("─", s.w)+"┘") + "\n")
	if s.dead {
		b.WriteString("\n" + st.name.Render("game over") + st.dim.Render(fmt.Sprintf("  ·  final score %d  ·  [r] restart  ·  [esc] back", s.score)))
	} else {
		b.WriteString("\n" + st.hint.Render("[← ↑ → ↓ / hjkl] move  ·  [esc] back  ·  [q] quit"))
	}
	return "\n" + b.String()
}

// ── Connection-aware greeting ─────────────────────────────────────────────────
// greetWord picks a salutation from the server's local time of day (this is
// chris's clock, not the visitor's — SSH doesn't reliably expose the client's
// timezone).
func greetWord(t time.Time) string {
	switch h := t.Hour(); {
	case h < 5:
		return "Burning the midnight oil"
	case h < 12:
		return "Good morning"
	case h < 17:
		return "Good afternoon"
	case h < 21:
		return "Good evening"
	default:
		return "Good night"
	}
}

// greetingName uses the SSH username the visitor connected as (e.g.
// `ssh nova@host` → "nova"), falling back to a friendly placeholder for the
// usual anonymous logins.
func greetingName(s ssh.Session) string {
	switch u := strings.TrimSpace(s.User()); strings.ToLower(u) {
	case "", "root", "guest", "anonymous", "user", "ssh", "visitor":
		return "stranger"
	default:
		return u
	}
}

// clientHint summarises the visitor's SSH client and terminal, e.g.
// "OpenSSH_9.6 · xterm-256color".
func clientHint(s ssh.Session, pty ssh.Pty) string {
	cv := s.Context().ClientVersion()
	cv = strings.TrimPrefix(cv, "SSH-2.0-")
	cv = strings.TrimPrefix(cv, "SSH-1.99-")
	if i := strings.IndexByte(cv, ' '); i > 0 {
		cv = cv[:i]
	}
	if cv == "" {
		cv = "unknown client"
	}
	if pty.Term == "" {
		return cv
	}
	return cv + " · " + pty.Term
}

// ── SSH Server ────────────────────────────────────────────────────────────────
func main() {
	p := os.Getenv("PORT")
	if p == "" {
		p = fmt.Sprintf("%d", port)
	}

	// Allow a stable host key to be supplied via the environment (e.g. a
	// platform secret) so the server's identity survives redeploys. If unset,
	// wish generates an ephemeral key on first start.
	if key := os.Getenv("SSH_HOST_KEY"); key != "" {
		if err := os.MkdirAll(".ssh", 0700); err != nil {
			fmt.Println("could not create .ssh dir:", err)
		} else if err := os.WriteFile(".ssh/term_info_ed25519", []byte(key), 0600); err != nil {
			fmt.Println("could not write host key:", err)
		}
	}

	// In-memory files served read-only over scp (vCard, resume, business card).
	assetFS := buildAssetFS()
	// Shared live state (presence + guestbook), persisted when $DATA_DIR is set.
	h := newHub(newStore())
	// Cap connections per source IP to keep a public, no-auth server civil.
	connLimiter := newLimiter(connsPerMin, time.Minute)

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%s", host, p)),
		wish.WithHostKeyPath(".ssh/term_info_ed25519"),
		wish.WithIdleTimeout(idleTimeout),
		wish.WithMaxTimeout(maxTimeout),
		withSFTP(assetFS), // read-only SFTP so modern `scp`/`sftp` work
		wish.WithMiddleware(
			// Innermost: the interactive TUI (PTY + no command). teaMiddleware
			// builds the per-session model and registers it with the hub.
			teaMiddleware(h),
			cliMiddleware, // `ssh host <cmd>` and pipes → plaintext
			scp.Middleware(scp.NewFSReadHandler(assetFS), nil), // `scp host:file .` downloads (read-only)
			rateLimitMiddleware(connLimiter),                   // reject IPs that connect too often
			lm.Middleware(),                                    // logging (outermost)
		),
	)
	if err != nil {
		fmt.Println("could not create server:", err)
		os.Exit(1)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("SSH portfolio listening on port %s\n", p)
	go func() {
		if err := s.ListenAndServe(); err != nil && err != ssh.ErrServerClosed {
			fmt.Println(err)
		}
	}()
	<-done

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil && err != ssh.ErrServerClosed {
		fmt.Println("could not shut down gracefully:", err)
	}
}
