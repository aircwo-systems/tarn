package sqs

import (
	"encoding/json"
	"encoding/xml"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/pkg/types"
)

func TestAttributeMD5Fixtures(t *testing.T) {
	// Literal digests generated independently using Python hashlib and struct.pack.
	single := map[string]*types.MessageAttribute{"eventType": {DataType: "String", StringValue: "vacancy-expired"}}
	multi := map[string]*types.MessageAttribute{
		"z":    {DataType: "Number.int", StringValue: "42"},
		"a":    {DataType: "String.custom", StringValue: "café 🌍"},
		"blob": {DataType: "Binary.png", BinaryValue: []byte{0, 255, 128, 1}},
	}
	for _, tc := range []struct {
		attrs map[string]*types.MessageAttribute
		want  string
	}{{nil, ""}, {single, "edb64b283f223d4680cfb8608c8e72d7"}, {multi, "36353936af40491f5594f4d0ef7dafc4"}} {
		for i := 0; i < 20; i++ {
			if got := attributeMD5(tc.attrs); got != tc.want {
				t.Fatalf("digest=%s want=%s", got, tc.want)
			}
		}
	}
}

func TestAttributeChecksumProtocols(t *testing.T) {
	for _, protocol := range []string{"json", "query"} {
		for _, selection := range []string{"All", ".*", "eventType", "event*", "missing", ""} {
			t.Run(protocol+"/"+selection, func(t *testing.T) {
				h := newTestHandler(t)
				q, err := h.svc.CreateQueue("checksums", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				call := func(action string, payload map[string]any, form url.Values) string {
					t.Helper()
					var body string
					if protocol == "json" {
						payload["QueueUrl"] = q.QueueUrl
						b, _ := json.Marshal(payload)
						body = string(b)
					} else {
						form.Set("Action", action)
						form.Set("QueueUrl", q.QueueUrl)
						body = form.Encode()
					}
					req := httptest.NewRequest("POST", "/", strings.NewReader(body))
					if protocol == "json" {
						req.Header.Set("Content-Type", "application/x-amz-json-1.0")
						req.Header.Set("X-Amz-Target", "AmazonSQS."+action)
					} else {
						req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					}
					rec := httptest.NewRecorder()
					h.Dispatch(rec, req)
					if rec.Code != 200 {
						t.Fatalf("%s: %s", action, rec.Body.String())
					}
					return rec.Body.String()
				}
				attrs := map[string]*types.MessageAttribute{"eventType": {DataType: "String", StringValue: "vacancy-expired"}}
				for _, batch := range []bool{false, true} {
					action := "SendMessage"
					payload := map[string]any{"MessageBody": "hello", "MessageAttributes": attrs}
					prefix := ""
					if batch {
						action = "SendMessageBatch"
						payload = map[string]any{"Entries": []any{map[string]any{"Id": "one", "MessageBody": "hello", "MessageAttributes": attrs}, map[string]any{"Id": "two", "MessageBody": "plain"}}}
						prefix = "SendMessageBatchRequestEntry.1."
					}
					form := url.Values{prefix + "MessageBody": {"hello"}, prefix + "MessageAttribute.1.Name": {"eventType"}, prefix + "MessageAttribute.1.Value.DataType": {"String"}, prefix + "MessageAttribute.1.Value.StringValue": {"vacancy-expired"}}
					if batch {
						form.Set(prefix+"Id", "one")
						form.Set("SendMessageBatchRequestEntry.2.Id", "two")
						form.Set("SendMessageBatchRequestEntry.2.MessageBody", "plain")
					}
					sent := call(action, payload, form)
					if strings.Count(sent, "edb64b283f223d4680cfb8608c8e72d7") != 1 {
						t.Fatal(sent)
					}
				}
				payload := map[string]any{"MaxNumberOfMessages": 10}
				form := url.Values{"MaxNumberOfMessages": {"10"}}
				if selection != "" {
					payload["MessageAttributeNames"] = []string{selection}
					form.Set("MessageAttributeName.1", selection)
				}
				received := call("ReceiveMessage", payload, form)
				want := selection == "All" || selection == ".*" || selection == "eventType"
				if strings.Contains(received, "edb64b283f223d4680cfb8608c8e72d7") != want {
					t.Fatal(received)
				}
				// Verify each plain message omits the digest and every body digest is intact.
				var messages []struct{ Body, ReceiptHandle, MD5OfBody, MD5OfMessageAttributes string }
				if protocol == "json" {
					var out struct {
						Messages []struct{ Body, ReceiptHandle, MD5OfBody, MD5OfMessageAttributes string }
					}
					if err := json.Unmarshal([]byte(received), &out); err != nil {
						t.Fatal(err)
					}
					messages = out.Messages
				} else {
					var out struct {
						Messages []struct{ Body, ReceiptHandle, MD5OfBody, MD5OfMessageAttributes string } `xml:"ReceiveMessageResult>Message"`
					}
					if err := xml.Unmarshal([]byte(received), &out); err != nil {
						t.Fatal(err)
					}
					messages = out.Messages
				}
				if len(messages) != 3 {
					t.Fatalf("messages=%d", len(messages))
				}
				for _, m := range messages {
					if m.Body == "plain" && m.MD5OfMessageAttributes != "" {
						t.Fatal("plain message has attribute digest")
					}
					if m.Body == "hello" && m.MD5OfBody != "5d41402abc4b2a76b9719d911017c592" {
						t.Fatal("body digest changed")
					}
					call("DeleteMessage", map[string]any{"ReceiptHandle": m.ReceiptHandle}, url.Values{"ReceiptHandle": {m.ReceiptHandle}})
				}
				if msgs, err := h.svc.ReceiveMessage("checksums", 10, 0, 0); err != nil || len(msgs) != 0 {
					t.Fatalf("queue not drained: %v %v", msgs, err)
				}
			})
		}
	}
}

func TestSelectMessageAttributes(t *testing.T) {
	attrs := map[string]*types.MessageAttribute{
		"event.type": {DataType: "String", StringValue: "expired"},
		"event.id":   {DataType: "String", StringValue: "420102"},
		"other":      {DataType: "String", StringValue: "unselected"},
	}
	for _, tc := range []struct {
		names []string
		count int
	}{{nil, 0}, {[]string{"All"}, 3}, {[]string{".*"}, 3}, {[]string{"event.*"}, 2}, {[]string{"event.type", "other"}, 2}, {[]string{"event*"}, 0}} {
		selected := selectMessageAttributes(attrs, tc.names)
		if len(selected) != tc.count {
			t.Fatalf("%v selected %v", tc.names, selected)
		}
		for name, value := range selected {
			if value != attrs[name] {
				t.Fatal("attribute changed")
			}
		}
	}
	if len(attrs) != 3 {
		t.Fatal("selection mutated stored attributes")
	}
}
