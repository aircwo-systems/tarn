package mcp

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DeployInput describes a function to create or replace.
type DeployInput struct {
	Name    string `json:"name" jsonschema:"Function name."`
	Runtime string `json:"runtime,omitempty" jsonschema:"Lambda runtime identifier, for example nodejs20.x or python3.12. Defaults to nodejs20.x."`
	Handler string `json:"handler,omitempty" jsonschema:"Entry point, for example index.handler. Defaults to index.handler."`

	Files   map[string]string `json:"files,omitempty" jsonschema:"Source files to package, keyed by path within the zip, for example {\"index.js\": \"exports.handler = ...\"}. Use this to deploy code you just wrote without creating a zip on disk. Either files or zipPath is required."`
	ZipPath string            `json:"zipPath,omitempty" jsonschema:"Absolute path to an existing deployment zip. Either files or zipPath is required."`

	Environment map[string]string `json:"environment,omitempty" jsonschema:"Environment variables for the function."`
	TimeoutSec  int               `json:"timeoutSec,omitempty" jsonschema:"Execution timeout in seconds. Defaults to 30."`
	MemoryMB    int               `json:"memoryMB,omitempty" jsonschema:"Memory in megabytes. Defaults to 512."`
	Account     string            `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// DeployOutput reports the created function.
type DeployOutput struct {
	Name     string `json:"name"`
	ARN      string `json:"arn,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	State    string `json:"state,omitempty" jsonschema:"Only Active functions can be invoked."`
	CodeSize int64  `json:"codeSize,omitempty"`
	Replaced bool   `json:"replaced" jsonschema:"Whether an existing function of the same name was replaced."`
	LogGroup string `json:"logGroup,omitempty" jsonschema:"Pass this to tarn_get_logs."`
}

const deployDescription = `Create or replace a Lambda function on the local Tarn instance.

This deploys to the emulator on this machine, not to AWS. Nothing is uploaded
anywhere and nothing costs money.

Provide the code either as "files", a map of path to source text that is zipped
in memory, or as "zipPath", an absolute path to an existing deployment package.
Prefer "files" when deploying code you just wrote.

Deploying over an existing function of the same name replaces its code.

After deploying, invoke it with tarn_invoke_lambda. If the function fails, the
invoke result carries the error and stack directly.`

func addDeployTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_deploy_lambda",
		Description: deployDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Deploy Lambda function",
			DestructiveHint: boolPtr(true),
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in DeployInput) (
		*mcp.CallToolResult, DeployOutput, error,
	) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, DeployOutput{}, errors.New("name is required")
		}

		code, err := deploymentPackage(in)
		if err != nil {
			return nil, DeployOutput{}, err
		}

		// Replacing means the function already existed; report that back so a
		// model does not silently clobber something it did not create.
		var existing struct {
			Configuration struct {
				FunctionName string `json:"FunctionName"`
			} `json:"Configuration"`
		}
		replaced := c.get(ctx, "/2015-03-31/functions/"+in.Name, in.Account, nil, &existing) == nil

		req := map[string]any{
			"FunctionName": in.Name,
			"Runtime":      defaultString(in.Runtime, "nodejs20.x"),
			"Handler":      defaultString(in.Handler, "index.handler"),
			"Role":         "arn:aws:iam::000000000000:role/tarn-mcp",
			"Timeout":      defaultInt(in.TimeoutSec, 30),
			"MemorySize":   defaultInt(in.MemoryMB, 512),
			"Code":         map[string]string{"ZipFile": base64.StdEncoding.EncodeToString(code)},
		}
		if len(in.Environment) > 0 {
			req["Environment"] = map[string]any{"Variables": in.Environment}
		}

		var created struct {
			FunctionName string `json:"FunctionName"`
			FunctionArn  string `json:"FunctionArn"`
			Runtime      string `json:"Runtime"`
			State        string `json:"State"`
			CodeSize     int64  `json:"CodeSize"`
		}
		if err := c.postJSON(ctx, "/2015-03-31/functions", in.Account, req, &created); err != nil {
			return nil, DeployOutput{}, err
		}

		return nil, DeployOutput{
			Name:     created.FunctionName,
			ARN:      created.FunctionArn,
			Runtime:  created.Runtime,
			State:    created.State,
			CodeSize: created.CodeSize,
			Replaced: replaced,
			LogGroup: logGroupFor(created.FunctionName),
		}, nil
	}

	mcp.AddTool(s, tool, handler)
}

