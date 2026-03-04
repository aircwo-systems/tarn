package eventsource

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/openstack-project/openstack/pkg/types"
)

// SQSInterface abstracts SQS operations needed by the poller.
type SQSInterface interface {
	ReceiveMessage(queueName string, maxCount, visTimeout, waitTimeSec int) ([]*types.SQSMessage, error)
	DeleteMessage(queueName, receiptHandle string) error
}

// LambdaInterface abstracts Lambda operations needed by the poller.
type LambdaInterface interface {
	Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error)
}

type poller struct {
	mapping  *types.EventSourceMapping
	sqs      SQSInterface
	lambda   LambdaInterface
	store    *Store
	done     chan struct{}
	stopOnce sync.Once
}

func newPoller(mapping *types.EventSourceMapping, sqsSvc SQSInterface, lambdaSvc LambdaInterface, store *Store) *poller {
	return &poller{
		mapping: mapping,
		sqs:     sqsSvc,
		lambda:  lambdaSvc,
		store:   store,
		done:    make(chan struct{}),
	}
}

func (p *poller) start() {
	go p.run()
}

func (p *poller) stop() {
	p.stopOnce.Do(func() {
		close(p.done)
	})
}

func (p *poller) run() {
	interval := p.mapping.MaximumBatchingWindowInSeconds
	if interval < 1 {
		interval = 1
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.poll()
		}
	}
}

func (p *poller) poll() {
	msgs, err := p.sqs.ReceiveMessage(p.mapping.QueueName, p.mapping.BatchSize, 30, 1)
	if err != nil {
		if isNotFoundError(err) {
			p.disableWithError(err)
			return
		}
		log.Printf("[eventsource] %s: receive error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		return
	}
	if len(msgs) == 0 {
		return
	}

	payload := buildSQSEventPayload(msgs, p.mapping.EventSourceArn, p.mapping.QueueName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	functionName := normalizeLambdaFunctionName(p.mapping.FunctionName)
	if functionName != p.mapping.FunctionName {
		// Heal persisted mappings that were saved with FunctionName as an ARN.
		p.mapping.FunctionName = functionName
		_ = p.store.Save(p.mapping)
	}

	_, err = p.lambda.Invoke(ctx, &types.InvokeInput{
		FunctionName:   functionName,
		Payload:        payload,
		InvocationType: "RequestResponse",
	})
	if err != nil {
		if isNotFoundError(err) {
			p.disableWithError(err)
			return
		}
		log.Printf("[eventsource] %s: invoke error: %v", p.mapping.UUID, err)
		p.updateResult(fmt.Sprintf("ERROR: %v", err))
		return
	}

	// On success, delete each message
	for _, msg := range msgs {
		if err := p.sqs.DeleteMessage(p.mapping.QueueName, msg.ReceiptHandle); err != nil {
			log.Printf("[eventsource] %s: delete message error: %v", p.mapping.UUID, err)
		}
	}

	p.updateResult("OK")
}

func (p *poller) updateResult(result string) {
	p.mapping.LastProcessingResult = result
	p.mapping.LastModified = time.Now().UTC()
	_ = p.store.Save(p.mapping)
}

func (p *poller) disableWithError(err error) {
	log.Printf("[eventsource] %s: disabling mapping due to missing resource: %v", p.mapping.UUID, err)
	p.mapping.Enabled = false
	p.mapping.State = "Disabled"
	p.mapping.LastProcessingResult = fmt.Sprintf("ERROR: %v", err)
	p.mapping.LastModified = time.Now().UTC()
	_ = p.store.Save(p.mapping)
	p.stop()
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

type sqsEventRecord struct {
	MessageId      string `json:"messageId"`
	ReceiptHandle  string `json:"receiptHandle"`
	Body           string `json:"body"`
	Md5OfBody      string `json:"md5OfBody"`
	EventSource    string `json:"eventSource"`
	EventSourceARN string `json:"eventSourceARN"`
	AwsRegion      string `json:"awsRegion"`
}

func buildSQSEventPayload(msgs []*types.SQSMessage, eventSourceArn, queueName string) []byte {
	records := make([]sqsEventRecord, len(msgs))
	for i, msg := range msgs {
		records[i] = sqsEventRecord{
			MessageId:      msg.MessageId,
			ReceiptHandle:  msg.ReceiptHandle,
			Body:           msg.Body,
			Md5OfBody:      msg.MD5OfBody,
			EventSource:    "aws:sqs",
			EventSourceARN: eventSourceArn,
			AwsRegion:      "us-east-1",
		}
	}
	data, _ := json.Marshal(map[string]any{"Records": records})
	return data
}
