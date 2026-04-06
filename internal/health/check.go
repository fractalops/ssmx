package health

import "github.com/charmbracelet/lipgloss"

// Severity indicates how actionable a health check result is.
type Severity int

const (
	SeverityOK    Severity = iota // ✓ — no action needed
	SeverityWarn                  // ! — degraded or missing optional config
	SeverityError                 // ✗ — session will not work
	SeveritySkip                  // ? — check was skipped (e.g. permission denied)
)

// Result is the output of a single health check.
type Result struct {
	Section  string
	Label    string
	Severity Severity
	Detail   string // optional — printed dim after the label
}

// Section name constants used by the runner.
const (
	SectionPrerequisites = "Prerequisites"
	SectionInstance      = "Instance"
	SectionCallerIAM     = "Caller IAM"
	SectionInstanceRole  = "Instance Role Permissions"
	SectionNetwork       = "Network"
)

// Glyph returns the single-character indicator for this severity.
func (s Severity) Glyph() string {
	switch s {
	case SeverityOK:
		return "✓"
	case SeverityWarn:
		return "!"
	case SeverityError:
		return "✗"
	default:
		return "?"
	}
}

// Style returns the lipgloss render style for this severity.
func (s Severity) Style() lipgloss.Style {
	switch s {
	case SeverityOK:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#00d7af")).Bold(true)
	case SeverityWarn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")).Bold(true)
	case SeverityError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#878787"))
	}
}
