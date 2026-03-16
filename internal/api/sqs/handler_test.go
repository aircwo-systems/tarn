package sqs

import "testing"

func TestXmlEscape_PreservesQuotesInElementText(t *testing.T) {
	input := `{"aggregateId":"agg-42","note":"it's fine"}`
	got := xmlEscape(input)
	if got != input {
		t.Fatalf("xmlEscape should not escape quotes in text nodes, got %q", got)
	}
}

func TestXmlEscape_EscapesReservedXMLChars(t *testing.T) {
	input := `<a>&</a>`
	got := xmlEscape(input)
	want := `&lt;a&gt;&amp;&lt;/a&gt;`
	if got != want {
		t.Fatalf("xmlEscape(%q) = %q, want %q", input, got, want)
	}
}
