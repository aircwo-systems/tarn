package sns

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/aircwo-systems/tarn/internal/config"
	snssvc "github.com/aircwo-systems/tarn/internal/sns"
	sqssvc "github.com/aircwo-systems/tarn/internal/sqs"
)

func TestPublishPreservesFIFOIdentifiers(t *testing.T) {
	for _, action := range []string{"Publish", "PublishBatch"} {
		for _, raw := range []bool{true, false} {
			for _, fifo := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/raw=%t/fifo=%t", action, raw, fifo), func(t *testing.T) {
					cfg := config.Default()
					cfg.DataDir = t.TempDir()
					cfg.PersistenceEnabled = false
					queues := sqssvc.NewService(cfg)
					queueName := "updates"
					if fifo {
						queueName += ".fifo"
					}
					queue, err := queues.CreateQueue(queueName, map[string]string{"FifoQueue": strconv.FormatBool(fifo)}, nil)
					if err != nil {
						t.Fatal(err)
					}
					topics := snssvc.NewService(cfg, queues, nil)
					topic, err := topics.CreateTopic("updates.fifo", map[string]string{"FifoTopic": "true"}, nil)
					if err != nil {
						t.Fatal(err)
					}
					_, err = topics.Subscribe(topic.TopicArn, "sqs", queue.QueueArn, map[string]string{"RawMessageDelivery": strconv.FormatBool(raw)})
					if err != nil {
						t.Fatal(err)
					}

					form := url.Values{"Action": {action}, "Version": {"2010-03-31"}, "TopicArn": {topic.TopicArn}}
					count := 1
					if action == "PublishBatch" {
						count = 2
					}
					for i := 1; i <= count; i++ {
						prefix := ""
						if action == "PublishBatch" {
							prefix = fmt.Sprintf("PublishBatchRequestEntries.member.%d.", i)
							form.Set(prefix+"Id", fmt.Sprintf("entry-%d", i))
						}
						form.Set(prefix+"Message", fmt.Sprintf("update-%d", i))
						form.Set(prefix+"MessageGroupId", fmt.Sprintf("citizen-%d", i))
						form.Set(prefix+"MessageDeduplicationId", fmt.Sprintf("request-%d", i))
					}
					request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					response := httptest.NewRecorder()
					NewHandler(topics).Dispatch(response, request)
					if response.Code != http.StatusOK {
						t.Fatalf("publish status=%d body=%s", response.Code, response.Body.String())
					}

					messages, err := queues.PeekMessages(queue.QueueName, 10)
					if err != nil {
						t.Fatal(err)
					}
					if len(messages) != count {
						t.Fatalf("routed %d messages, want %d", len(messages), count)
					}
					seenBodies := make(map[string]bool, count)
					for _, message := range messages {
						body := message.Body
						if !raw {
							var envelope struct{ Message string }
							if err := json.Unmarshal([]byte(body), &envelope); err != nil {
								t.Fatal(err)
							}
							body = envelope.Message
						}
						index := strings.TrimPrefix(body, "update-")
						if index != "1" && (count != 2 || index != "2") {
							t.Fatalf("unexpected message body %q", body)
						}
						if seenBodies[body] {
							t.Fatalf("duplicate message body %q", body)
						}
						seenBodies[body] = true
						groupID, dedupID := "", ""
						if fifo {
							groupID, dedupID = "citizen-"+index, "request-"+index
						}
						if message.MessageGroupId != groupID || message.MessageDeduplicationId != dedupID {
							t.Fatalf("message %q: group=%q dedup=%q, want group=%q dedup=%q", body, message.MessageGroupId, message.MessageDeduplicationId, groupID, dedupID)
						}
					}
				})
			}
		}
	}
}
