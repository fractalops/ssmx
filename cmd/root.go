package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/fractalops/ssmx/internal/session"
)

var (
	flagProfile     string
	flagRegion      string
	flagInteractive bool
	flagList        bool
	flagConfigure   bool
	flagProxy       bool
	flagForwards    []string
	flagPersist     bool
	flagTimeout     time.Duration
	flagHealth      bool
)

// Workflow flags
var (
	flagRun          string
	flagParams       []string
	flagWorkflows    bool
	flagWorkflowInfo string
	flagDryRun       bool
	flagConcurrency  int      // --concurrency N; 0 = unlimited
	flagTags         []string // --tag key=value; used by --list filtering and --run fleet targeting
)

type rootAction int

const (
	actionHelp      rootAction = iota
	actionPicker               // -i flag
	actionConnect              // positional target, no remote cmd
	actionExec                 // positional target + -- cmd
	actionList                 // -l / --list
	actionConfigure            // --configure
	actionSSHProxy             // --proxy (internal)
	actionForward              // one or more -L flags
	actionHealth               // --health flag
	actionRun                  // --run <workflow> with positional target
	actionRunMissingTarget     // --run set but no positional target given
	actionWorkflows            // --workflows: list available workflows
	actionWorkflowInfo         // --workflow-info <name>: show workflow schema
	actionRunFleet             // --run with --tag flag and no positional instance
)

type rootArgs struct {
	action    rootAction
	target    string
	remoteCmd []string
	user      string // for actionSSHProxy: SSH username from %r
}

// parseRootArgs determines what action to take given root command invocation.
// dashAt is the index into args where -- appeared (-1 if absent), as returned
// by cobra's ArgsLenAtDash().
func parseRootArgs(interactive, list, configure, proxy, hasForwards, health bool, args []string, dashAt int,
	run string, workflows bool, params []string, workflowInfo string, dryRun bool,
	tags []string, concurrency int,
) rootArgs {
	if workflows {
		return rootArgs{action: actionWorkflows}
	}
	if workflowInfo != "" {
		return rootArgs{action: actionWorkflowInfo, target: workflowInfo}
	}
	if run != "" {
		if len(args) == 0 {
			if len(tags) > 0 {
				return rootArgs{action: actionRunFleet}
			}
			return rootArgs{action: actionRunMissingTarget}
		}
		return rootArgs{action: actionRun, target: args[0]}
	}
	if proxy && len(args) >= 2 {
		return rootArgs{action: actionSSHProxy, target: args[0], user: args[1]}
	}
	if configure {
		return rootArgs{action: actionConfigure}
	}
	if list {
		return rootArgs{action: actionList}
	}
	if interactive {
		return rootArgs{action: actionPicker}
	}
	if hasForwards && len(args) > 0 {
		return rootArgs{action: actionForward, target: args[0]}
	}
	if len(args) == 0 || dashAt == 0 {
		return rootArgs{action: actionHelp}
	}
	target := args[0]
	if health && target != "" {
		return rootArgs{action: actionHealth, target: target}
	}
	if dashAt > 0 {
		return rootArgs{action: actionExec, target: target, remoteCmd: args[dashAt:]}
	}
	return rootArgs{action: actionConnect, target: target}
}

// parseForwards converts raw -L flag strings into ForwardSpec values.
func parseForwards(raw []string) ([]session.ForwardSpec, error) {
	forwards := make([]session.ForwardSpec, 0, len(raw))
	for _, s := range raw {
		fwd, err := parseForward(s)
		if err != nil {
			return nil, err
		}
		forwards = append(forwards, fwd)
	}
	return forwards, nil
}

var rootCmd = &cobra.Command{
	Use:   "ssmx [target] [-- command...]",
	Short: "The SSM CLI that AWS should have built",
	Long: `ssmx makes AWS Systems Manager usable: interactive instance picker, smart target resolution, diagnostics, and more.

  ssmx -i                            interactive instance picker
  ssmx web-prod                      connect to an instance
  ssmx web-prod -- df -h             run a one-shot command
  ssmx -l                            list instances + SSM health
  ssmx --configure                   open settings menu
  ssmx web-prod -L 5432:db:5432      port forward (no shell)
  ssmx web-prod -L 8080              forward local 8080 → instance:8080`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		parsed := parseRootArgs(
			flagInteractive, flagList, flagConfigure,
			flagProxy, len(flagForwards) > 0, flagHealth,
			args, cmd.ArgsLenAtDash(),
			flagRun, flagWorkflows, flagParams, flagWorkflowInfo, flagDryRun,
			flagTags, flagConcurrency,
		)
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
		case actionSSHProxy:
			return runProxy(parsed.target, parsed.user)
		case actionForward:
			forwards, err := parseForwards(flagForwards)
			if err != nil {
				return err
			}
			return runForward(cmd, parsed.target, forwards, flagPersist)
		case actionHealth:
			return runHealth(cmd, parsed.target)
		case actionRunFleet:
			return runWorkflowFleet(cmd)
		case actionRunMissingTarget:
			return runWorkflowFleet(cmd)
		case actionRun:
			return runWorkflow(cmd, parsed.target)
		case actionWorkflows:
			return runWorkflowList()
		case actionWorkflowInfo:
			return runWorkflowInfo(parsed.target)
		}
		return nil
	},
}

// Execute runs the root ssmx command.
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
	rootCmd.Flags().BoolVar(&flagProxy, "proxy", false, "")
	rootCmd.Flags().StringArrayVarP(&flagForwards, "forward", "L", nil, "port forward: localPort:remoteHost:remotePort or port (repeatable)")
	rootCmd.Flags().BoolVar(&flagPersist, "persist", false, "auto-reconnect port forwards on drop")
	rootCmd.Flags().DurationVar(&flagTimeout, "timeout", 0, "timeout for one-shot exec (e.g. 30s, 2m); 0 means no timeout")
	rootCmd.Flags().BoolVar(&flagHealth, "health", false, "run connectivity health checks for target")
	rootCmd.Flags().StringVar(&flagRun, "run", "", "workflow to run")
	rootCmd.Flags().StringArrayVar(&flagParams, "param", nil, "workflow input: key=value (repeatable)")
	rootCmd.Flags().BoolVar(&flagWorkflows, "workflows", false, "list available workflows")
	rootCmd.Flags().StringVar(&flagWorkflowInfo, "workflow-info", "", "show schema for a workflow")
	rootCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "print resolved steps without executing")
	rootCmd.Flags().IntVar(&flagConcurrency, "concurrency", 0, "max instances to run concurrently (0 = unlimited)")
	rootCmd.Flags().StringArrayVar(&flagTags, "tag", nil, "filter instances by tag key=value (repeatable); works with --list and --run")
	_ = rootCmd.Flags().MarkHidden("proxy")
}
