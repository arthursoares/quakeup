// Package ui renders the quakeup installer as an inline Bubble Tea app.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/arthursoares/quakeup/internal/install"
)

var (
	// Quake-flavored bronze on terminals light and dark.
	accent   = lipgloss.AdaptiveColor{Light: "#a34f1c", Dark: "#e08744"}
	subtle   = lipgloss.AdaptiveColor{Light: "#7a7a7a", Dark: "#8a8a8a"}
	good     = lipgloss.AdaptiveColor{Light: "#2a7d3f", Dark: "#5dbb75"}
	bad      = lipgloss.AdaptiveColor{Light: "#b02a2a", Dark: "#e06c6c"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	stepDone    = lipgloss.NewStyle().Foreground(good)
	stepFail    = lipgloss.NewStyle().Foreground(bad)
	stepPend    = lipgloss.NewStyle().Foreground(subtle)
	noteStyle   = lipgloss.NewStyle().Foreground(subtle)
	promptStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
)

type stepStatus int

const (
	pending stepStatus = iota
	active
	done
	skipped
	failed
)

type stepState struct {
	status stepStatus
	note   string
	pct    float64
	bytes  int64
	total  int64
	verb   string
	speed  float64 // bytes/sec, smoothed
	lastAt time.Time
}

type engineClosed struct{}

type Model struct {
	events  <-chan install.Event
	cancel  context.CancelFunc
	opts    install.Options
	steps   []stepState
	order   []install.StepID
	spin    spinner.Model
	bar     progress.Model
	input   textinput.Model
	asking  *install.Event
	lastLog string
	fatal   string
	fatalAt install.StepID
	done    bool
	width   int
}

func New(events <-chan install.Event, cancel context.CancelFunc, opts install.Options) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(accent)))
	bar := progress.New(progress.WithSolidFill("#e08744"), progress.WithoutPercentage())
	bar.Width = 34
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 128
	order := []install.StepID{
		install.StepPreflight, install.StepSteamCMD, install.StepQuake1,
		install.StepQuake3, install.StepVkQuake, install.StepIoquake3,
		install.StepEzQuake, install.StepExtras, install.StepServerFiles, install.StepWiring,
	}
	return Model{
		events: events,
		cancel: cancel,
		opts:   opts,
		steps:  make([]stepState, len(order)),
		order:  order,
		spin:   sp,
		bar:    bar,
		input:  ti,
		width:  80,
	}
}

func waitForEvent(ch <-chan install.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return engineClosed{}
		}
		return ev
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, waitForEvent(m.events))
}

