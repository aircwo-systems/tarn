package interpreter

import (
	"encoding/json"
	"testing"
)

// TestTaskParametersResultPathPreservesInput is the focused regression test for
// the bug where a Task with BOTH Parameters and ResultPath dropped the original
// state input: ResultPath must merge the result into the state input (post
// InputPath), not into the Parameters-shaped payload sent to the resource.
func TestTaskParametersResultPathPreservesInput(t *testing.T) {
	def := `{
      "StartAt": "T",
      "States": {
        "T": {
          "Type": "Task",
          "Resource": "arn:aws:states:::lambda:invoke",
          "Parameters": { "FunctionName": "f", "Payload": { "x.$": "$.userId" } },
          "ResultPath": "$.taskResult",
          "Next": "P"
        },
        "P": {
          "Type": "Pass",
          "Parameters": { "userId.$": "$.userId" },
          "End": true
        }
      }
    }`
	exec := &fakeExecutor{result: json.RawMessage(`{"ok":true}`)}
	out, err := runASL(t, def, `{"userId":"u1"}`, exec)
	if err != nil {
		t.Fatalf("Task with Parameters+ResultPath dropped the state input: %v", err)
	}
	if got := out.(map[string]any)["userId"]; got != "u1" {
		t.Fatalf("userId not preserved past a Task with Parameters: got %#v", got)
	}
}

// TestNestedMapPreservesParentFields mirrors the multi-delete batch: an outer Map
// over users; each user runs an inner Map over apiIds, then a Task master step
// (Parameters + ResultPath), then a Pass that reads $.userId / $.masterUuid. This
// is the end-to-end shape that failed with `key "userId" not found`.
func TestNestedMapPreservesParentFields(t *testing.T) {
	def := `{
      "StartAt": "Users",
      "States": {
        "Users": {
          "Type": "Map",
          "ItemsPath": "$.users",
          "MaxConcurrency": 1,
          "ItemProcessor": {
            "StartAt": "Apis",
            "States": {
              "Apis": {
                "Type": "Map",
                "ItemsPath": "$.apiIds",
                "MaxConcurrency": 1,
                "ItemProcessor": {
                  "StartAt": "DeleteOne",
                  "States": {
                    "DeleteOne": {
                      "Type": "Task",
                      "Resource": "arn:aws:states:::lambda:invoke",
                      "Parameters": { "FunctionName": "f", "Payload.$": "$" },
                      "End": true
                    }
                  }
                },
                "ResultPath": "$.deleteResults",
                "Next": "Master"
              },
              "Master": {
                "Type": "Task",
                "Resource": "arn:aws:states:::lambda:invoke",
                "Parameters": {
                  "FunctionName": "f",
                  "Payload": { "userId.$": "$.userId", "masterUuid.$": "$.masterUuid" }
                },
                "ResultPath": "$.userDeleteResult",
                "Next": "Done"
              },
              "Done": {
                "Type": "Pass",
                "Parameters": {
                  "userId.$": "$.userId",
                  "masterUuid.$": "$.masterUuid",
                  "status": "deleted"
                },
                "End": true
              }
            }
          },
          "ResultPath": "$.results",
          "End": true
        }
      }
    }`

	exec := &fakeExecutor{result: json.RawMessage(`{"status":"deleted"}`)}
	input := `{"batchId":"b1","users":[{"userId":"u1","masterUuid":"m1","apiIds":[{"userId":"u1","api":"profile","uuid":"x1"}]}]}`

	out, err := runASL(t, def, input, exec)
	if err != nil {
		t.Fatalf("nested Map/Task dropped parent fields before the master step: %v", err)
	}

	results, ok := out.(map[string]any)["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected 1 user result, got: %#v", out)
	}
	user := results[0].(map[string]any)
	if user["userId"] != "u1" {
		t.Errorf("userId not preserved: got %#v", user["userId"])
	}
	if user["masterUuid"] != "m1" {
		t.Errorf("masterUuid not preserved: got %#v", user["masterUuid"])
	}
}
