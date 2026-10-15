package s3

import (
	"encoding/xml"
	"github.com/aircwo-systems/tarn/pkg/types"
	"net/http"
)

type notificationFilterRule struct {
	Name  string `xml:"Name"`
	Value string `xml:"Value"`
}
type notificationFilter struct {
	Rules []notificationFilterRule `xml:"S3Key>FilterRule"`
}
type notificationDestination struct {
	ID       string              `xml:"Id,omitempty"`
	Function string              `xml:"CloudFunction,omitempty"`
	Queue    string              `xml:"Queue,omitempty"`
	Events   []types.S3EventName `xml:"Event"`
	Filter   *notificationFilter `xml:"Filter,omitempty"`
}
type notificationConfiguration struct {
	XMLName xml.Name                  `xml:"NotificationConfiguration"`
	XMLNS   string                    `xml:"xmlns,attr,omitempty"`
	Lambdas []notificationDestination `xml:"CloudFunctionConfiguration,omitempty"`
	Queues  []notificationDestination `xml:"QueueConfiguration,omitempty"`
	Topics  []notificationDestination `xml:"TopicConfiguration,omitempty"`
}

func notificationRules(prefix, suffix string) *notificationFilter {
	f := &notificationFilter{}
	if prefix != "" {
		f.Rules = append(f.Rules, notificationFilterRule{"prefix", prefix})
	}
	if suffix != "" {
		f.Rules = append(f.Rules, notificationFilterRule{"suffix", suffix})
	}
	if len(f.Rules) == 0 {
		return nil
	}
	return f
}
func notificationPrefixSuffix(f *notificationFilter) (prefix, suffix string) {
	if f != nil {
		for _, r := range f.Rules {
			if r.Name == "prefix" {
				prefix = r.Value
			}
			if r.Name == "suffix" {
				suffix = r.Value
			}
		}
	}
	return
}
func (h *Handler) putBucketNotification(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, 404, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	var input notificationConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&input); err != nil {
		writeS3Error(w, 400, "MalformedXML", "The XML you provided was not well-formed")
		return
	}
	if len(input.Topics) > 0 {
		writeS3Error(w, 501, "NotImplemented", "SNS notification destinations are not supported")
		return
	}
	cfg := &types.BucketNotificationConfiguration{}
	for _, d := range input.Lambdas {
		prefix, suffix := notificationPrefixSuffix(d.Filter)
		cfg.LambdaConfigurations = append(cfg.LambdaConfigurations, types.S3LambdaNotification{ID: d.ID, LambdaFunctionArn: d.Function, LambdaFunctionName: extractFunctionName(d.Function), Events: d.Events, FilterPrefix: prefix, FilterSuffix: suffix})
	}
	for _, d := range input.Queues {
		prefix, suffix := notificationPrefixSuffix(d.Filter)
		cfg.QueueConfigurations = append(cfg.QueueConfigurations, types.S3QueueNotification{ID: d.ID, QueueArn: d.Queue, Events: d.Events, FilterPrefix: prefix, FilterSuffix: suffix})
	}
	if err := h.svc.PutBucketNotificationConfiguration(bucket, cfg); err != nil {
		writeS3Error(w, 400, "InvalidArgument", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
func (h *Handler) getBucketNotification(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, 404, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	resp := notificationConfiguration{XMLNS: s3Namespace}
	if cfg := h.svc.GetBucketNotificationConfiguration(bucket); cfg != nil {
		for _, d := range cfg.LambdaConfigurations {
			resp.Lambdas = append(resp.Lambdas, notificationDestination{ID: d.ID, Function: d.LambdaFunctionArn, Events: d.Events, Filter: notificationRules(d.FilterPrefix, d.FilterSuffix)})
		}
		for _, d := range cfg.QueueConfigurations {
			resp.Queues = append(resp.Queues, notificationDestination{ID: d.ID, Queue: d.QueueArn, Events: d.Events, Filter: notificationRules(d.FilterPrefix, d.FilterSuffix)})
		}
	}
	writeXML(w, 200, resp)
}
