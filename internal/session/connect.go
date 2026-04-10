package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
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

// ForwardSpec describes a single port-forwarding rule.
type ForwardSpec struct {
	LocalPort  string
	RemoteHost string
	RemotePort string
}

// IsLocal reports whether the forward targets the instance itself
// (remoteHost is localhost or 127.0.0.1), selecting AWS-StartPortForwardingSession
// instead of AWS-StartPortForwardingSessionToRemoteHost.
func (f ForwardSpec) IsLocal() bool {
	return f.RemoteHost == "localhost" || f.RemoteHost == "127.0.0.1"
}

// Forward starts a native SSM port-forwarding session via session-manager-plugin.
// It blocks until the plugin exits. The caller is responsible for running multiple
// forwards concurrently when needed.
func Forward(ctx context.Context, cfg aws.Config, instanceID, region, profile string, fwd ForwardSpec) error {
	var output *ssm.StartSessionOutput
	var err error

	// AWS uses two separate SSM documents depending on whether the final destination
	// is the instance itself (AWS-StartPortForwardingSession) or a host reachable
	// through the instance (AWS-StartPortForwardingSessionToRemoteHost). The latter
	// adds a "host" parameter that the plugin forwards traffic to via the instance.
	if fwd.IsLocal() {
		output, err = awsclient.StartPortForwardingSession(ctx, cfg, instanceID, fwd.LocalPort, fwd.RemotePort)
	} else {
		output, err = awsclient.StartPortForwardingSessionToRemoteHost(ctx, cfg, instanceID, fwd.LocalPort, fwd.RemoteHost, fwd.RemotePort)
	}
	if err != nil {
		return fmt.Errorf("starting port forward session: %w", err)
	}

	responseJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshalling session response: %w", err)
	}

	var docName string
	var params map[string]any

	// The plugin's 5th argv is the original StartSession request reproduced as JSON.
	// Parameters must match the document schema exactly — field names differ between
	// the two documents ("portNumber" vs "portNumber"+"host").
	if fwd.IsLocal() {
		docName = "AWS-StartPortForwardingSession"
		params = map[string]any{
			"Target":       instanceID,
			"DocumentName": docName,
			"Parameters": map[string][]string{
				"portNumber":      {fwd.RemotePort},
				"localPortNumber": {fwd.LocalPort},
			},
		}
	} else {
		docName = "AWS-StartPortForwardingSessionToRemoteHost"
		params = map[string]any{
			"Target":       instanceID,
			"DocumentName": docName,
			"Parameters": map[string][]string{
				"host":            {fwd.RemoteHost},
				"portNumber":      {fwd.RemotePort},
				"localPortNumber": {fwd.LocalPort},
			},
		}
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshalling session params: %w", err)
	}

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)
	pluginPath, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found: %w", err)
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

// SSHProxy starts an AWS-StartSSHSession over session-manager-plugin, piping
// stdin/stdout as the SSH transport. Used as the backend for the --proxy
// ProxyCommand so standard SSH tools (scp, rsync, ssh) work over SSM.
func SSHProxy(ctx context.Context, cfg aws.Config, instanceID, region, profile string) error {
	output, err := awsclient.StartSSHSession(ctx, cfg, instanceID)
	if err != nil {
		return fmt.Errorf("starting SSH session: %w", err)
	}

	responseJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshalling session response: %w", err)
	}

	paramsJSON, err := json.Marshal(map[string]any{
		"Target":       instanceID,
		"DocumentName": "AWS-StartSSHSession",
		"Parameters":   map[string][]string{"portNumber": {"22"}},
	})
	if err != nil {
		return fmt.Errorf("marshalling session params: %w", err)
	}

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)
	pluginPath, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found: %w", err)
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
