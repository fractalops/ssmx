package aws

import (
	"context"
	"fmt"

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
