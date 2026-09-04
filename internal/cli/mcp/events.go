package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FireRuleInput selects a rule to trigger.
type FireRuleInput struct {
	Rule    string `json:"rule" jsonschema:"EventBridge rule name."`
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// FireRuleOutput reports what the rule dispatched.
type FireRuleOutput struct {
	Rule       string `json:"rule"`
	Targets    int    `json:"targets" jsonschema:"Targets the rule dispatched to."`
	Successful int    `json:"successful"`
	Failed     int    `json:"failed" jsonschema:"Targets that returned an error. Read the target function's logs to find out why."`
	FiredAt    string `json:"firedAt,omitempty"`
}

const fireRuleDescription = `Trigger a scheduled EventBridge rule immediately on the local Tarn instance.

Scheduled rules normally wait for their next interval, so this is how to test
one without waiting. The rule fires once, against its real targets.

The result counts targets that succeeded and failed but does not say why a
target failed. Read the target function's output with tarn_get_logs for that.`

func addFireRuleTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_fire_rule",
		Description: fireRuleDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Fire EventBridge rule",
			DestructiveHint: boolPtr(true),
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in FireRuleInput) (
		*mcp.CallToolResult, FireRuleOutput, error,
	) {
		if strings.TrimSpace(in.Rule) == "" {
			return nil, FireRuleOutput{}, errors.New("rule is required")
		}

		var result struct {
			RuleName   string `json:"ruleName"`
			FiredAt    string `json:"firedAt"`
			Targets    int    `json:"targets"`
			Successful int    `json:"successful"`
			Failed     int    `json:"failed"`
		}
		if err := c.postJSON(ctx, "/_tarn/admin/eventbridge/fire", in.Account,
			map[string]string{"ruleName": in.Rule}, &result); err != nil {
			return nil, FireRuleOutput{}, err
		}

		return nil, FireRuleOutput{
			Rule:       result.RuleName,
			Targets:    result.Targets,
			Successful: result.Successful,
			Failed:     result.Failed,
			FiredAt:    result.FiredAt,
		}, nil
	}

	mcp.AddTool(s, tool, handler)
}
