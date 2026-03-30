package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagProfile string
	flagRegion  string
)

var rootCmd = &cobra.Command{
	Use:   "ssmx",
	Short: "The SSM CLI that AWS should have built",
	Long:  `ssmx makes AWS Systems Manager usable: interactive instance picker, smart target resolution, diagnostics, and more.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConnect(cmd, args)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagProfile, "profile", "p", "", "AWS profile to use")
	rootCmd.PersistentFlags().StringVarP(&flagRegion, "region", "r", "", "AWS region to use")
}
