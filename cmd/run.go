package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/config"
	"github.com/fractalops/ssmx/internal/workflow"
)

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

	engine := workflow.New(awsCfg, inst.InstanceID, region, profile)
	return engine.Run(ctx, wf, workflow.RunOptions{
		Inputs: params,
		DryRun: flagDryRun,
	})
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

	fmt.Fprintln(w, "\nsteps:")
	levels, err := workflow.Levels(wf.Steps)
	if err != nil {
		fmt.Fprintf(w, "warning: step ordering invalid (%v); fix the workflow before running\n", err)
		for stepName, step := range wf.Steps {
			fmt.Fprintf(w, "  %-24s  [%s]\n", stepName, step.Kind())
		}
		return
	}
	for _, level := range levels {
		for _, stepName := range level {
			step := wf.Steps[stepName]
			deps := ""
			if len(step.Needs) > 0 {
				deps = fmt.Sprintf("  (needs: %s)", strings.Join(step.Needs, ", "))
			}
			fmt.Fprintf(w, "  %-24s  [%s]%s\n", stepName, step.Kind(), deps)
		}
	}
}
