package cmd

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed NOTICE
var licenseNotice string

//go:embed LICENSE
var license string

var licensesCmd = &cobra.Command{
	Use:   "licenses",
	Short: "Display license information of this project.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("This software's license:\n%s\n\n3rd Party Licenses:\n%s\n", license, licenseNotice)
	},
}

func init() {
	rootCmd.AddCommand(licensesCmd)
}
