package lambda

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Lambda functions",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint := getEndpoint(cmd)

			resp, err := http.Get(endpoint + "/2015-03-31/functions")
			if err != nil {
				return fmt.Errorf("failed to connect to OpenStack at %s: %w", endpoint, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			var result struct {
				Functions []struct {
					FunctionName string `json:"FunctionName"`
					Runtime      string `json:"Runtime"`
					Handler      string `json:"Handler"`
					MemorySize   int    `json:"MemorySize"`
					Timeout      int    `json:"Timeout"`
					State        string `json:"State"`
				} `json:"Functions"`
			}

			if err := json.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(result.Functions) == 0 {
				fmt.Println("No functions found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tRUNTIME\tHANDLER\tMEMORY\tTIMEOUT\tSTATE")
			for _, fn := range result.Functions {
				fmt.Fprintf(w, "%s\t%s\t%s\t%dMB\t%ds\t%s\n",
					fn.FunctionName, fn.Runtime, fn.Handler, fn.MemorySize, fn.Timeout, fn.State)
			}
			w.Flush()
			return nil
		},
	}
}
