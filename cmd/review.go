package cmd

import (
	"strconv"

	"github.com/helmholtzcloud/gitgut/internal/review"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review <project_id> <change_id>",
	Short: "Run a code review and post/update the comment.",
	RunE: func(cmd *cobra.Command, args []string) error {
		isDryRun, err := cmd.Flags().GetBool("dry-run")
		if err != nil {
			return err
		}

		projectID, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}

		changeID, err := strconv.Atoi(args[1])
		if err != nil {
			return err
		}

		return review.RunReview(int64(projectID), int64(changeID), isDryRun)
	},
	Args: cobra.ExactArgs(2),
}

func init() {
	rootCmd.AddCommand(reviewCmd)

	reviewCmd.Flags().BoolP("dry-run", "d", false, "Dry-run the review and print output to the console.")
}
