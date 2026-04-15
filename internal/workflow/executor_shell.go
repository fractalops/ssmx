package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsclient "github.com/fractalops/ssmx/internal/aws"
)

// shellRunner abstracts SSM SendCommand + polling, enabling test injection
// without live AWS calls.
type shellRunner interface {
	sendShellCommand(ctx context.Context, instanceID string, commands []string, env map[string]string, timeoutSecs int32) (string, error)
	waitForShellCommand(ctx context.Context, instanceID, commandID string) (stdout, stderr string, exitCode int, err error)
}

// awsShellRunner is the production implementation backed by the AWS SDK.
type awsShellRunner struct {
	cfg aws.Config
}

func (r *awsShellRunner) sendShellCommand(ctx context.Context, instanceID string, commands []string, env map[string]string, timeoutSecs int32) (string, error) {
	return awsclient.SendShellCommand(ctx, r.cfg, instanceID, commands, env, timeoutSecs)
}

func (r *awsShellRunner) waitForShellCommand(ctx context.Context, instanceID, commandID string) (string, string, int, error) {
	return awsclient.WaitForShellCommand(ctx, r.cfg, instanceID, commandID)
}

// runShellStep executes a shell step on instanceID and returns the result.
// It resolves all ${{ }} expressions in the script and env block, prepends
// "set -euo pipefail" for safety, then dispatches via SSM RunShellScript.
func runShellStep(ctx context.Context, runner shellRunner, instanceID string, step *Step, exprCtx ExprContext) (*StepResult, error) {
	// Resolve the shell script body.
	resolvedScript, err := Resolve(step.Shell, exprCtx)
	if err != nil {
		return nil, fmt.Errorf("resolving shell script: %w", err)
	}

	// Build commands: safety preamble + each line of the script.
	lines := strings.Split(strings.TrimSpace(resolvedScript), "\n")
	commands := append([]string{"set -euo pipefail"}, lines...)

	// Resolve env: workflow-level env is the base; step-level overrides.
	resolvedEnv := make(map[string]string, len(exprCtx.Env)+len(step.Env))
	for k, v := range exprCtx.Env {
		resolvedEnv[k] = v
	}
	for k, v := range step.Env {
		rv, err := Resolve(v, exprCtx)
		if err != nil {
			return nil, fmt.Errorf("resolving env %s: %w", k, err)
		}
		resolvedEnv[k] = rv
	}

	// Parse optional step-level timeout.
	var timeoutSecs int32
	if step.Timeout != "" {
		d, err := time.ParseDuration(step.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parsing timeout %q: %w", step.Timeout, err)
		}
		timeoutSecs = int32(d.Seconds())
	}

	cmdID, err := runner.sendShellCommand(ctx, instanceID, commands, resolvedEnv, timeoutSecs)
	if err != nil {
		return nil, fmt.Errorf("sending shell command: %w", err)
	}

	stdout, stderr, exitCode, err := runner.waitForShellCommand(ctx, instanceID, cmdID)
	if err != nil {
		return nil, fmt.Errorf("waiting for shell command: %w", err)
	}

	res := &StepResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Success:  exitCode == 0,
	}

	// Resolve outputs using the just-completed step's stdout and exit code.
	if len(step.Outputs) > 0 {
		outputCtx := exprCtx
		outputCtx.CurrentStdout = stdout
		outputCtx.CurrentExitCode = exitCode
		res.Outputs = make(map[string]string, len(step.Outputs))
		for name, expr := range step.Outputs {
			v, err := Resolve(expr, outputCtx)
			if err != nil {
				return nil, fmt.Errorf("resolving output %q: %w", name, err)
			}
			res.Outputs[name] = v
		}
	}

	return res, nil
}
