package health

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// Run performs all health checks for inst and streams Result values on the
// returned channel. The channel is closed when all checks are complete.
// Callers should range over the channel.
func Run(ctx context.Context, cfg aws.Config, inst *awsclient.Instance) <-chan Result {
	ch := make(chan Result, 30)
	go func() {
		defer close(ch)
		runPrereqs(cfg, ch)
		runInstanceChecks(inst, ch)
		runCallerIAM(ctx, cfg, ch)
		if inst.IAMProfileARN != "" {
			runInstanceRoleChecks(ctx, cfg, inst.IAMProfileARN, ch)
		} else {
			ch <- Result{
				Section:  SectionInstanceRole,
				Label:    "No IAM instance profile attached",
				Severity: SeverityError,
				Detail:   "attach a role with AmazonSSMManagedInstanceCore",
			}
		}
		if inst.VPCID != "" {
			runNetworkChecks(ctx, cfg, inst.VPCID, cfg.Region, ch)
		}
	}()
	return ch
}

func runPrereqs(cfg aws.Config, ch chan<- Result) {
	const sec = SectionPrerequisites

	if _, err := exec.LookPath("session-manager-plugin"); err == nil {
		ch <- Result{Section: sec, Label: "session-manager-plugin installed", Severity: SeverityOK}
	} else {
		ch <- Result{
			Section:  sec,
			Label:    "session-manager-plugin not found",
			Severity: SeverityError,
			Detail:   "install from https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html",
		}
	}

	// Credentials were already verified by awsclient.NewConfig before Run is called.
	ch <- Result{Section: sec, Label: "AWS credentials configured", Severity: SeverityOK}

	if cfg.Region != "" {
		ch <- Result{Section: sec, Label: "Region: " + cfg.Region, Severity: SeverityOK}
	} else {
		ch <- Result{
			Section:  sec,
			Label:    "No region set",
			Severity: SeverityWarn,
			Detail:   "use -r flag or set default_region in ~/.ssmx/config.yaml",
		}
	}
}

func runInstanceChecks(inst *awsclient.Instance, ch chan<- Result) {
	const sec = SectionInstance

	switch inst.State {
	case "running":
		ch <- Result{Section: sec, Label: "EC2 instance running", Severity: SeverityOK, Detail: inst.InstanceID}
	case "stopped", "stopping":
		ch <- Result{Section: sec, Label: "EC2 instance " + inst.State, Severity: SeverityError}
	default:
		ch <- Result{Section: sec, Label: "EC2 instance " + inst.State, Severity: SeverityWarn}
	}

	if inst.IAMProfileARN != "" {
		ch <- Result{Section: sec, Label: "IAM instance profile attached", Severity: SeverityOK, Detail: inst.IAMProfileARN}
	} else {
		ch <- Result{
			Section:  sec,
			Label:    "No IAM instance profile",
			Severity: SeverityError,
			Detail:   "attach a role with AmazonSSMManagedInstanceCore",
		}
	}

	switch inst.SSMStatus {
	case "online":
		detail := inst.AgentVersion
		if inst.LastPingAt != "" {
			detail += ", last ping " + inst.LastPingAt
		}
		ch <- Result{Section: sec, Label: "SSM agent online", Severity: SeverityOK, Detail: detail}
	case "offline":
		ch <- Result{
			Section:  sec,
			Label:    "SSM agent offline",
			Severity: SeverityError,
			Detail:   "check SSM agent status on the instance",
		}
	default:
		ch <- Result{
			Section:  sec,
			Label:    "SSM agent not registered",
			Severity: SeverityError,
			Detail:   "SSM agent may not be installed or instance role is missing permissions",
		}
	}
}

type actionCheck struct {
	action  string
	denySev Severity
}

