package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
	"github.com/muesli/termenv"
)



const (
    host = "0.0.0.0"
    port = 23234
)

var (
    cyan      = lipgloss.Color("#4ec9b0")
    white     = lipgloss.Color("#d4d4d4")
    dimmed    = lipgloss.Color("#6a737d")
)


type styles struct {
	name, title, body, dim, hint, selected lipgloss.Style
}

func makeStyles(r *lipgloss.Renderer) styles {
	return styles{
		name:     r.NewStyle().Foreground(cyan).Bold(true),
		title:    r.NewStyle().Foreground(cyan).Underline(true),
		body:     r.NewStyle().Foreground(white),
		dim:      r.NewStyle().Foreground(dimmed),
		hint:     r.NewStyle().Foreground(dimmed),
		selected: r.NewStyle().Foreground(cyan).SetString("✦ "),
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
)

type article struct {
    title, summary, link string
}

type model struct {
    page        page
    navIndex    int    // home nav: 0=Creations,1=Reflections,2=Contacts
    reflIndex   int    // reflections list cursor
    articleOpen *article
    width, height int
    frame       int
    styles      styles
}


var articles = []article{
    {
        title:   "Reimagining Human Labor in the Age of AI",
        summary: "The true crisis AI reveals isn't job loss but the absence of meaning...",
        // link:    "https://example.com/article1",
    },
    // {
    //     title:   "AI as a Creative Springboard",
    //     summary: "Enhancing, Not Replacing, Human Ingenuity...",
    //     link:    "https://example.com/article2",
    // },
}

func initialModel() model {
    return model{page: pageHome, navIndex: 2} // default highlight: Contacts
}

type tickMsg time.Time

func tick() tea.Cmd {
    return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}

func (m model) Init() tea.Cmd { return tick() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height

    case tickMsg:
        m.frame++
        return m, tick()

    case tea.KeyMsg:
        switch m.page {
        case pageHome:
            switch msg.String() {
            case "left", "h":
                if m.navIndex > 0 { m.navIndex-- }
            case "right", "l":
                if m.navIndex < 2 { m.navIndex++ }
            case "enter":
                switch m.navIndex {
                case 1: m.page = pageReflections
                case 2: m.page = pageContacts
                }
            case "q", "ctrl+c":
                return m, tea.Quit
            }

        case pageReflections:
            switch msg.String() {
            case "up", "k":
                if m.reflIndex > 0 { m.reflIndex-- }
            case "down", "j":
                if m.reflIndex < len(articles)-1 { m.reflIndex++ }
            case "enter":
                a := articles[m.reflIndex]
                m.articleOpen = &a
                m.page = pageArticle
            case "esc":
                m.page = pageHome
            case "q", "ctrl+c":
                return m, tea.Quit
            }

        case pageArticle, pageContacts:
            switch msg.String() {
            case "esc":
                if m.page == pageArticle {
                    m.page = pageReflections
                } else {
                    m.page = pageHome
                }
            case "q", "ctrl+c":
                return m, tea.Quit
            }
        }
    }
    return m, nil
}

