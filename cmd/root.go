package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

var (
	flagProfile     string
	flagRegion      string
	flagInteractive bool
	flagList        bool
	flagConfigure   bool
	flagTimeout     time.Duration
)

type rootAction int

const (
	actionHelp      rootAction = iota
	actionPicker               // -i flag
	actionConnect              // positional target, no remote cmd
	actionExec                 // positional target + -- cmd
	actionList                 // -l / --list
	actionConfigure            // --configure
)

type rootArgs struct {
	action    rootAction
	target    string
	remoteCmd []string
}

// parseRootArgs determines what action to take given root command invocation.
// dashAt is the index into args where -- appeared (-1 if absent), as returned
// by cobra's ArgsLenAtDash().
func parseRootArgs(interactive, list, configure bool, args []string, dashAt int) rootArgs {
	if configure {
		return rootArgs{action: actionConfigure}
	}
	if list {
		return rootArgs{action: actionList}
	}
	if interactive {
		return rootArgs{action: actionPicker}
	}
	if len(args) == 0 || dashAt == 0 {
		return rootArgs{action: actionHelp}
	}
	target := args[0]
	if dashAt > 0 {
		return rootArgs{action: actionExec, target: target, remoteCmd: args[dashAt:]}
	}
	return rootArgs{action: actionConnect, target: target}
}

var rootCmd = &cobra.Command{
	Use:   "ssmx [target] [-- command...]",
	Short: "The SSM CLI that AWS should have built",
	Long: `ssmx makes AWS Systems Manager usable: interactive instance picker, smart target resolution, diagnostics, and more.

  ssmx -i                  interactive instance picker
  ssmx web-prod            connect to an instance
  ssmx web-prod -- df -h   run a one-shot command
  ssmx -l                  list instances + SSM health
  ssmx --configure         open settings menu`,
	Args:               cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		parsed := parseRootArgs(flagInteractive, flagList, flagConfigure, args, cmd.ArgsLenAtDash())
		switch parsed.action {
		case actionHelp:
			return cmd.Help()
		case actionPicker:
			return runConnect(cmd, []string{})
		case actionConnect:
			return runConnect(cmd, []string{parsed.target})
		case actionExec:
			return runExec(cmd, parsed.target, parsed.remoteCmd)
		case actionList:
			return runList(cmd)
		case actionConfigure:
			return runConfigInteractive()
		}
		return nil
	},
}

func Execute(version, buildTime string) {
	rootCmd.Version = version
	if buildTime != "" {
		rootCmd.SetVersionTemplate("ssmx " + version + " (built " + buildTime + ")\n")
	}

	// Silence cobra's own error printing — we handle it below.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	if err := rootCmd.Execute(); err != nil {
		// Propagate the remote command's exit code directly so that
		// ssmx is transparent to scripts (echo $? reflects the remote exit).
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		// errOffline already printed a user-facing message; just exit non-zero.
		var offline *errOffline
		if errors.As(err, &offline) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagProfile, "profile", "p", "", "AWS profile to use")
	rootCmd.PersistentFlags().StringVarP(&flagRegion, "region", "r", "", "AWS region to use")
	rootCmd.Flags().BoolVarP(&flagInteractive, "interactive", "i", false, "open interactive instance picker")
	rootCmd.Flags().BoolVarP(&flagList, "list", "l", false, "list instances and SSM health")
	rootCmd.Flags().BoolVar(&flagConfigure, "configure", false, "open interactive settings menu")
	rootCmd.Flags().DurationVar(&flagTimeout, "timeout", 0, "timeout for one-shot exec (e.g. 30s, 2m); 0 means no timeout")
}
