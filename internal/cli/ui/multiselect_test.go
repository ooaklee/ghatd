package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mustModel(m tea.Model, _ tea.Cmd) Model {
	return m.(Model)
}

func pressKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func TestModelUpdate_Navigation(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		keys     []tea.KeyMsg
		wantCur  int
		wantDone bool
		wantCanc bool
	}{
		{
			name:    "Success - j moves down",
			items:   []string{"a", "b", "c"},
			keys:    []tea.KeyMsg{pressKey('j')},
			wantCur: 1,
		},
		{
			name:    "Success - k moves up from middle",
			items:   []string{"a", "b", "c"},
			keys:    []tea.KeyMsg{pressKey('j'), pressKey('j'), pressKey('k')},
			wantCur: 1,
		},
		{
			name:    "Success - k at top stays at top",
			items:   []string{"a", "b", "c"},
			keys:    []tea.KeyMsg{pressKey('k')},
			wantCur: 0,
		},
		{
			name:    "Success - j at bottom stays at bottom",
			items:   []string{"a", "b", "c"},
			keys:    []tea.KeyMsg{pressKey('j'), pressKey('j'), pressKey('j')},
			wantCur: 2,
		},
		{
			name:    "Success - up arrow moves up",
			items:   []string{"a", "b", "c"},
			keys:    []tea.KeyMsg{pressKey('j'), keyType(tea.KeyUp)},
			wantCur: 0,
		},
		{
			name:    "Success - down arrow moves down",
			items:   []string{"a", "b", "c"},
			keys:    []tea.KeyMsg{keyType(tea.KeyDown)},
			wantCur: 1,
		},
		{
			name:    "Success - enter confirms",
			items:   []string{"a"},
			keys:    []tea.KeyMsg{keyType(tea.KeyEnter)},
			wantCur: 0, wantDone: true,
		},
		{
			name:    "Success - y confirms",
			items:   []string{"a"},
			keys:    []tea.KeyMsg{pressKey('y')},
			wantCur: 0, wantDone: true,
		},
		{
			name:    "Success - q cancels",
			items:   []string{"a"},
			keys:    []tea.KeyMsg{pressKey('q')},
			wantCur: 0, wantCanc: true,
		},
		{
			name:    "Success - ctrl+c cancels",
			items:   []string{"a"},
			keys:    []tea.KeyMsg{keyType(tea.KeyCtrlC)},
			wantCur: 0, wantCanc: true,
		},
		{
			name:    "Success - esc cancels",
			items:   []string{"a"},
			keys:    []tea.KeyMsg{keyType(tea.KeyEsc)},
			wantCur: 0, wantCanc: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel("Test", tt.items)
			for _, msg := range tt.keys {
				m = mustModel(m.Update(msg))
			}
			if m.cursor != tt.wantCur {
				t.Fatalf("cursor = %d, want %d", m.cursor, tt.wantCur)
			}
			if m.done != tt.wantDone {
				t.Fatalf("done = %v, want %v", m.done, tt.wantDone)
			}
			if m.cancelled != tt.wantCanc {
				t.Fatalf("cancelled = %v, want %v", m.cancelled, tt.wantCanc)
			}
		})
	}
}

func TestModelUpdate_Toggle(t *testing.T) {
	tests := []struct {
		name       string
		items      []string
		preSelect  []int
		toggleAt   int
		wantSelect []int
	}{
		{
			name:       "Success - toggle unselected item on",
			items:      []string{"a", "b", "c"},
			preSelect:  nil,
			toggleAt:   0,
			wantSelect: []int{0},
		},
		{
			name:       "Success - toggle selected item off",
			items:      []string{"a", "b", "c"},
			preSelect:  []int{0},
			toggleAt:   0,
			wantSelect: []int{},
		},
		{
			name:       "Success - toggle middle item on",
			items:      []string{"a", "b", "c"},
			preSelect:  []int{0},
			toggleAt:   1,
			wantSelect: []int{0, 1},
		},
		{
			name:       "Success - toggle with empty list no-op",
			items:      nil,
			preSelect:  nil,
			toggleAt:   0,
			wantSelect: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel("Test", tt.items)
			for _, idx := range tt.preSelect {
				m.selected[idx] = true
			}

			var navKeys []tea.KeyMsg
			for i := 0; i < tt.toggleAt; i++ {
				navKeys = append(navKeys, pressKey('j'))
			}
			navKeys = append(navKeys, keyType(tea.KeySpace))

			for _, msg := range navKeys {
				m = mustModel(m.Update(msg))
			}

			got := m.SelectedIndices()
			if !reflect.DeepEqual(got, tt.wantSelect) {
				t.Fatalf("SelectedIndices() = %v, want %v", got, tt.wantSelect)
			}
		})
	}
}

func TestModelUpdate_SelectAll(t *testing.T) {
	m := NewModel("Test", []string{"a", "b", "c"})
	m = mustModel(m.Update(pressKey('a')))

	want := []int{0, 1, 2}
	got := m.SelectedIndices()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectedIndices() = %v, want %v", got, want)
	}
}

func TestModelUpdate_DeselectAll(t *testing.T) {
	m := NewModel("Test", []string{"a", "b", "c"})
	for i := range m.items {
		m.selected[i] = true
	}
	m = mustModel(m.Update(pressKey('n')))

	got := m.SelectedIndices()
	if len(got) != 0 {
		t.Fatalf("SelectedIndices() = %v, want []", got)
	}
}

func TestModelUpdate_ConfirmReturnsSelected(t *testing.T) {
	m := NewModel("Test", []string{"a", "b", "c"})
	m.selected[0] = true
	m.selected[2] = true

	m = mustModel(m.Update(keyType(tea.KeyEnter)))

	if !m.done {
		t.Fatal("expected done=true after enter")
	}
	want := []int{0, 2}
	got := m.SelectedIndices()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectedIndices() = %v, want %v", got, want)
	}
}

func TestModelUpdate_IgnoresKeysAfterQuit(t *testing.T) {
	m := NewModel("Test", []string{"a", "b", "c"})
	m.quitting = true

	m = mustModel(m.Update(pressKey('j')))

	if m.cursor != 0 {
		t.Fatal("cursor moved after quit")
	}
}

func TestModelUpdate_WindowSizePreservesState(t *testing.T) {
	m := NewModel("Test", []string{"a", "b", "c"})
	m.selected[0] = true
	m.cursor = 1

	m = mustModel(m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}))

	if m.cursor != 1 {
		t.Fatalf("cursor changed to %d after window resize", m.cursor)
	}
	if !m.selected[0] {
		t.Fatal("selected items changed after window resize")
	}
}

func TestModelView_RendersAllItems(t *testing.T) {
	m := NewModel("Pick one", []string{"a", "b"})
	m.selected[0] = true

	view := m.View()

	if !contains(view, "Pick one") {
		t.Fatal("view missing title")
	}
	if !contains(view, "> [x] a") {
		t.Fatal("view missing cursor+selected first item")
	}
	if !contains(view, "  [ ] b") {
		t.Fatal("view missing unselected second item")
	}
	if !contains(view, "j/k, up/down: navigate") {
		t.Fatal("view missing help text")
	}
}

func TestModelView_NoItems(t *testing.T) {
	m := NewModel("Empty", nil)
	view := m.View()
	if !contains(view, "Empty") {
		t.Fatal("view missing title for empty list")
	}
}

func TestModelView_QuittingIsEmpty(t *testing.T) {
	m := NewModel("Test", []string{"a"})
	m.quitting = true
	view := m.View()
	if view != "" {
		t.Fatalf("View() when quitting = %q, want \"\"", view)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
