package asl

import (
	"errors"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := map[string]string{
		"minimal pass": `{"StartAt":"A","States":{"A":{"Type":"Pass","End":true}}}`,
		"task choice wait succeed fail": `{
			"StartAt":"Run",
			"States":{
				"Run":{"Type":"Task","Resource":"arn:aws:lambda:us-east-1:000000000000:function:f","Next":"Decide",
					"Retry":[{"ErrorEquals":["States.ALL"],"MaxAttempts":2,"IntervalSeconds":1,"BackoffRate":2.0}],
					"Catch":[{"ErrorEquals":["States.TaskFailed"],"Next":"Bad"}]},
				"Decide":{"Type":"Choice","Choices":[{"Variable":"$.ok","BooleanEquals":true,"Next":"Hold"}],"Default":"Bad"},
				"Hold":{"Type":"Wait","Seconds":3,"Next":"Done"},
				"Done":{"Type":"Succeed"},
				"Bad":{"Type":"Fail","Error":"E","Cause":"c"}
			}
		}`,
		"parallel and map": `{
			"StartAt":"P",
			"States":{
				"P":{"Type":"Parallel","Next":"M","Branches":[
					{"StartAt":"B1","States":{"B1":{"Type":"Pass","End":true}}}]},
				"M":{"Type":"Map","ItemsPath":"$.items","End":true,
					"ItemProcessor":{"StartAt":"I1","States":{"I1":{"Type":"Pass","End":true}}}}
			}
		}`,
	}
	for name, def := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := Parse(def)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.StartAt == "" || len(m.States) == 0 {
				t.Fatalf("parsed machine is empty: %+v", m)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name string
		def  string
		want string
	}{
		{"empty", ``, "definition is empty"},
		{"bad json", `{`, "invalid JSON"},
		{"missing startat", `{"States":{"A":{"Type":"Pass","End":true}}}`, "StartAt is required"},
		{"startat undefined", `{"StartAt":"Z","States":{"A":{"Type":"Pass","End":true}}}`, "not a defined state"},
		{"dangling next", `{"StartAt":"A","States":{"A":{"Type":"Pass","Next":"Z"}}}`, "Next \"Z\" is not a defined state"},
		{"next and end", `{"StartAt":"A","States":{"A":{"Type":"Pass","Next":"A","End":true}}}`, "must not set both Next and End"},
		{"no transition", `{"StartAt":"A","States":{"A":{"Type":"Pass"}}}`, "must set Next or End"},
		{"unknown type", `{"StartAt":"A","States":{"A":{"Type":"Bogus","End":true}}}`, "unknown state Type"},
		{"task no resource", `{"StartAt":"A","States":{"A":{"Type":"Task","End":true}}}`, "requires a Resource"},
		{"choice no rules", `{"StartAt":"A","States":{"A":{"Type":"Choice"}}}`, "at least one rule"},
		{"choice dangling next", `{"StartAt":"A","States":{"A":{"Type":"Choice","Choices":[{"Variable":"$.x","StringEquals":"y","Next":"Z"}]}}}`, "not a defined state"},
		{"wait two of", `{"StartAt":"A","States":{"A":{"Type":"Wait","Seconds":1,"Timestamp":"2020-01-01T00:00:00Z","End":true}}}`, "exactly one of"},
		{"parallel no branches", `{"StartAt":"A","States":{"A":{"Type":"Parallel","End":true}}}`, "at least one branch"},
		{"map no processor", `{"StartAt":"A","States":{"A":{"Type":"Map","End":true}}}`, "requires an ItemProcessor"},
		{"nested branch invalid", `{"StartAt":"P","States":{"P":{"Type":"Parallel","End":true,"Branches":[{"StartAt":"X","States":{"Y":{"Type":"Pass","End":true}}}]}}}`, "Branches[0].StartAt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.def)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("expected *ParseError, got %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPathOr(t *testing.T) {
	var absent Path
	if got := absent.Or("$"); got != "$" {
		t.Fatalf("absent.Or = %q, want $", got)
	}
	nullPath := Path{Set: true, Null: true}
	if got := nullPath.Or("$"); got != "" {
		t.Fatalf("null.Or = %q, want empty", got)
	}
	set := Path{Set: true, Value: "$.x"}
	if got := set.Or("$"); got != "$.x" {
		t.Fatalf("set.Or = %q, want $.x", got)
	}
}
