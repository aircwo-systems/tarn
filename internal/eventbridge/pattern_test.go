package eventbridge

import (
	"testing"
)

func TestMatchExactString(t *testing.T) {
	pattern := `{"source": ["aws.s3"]}`
	event := `{"source": "aws.s3", "detail-type": "Object Created"}`
	assertMatch(t, pattern, event, true)

	event2 := `{"source": "aws.ec2", "detail-type": "Object Created"}`
	assertMatch(t, pattern, event2, false)
}

func TestMatchMultipleValuesOR(t *testing.T) {
	pattern := `{"source": ["aws.s3", "aws.ec2"]}`
	assertMatch(t, pattern, `{"source": "aws.s3"}`, true)
	assertMatch(t, pattern, `{"source": "aws.ec2"}`, true)
	assertMatch(t, pattern, `{"source": "aws.lambda"}`, false)
}

func TestMatchMultipleKeysAND(t *testing.T) {
	pattern := `{"source": ["aws.s3"], "detail-type": ["Object Created"]}`
	assertMatch(t, pattern, `{"source": "aws.s3", "detail-type": "Object Created"}`, true)
	assertMatch(t, pattern, `{"source": "aws.s3", "detail-type": "Object Deleted"}`, false)
	assertMatch(t, pattern, `{"source": "aws.ec2", "detail-type": "Object Created"}`, false)
}

func TestMatchNestedObject(t *testing.T) {
	pattern := `{"detail": {"bucket": {"name": ["my-bucket"]}}}`
	assertMatch(t, pattern, `{"detail": {"bucket": {"name": "my-bucket"}}}`, true)
	assertMatch(t, pattern, `{"detail": {"bucket": {"name": "other"}}}`, false)
	assertMatch(t, pattern, `{"detail": {"foo": "bar"}}`, false)
}

func TestMatchPrefix(t *testing.T) {
	pattern := `{"source": [{"prefix": "aws."}]}`
	assertMatch(t, pattern, `{"source": "aws.s3"}`, true)
	assertMatch(t, pattern, `{"source": "aws.ec2"}`, true)
	assertMatch(t, pattern, `{"source": "custom.myapp"}`, false)
}

func TestMatchSuffix(t *testing.T) {
	pattern := `{"source": [{"suffix": ".s3"}]}`
	assertMatch(t, pattern, `{"source": "aws.s3"}`, true)
	assertMatch(t, pattern, `{"source": "custom.s3"}`, true)
	assertMatch(t, pattern, `{"source": "aws.ec2"}`, false)
}

func TestMatchAnythingButStrings(t *testing.T) {
	pattern := `{"source": [{"anything-but": ["aws.s3"]}]}`
	assertMatch(t, pattern, `{"source": "aws.ec2"}`, true)
	assertMatch(t, pattern, `{"source": "aws.s3"}`, false)
}

func TestMatchAnythingButSingleString(t *testing.T) {
	pattern := `{"source": [{"anything-but": "internal"}]}`
	assertMatch(t, pattern, `{"source": "external"}`, true)
	assertMatch(t, pattern, `{"source": "internal"}`, false)
}

func TestMatchAnythingButPrefix(t *testing.T) {
	pattern := `{"source": [{"anything-but": {"prefix": "internal."}}]}`
	assertMatch(t, pattern, `{"source": "external.service"}`, true)
	assertMatch(t, pattern, `{"source": "internal.service"}`, false)
}

func TestMatchNumericEquals(t *testing.T) {
	pattern := `{"detail": {"size": [{"numeric": ["=", 100]}]}}`
	assertMatch(t, pattern, `{"detail": {"size": 100}}`, true)
	assertMatch(t, pattern, `{"detail": {"size": 99}}`, false)
}

func TestMatchNumericRange(t *testing.T) {
	pattern := `{"detail": {"size": [{"numeric": [">", 0, "<=", 100]}]}}`
	assertMatch(t, pattern, `{"detail": {"size": 50}}`, true)
	assertMatch(t, pattern, `{"detail": {"size": 100}}`, true)
	assertMatch(t, pattern, `{"detail": {"size": 0}}`, false)
	assertMatch(t, pattern, `{"detail": {"size": 101}}`, false)
}

func TestMatchExists(t *testing.T) {
	pattern := `{"detail": {"key": [{"exists": true}]}}`
	assertMatch(t, pattern, `{"detail": {"key": "value"}}`, true)
	assertMatch(t, pattern, `{"detail": {"key": null}}`, true) // exists but null
	assertMatch(t, pattern, `{"detail": {"other": "value"}}`, false)

	notExistsPattern := `{"detail": {"key": [{"exists": false}]}}`
	assertMatch(t, notExistsPattern, `{"detail": {"other": "value"}}`, true)
	assertMatch(t, notExistsPattern, `{"detail": {"key": "value"}}`, false)
}

