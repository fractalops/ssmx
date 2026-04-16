package aws

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// SSMInfo holds the Systems Manager view of a managed instance.
type SSMInfo struct {
	InstanceID   string
	PingStatus   string
	AgentVersion string
	LastPingAt   string
	PlatformName string
}

// ListManagedInstances returns SSM's view of all managed instances.
func ListManagedInstances(ctx context.Context, cfg aws.Config) (map[string]SSMInfo, error) {
	client := ssm.NewFromConfig(cfg)

	result := make(map[string]SSMInfo)
	paginator := ssm.NewDescribeInstanceInformationPaginator(client, &ssm.DescribeInstanceInformationInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSM managed instances: %w", err)
		}
		for _, info := range page.InstanceInformationList {
			id := aws.ToString(info.InstanceId)
			result[id] = SSMInfo{
				InstanceID:   id,
				PingStatus:   string(info.PingStatus),
				AgentVersion: aws.ToString(info.AgentVersion),
				LastPingAt:   info.LastPingDateTime.String(),
				PlatformName: aws.ToString(info.PlatformName),
			}
		}
	}
	return result, nil
}

// MergeSSMInfo enriches a slice of Instance values with SSM ping status and
// agent version from the SSM info map.
func MergeSSMInfo(instances []Instance, ssmInfo map[string]SSMInfo) {
	for i, inst := range instances {
		if info, ok := ssmInfo[inst.InstanceID]; ok {
			switch ssmtypes.PingStatus(info.PingStatus) {
			case ssmtypes.PingStatusOnline:
				instances[i].SSMStatus = "online"
			case ssmtypes.PingStatusConnectionLost:
				instances[i].SSMStatus = "offline"
			default:
				instances[i].SSMStatus = "unknown"
			}
			instances[i].AgentVersion = info.AgentVersion
			instances[i].LastPingAt = info.LastPingAt
			instances[i].PlatformName = info.PlatformName
		}
	}
}