func runCallerIAM(ctx context.Context, cfg aws.Config, ch chan<- Result) {
	const sec = SectionCallerIAM

	callerARN, err := CallerIdentity(ctx, cfg)
	if err != nil {
		ch <- Result{Section: sec, Label: "Could not determine caller identity", Severity: SeverityError, Detail: err.Error()}
		return
	}
	ch <- Result{Section: sec, Label: "Caller identity", Severity: SeverityOK, Detail: callerARN}

	checks := []actionCheck{
		{"ssm:StartSession", SeverityError},
		{"ssm:TerminateSession", SeverityWarn},
		{"ssm:ResumeSession", SeverityWarn},
		{"ssm:GetConnectionStatus", SeverityWarn},
		{"ssm:DescribeSessions", SeverityWarn},
	}
	actions := make([]string, len(checks))
	for i, c := range checks {
		actions[i] = c.action
	}

	allowed, err := SimulatePolicy(ctx, cfg, callerARN, actions)
	if err != nil {
		ch <- Result{
			Section:  sec,
			Label:    "IAM simulation skipped",
			Severity: SeveritySkip,
			Detail:   "iam:SimulatePrincipalPolicy not permitted — verify permissions manually",
		}
		return
	}

	for _, c := range checks {
		if allowed[c.action] {
			ch <- Result{Section: sec, Label: c.action, Severity: SeverityOK}
		} else {
			ch <- Result{Section: sec, Label: c.action + " — denied", Severity: c.denySev}
		}
	}
}

func runInstanceRoleChecks(ctx context.Context, cfg aws.Config, profileARN string, ch chan<- Result) {
	const sec = SectionInstanceRole

	roleARN, err := InstanceProfileRoleARN(ctx, cfg, profileARN)
	if err != nil {
		ch <- Result{
			Section:  sec,
			Label:    "Could not retrieve instance role",
			Severity: SeveritySkip,
			Detail:   fmt.Sprintf("%s — verify AmazonSSMManagedInstanceCore is attached to %s", err.Error(), instanceProfileName(profileARN)),
		}
		return
	}

	actions := []string{
		"ssm:UpdateInstanceInformation",
		"ssmmessages:CreateControlChannel",
		"ssmmessages:CreateDataChannel",
		"ssmmessages:OpenControlChannel",
		"ssmmessages:OpenDataChannel",
	}

	allowed, err := SimulatePolicy(ctx, cfg, roleARN, actions)
	if err != nil {
		ch <- Result{
			Section:  sec,
			Label:    "IAM simulation skipped",
			Severity: SeveritySkip,
			Detail:   fmt.Sprintf("iam:SimulatePrincipalPolicy not permitted — verify %s has AmazonSSMManagedInstanceCore attached", instanceProfileName(profileARN)),
		}
		return
	}

	for _, action := range actions {
		if allowed[action] {
			ch <- Result{Section: sec, Label: action, Severity: SeverityOK}
		} else {
			ch <- Result{Section: sec, Label: action + " — denied", Severity: SeverityError}
		}
	}
}

func runNetworkChecks(ctx context.Context, cfg aws.Config, vpcID, region string, ch chan<- Result) {
	const sec = SectionNetwork

	services := []string{
		"com.amazonaws." + region + ".ssm",
		"com.amazonaws." + region + ".ssmmessages",
	}

	out, err := ec2.NewFromConfig(cfg).DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
			{Name: aws.String("service-name"), Values: services},
			{Name: aws.String("vpc-endpoint-state"), Values: []string{"available"}},
		},
	})
	if err != nil {
		ch <- Result{Section: sec, Label: "Could not check VPC endpoints", Severity: SeverityWarn, Detail: err.Error()}
		return
	}

	found := make(map[string]string) // service → endpoint ID
	for _, ep := range out.VpcEndpoints {
		found[aws.ToString(ep.ServiceName)] = aws.ToString(ep.VpcEndpointId)
	}

	for _, svc := range services {
		shortName := svc[strings.LastIndex(svc, ".")+1:]
		if id, ok := found[svc]; ok {
			ch <- Result{Section: sec, Label: "VPC endpoint for " + shortName, Severity: SeverityOK, Detail: id}
		} else {
			ch <- Result{
				Section:  sec,
				Label:    "VPC endpoint for " + shortName + " — not found",
				Severity: SeverityWarn,
				Detail:   "public SSM endpoint will be used; ok for public subnets",
			}
		}
	}
}
