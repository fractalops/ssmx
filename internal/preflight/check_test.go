package preflight

import (
	"strings"
	"testing"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

func TestFormatConfigError(t *testing.T) {
	tests := []struct {
		name    string
		e       *awsclient.ConfigError
		contain string
	}{
		{
			name:    "no credentials",
			e:       &awsclient.ConfigError{Kind: awsclient.ConfigErrNoCredentials},
			contain: "aws configure",
		},
		{
			name:    "profile not found",
			e:       &awsclient.ConfigError{Kind: awsclient.ConfigErrProfileNotFound, Profile: "myprofile"},
			contain: "myprofile",
		},
		{
			name:    "sso expired",
			e:       &awsclient.ConfigError{Kind: awsclient.ConfigErrSSOExpired, Profile: "sso-dev"},
			contain: "aws sso login --profile sso-dev",
		},
		{
			name:    "generic",
			e:       &awsclient.ConfigError{Kind: awsclient.ConfigErrGeneric, Profile: ""},
			contain: "AWS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatConfigError(tt.e)
			if !strings.Contains(got, tt.contain) {
				t.Errorf("formatConfigError() = %q, want it to contain %q", got, tt.contain)
			}
		})
	}
}