func (m Model) idx(s install.StepID) int {
	for i, id := range m.order {
		if id == s {
			return i
		}
	}
	return 0
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		if m.asking != nil {
			switch msg.Type {
			case tea.KeyEnter:
				m.asking.Reply <- m.input.Value()
				m.asking = nil
				m.input.Reset()
				return m, nil
			case tea.KeyCtrlC:
				m.cancel()
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "q", "ctrl+c":
			if m.done || m.fatal != "" {
				return m, tea.Quit
			}
			m.cancel()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case install.Event:
		return m.handleEvent(msg)

	case engineClosed:
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleEvent(ev install.Event) (tea.Model, tea.Cmd) {
	i := m.idx(ev.Step)
	st := &m.steps[i]
	switch ev.Kind {
	case install.EvStepStart:
		st.status = active
	case install.EvStepDone:
		st.status = done
		st.note = ev.Msg
	case install.EvStepSkip:
		st.status = skipped
		st.note = ev.Msg
	case install.EvProgress:
		now := time.Now()
		if !st.lastAt.IsZero() && ev.BytesDone > st.bytes {
			dt := now.Sub(st.lastAt).Seconds()
			if dt > 0 {
				inst := float64(ev.BytesDone-st.bytes) / dt
				if st.speed == 0 {
					st.speed = inst
				} else {
					st.speed = 0.7*st.speed + 0.3*inst
				}
			}
		}
		st.lastAt = now
		st.pct, st.bytes, st.total, st.verb = ev.Pct, ev.BytesDone, ev.BytesTotal, ev.Msg
	case install.EvLog:
		m.lastLog = ev.Msg
	case install.EvNeedInput:
		evCopy := ev
		m.asking = &evCopy
		m.input.Reset()
		if ev.Masked {
			m.input.EchoMode = textinput.EchoPassword
		} else {
			m.input.EchoMode = textinput.EchoNormal
		}
		m.input.Focus()
		return m, tea.Batch(textinput.Blink, waitForEvent(m.events))
	case install.EvFatal:
		st.status = failed
		m.fatal = ev.Msg
		m.fatalAt = ev.Step
	case install.EvDone:
		m.done = true
	}
	return m, waitForEvent(m.events)
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("QUAKEUP") +
		noteStyle.Render("  ·  Quake + Quake III Arena for your Mac") + "\n\n")

	for i, id := range m.order {
		st := m.steps[i]
		var icon, line string
		switch st.status {
		case pending:
			icon = stepPend.Render("○")
			line = stepPend.Render(id.Title())
		case active:
			icon = m.spin.View()
			line = id.Title()
		case done:
			icon = stepDone.Render("✓")
			line = id.Title()
		case skipped:
			// "not selected" is a non-goal, not an achievement — render it
			// dimmed; genuine already-done skips keep the checkmark.
			if st.note == "not selected" {
				icon = stepPend.Render("–")
				line = stepPend.Render(id.Title()) + noteStyle.Render("  (not selected)")
			} else {
				icon = stepDone.Render("✓")
				line = id.Title() + noteStyle.Render("  ("+st.note+")")
			}
		case failed:
			icon = stepFail.Render("✗")
			line = stepFail.Render(id.Title())
		}
		b.WriteString("  " + icon + " " + line + "\n")

		if st.status == active && st.total > 0 {
			info := fmt.Sprintf("%s / %s", human(st.bytes), human(st.total))
			if st.speed > 1 {
				info += fmt.Sprintf("  ·  %s/s", human(int64(st.speed)))
			}
			b.WriteString("      " + m.bar.ViewAs(st.pct) + "  " + noteStyle.Render(info) + "\n")
		}
	}

	if m.asking != nil {
		b.WriteString("\n  " + promptStyle.Render(m.asking.Msg) + "\n  " + m.input.View() + "\n")
	} else if m.fatal != "" {
		b.WriteString("\n  " + stepFail.Render("✗ "+m.fatalAt.Title()+" failed: "+m.fatal) + "\n")
		b.WriteString(noteStyle.Render("  quakeup is safe to re-run; finished steps are skipped. Press q to exit.") + "\n")
	} else if m.done {
		b.WriteString("\n  " + stepDone.Render("Done!") + "\n")
		var lines []string
		for _, l := range [][2]string{
			{"Quake 1:         ", "play-quake1.sh"},
			{"QuakeWorld:      ", "play-quakeworld.sh"},
			{"Quake III Arena: ", "play-quake3.sh"},
		} {
			path := m.opts.Dir + "/" + l[1]
			if _, err := os.Stat(path); err == nil {
				lines = append(lines, "    "+l[0]+" "+path)
			}
		}
		if _, err := os.Stat(m.opts.Dir + "/server/docker-compose.yml"); err == nil {
			lines = append(lines, "    Servers:          edit "+m.opts.Dir+"/server/.env, then docker compose up -d")
		}
		if m.opts.Quake1 {
			lines = append(lines, "    Mission packs:    play-quake1.sh -hipnotic | -rogue | -game mg1")
		}
		b.WriteString(noteStyle.Render(strings.Join(lines, "\n")+"\n\n  Press q to exit.") + "\n")
	} else if m.lastLog != "" {
		log := m.lastLog
		if max := m.width - 6; max > 10 && len(log) > max {
			log = log[:max-1] + "…"
		}
		b.WriteString("\n  " + noteStyle.Render(log) + "\n")
	}
	return b.String()
}

// Failed reports whether the run ended in an error, for the process exit code.
func (m Model) Failed() bool { return m.fatal != "" }
func (m Model) Done() bool   { return m.done }
