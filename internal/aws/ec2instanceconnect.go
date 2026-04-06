package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2instanceconnect"
)

// SendSSHPublicKey pushes pubKey to instanceID for osUser via EC2 Instance Connect.
// The key is valid for 60 seconds — long enough for the SSH handshake.
// az is the instance's availability zone (e.g. "us-east-1a").
func SendSSHPublicKey(ctx context.Context, cfg aws.Config, instanceID, az, osUser, pubKey string) error {
	client := ec2instanceconnect.NewFromConfig(cfg)
	_, err := client.SendSSHPublicKey(ctx, &ec2instanceconnect.SendSSHPublicKeyInput{
		InstanceId:       aws.String(instanceID),
		InstanceOSUser:   aws.String(osUser),
		SSHPublicKey:     aws.String(pubKey),
		AvailabilityZone: aws.String(az),
	})
	if err != nil {
		return fmt.Errorf("ec2-instance-connect: %w", err)
	}
	return nil
}
