// Package aws provides AWS client helpers for ssmx.
package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// ConfigErrorKind classifies why AWS credential/config loading failed.
type ConfigErrorKind int

// ConfigErrorKind values classify why AWS credential/config loading failed.
const (
	ConfigErrNoCredentials   ConfigErrorKind = iota // no credentials configured at all
	ConfigErrProfileNotFound                        // --profile name not in ~/.aws/config
	ConfigErrSSOExpired                             // SSO profile exists but session is expired
	ConfigErrGeneric                                // any other AWS config/auth error
)

// ConfigError is returned by NewConfig when credentials or configuration are
// unavailable. Kind allows callers to show targeted remediation messages.
type ConfigError struct {
	Kind    ConfigErrorKind
	Profile string // the --profile value that was passed, if any
	Err     error  // underlying SDK error
}

func (e *ConfigError) Error() string { return e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// classifyCredentialError inspects the error message to assign a ConfigErrorKind.
func classifyCredentialError(err error, profile string) ConfigErrorKind {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "shared config profile") ||
		(strings.Contains(msg, "failed to get shared config profile") && profile != "") ||
		(strings.Contains(msg, "does not exist") && profile != ""):
		return ConfigErrProfileNotFound
	case strings.Contains(msg, "token has expired") ||
		strings.Contains(msg, "SSO session not found") ||
		strings.Contains(msg, "SSO token") ||
		strings.Contains(msg, "not logged in") ||
		strings.Contains(msg, "sso profile") ||
		strings.Contains(msg, "refresh cached credentials"):
		return ConfigErrSSOExpired
	case strings.Contains(msg, "credentials") || strings.Contains(msg, "retrieve"):
		return ConfigErrNoCredentials
	default:
		return ConfigErrGeneric
	}
}

// NewConfig builds an aws.Config honouring explicit profile and region
// overrides. Either argument may be empty to fall back to SDK defaults
// (AWS_PROFILE env var, ~/.aws/config, etc.).
func NewConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}

	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("loading AWS config: %w", err)
	}

	// Verify we actually have credentials — fail fast with a human-readable
	// error rather than a cryptic 403 later.
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		kind := classifyCredentialError(err, profile)
		return aws.Config{}, &ConfigError{Kind: kind, Profile: profile, Err: err}
	}
	_ = creds

	return cfg, nil
}
