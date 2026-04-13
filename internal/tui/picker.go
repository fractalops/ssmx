// Package tui provides terminal UI components for ssmx.
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
		return nil, fmt.Errorf("running instance picker: %w", err)
	}
	return final.(PickerModel).result.Instance, nil
}

// Init implements bubbletea.Model.
func (m PickerModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements bubbletea.Model.
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
		if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		} else {
			m.cursor = 0
		}
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

// View implements bubbletea.Model.
func (m PickerModel) View() string {
	var sb strings.Builder

	sb.WriteString(StyleHeader.Render(" ssmx — select an instance") + "\n\n")
	sb.WriteString(" " + m.search.View() + "\n\n")
	header := "  " +
		lipgloss.NewStyle().Width(30).Render("NAME") + " " +
		lipgloss.NewStyle().Width(21).Render("INSTANCE ID") + " " +
		lipgloss.NewStyle().Width(9).Render("STATE") + " " +
		lipgloss.NewStyle().Width(6).Render("SSM") + " " +
		lipgloss.NewStyle().Width(15).Render("PRIVATE IP")
	sb.WriteString(StyleDim.Render(header) + "\n")

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

		// Build each cell using lipgloss width so ANSI codes don't break padding.
		nameText := inst.Name
		if nameText == "" {
			nameText = StyleDim.Render("(no name)")
		} else {
			nameText = truncate(nameText, 30)
		}
		nameCell := lipgloss.NewStyle().Width(30).Render(nameText)

		idCell := lipgloss.NewStyle().Width(21).Render(inst.InstanceID)
		stateCell := lipgloss.NewStyle().Width(9).Render(inst.State)
		ssmCell := lipgloss.NewStyle().Width(6).Render(SSMStatusStyle(inst.SSMStatus).Render(SSMStatusGlyph(inst.SSMStatus)))
		ipCell := lipgloss.NewStyle().Width(15).Render(inst.PrivateIP)

		row := "  " + nameCell + " " + idCell + " " + stateCell + " " + ssmCell + " " + ipCell

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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
