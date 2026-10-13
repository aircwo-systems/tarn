package sqs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func receiveRequest(protocol, queueURL string, wait int) *http.Request {
	if protocol == "json" {
		body := fmt.Sprintf(`{"QueueUrl":%q,"WaitTimeSeconds":%d}`, queueURL, wait)
		r := httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-amz-json-1.0")
		r.Header.Set("X-Amz-Target", "AmazonSQS.ReceiveMessage")
		return r
	}
	form := url.Values{
		"Action": {"ReceiveMessage"}, "QueueUrl": {queueURL}, "WaitTimeSeconds": {fmt.Sprint(wait)},
	}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestReceiveMessageRequestCancellation(t *testing.T) {
	for _, protocol := range []string{"query", "json"} {
		t.Run(protocol, func(t *testing.T) {
			h := newTestHandler(t)
			t.Cleanup(h.svc.Stop)
			q, err := h.svc.CreateQueue("jobs", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := receiveRequest(protocol, q.QueueUrl, 20).WithContext(ctx)
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				h.Dispatch(rec, r)
				close(done)
			}()
			// Give the handler an opportunity to start waiting on the empty queue.
			select {
			case <-done:
				t.Fatal("long poll returned before cancellation")
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			select {
			case <-done:
				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
			case <-time.After(time.Second):
				t.Fatal("request cancellation did not end the long poll")
			}
		})
	}
}

func TestCancelledReceiveRequestDoesNotClaimMessages(t *testing.T) {
	for _, protocol := range []string{"query", "json"} {
		for _, wait := range []int{0, 20} {
			t.Run(fmt.Sprintf("%s/wait=%d", protocol, wait), func(t *testing.T) {
				h := newTestHandler(t)
				t.Cleanup(h.svc.Stop)
				q, err := h.svc.CreateQueue("jobs", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.svc.SendMessage("jobs", "pending", 0, nil, "", ""); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				r := receiveRequest(protocol, q.QueueUrl, wait).WithContext(ctx)
				rec := httptest.NewRecorder()
				h.Dispatch(rec, r)
				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
				msgs, err := h.svc.ReceiveMessage("jobs", 1, 30, 0)
				if err != nil || len(msgs) != 1 {
					t.Fatalf("cancelled request hid the message: %v %v", msgs, err)
				}
				if msgs[0].ApproximateReceiveCount != 1 {
					t.Fatalf("cancelled request incremented receive count: %d", msgs[0].ApproximateReceiveCount)
				}
			})
		}
	}
}
