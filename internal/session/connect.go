package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// Connect starts an interactive SSM session on instanceID by exec-ing
// session-manager-plugin. Only returns if an error occurs before exec.
func Connect(ctx context.Context, cfg aws.Config, instanceID, region, profile string) error {
	// Call SSM to get a session token.
	output, err := awsclient.StartSession(ctx, cfg, instanceID)
	if err != nil {
		return fmt.Errorf("starting SSM session: %w", err)
	}

	// session-manager-plugin expects the StartSession response as JSON.
	responseJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshalling session response: %w", err)
	}

	// Build the parameters argument. The plugin expects Target as a plain
	// string, not an array.
	paramsJSON, err := json.Marshal(map[string]string{
		"Target": instanceID,
	})
	if err != nil {
		return fmt.Errorf("marshalling session params: %w", err)
	}

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)

	pluginPath, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found on PATH: %w", err)
	}

	// session-manager-plugin argv:
	//   <response-json> <region> StartSession <profile> <params-json> <endpoint>
	cmd := exec.CommandContext(ctx, pluginPath,
		string(responseJSON),
		region,
		"StartSession",
		profile,
		string(paramsJSON),
		endpoint,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Exec starts a non-interactive SSM session that runs command on instanceID,
// streaming output through session-manager-plugin. The context may carry a
// deadline (e.g. from --timeout); cancellation kills the plugin process.
func Exec(ctx context.Context, cfg aws.Config, instanceID, region, profile, command string) error {
	output, err := awsclient.StartInteractiveCommand(ctx, cfg, instanceID, command)
	if err != nil {
		return fmt.Errorf("starting SSM exec session: %w", err)
	}

	responseJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshalling session response: %w", err)
	}

	// Pass the full original request parameters as the plugin's 6th argument,
	// matching the AWS CLI convention (sessionmanager.py: json.dumps(parameters)).
	paramsJSON, err := json.Marshal(map[string]any{
		"Target":       instanceID,
		"DocumentName": "AWS-StartInteractiveCommand",
		"Parameters":   map[string][]string{"command": {command}},
	})
	if err != nil {
		return fmt.Errorf("marshalling session params: %w", err)
	}

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)

	pluginPath, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found on PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, pluginPath,
		string(responseJSON),
		region,
		"StartSession",
		profile,
		string(paramsJSON),
		endpoint,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
