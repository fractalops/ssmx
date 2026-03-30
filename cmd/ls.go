package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	awsclient "github.com/fractalops/ssmx/internal/aws"
	"github.com/fractalops/ssmx/internal/state"
	"github.com/fractalops/ssmx/internal/tui"
)

var (
	lsFlagTags      []string
	lsFlagUnhealthy bool
	lsFlagFormat    string
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List EC2 instances and their SSM health",
	RunE:  runLS,
}

func init() {
	lsCmd.Flags().StringArrayVar(&lsFlagTags, "tag", nil, "Filter by tag (e.g. --tag env=prod)")
	lsCmd.Flags().BoolVar(&lsFlagUnhealthy, "unhealthy", false, "Show only instances with SSM issues")
	lsCmd.Flags().StringVar(&lsFlagFormat, "format", "table", "Output format: table, json, tsv")
	rootCmd.AddCommand(lsCmd)
}

func runLS(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := awsclient.NewConfig(ctx, flagProfile, flagRegion)
	if err != nil {
		return err
	}
	region := cfg.Region

	db, err := state.Open()
	if err != nil {
		return err
	}
	defer db.Close()

	profile := flagProfile
	if profile == "" {
		profile = "default"
	}

	cached, err := state.GetCachedInstances(db, profile, region)
	var instances []awsclient.Instance

	if err == nil && len(cached) > 0 {
		for _, c := range cached {
			instances = append(instances, awsclient.Instance{
				InstanceID:   c.InstanceID,
				Name:         c.Name,
				State:        c.State,
				SSMStatus:    c.SSMStatus,
				PrivateIP:    c.PrivateIP,
				AgentVersion: c.AgentVersion,
			})
		}
	} else {
		instances, err = awsclient.ListInstances(ctx, cfg, lsFlagTags)
		if err != nil {
			return fmt.Errorf("listing instances: %w", err)
		}

		ssmInfo, err := awsclient.ListManagedInstances(ctx, cfg)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not fetch SSM info: %v\n", err)
		} else {
			awsclient.MergeSSMInfo(instances, ssmInfo)
		}

		var toCache []state.CachedInstance
		for _, inst := range instances {
			toCache = append(toCache, state.CachedInstance{
				InstanceID:   inst.InstanceID,
				Name:         inst.Name,
				State:        inst.State,
				SSMStatus:    inst.SSMStatus,
				PrivateIP:    inst.PrivateIP,
				AgentVersion: inst.AgentVersion,
				Region:       region,
				Profile:      profile,
			})
		}
		_ = state.UpsertInstances(db, toCache)
	}

	if lsFlagUnhealthy {
		var filtered []awsclient.Instance
		for _, inst := range instances {
			if inst.State == "running" && inst.SSMStatus != "online" {
				filtered = append(filtered, inst)
			}
		}
		instances = filtered
	}

	return printInstances(instances, lsFlagFormat)
}

func printInstances(instances []awsclient.Instance, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(instances)
	case "tsv":
		fmt.Println(strings.Join([]string{"NAME", "INSTANCE_ID", "STATE", "SSM_STATUS", "PRIVATE_IP", "AGENT_VERSION"}, "\t"))
		for _, inst := range instances {
			fmt.Println(strings.Join([]string{
				inst.Name, inst.InstanceID, inst.State, inst.SSMStatus, inst.PrivateIP, inst.AgentVersion,
			}, "\t"))
		}
		return nil
	default:
		fmt.Printf("%s\n", tui.StyleHeader.Render(fmt.Sprintf(
			"  %-30s %-21s %-9s %-8s %-15s %-12s",
			"NAME", "INSTANCE ID", "STATE", "SSM", "PRIVATE IP", "AGENT",
		)))
		for _, inst := range instances {
			name := inst.Name
			if name == "" {
				name = tui.StyleDim.Render("(no name)")
			}
			ssmGlyph := tui.SSMStatusGlyph(inst.SSMStatus)
			ssmStyled := tui.SSMStatusStyle(inst.SSMStatus).Render(ssmGlyph)

			fmt.Printf("  %-30s %-21s %-9s %-8s %-15s %-12s\n",
				truncateName(name, 30),
				inst.InstanceID,
				inst.State,
				ssmStyled,
				inst.PrivateIP,
				inst.AgentVersion,
			)
		}
		fmt.Printf("\n%s\n", tui.StyleDim.Render(fmt.Sprintf("  %d instances", len(instances))))
		return nil
	}
}

func truncateName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
