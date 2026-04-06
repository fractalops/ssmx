package cmd

import (
	"testing"

	"github.com/fractalops/ssmx/internal/session"
)

func TestParseForward(t *testing.T) {
	cases := []struct {
		input   string
		want    session.ForwardSpec
		wantErr bool
	}{
		{
			input: "8080",
			want:  session.ForwardSpec{LocalPort: "8080", RemoteHost: "localhost", RemotePort: "8080"},
		},
		{
			input: "8080:localhost:8080",
			want:  session.ForwardSpec{LocalPort: "8080", RemoteHost: "localhost", RemotePort: "8080"},
		},
		{
			input: "5432:db.internal:5432",
			want:  session.ForwardSpec{LocalPort: "5432", RemoteHost: "db.internal", RemotePort: "5432"},
		},
		{
			input: "5432:db.internal:5433",
			want:  session.ForwardSpec{LocalPort: "5432", RemoteHost: "db.internal", RemotePort: "5433"},
		},
		{
			input:   "notaport",
			wantErr: true,
		},
		{
			input:   "99999",
			wantErr: true, // port out of range
		},
		{
			input:   ":db:5432",
			wantErr: true, // empty local port
		},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseForward(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseForward(%q): expected error, got %+v", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseForward(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseForward(%q) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}
