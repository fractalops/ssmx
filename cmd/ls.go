package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

func init() {
	rootCmd.Flags().StringArrayVar(&lsFlagTags, "tag", nil, "filter by tag (e.g. --tag env=prod); requires --list")
	rootCmd.Flags().BoolVar(&lsFlagUnhealthy, "unhealthy", false, "show only SSM-unreachable instances; requires --list")
	rootCmd.Flags().StringVar(&lsFlagFormat, "format", "table", "output format: table, json, tsv; requires --list")
}

func runList(cmd *cobra.Command) error {
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
		col := func(s string, w int) string {
			return lipgloss.NewStyle().Width(w).Render(s)
		}
		header := "  " +
			col("NAME", 30) + " " +
			col("INSTANCE ID", 21) + " " +
			col("STATE", 9) + " " +
			col("SSM", 6) + " " +
			col("PRIVATE IP", 15) + " " +
			col("AGENT", 12)
		fmt.Println(tui.StyleHeader.Render(header))
		for _, inst := range instances {
			nameText := inst.Name
			if nameText == "" {
				nameText = tui.StyleDim.Render("(no name)")
			} else {
				nameText = truncateName(nameText, 30)
			}
			ssmCell := tui.SSMStatusStyle(inst.SSMStatus).Render(tui.SSMStatusGlyph(inst.SSMStatus))

			row := "  " +
				col(nameText, 30) + " " +
				col(inst.InstanceID, 21) + " " +
				col(inst.State, 9) + " " +
				col(ssmCell, 6) + " " +
				col(inst.PrivateIP, 15) + " " +
				col(inst.AgentVersion, 12)
			fmt.Println(row)
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