func TestMatchWildcard(t *testing.T) {
	pattern := `{"source": [{"wildcard": "aws.*"}]}`
	assertMatch(t, pattern, `{"source": "aws.s3"}`, true)
	assertMatch(t, pattern, `{"source": "aws.ec2.instances"}`, true)
	assertMatch(t, pattern, `{"source": "custom.app"}`, false)
}

func TestMatchWildcardMultiple(t *testing.T) {
	pattern := `{"source": [{"wildcard": "*.s3.*"}]}`
	assertMatch(t, pattern, `{"source": "aws.s3.bucket"}`, true)
	assertMatch(t, pattern, `{"source": "gcp.s3.fake"}`, true)
	assertMatch(t, pattern, `{"source": "aws.ec2.instances"}`, false)
}

func TestMatchNull(t *testing.T) {
	pattern := `{"detail": {"key": [null]}}`
	assertMatch(t, pattern, `{"detail": {"key": null}}`, true)
	assertMatch(t, pattern, `{"detail": {"key": "value"}}`, false)
}

func TestMatchBoolean(t *testing.T) {
	pattern := `{"detail": {"active": [true]}}`
	assertMatch(t, pattern, `{"detail": {"active": true}}`, true)
	assertMatch(t, pattern, `{"detail": {"active": false}}`, false)
}

func TestMatchNumber(t *testing.T) {
	pattern := `{"detail": {"code": [200, 201]}}`
	assertMatch(t, pattern, `{"detail": {"code": 200}}`, true)
	assertMatch(t, pattern, `{"detail": {"code": 201}}`, true)
	assertMatch(t, pattern, `{"detail": {"code": 404}}`, false)
}

func TestEmptyPatternMatchesEverything(t *testing.T) {
	pattern := `{}`
	assertMatch(t, pattern, `{"source": "anything", "detail": {"nested": true}}`, true)
}

func TestMatchMixedLiteralAndOperator(t *testing.T) {
	// Array with both literal and prefix: OR'd
	pattern := `{"source": ["exact-match", {"prefix": "aws."}]}`
	assertMatch(t, pattern, `{"source": "exact-match"}`, true)
	assertMatch(t, pattern, `{"source": "aws.s3"}`, true)
	assertMatch(t, pattern, `{"source": "other"}`, false)
}

func TestPatternIgnoresExtraEventFields(t *testing.T) {
	pattern := `{"source": ["aws.s3"]}`
	event := `{"source": "aws.s3", "detail-type": "anything", "extra": "ignored"}`
	assertMatch(t, pattern, event, true)
}

func TestValidateEventPatternValid(t *testing.T) {
	cases := []string{
		`{"source": ["aws.s3"]}`,
		`{"detail": {"key": [{"prefix": "val"}]}}`,
		`{"source": ["a", "b"], "detail-type": ["c"]}`,
		`{}`,
		`{"detail": {"nested": {"deep": [1, 2, 3]}}}`,
	}
	for _, c := range cases {
		if err := ValidateEventPattern(c); err != nil {
			t.Errorf("ValidateEventPattern(%s) = %v, want nil", c, err)
		}
	}
}

func TestValidateEventPatternInvalid(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"empty string", ""},
		{"not json", "not-json"},
		{"bare string value", `{"source": "aws.s3"}`},
		{"empty array", `{"source": []}`},
		{"unknown operator", `{"source": [{"unknown": "val"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateEventPattern(tc.pattern); err == nil {
				t.Errorf("ValidateEventPattern(%s) = nil, want error", tc.pattern)
			}
		})
	}
}

func TestMatchWildcardFunction(t *testing.T) {
	cases := []struct {
		pattern string
		s       string
		want    bool
	}{
		{"*", "anything", true},
		{"*", "", true},
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"a*c", "abc", true},
		{"a*c", "ac", true},
		{"a*c", "abbc", true},
		{"a*c", "abd", false},
		{"*b*", "abc", true},
		{"*b*", "b", true},
		{"*b*", "aaa", false},
	}
	for _, tc := range cases {
		got := matchWildcard(tc.pattern, tc.s)
		if got != tc.want {
			t.Errorf("matchWildcard(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.want)
		}
	}
}

func assertMatch(t *testing.T, pattern, event string, want bool) {
	t.Helper()
	got, err := MatchEventPattern([]byte(pattern), []byte(event))
	if err != nil {
		t.Fatalf("MatchEventPattern error: %v\n  pattern: %s\n  event:   %s", err, pattern, event)
	}
	if got != want {
		t.Errorf("MatchEventPattern = %v, want %v\n  pattern: %s\n  event:   %s", got, want, pattern, event)
	}
}
