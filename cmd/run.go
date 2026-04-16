package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/workflow"
)

// runWorkflowFleet resolves a fleet of target instances and runs the workflow
// against all of them concurrently. Fleet sources, in priority order:
//  1. --tag flags (override workflow targets:)
//  2. workflow targets: block
//
// A positional instance arg always routes to runWorkflow instead (single-instance).
func runWorkflowFleet(cmd *cobra.Command) error {
	ctx := context.Background()
	if flagTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, flagTimeout)
		defer cancel()
	}

	awsCfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}
	region := awsCfg.Region
	profile := flagProfile
	if profile == "" {
		profile = defaultProfile
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	wf, err := workflow.Load(flagRun)
	if err != nil {
		return err
	}

	// Resolve effective tags: --tag wins over workflow targets: block.
	effectiveTags := flagTags
	if len(effectiveTags) == 0 && wf.Targets != nil {
		for k, v := range wf.Targets.Tags {
			effectiveTags = append(effectiveTags, k+"="+v)
		}
	}

	// Resolve effective instance IDs (only used when no tags are set).
	var effectiveInstanceIDs []string
	if len(effectiveTags) == 0 && wf.Targets != nil {
		effectiveInstanceIDs = wf.Targets.InstanceIDs
	}

	if len(effectiveTags) == 0 && len(effectiveInstanceIDs) == 0 {
		return fmt.Errorf("--run %q requires a target instance (e.g. ssmx web-prod --run %s), --tag flag, or workflow targets: block", wf.Name, wf.Name)
	}

	instances, err := resolveFleet(ctx, awsCfg, effectiveTags, effectiveInstanceIDs)
	if err != nil {
		return err
	}

	// Resolve concurrency: --concurrency wins over targets.max-concurrency.
	concurrency := flagConcurrency
	if concurrency == 0 && wf.Targets != nil {
		concurrency = wf.Targets.MaxConcurrency
	}

	params := make(map[string]string, len(flagParams))
	for _, p := range flagParams {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --param %q (expected key=value)", p)
		}
		params[parts[0]] = parts[1]
	}

	fe := workflow.NewFleetEngineWithConfig(awsCfg, instances, concurrency, region, profile, cfg.DocAliases)
	return fe.Run(ctx, wf, workflow.RunOptions{
		Inputs: params,
		DryRun: flagDryRun,
	})
}

// resolveFleet returns SSM-online instances matching the given tag filters or
// explicit instance IDs. Exactly one of tags or instanceIDs should be non-empty.
func resolveFleet(ctx context.Context, awsCfg aws.Config, tags []string, instanceIDs []string) ([]awsclient.Instance, error) {
	var (
		instances []awsclient.Instance
		err       error
	)
	if len(tags) > 0 {
		instances, err = awsclient.ListInstances(ctx, awsCfg, tags)
		if err != nil {
			return nil, fmt.Errorf("listing instances: %w", err)
		}
	} else {
		instances, err = awsclient.ListInstancesByIDs(ctx, awsCfg, instanceIDs)
		if err != nil {
			return nil, fmt.Errorf("listing instances by ID: %w", err)
		}
	}

	ssmInfo, err := awsclient.ListManagedInstances(ctx, awsCfg)
	if err != nil {
		return nil, fmt.Errorf("fetching SSM info: %w", err)
	}
	awsclient.MergeSSMInfo(instances, ssmInfo)

	var online []awsclient.Instance
	for _, inst := range instances {
		if inst.SSMStatus == "online" {
			online = append(online, inst)
		}
	}
	if len(online) == 0 {
		return nil, fmt.Errorf("no SSM-online instances matched the given filters")
	}
	return online, nil
}

func runWorkflow(cmd *cobra.Command, target string) error {
	ctx := context.Background()
	if flagTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, flagTimeout)
		defer cancel()
	}

	awsCfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}
	region := awsCfg.Region
	profile := flagProfile
	if profile == "" {
		profile = defaultProfile
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	inst, err := resolveTarget(ctx, cmd, awsCfg, cfg, target)
	if err != nil {
		return err
	}
	if inst == nil {
		return nil // user cancelled picker
	}

	wf, err := workflow.Load(flagRun)
	if err != nil {
		return err
	}

	params := make(map[string]string, len(flagParams))
	for _, p := range flagParams {
		parts := strings.SplitN(p, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --param %q (expected key=value)", p)
		}
		params[parts[0]] = parts[1]
	}

	engine := workflow.New(awsCfg, inst.InstanceID, region, profile, cfg.DocAliases)
	_, err = engine.Run(ctx, wf, workflow.RunOptions{
		Inputs: params,
		DryRun: flagDryRun,
	})
	return err
}