// deploymentPackage resolves the input's code into zip bytes.
func deploymentPackage(in DeployInput) ([]byte, error) {
	switch {
	case len(in.Files) > 0 && in.ZipPath != "":
		return nil, errors.New("provide either files or zipPath, not both")

	case len(in.Files) > 0:
		return zipFiles(in.Files)

	case in.ZipPath != "":
		code, err := os.ReadFile(in.ZipPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read zipPath: %w", err)
		}
		return code, nil

	default:
		return nil, errors.New("either files or zipPath is required")
	}
}

// zipFiles packages source text into a deployment zip in memory. Entries are
// written in sorted order so the same input always produces the same archive.
func zipFiles(files map[string]string) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// InvokeInput describes one invocation.
type InvokeInput struct {
	Name    string `json:"name" jsonschema:"Function name."`
	Payload string `json:"payload,omitempty" jsonschema:"Event payload as a JSON string. Defaults to an empty object."`
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// InvokeOutput fuses everything needed to judge one invocation into a single
// result: whether it failed, the error and stack when it did, the response when
// it did not, and where to look next.
type InvokeOutput struct {
	Succeeded bool `json:"succeeded" jsonschema:"False when the handler threw or returned an error envelope. A failed invocation still answers HTTP 200, so this flag is the signal to read."`

	Response string `json:"response,omitempty" jsonschema:"The handler's return value, present when succeeded is true."`

	ErrorType    string   `json:"errorType,omitempty"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
	Stack        []string `json:"stack,omitempty" jsonschema:"Stack frames from the runtime, usually including the failing file and line."`

	RequestID string `json:"requestId,omitempty"`
	LogGroup  string `json:"logGroup,omitempty" jsonschema:"Pass this to tarn_get_logs to read what the invocation printed."`
	DurationM int64  `json:"durationMs,omitempty"`
}

const invokeDescription = `Invoke a Lambda function on the local Tarn instance and report what happened.

This runs the function in a container on this machine. It does not call AWS.

The result fuses the outcome into one response: a "succeeded" flag, the return
value on success, and the error type, message, and stack frames on failure. A
failed invocation still answers HTTP 200, so read "succeeded" rather than
assuming a response means success.

For an unhandled exception the stack usually names the failing file and line,
which is often enough to diagnose the fault without reading logs. Read logs with
tarn_get_logs when the handler caught its own error, returned wrong data, or
printed something you need to see.`

func addInvokeTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_invoke_lambda",
		Description: invokeDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Invoke Lambda function",
			DestructiveHint: boolPtr(true),
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in InvokeInput) (
		*mcp.CallToolResult, InvokeOutput, error,
	) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, InvokeOutput{}, errors.New("name is required")
		}

		payload := strings.TrimSpace(in.Payload)
		if payload == "" {
			payload = "{}"
		}

		resp, err := c.do(ctx, http.MethodPost,
			"/2015-03-31/functions/"+in.Name+"/invocations",
			in.Account, "application/json", strings.NewReader(payload))
		if err != nil {
			return nil, InvokeOutput{}, err
		}
		defer func() { _ = resp.Body.Close() }()

		body := readAllBounded(resp.Body)
		if resp.StatusCode >= 300 {
			return nil, InvokeOutput{}, fmt.Errorf("invoke returned HTTP %d: %s",
				resp.StatusCode, truncate(string(body), 400))
		}

		out := InvokeOutput{
			Succeeded: true,
			RequestID: resp.Header.Get("x-amzn-RequestId"),
			LogGroup:  logGroupFor(in.Name),
		}

		// The RIE header is the documented failure signal, but not every build
		// sends it, so fall back to the error envelope in the body.
		if envelope, failed := functionError(body); failed || resp.Header.Get("X-Amz-Function-Error") != "" {
			out.Succeeded = false
			out.ErrorType = envelope.ErrorType
			out.ErrorMessage = envelope.ErrorMessage
			out.Stack = envelope.Trace
			return nil, out, nil
		}

		out.Response = string(body)
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}

// errorEnvelope is how a Lambda runtime serializes an unhandled failure.
type errorEnvelope struct {
	ErrorType    string   `json:"errorType"`
	ErrorMessage string   `json:"errorMessage"`
	Trace        []string `json:"trace"`
}

// functionError reports whether an invoke body is an error envelope rather
// than a handler return value.
func functionError(body []byte) (errorEnvelope, bool) {
	var envelope errorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return errorEnvelope{}, false
	}
	return envelope, envelope.ErrorType != "" || envelope.ErrorMessage != ""
}

// logGroupFor returns the log group Tarn writes a function's output to.
func logGroupFor(name string) string {
	if name == "" {
		return ""
	}
	return "/aws/lambda/" + name
}
