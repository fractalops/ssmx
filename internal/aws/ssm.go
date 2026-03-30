package aws

import (
	"context"

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
}

// ListManagedInstances returns SSM's view of all managed instances.
func ListManagedInstances(ctx context.Context, cfg aws.Config) (map[string]SSMInfo, error) {
	client := ssm.NewFromConfig(cfg)

	result := make(map[string]SSMInfo)
	paginator := ssm.NewDescribeInstanceInformationPaginator(client, &ssm.DescribeInstanceInformationInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, info := range page.InstanceInformationList {
			id := aws.ToString(info.InstanceId)
			result[id] = SSMInfo{
				InstanceID:   id,
				PingStatus:   string(info.PingStatus),
				AgentVersion: aws.ToString(info.AgentVersion),
				LastPingAt:   info.LastPingDateTime.String(),
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
		}
	}
}

// StartSession starts an SSM session on the target instance and returns the
// raw response needed by session-manager-plugin.
func StartSession(ctx context.Context, cfg aws.Config, instanceID string) (*ssm.StartSessionOutput, error) {
	client := ssm.NewFromConfig(cfg)
	return client.StartSession(ctx, &ssm.StartSessionInput{
		Target: aws.String(instanceID),
	})
}
