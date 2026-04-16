package aws

import (
	"errors"
	"testing"
)

func TestClassifyCredentialError(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		profile string
		want    ConfigErrorKind
	}{
		{"no credentials", "failed to retrieve credentials", "", ConfigErrNoCredentials},
		{"profile not found", "failed to get shared config profile, myprofile", "myprofile", ConfigErrProfileNotFound},
		{"sso expired", "token has expired", "sso-dev", ConfigErrSSOExpired},
		{"sso not logged in", "SSO session not found", "sso-dev", ConfigErrSSOExpired},
		{"sso with profile in msg", "failed to refresh cached credentials for sso profile dev", "dev", ConfigErrSSOExpired},
		{"generic", "some other error", "", ConfigErrGeneric},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCredentialError(errors.New(tt.msg), tt.profile)
			if got != tt.want {
				t.Errorf("classifyCredentialError(%q, %q) = %v, want %v", tt.msg, tt.profile, got, tt.want)
			}
		})
	}
}

func TestConfigError_Error(t *testing.T) {
	inner := errors.New("inner")
	e := &ConfigError{Kind: ConfigErrSSOExpired, Profile: "dev", Err: inner}
	if e.Error() == "" {
		t.Error("Error() must not be empty")
	}
	if !errors.Is(e, inner) {
		t.Error("Unwrap must expose inner error")
	}
}
