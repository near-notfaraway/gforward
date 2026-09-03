package cmd

import (
	"bytes"
	"errors"
	"os"
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"
)

func removeRootSubcommands() {
	for _, child := range rootCmd.Commands() {
		if child == clientCmd || child == serverCmd {
			rootCmd.RemoveCommand(child)
		}
	}
}

func TestRootCommand(t *testing.T) {
	PatchConvey("Test rootCmd", t, func() {
		PatchConvey("Running without a subcommand should show help", func() {
			var output bytes.Buffer
			removeRootSubcommands()
			rootCmd.SetArgs([]string{})
			rootCmd.SetOut(&output)
			rootCmd.SetErr(&output)
			rootCmd.AddCommand(clientCmd, serverCmd)
			defer func() {
				rootCmd.SetArgs(nil)
				rootCmd.SetOut(nil)
				rootCmd.SetErr(nil)
				removeRootSubcommands()
			}()

			_, err := rootCmd.ExecuteC()

			So(err, ShouldBeNil)
			So(output.String(), ShouldContainSubstring, "Usage:")
			So(output.String(), ShouldContainSubstring, "client")
			So(output.String(), ShouldContainSubstring, "server")
		})
	})
}

func TestExecute(t *testing.T) {
	PatchConvey("Test Execute", t, func() {
		removeRootSubcommands()
		defer removeRootSubcommands()

		PatchConvey("Successful command execution should not exit", func() {
			var exitCalled bool
			Mock((*cobra.Command).Execute).Return(nil).Build()
			Mock(os.Exit).To(func(_ int) {
				exitCalled = true
			}).Build()

			Execute()

			So(exitCalled, ShouldBeFalse)
			So(rootCmd.Commands(), ShouldContain, clientCmd)
			So(rootCmd.Commands(), ShouldContain, serverCmd)
		})

		PatchConvey("Command errors should be written and exit with code 1", func() {
			commandErr := errors.New("execute failed")
			var exitCode int
			var stderr bytes.Buffer
			rootCmd.SetErr(&stderr)
			defer rootCmd.SetErr(nil)
			Mock((*cobra.Command).Execute).Return(commandErr).Build()
			Mock(os.Exit).To(func(code int) {
				exitCode = code
			}).Build()

			Execute()

			So(exitCode, ShouldEqual, 1)
		})
	})
}
