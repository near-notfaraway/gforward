package cmd

import (
	"bytes"
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
)

func TestRootCommandWithoutSubcommandShowsHelp(t *testing.T) {
	PatchConvey("Running gforward without a subcommand should show help", t, func() {
		var output bytes.Buffer
		rootCmd.SetArgs([]string{})
		rootCmd.SetOut(&output)
		rootCmd.SetErr(&output)
		rootCmd.AddCommand(clientCmd, serverCmd)
		defer func() {
			rootCmd.SetArgs(nil)
			rootCmd.SetOut(nil)
			rootCmd.SetErr(nil)
			rootCmd.RemoveCommand(clientCmd, serverCmd)
		}()

		_, err := rootCmd.ExecuteC()

		So(err, ShouldBeNil)
		So(output.String(), ShouldContainSubstring, "Usage:")
		So(output.String(), ShouldContainSubstring, "client")
		So(output.String(), ShouldContainSubstring, "server")
	})
}
