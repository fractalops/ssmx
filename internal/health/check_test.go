package health

import "testing"

func TestSeverityGlyph(t *testing.T) {
	cases := []struct {
		sev  Severity
		want string
	}{
		{SeverityOK, "✓"},
		{SeverityWarn, "!"},
		{SeverityError, "✗"},
		{SeveritySkip, "?"},
	}
	for _, tc := range cases {
		if got := tc.sev.Glyph(); got != tc.want {
			t.Errorf("Severity(%d).Glyph() = %q, want %q", tc.sev, got, tc.want)
		}
	}
}
