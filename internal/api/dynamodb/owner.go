package dynamodb

import (
	"encoding/json"
	"strings"
)

// TableNames returns the tables a DynamoDB request reads or writes, for
// finding the account that owns them when the caller's credentials do not
// name one. CreateTable and ListTables return nil: a create belongs to the
// caller's account, and a listing has no table to look up.
func TableNames(target string, body []byte) []string {
	action, ok := strings.CutPrefix(strings.TrimSpace(target), dynamoPrefix)
	if !ok || action == "CreateTable" || action == "ListTables" {
		return nil
	}
	var req struct {
		TableName     string                     `json:"TableName"`
		RequestItems  map[string]json.RawMessage `json:"RequestItems"`
		TransactItems []map[string]struct {
			TableName string `json:"TableName"`
		} `json:"TransactItems"`
	}
	if json.Unmarshal(body, &req) != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	add(req.TableName)
	for name := range req.RequestItems {
		add(name)
	}
	for _, item := range req.TransactItems {
		for _, action := range item {
			add(action.TableName)
		}
	}
	return names
}

// HasTables reports whether every named table exists in this account.
func (h *Handler) HasTables(names []string) bool {
	for _, name := range names {
		if _, err := h.svc.DescribeTable(name); err != nil {
			return false
		}
	}
	return true
}
