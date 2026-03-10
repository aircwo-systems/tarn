package cli

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestMatchesTagSelector(t *testing.T) {
	tags := map[string]string{
		"feature":   "r10",
		"component": "gateway",
	}

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "empty matches all", query: "", want: true},
		{name: "bare value", query: "r10", want: true},
		{name: "pair with equals", query: "feature=r10", want: true},
		{name: "pair with colon", query: "feature:r10", want: true},
		{name: "component value", query: "gateway", want: true},
		{name: "missing value", query: "r11", want: false},
		{name: "missing key", query: "team=payments", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesTagSelector(tags, tt.query)
			if got != tt.want {
				t.Fatalf("matchesTagSelector(%v, %q) = %v, want %v", tags, tt.query, got, tt.want)
			}
		})
	}
}

func TestRunFlushDeletesOnlyMatchingTaggedResources(t *testing.T) {
	const endpoint = "http://openstack.test"
	var deleted []string

	prevClient := cliHTTPClient
	cliHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodGet && r.URL.String() == endpoint+"/_openstack/admin/overview":
				return jsonResponse(http.StatusOK, `{
						"config":{"accountId":"000000000000"},
						"gateways":[
							{"apiId":"gw-r10","name":"r10-api","tags":{"feature":"r10"},"version":"v2"},
							{"apiId":"gw-r10-rest","name":"r10-rest-api","tags":{"feature":"r10"},"version":"v1"},
						{"apiId":"gw-r9","name":"r9-api","tags":{"feature":"r9"},"version":"v2"}
					],
					"functions":[
						{"name":"r10-fn","tags":{"feature":"r10"}},
						{"name":"other-fn","tags":{"feature":"r9"}}
					],
					"queues":[
						{"name":"r10-queue","url":"`+endpoint+`/000000000000/r10-queue","tags":{"feature":"r10"}},
						{"name":"other-queue","url":"`+endpoint+`/000000000000/other-queue","tags":{"feature":"r9"}}
					],
						"secrets":[
							{"name":"r10-secret","tags":{"feature":"r10"}},
							{"name":"other-secret","tags":{"feature":"r9"}}
						],
						"eventSourceMappings":[
							{"uuid":"esm-r10","queueName":"r10-queue","functionName":"arn:aws:lambda:us-east-1:000000000000:function:r10-fn"},
							{"uuid":"esm-r9","queueName":"other-queue","functionName":"other-fn"}
						]
					}`)
			case r.Method == http.MethodDelete && r.URL.String() == endpoint+"/2015-03-31/event-source-mappings/esm-r10":
				deleted = append(deleted, "mapping:esm-r10")
				return jsonResponse(http.StatusNoContent, "")
			case r.Method == http.MethodDelete && r.URL.String() == endpoint+"/v2/apis/gw-r10":
				deleted = append(deleted, "gateway:gw-r10")
				return jsonResponse(http.StatusNoContent, "")
			case r.Method == http.MethodDelete && r.URL.String() == endpoint+"/restapis/gw-r10-rest":
				deleted = append(deleted, "gateway:gw-r10-rest")
				return jsonResponse(http.StatusNoContent, "")
			case r.Method == http.MethodDelete && r.URL.String() == endpoint+"/2015-03-31/functions/r10-fn":
				deleted = append(deleted, "function:r10-fn")
				return jsonResponse(http.StatusNoContent, "")
			case r.Method == http.MethodPost && r.URL.String() == endpoint+"/000000000000/r10-queue":
				deleted = append(deleted, "queue:r10-queue")
				return jsonResponse(http.StatusOK, `<DeleteQueueResponse/>`)
			case r.Method == http.MethodPost && r.URL.String() == endpoint+"/":
				if r.Header.Get("X-Amz-Target") == "secretsmanager.DeleteSecret" {
					deleted = append(deleted, "secret:r10-secret")
					return jsonResponse(http.StatusOK, `{"Name":"r10-secret","ARN":"arn:aws:secretsmanager:us-east-1:000000000000:secret:r10-secret"}`)
				}
			}
			return jsonResponse(http.StatusNotFound, `{"Message":"not found"}`)
		}),
	}
	defer func() { cliHTTPClient = prevClient }()

	cmd := &cobra.Command{Use: "openstack"}
	t.Setenv("OPENSTACK_ENDPOINT", endpoint)

	var out bytes.Buffer
	err := runFlush(cmd, &out, flushOptions{TagFilter: "feature=r10"})
	if err != nil {
		t.Fatalf("runFlush returned error: %v", err)
	}

	gotDeleted := strings.Join(deleted, ",")
	for _, want := range []string{
		"gateway:gw-r10",
		"gateway:gw-r10-rest",
		"mapping:esm-r10",
		"queue:r10-queue",
		"secret:r10-secret",
		"function:r10-fn",
	} {
		if !strings.Contains(gotDeleted, want) {
			t.Fatalf("expected deletion %q in %q", want, gotDeleted)
		}
	}
	for _, unwanted := range []string{
		"gw-r9",
		"esm-r9",
		"other-queue",
		"other-secret",
		"other-fn",
	} {
		if strings.Contains(gotDeleted, unwanted) {
			t.Fatalf("unexpected deletion %q in %q", unwanted, gotDeleted)
		}
	}
}

func jsonResponse(status int, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}
