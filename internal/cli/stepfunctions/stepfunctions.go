package stepfunctions

import (
	"github.com/spf13/cobra"
)

// NewStepFunctionsCmd creates the `tarn stepfunctions` command group.
func NewStepFunctionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "stepfunctions",
		Aliases: []string{"sfn"},
		Short:   "Manage Step Functions state machines and executions",
		Long:    "Create, list, describe, and delete Step Functions state machines, and start, inspect, and stop executions on your local Tarn instance.",
	}

	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newDescribeCmd())
	cmd.AddCommand(newDeleteCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newExecutionsCmd())
	cmd.AddCommand(newExecutionCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newStopCmd())

	return cmd
}
