package common

import (
	"testing"

	"github.com/spf13/cobra"
)

// endpointFor runs a subcommand of a root that defines --host and --port the
// way Tarn's root command does, and returns the endpoint it resolves.
func endpointFor(t *testing.T, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: "tarn"}
	root.PersistentFlags().String("host", "0.0.0.0", "")
	root.PersistentFlags().Int("port", 4566, "")
	var got string
	root.AddCommand(&cobra.Command{
		Use: "sub",
		Run: func(cmd *cobra.Command, _ []string) { got = Endpoint(cmd) },
	})
	root.SetArgs(append([]string{"sub"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return got
}

func TestEndpointPrecedence(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{name: "defaults", want: "http://127.0.0.1:4566"},
		{name: "env port", env: map[string]string{"TARN_PORT": "4599"}, want: "http://127.0.0.1:4599"},
		{name: "env host", env: map[string]string{"TARN_HOST": "10.0.0.5"}, want: "http://10.0.0.5:4566"},
		{name: "flag beats env", env: map[string]string{"TARN_PORT": "4599"}, args: []string{"--port", "4600"}, want: "http://127.0.0.1:4600"},
		{name: "flag host", args: []string{"--host", "192.168.1.2"}, want: "http://192.168.1.2:4566"},
		{
			name: "TARN_ENDPOINT wins",
			env:  map[string]string{"TARN_ENDPOINT": "http://tarn.test:9000", "TARN_PORT": "4599"},
			args: []string{"--port", "4600"},
			want: "http://tarn.test:9000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"TARN_ENDPOINT", "TARN_HOST", "TARN_PORT"} {
				t.Setenv(k, tc.env[k])
			}
			if got := endpointFor(t, tc.args...); got != tc.want {
				t.Errorf("endpoint = %q, want %q", got, tc.want)
			}
		})
	}
}
