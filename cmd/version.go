package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type VersionInfo struct {
	Name      string
	Version   string
	Revision  string
	Reference string
	BuiltAt   string
}

var VERSION_INFO = VersionInfo{
	Version:   "0.0.0-git+unknown",
	Revision:  "latest",
	Reference: "latest",
	BuiltAt:   "N/A",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version information of this build",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("GitGut Build Info\n\nVersion: %s\nRevision: %s\nReference: %s\nBuilt At: %s\n", VERSION_INFO.Version, VERSION_INFO.Revision, VERSION_INFO.Reference, VERSION_INFO.BuiltAt)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
