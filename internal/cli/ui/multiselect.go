package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	checkboxSelected   = "[x]"
	checkboxUnselected = "[ ]"
	cursorMarker       = ">"
	helpStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	titleStyle         = lipgloss.NewStyle().Bold(true)
)

type Model struct {
	items     []string
	cursor    int
	selected  map[int]bool
	title     string
	done      bool
	cancelled bool
	quitting  bool
}

func NewModel(title string, items []string) Model {
	selected := make(map[int]bool, len(items))
	return Model{
		items:    items,
		title:    title,
		selected: selected,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// no-op; preserved for future layout
		return m, nil
	case tea.KeyMsg:
		if m.quitting {
			return m, nil
		}
		switch {
		case key.Matches(msg, defaultKeyMap.Quit):
			m.cancelled = true
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, defaultKeyMap.Confirm):
			m.done = true
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, defaultKeyMap.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, defaultKeyMap.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case key.Matches(msg, defaultKeyMap.Toggle):
			if len(m.items) > 0 {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}
		case key.Matches(msg, defaultKeyMap.SelectAll):
			for i := range m.items {
				m.selected[i] = true
			}
		case key.Matches(msg, defaultKeyMap.DeselectAll):
			m.selected = make(map[int]bool)
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	if m.title != "" {
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n\n")
	}

	for i, item := range m.items {
		checked := checkboxUnselected
		if m.selected[i] {
			checked = checkboxSelected
		}

		switch {
		case i == m.cursor:
			b.WriteString(fmt.Sprintf("%s %s %s\n", cursorMarker, checked, item))
		default:
			b.WriteString(fmt.Sprintf("  %s %s\n", checked, item))
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k, up/down: navigate  space: toggle  a: all  n: none  enter/y: done  q/esc: quit"))

	return b.String()
}

func (m Model) SelectedIndices() []int {
	result := make([]int, 0, len(m.selected))
	for i := range m.items {
		if m.selected[i] {
			result = append(result, i)
		}
	}
	return result
}

func (m Model) Cancelled() bool {
	return m.cancelled
}

func MultiSelect(title string, items []string) ([]int, error) {
	m := NewModel(title, items)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("ui: %w", err)
	}
	fm := final.(Model)
	if fm.Cancelled() {
		return nil, nil
	}
	return fm.SelectedIndices(), nil
}

type keyMap struct {
	Up          key.Binding
	Down        key.Binding
	Toggle      key.Binding
	SelectAll   key.Binding
	DeselectAll key.Binding
	Confirm     key.Binding
	Quit        key.Binding
}

var defaultKeyMap = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "down"),
	),
	Toggle: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "toggle"),
	),
	SelectAll: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "select all"),
	),
	DeselectAll: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "deselect all"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter", "y"),
		key.WithHelp("enter/y", "confirm"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q/ctrl+c/esc", "quit"),
	),
}
