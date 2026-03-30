package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// PickerResult is returned when the user selects an instance or cancels.
type PickerResult struct {
	Instance *awsclient.Instance
}

// PickerModel is the bubbletea model for the interactive instance picker.
type PickerModel struct {
	instances []awsclient.Instance
	filtered  []awsclient.Instance
	cursor    int
	search    textinput.Model
	done      bool
	result    PickerResult
	width     int
	height    int
}

// NewPickerModel creates a PickerModel populated with the given instances.
func NewPickerModel(instances []awsclient.Instance) PickerModel {
	ti := textinput.New()
	ti.Placeholder = "fuzzy search..."
	ti.Focus()
	ti.CharLimit = 64
	ti.Width = 40

	return PickerModel{
		instances: instances,
		filtered:  instances,
		search:    ti,
	}
}

// RunPicker runs the bubbletea instance picker and returns the chosen instance,
// or nil if the user cancelled.
func RunPicker(instances []awsclient.Instance) (*awsclient.Instance, error) {
	m := NewPickerModel(instances)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	return final.(PickerModel).result.Instance, nil
}

func (m PickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.done = true
			m.result = PickerResult{Instance: nil}
			return m, tea.Quit

		case "enter":
			if len(m.filtered) > 0 {
				inst := m.filtered[m.cursor]
				m.done = true
				m.result = PickerResult{Instance: &inst}
				return m, tea.Quit
			}

		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		}
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	m.applyFilter()
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	return m, cmd
}

func (m *PickerModel) applyFilter() {
	query := strings.ToLower(m.search.Value())
	if query == "" {
		m.filtered = m.instances
		return
	}
	var out []awsclient.Instance
	for _, inst := range m.instances {
		haystack := strings.ToLower(inst.Name + " " + inst.InstanceID + " " + inst.PrivateIP)
		if strings.Contains(haystack, query) {
			out = append(out, inst)
		}
	}
	m.filtered = out
}

func (m PickerModel) View() string {
	var sb strings.Builder

	sb.WriteString(StyleHeader.Render(" ssmx — select an instance") + "\n\n")
	sb.WriteString(" " + m.search.View() + "\n\n")
	sb.WriteString(StyleDim.Render(fmt.Sprintf(
		"  %-30s %-21s %-9s %-8s %-15s\n",
		"NAME", "INSTANCE ID", "STATE", "SSM", "PRIVATE IP",
	)))

	maxRows := m.height - 8
	if maxRows < 1 {
		maxRows = 10
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	for i := start; i < end; i++ {
		inst := m.filtered[i]
		ssmGlyph := SSMStatusGlyph(inst.SSMStatus)
		ssmStyled := SSMStatusStyle(inst.SSMStatus).Render(ssmGlyph)

		name := inst.Name
		if name == "" {
			name = StyleDim.Render("(no name)")
		}

		row := fmt.Sprintf("  %-30s %-21s %-9s %-8s %-15s",
			truncate(name, 30),
			inst.InstanceID,
			inst.State,
			ssmStyled,
			inst.PrivateIP,
		)

		if i == m.cursor {
			row = StyleSelected.Render(row)
		}
		sb.WriteString(row + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(StyleDim.Render(fmt.Sprintf(
		"  %d instances  ↑↓ navigate  enter select  esc cancel",
		len(m.filtered),
	)))

	return lipgloss.NewStyle().Width(m.width).Render(sb.String())
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
