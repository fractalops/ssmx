package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

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
		return aws.Config{}, fmt.Errorf("no AWS credentials found (run `aws configure` or set AWS_ACCESS_KEY_ID): %w", err)
	}
	_ = creds

	return cfg, nil
}