func runWorkflowList() error {
	names, err := workflow.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "No workflows found (.ssmx/workflows/ or ~/.ssmx/workflows/)")
		return nil
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}

func runWorkflowInfo(name string) error {
	wf, err := workflow.Load(name)
	if err != nil {
		return err
	}
	writeWorkflowInfo(os.Stdout, wf)
	return nil
}

func writeWorkflowInfo(w io.Writer, wf *workflow.Workflow) {
	fmt.Fprintf(w, "workflow: %s\n", wf.Name)
	if wf.Description != "" {
		fmt.Fprintf(w, "description: %s\n", wf.Description)
	}
	if wf.Version != "" {
		fmt.Fprintf(w, "version: %s\n", wf.Version)
	}

	if len(wf.Inputs) > 0 {
		fmt.Fprintln(w, "\ninputs:")
		inputNames := make([]string, 0, len(wf.Inputs))
		for n := range wf.Inputs {
			inputNames = append(inputNames, n)
		}
		sort.Strings(inputNames)
		for _, inputName := range inputNames {
			input := wf.Inputs[inputName]
			req := ""
			if input.Required {
				req = " (required)"
			}
			def := ""
			if input.Default != nil {
				def = fmt.Sprintf(" [default: %v]", input.Default)
			}
			fmt.Fprintf(w, "  %-20s  %s%s%s\n", inputName, input.Type, req, def)
		}
	}

	if len(wf.Secrets) > 0 {
		fmt.Fprintln(w, "\nsecrets:")
		for _, s := range wf.Secrets {
			fmt.Fprintf(w, "  %-20s  → %s\n", s.Name, s.SSM)
		}
	}

	if len(wf.Outputs) > 0 {
		fmt.Fprintln(w, "\noutputs:")
		outKeys := make([]string, 0, len(wf.Outputs))
		for k := range wf.Outputs {
			outKeys = append(outKeys, k)
		}
		sort.Strings(outKeys)
		for _, k := range outKeys {
			fmt.Fprintf(w, "  %-20s  %s\n", k, wf.Outputs[k])
		}
	}

	fmt.Fprintln(w, "\nsteps:")
	levels, err := workflow.Levels(wf.Steps)
	if err != nil {
		fmt.Fprintf(w, "warning: step ordering invalid (%v); fix the workflow before running\n", err)
		for stepName, step := range wf.Steps {
			fmt.Fprintf(w, "  %-24s  [%s]\n", stepName, step.Kind())
		}
		return
	}
	for li, level := range levels {
		if len(levels) > 1 {
			if len(level) == 1 {
				fmt.Fprintf(w, "  level %d:\n", li+1)
			} else {
				fmt.Fprintf(w, "  level %d (parallel — %d steps):\n", li+1, len(level))
			}
		}
		for _, stepName := range level {
			step := wf.Steps[stepName]
			var tags []string
			if step.Always {
				tags = append(tags, "always")
			}
			if step.Timeout != "" {
				tags = append(tags, "timeout:"+step.Timeout)
			}
			if step.If != "" {
				tags = append(tags, "if")
			}
			tagStr := ""
			if len(tags) > 0 {
				tagStr = " +" + strings.Join(tags, ",")
			}
			deps := ""
			if len(step.Needs) > 0 {
				deps = fmt.Sprintf("  (needs: %s)", strings.Join(step.Needs, ", "))
			}
			indent := "  "
			if len(levels) > 1 {
				indent = "    "
			}
			fmt.Fprintf(w, "%s%-24s  [%s%s]%s\n", indent, stepName, step.Kind(), tagStr, deps)
			if len(step.Outputs) > 0 {
				outKeys := make([]string, 0, len(step.Outputs))
				for k := range step.Outputs {
					outKeys = append(outKeys, k)
				}
				sort.Strings(outKeys)
				for _, k := range outKeys {
					fmt.Fprintf(w, "%s  outputs.%-16s  %s\n", indent, k, step.Outputs[k])
				}
			}
		}
	}
}
