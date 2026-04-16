package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// Instance is a normalised view of an EC2 instance used throughout ssmx.
type Instance struct {
	InstanceID       string
	Name             string
	State            string
	PrivateIP        string
	PublicIP         string
	Platform         string
	SubnetID         string
	VPCID            string
	IAMProfileARN    string
	SSMStatus        string
	AgentVersion     string
	LastPingAt       string
	PlatformName     string // e.g. "Amazon Linux", "Ubuntu" — from SSM DescribeInstanceInformation
	AvailabilityZone string // e.g. "us-east-1a" — from EC2 Placement.AvailabilityZone
}

// ListInstances returns all EC2 instances visible to the caller, optionally
// filtered by tag key=value pairs (e.g. ["env=prod", "role=web"]).
func ListInstances(ctx context.Context, cfg aws.Config, tagFilters []string) ([]Instance, error) {
	client := ec2.NewFromConfig(cfg)

	input := &ec2.DescribeInstancesInput{}
	if len(tagFilters) > 0 {
		input.Filters = buildTagFilters(tagFilters)
	}

	var instances []Instance
	paginator := ec2.NewDescribeInstancesPaginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing EC2 instances: %w", err)
		}
		for _, reservation := range page.Reservations {
			for _, i := range reservation.Instances {
				inst := Instance{
					InstanceID: aws.ToString(i.InstanceId),
					State:      string(i.State.Name),
					PrivateIP:  aws.ToString(i.PrivateIpAddress),
					PublicIP:   aws.ToString(i.PublicIpAddress),
					SubnetID:   aws.ToString(i.SubnetId),
					VPCID:      aws.ToString(i.VpcId),
					SSMStatus:  "unknown",
				}
				if i.Platform != "" {
					inst.Platform = string(i.Platform)
				}
				if i.IamInstanceProfile != nil {
					inst.IAMProfileARN = aws.ToString(i.IamInstanceProfile.Arn)
				}
				inst.Name = tagValue(i.Tags, "Name")
				if i.Placement != nil {
					inst.AvailabilityZone = aws.ToString(i.Placement.AvailabilityZone)
				}
				instances = append(instances, inst)
			}
		}
	}
	return instances, nil
}

// ListInstancesByIDs returns EC2 instances for the given instance IDs.
// Returns the same Instance shape as ListInstances; SSMStatus defaults to "unknown".
// Returns nil, nil when ids is empty.
func ListInstancesByIDs(ctx context.Context, cfg aws.Config, ids []string) ([]Instance, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	client := ec2.NewFromConfig(cfg)
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("describing instances by ID: %w", err)
	}
	var instances []Instance
	for _, reservation := range out.Reservations {
		for _, i := range reservation.Instances {
			inst := Instance{
				InstanceID: aws.ToString(i.InstanceId),
				State:      string(i.State.Name),
				PrivateIP:  aws.ToString(i.PrivateIpAddress),
				PublicIP:   aws.ToString(i.PublicIpAddress),
				SubnetID:   aws.ToString(i.SubnetId),
				VPCID:      aws.ToString(i.VpcId),
				SSMStatus:  "unknown",
			}
			if i.Platform != "" {
				inst.Platform = string(i.Platform)
			}
			if i.IamInstanceProfile != nil {
				inst.IAMProfileARN = aws.ToString(i.IamInstanceProfile.Arn)
			}
			inst.Name = tagValue(i.Tags, "Name")
			if i.Placement != nil {
				inst.AvailabilityZone = aws.ToString(i.Placement.AvailabilityZone)
			}
			instances = append(instances, inst)
		}
	}
	return instances, nil
}

func tagValue(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func buildTagFilters(tagFilters []string) []ec2types.Filter {
	filters := make([]ec2types.Filter, 0, len(tagFilters))
	for _, tf := range tagFilters {
		parts := strings.SplitN(tf, "=", 2)
		if len(parts) != 2 {
			continue
		}
		filters = append(filters, ec2types.Filter{
			Name:   aws.String("tag:" + parts[0]),
			Values: []string{parts[1]},
		})
	}
	return filters
}