// StartSession starts an SSM session on the target instance and returns the
// raw response needed by session-manager-plugin.
func StartSession(ctx context.Context, cfg aws.Config, instanceID string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.StartSession(ctx, &ssm.StartSessionInput{
		Target: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("SSM StartSession for %s: %w", instanceID, err)
	}
	return out, nil
}

// StartInteractiveCommand starts an SSM session using AWS-StartInteractiveCommand,
// which runs command on the target instance and streams output back through the plugin.
func StartInteractiveCommand(ctx context.Context, cfg aws.Config, instanceID, command string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.StartSession(ctx, &ssm.StartSessionInput{
		Target:       aws.String(instanceID),
		DocumentName: aws.String("AWS-StartInteractiveCommand"),
		Parameters:   map[string][]string{"command": {command}},
	})
	if err != nil {
		return nil, fmt.Errorf("SSM StartInteractiveCommand for %s: %w", instanceID, err)
	}
	return out, nil
}

// StartSSHSession opens an SSM session using AWS-StartSSHSession, which
// bridges the SSM data channel to the instance's SSH port.
func StartSSHSession(ctx context.Context, cfg aws.Config, instanceID string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.StartSession(ctx, &ssm.StartSessionInput{
		Target:       aws.String(instanceID),
		DocumentName: aws.String("AWS-StartSSHSession"),
		Parameters:   map[string][]string{"portNumber": {"22"}},
	})
	if err != nil {
		return nil, fmt.Errorf("SSM StartSSHSession for %s: %w", instanceID, err)
	}
	return out, nil
}

// StartPortForwardingSession opens a native SSM port forward from
// localPort on the client to remotePort on the instance (localhost).
func StartPortForwardingSession(ctx context.Context, cfg aws.Config, instanceID, localPort, remotePort string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.StartSession(ctx, &ssm.StartSessionInput{
		Target:       aws.String(instanceID),
		DocumentName: aws.String("AWS-StartPortForwardingSession"),
		Parameters: map[string][]string{
			"portNumber":      {remotePort},
			"localPortNumber": {localPort},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("SSM StartPortForwardingSession for %s: %w", instanceID, err)
	}
	return out, nil
}

// StartPortForwardingSessionToRemoteHost opens a native SSM port forward
// from localPort on the client to remotePort on remoteHost (reachable from
// the instance).
func StartPortForwardingSessionToRemoteHost(ctx context.Context, cfg aws.Config, instanceID, localPort, remoteHost, remotePort string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.StartSession(ctx, &ssm.StartSessionInput{
		Target:       aws.String(instanceID),
		DocumentName: aws.String("AWS-StartPortForwardingSessionToRemoteHost"),
		Parameters: map[string][]string{
			"host":            {remoteHost},
			"portNumber":      {remotePort},
			"localPortNumber": {localPort},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("SSM StartPortForwardingSessionToRemoteHost for %s: %w", instanceID, err)
	}
	return out, nil
}

// SendShellCommand sends AWS-RunShellScript to instanceID and returns the
// SSM commandID. env entries are injected as shell exports prepended to
// commands. timeoutSecs of 0 uses SSM's default (3600s).
func SendShellCommand(ctx context.Context, cfg aws.Config, instanceID string, commands []string, env map[string]string, timeoutSecs int32) (string, error) {
	client := ssm.NewFromConfig(cfg)

	// Build sorted export lines for deterministic ordering.
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	all := make([]string, 0, len(env)+len(commands))
	for _, k := range keys {
		// Single-quote the value with shell escaping for embedded single quotes.
		escaped := strings.ReplaceAll(env[k], "'", `'\''`)
		all = append(all, "export "+k+"='"+escaped+"'")
	}
	all = append(all, commands...)

	input := &ssm.SendCommandInput{
		InstanceIds:  []string{instanceID},
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters:   map[string][]string{"commands": all},
	}
	if timeoutSecs > 0 {
		input.TimeoutSeconds = aws.Int32(timeoutSecs)
	}

	out, err := client.SendCommand(ctx, input)
	if err != nil {
		return "", fmt.Errorf("SSM SendCommand on %s: %w", instanceID, err)
	}
	return aws.ToString(out.Command.CommandId), nil
}

// SendDocCommand sends an arbitrary SSM document command to a single instance.
// params values are automatically wrapped in string arrays as required by the SSM API.
// timeoutSecs of 0 uses SSM's default (3600s).
// Returns the CommandId. Use WaitForShellCommand to poll for completion.
func SendDocCommand(ctx context.Context, cfg aws.Config, instanceID, docName string, params map[string]string, timeoutSecs int32) (string, error) {
	client := ssm.NewFromConfig(cfg)

	ssmParams := make(map[string][]string, len(params))
	for k, v := range params {
		ssmParams[k] = []string{v}
	}

	input := &ssm.SendCommandInput{
		InstanceIds:  []string{instanceID},
		DocumentName: aws.String(docName),
		Parameters:   ssmParams,
	}
	if timeoutSecs > 0 {
		input.TimeoutSeconds = aws.Int32(timeoutSecs)
	}

	out, err := client.SendCommand(ctx, input)
	if err != nil {
		return "", fmt.Errorf("SSM SendCommand (doc %s) on %s: %w", docName, instanceID, err)
	}
	return aws.ToString(out.Command.CommandId), nil
}

// WaitForShellCommand polls GetCommandInvocation until the command reaches
// a terminal state. Returns stdout, stderr, and exit code.
// On SSM-level errors (TimedOut, Cancelled) it returns a non-nil error.
// A non-zero exit code from the script itself is not an error — the caller
// decides whether to treat it as failure.
// If progress is non-nil, new stdout chunks are written to it incrementally
// as they arrive during polling.
func WaitForShellCommand(ctx context.Context, cfg aws.Config, instanceID, commandID string, progress io.Writer) (stdout, stderr string, exitCode int, err error) {
	client := ssm.NewFromConfig(cfg)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var prevRawLen int
	for first := true; ; first = false {
		if !first {
			select {
			case <-ctx.Done():
				return "", "", -1, ctx.Err() //nolint:wrapcheck // context sentinel — must not be double-wrapped
			case <-ticker.C:
			}
		}

		out, pollErr := client.GetCommandInvocation(ctx, &ssm.GetCommandInvocationInput{
			CommandId:  aws.String(commandID),
			InstanceId: aws.String(instanceID),
		})
		if pollErr != nil {
			var notFound *ssmtypes.InvocationDoesNotExist
			if errors.As(pollErr, &notFound) {
				continue // command registered but invocation record not yet visible
			}
			return "", "", -1, fmt.Errorf("GetCommandInvocation: %w", pollErr)
		}

		rawStdout := aws.ToString(out.StandardOutputContent)
		stderrStr := strings.TrimSpace(aws.ToString(out.StandardErrorContent))
		code := int(out.ResponseCode)

		// Stream new stdout to caller incrementally.
		if progress != nil && len(rawStdout) > prevRawLen {
			_, _ = io.WriteString(progress, rawStdout[prevRawLen:])
			prevRawLen = len(rawStdout)
		}

		switch out.Status {
		case ssmtypes.CommandInvocationStatusSuccess,
			ssmtypes.CommandInvocationStatusFailed:
			return strings.TrimSpace(rawStdout), stderrStr, code, nil
		case ssmtypes.CommandInvocationStatusTimedOut:
			return "", "", -1, fmt.Errorf("SSM command timed out on %s", instanceID)
		case ssmtypes.CommandInvocationStatusCancelled:
			return "", "", -1, fmt.Errorf("SSM command cancelled on %s", instanceID)
		// Pending, InProgress, Delayed, Cancelling: keep polling.
		case ssmtypes.CommandInvocationStatusCancelling:
			// transient — keep polling until Cancelled is returned
		}
	}
}
