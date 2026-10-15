package s3

import (
	"encoding/json"
	"log"
	"net/url"
	"strings"
	"time"
)

// SetQueueSender connects this account's S3 service to its SQS service.
func (s *Service) SetQueueSender(send func(queueARN, body string) error) { s.queueSender = send }
func (s *Service) deliverQueueNotifications(event, bucket, key string, size int64, etag string) {
	if s.queueSender == nil {
		return
	}
	cfg := s.GetBucketNotificationConfiguration(bucket)
	if cfg == nil {
		return
	}
	for _, d := range cfg.QueueConfigurations {
		matches := false
		for _, pattern := range d.Events {
			p := string(pattern)
			if p == event || (strings.HasSuffix(p, "*") && strings.HasPrefix(event, strings.TrimSuffix(p, "*"))) {
				matches = true
			}
		}
		if !matches || !strings.HasPrefix(key, d.FilterPrefix) || !strings.HasSuffix(key, d.FilterSuffix) {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"Records": []any{map[string]any{
			"eventVersion": "2.1", "eventSource": "aws:s3", "awsRegion": s.cfg.Region, "eventTime": time.Now().UTC().Format(time.RFC3339), "eventName": strings.TrimPrefix(event, "s3:"),
			"s3": map[string]any{"configurationId": d.ID, "bucket": map[string]any{"name": bucket, "arn": "arn:aws:s3:::" + bucket}, "object": map[string]any{"key": url.QueryEscape(key), "size": size, "eTag": strings.Trim(etag, `"`)}},
		}}})
		if err := s.queueSender(d.QueueArn, string(payload)); err != nil {
			log.Printf("[s3] notification for %s/%s to %s: %v", bucket, key, d.QueueArn, err)
		}
	}
}
