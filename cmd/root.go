package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var rootCmd = &cobra.Command{
	Use:   "gforward",
	Short: "A forward proxy in Go",
	Long:  `A forward proxy which encompasses the access, forwarding and back-to-source of traffic`,
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func Execute() {
	rootCmd.AddCommand(clientCmd, serverCmd)
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
}