func (m model) View() string {
    switch m.page {
    case pageHome:      return m.viewHome()
    case pageReflections: return m.viewReflections()
    case pageContacts:  return m.viewContacts()
    case pageArticle:   return m.viewArticle()
    }
    return ""
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
        if x < 0 { x = 0 } else if x >= canvasW { x = canvasW - 1 }
        if y < 0 { y = 0 } else if y >= canvasH { y = canvasH - 1 }

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
                 grid[y][x] = m.styles.body.Foreground(white).Render(char)
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

func (m model) viewHome() string {
    left := m.styles.body.Render(asciiPortrait)

    // Limit width of bio text to avoid stretching
    maxWidth := 55
    
    name  := m.renderAnimatedName()
    bio1 := m.styles.body.Render(m.wrapText("is a software engineer building intelligent systems on the internet, developing scalable products and experimenting with AI.", maxWidth))
    bio2 := m.styles.body.Render("\n" + m.wrapText("He works across full-stack and backend systems, building APIs, cloud applications, and AI-powered tools.", maxWidth))
    bio3 := m.styles.dim.Render(m.wrapText("Previously, he studied Computer Science Engineering at Symbiosis Institute of Technology, where he built projects in machine learning, computer vision, and AI-driven data systems.", maxWidth))
    bio4 := m.styles.dim.Render("\n" + m.wrapText("His work sits at the intersection of software engineering, artificial intelligence, and real-world problem solving.", maxWidth))
    bio5 := m.styles.dim.Render("Explore the directories below ↓")

    navItems := []string{"Creations(soon)", "Reflections", "Contacts"}
    nav := ""
    for i, item := range navItems {
        if i == m.navIndex {
            nav += m.styles.selected.String() + m.styles.name.Render(item) + "   "
        } else {
            nav += "  " + m.styles.body.Render(item) + "   "
        }
    }

    right := lipgloss.JoinVertical(lipgloss.Left, name, bio1, bio2, bio3, bio4, bio5, "\n"+nav)
    content := lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
    hint := m.styles.hint.Render("\n[← → to select · enter to open · q to quit]")
    return lipgloss.JoinVertical(lipgloss.Left, "\n"+content, hint)
}

func (m model) viewReflections() string {
    out := m.styles.title.Render("Reflections") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
    out += m.styles.dim.Render("technology") + "\n"
    for i, a := range articles {
        prefix := "    "
        title  := m.styles.body.Render(a.title)
        if i == m.reflIndex {
            prefix = m.styles.selected.String()
            title  = m.styles.name.Render(a.title)
        }
        out += prefix + title + "\n"
    }
    out += "\n" + m.styles.hint.Render("[↑ ↓ to select · enter to open · esc back]")
    return "\n" + out
}

func (m model) viewContacts() string {
    out := m.styles.title.Render("Contacts") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
    contacts := []struct{label, display, url string}{
        {"IG ", "instagram.com/chrisdcosta777", "https://instagram.com/chrisdcosta777"},
        {"LI ", "linkedin.com/in/chrisdcosta777", "https://linkedin.com/in/chrisdcosta777"},
        {"GH ", "github.com/ChrisDc777", "https://github.com/ChrisDc777"},
    }
    for _, c := range contacts {
        clickableLink := m.renderLink(m.styles.body.Render(c.display), c.url)
        out += m.styles.name.Render(c.label) + "  " + clickableLink + "\n\n"
    }
    out += m.styles.hint.Render("[esc] back")
    return "\n" + out
}

func (m model) viewArticle() string {
    if m.articleOpen == nil { return "" }
    a := m.articleOpen
    out := m.styles.title.Render("Reflections") + "\n" + m.styles.dim.Render("──────────────") + "\n\n"
    out += m.styles.body.Bold(true).Render(a.title) + "\n\n"
    out += m.styles.body.Render(a.summary) + "\n\n"
    if a.link != "" {
        clickableLink := m.renderLink(m.styles.body.Render(a.link), a.link)
        out += m.styles.name.Render("Read → ") + clickableLink + "\n\n"
    }
    out += m.styles.hint.Render("[esc] back")
    return "\n" + out
}


// ── SSH Server ────────────────────────────────────────────────────────────────
func main() {
	p := os.Getenv("PORT")
	if p == "" {
		p = fmt.Sprintf("%d", port)
	}

	if key := os.Getenv("SSH_HOST_KEY"); key != "" {
		os.MkdirAll(".ssh", 0700)
		os.WriteFile(".ssh/term_info_ed25519", []byte(key), 0600)
	}

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%s", host, p)),
		wish.WithHostKeyPath(".ssh/term_info_ed25519"),
		wish.WithMiddleware(
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				pty, _, _ := s.Pty()
				m := initialModel()
				m.width, m.height = pty.Window.Width, pty.Window.Height

				// Explicitly handle color profile based on PTY term
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

				// Create a renderer for the session and force the color profile
				renderer := lipgloss.NewRenderer(s)
				renderer.SetColorProfile(profile)
				m.styles = makeStyles(renderer)

				return m, []tea.ProgramOption{
					tea.WithAltScreen(),
				}

			}),
			lm.Middleware(),
		),

	)

	if err != nil {
		panic(err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("SSH portfolio listening on port %s\n", p)
	go func() {
		if err := s.ListenAndServe(); err != nil {
			fmt.Println(err)
		}
	}()
	<-done

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.Shutdown(ctx)
}
