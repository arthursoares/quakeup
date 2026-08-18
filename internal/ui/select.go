package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arthursoares/quakeup/internal/install"
)

// Selection is the multi-select screen shown before installation when the
// user didn't pick games via flags. It fills in the selection fields of an
// install.Options.
type selItem struct {
	label   string
	desc    string
	checked bool
	apply   func(*install.Options)
}

type Selection struct {
	items   []selItem
	cursor  int
	aborted bool
	done    bool
}

func NewSelection() Selection {
	return Selection{items: []selItem{
		{
			label: "Quake (2021 rerelease) — vkQuake", checked: true,
			desc:  "campaign, mission packs, Dimension of the Machine",
			apply: func(o *install.Options) { o.Quake1 = true },
		},
		{
			label: "QuakeWorld deathmatch — ezQuake",
			desc:  "the competitive internet-play client (uses the Quake download)",
			apply: func(o *install.Options) { o.EzQuake = true },
		},
		{
			label: "Quake III Arena — ioquake3", checked: true,
			desc:  "singleplayer and multiplayer",
			apply: func(o *install.Options) { o.Quake3 = true },
		},
		{
			label: "Quake 3 extras — CPMA + community packs",
			desc:  "competitive mod, 38 classic maps, QL sounds, HD textures & weapons",
			apply: func(o *install.Options) { o.Extras = true },
		},
		{
			label: "Server files — docker-compose",
			desc:  "self-host QuakeWorld + Quake III servers on any Docker host",
			apply: func(o *install.Options) { o.ServerFiles = true },
		},
	}}
}

func (s Selection) Init() tea.Cmd { return nil }

func (s Selection) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.items)-1 {
				s.cursor++
			}
		case " ", "x":
			s.items[s.cursor].checked = !s.items[s.cursor].checked
		case "a":
			for i := range s.items {
				s.items[i].checked = true
			}
		case "enter":
			for _, it := range s.items {
				if it.checked {
					s.done = true
					return s, tea.Quit
				}
			}
		case "q", "ctrl+c", "esc":
			s.aborted = true
			return s, tea.Quit
		}
	}
	return s, nil
}

func (s Selection) View() string {
	if s.done || s.aborted {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("QUAKEUP") +
		noteStyle.Render("  ·  what should be installed?") + "\n\n")
	for i, it := range s.items {
		box := stepPend.Render("[ ]")
		if it.checked {
			box = stepDone.Render("[x]")
		}
		cursor := "  "
		label := it.label
		if i == s.cursor {
			cursor = promptStyle.Render("> ")
		}
		b.WriteString("  " + cursor + box + " " + label + "\n")
		b.WriteString("        " + noteStyle.Render(it.desc) + "\n")
	}
	b.WriteString("\n  " + noteStyle.Render("space toggle · a all · enter continue · q quit") + "\n")
	return b.String()
}

// Apply writes the chosen selection into opts. Reports false if the user
// aborted or selected nothing.
func (s Selection) Apply(opts *install.Options) bool {
	if s.aborted || !s.done {
		return false
	}
	for _, it := range s.items {
		if it.checked {
			it.apply(opts)
		}
	}
	return true
}
