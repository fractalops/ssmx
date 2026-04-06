package health

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// CallerIdentity returns the ARN of the currently authenticated identity
// via sts:GetCallerIdentity.
func CallerIdentity(ctx context.Context, cfg aws.Config) (string, error) {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("sts:GetCallerIdentity: %w", err)
	}
	return aws.ToString(out.Arn), nil
}

// InstanceProfileRoleARN returns the IAM role ARN attached to the given
// instance profile. profileARN is the full ARN, e.g.
// "arn:aws:iam::123:instance-profile/MyProfile".
// Returns an error if the profile has no roles attached or the API call fails.
func InstanceProfileRoleARN(ctx context.Context, cfg aws.Config, profileARN string) (string, error) {
	name := instanceProfileName(profileARN)
	out, err := iam.NewFromConfig(cfg).GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{
		InstanceProfileName: aws.String(name),
	})
	if err != nil {
		return "", fmt.Errorf("iam:GetInstanceProfile: %w", err)
	}
	if len(out.InstanceProfile.Roles) == 0 {
		return "", fmt.Errorf("instance profile %s has no roles attached", name)
	}
	return aws.ToString(out.InstanceProfile.Roles[0].Arn), nil
}

// SimulatePolicy uses iam:SimulatePrincipalPolicy to test whether sourceARN is
// allowed to perform each of the given actions against "*". Returns a map of
// action name → allowed. If the API call itself fails (e.g. AccessDenied),
// the error is returned and the caller should degrade gracefully.
func SimulatePolicy(ctx context.Context, cfg aws.Config, sourceARN string, actions []string) (map[string]bool, error) {
	out, err := iam.NewFromConfig(cfg).SimulatePrincipalPolicy(ctx, &iam.SimulatePrincipalPolicyInput{
		PolicySourceArn: aws.String(sourceARN),
		ActionNames:     actions,
		ResourceArns:    []string{"*"},
	})
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(out.EvaluationResults))
	for _, r := range out.EvaluationResults {
		result[aws.ToString(r.EvalActionName)] = string(r.EvalDecision) == "allowed"
	}
	return result, nil
}

// instanceProfileName extracts the profile name from an IAM instance profile ARN.
// "arn:aws:iam::123:instance-profile/MyProfile" → "MyProfile"
// If no slash is found, the input is returned unchanged.
func instanceProfileName(arn string) string {
	if idx := strings.LastIndex(arn, "/"); idx >= 0 {
		return arn[idx+1:]
	}
	return arn
}
