package tui

import "github.com/charmbracelet/lipgloss"

var (
	colourOnline   = lipgloss.Color("#00d7af")
	colourOffline  = lipgloss.Color("#ff5f5f")
	colourUnknown  = lipgloss.Color("#878787")
	colourSelected = lipgloss.Color("#5f87ff")

	StyleOnline   = lipgloss.NewStyle().Foreground(colourOnline).Bold(true)
	StyleOffline  = lipgloss.NewStyle().Foreground(colourOffline).Bold(true)
	StyleUnknown  = lipgloss.NewStyle().Foreground(colourUnknown)
	StyleHeader   = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#ffffff"))
	StyleSelected = lipgloss.NewStyle().Background(colourSelected).Foreground(lipgloss.Color("#ffffff"))
	StyleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	StyleBold     = lipgloss.NewStyle().Bold(true)
	StyleSuccess  = lipgloss.NewStyle().Foreground(colourOnline)
	StyleError    = lipgloss.NewStyle().Foreground(colourOffline)
	StyleWarning  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00"))
)

// SSMStatusStyle returns the appropriate lipgloss style for an SSM status string.
func SSMStatusStyle(status string) lipgloss.Style {
	switch status {
	case "online":
		return StyleOnline
	case "offline":
		return StyleOffline
	default:
		return StyleUnknown
	}
}

// SSMStatusGlyph returns a single character indicator for SSM status.
func SSMStatusGlyph(status string) string {
	switch status {
	case "online":
		return "✓"
	case "offline":
		return "✗"
	default:
		return "?"
	}
}
