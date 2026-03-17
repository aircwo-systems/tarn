package types

// SNSTopic represents an SNS topic.
type SNSTopic struct {
	TopicArn                  string            `json:"TopicArn"`
	Name                      string            `json:"Name"`
	DisplayName               string            `json:"DisplayName,omitempty"`
	Owner                     string            `json:"Owner"`
	Policy                    string            `json:"Policy,omitempty"`
	DeliveryPolicy            string            `json:"DeliveryPolicy,omitempty"`
	EffectiveDeliveryPolicy   string            `json:"EffectiveDeliveryPolicy,omitempty"`
	KmsMasterKeyId            string            `json:"KmsMasterKeyId,omitempty"`
	FifoTopic                 bool              `json:"FifoTopic,omitempty"`
	ContentBasedDeduplication bool              `json:"ContentBasedDeduplication,omitempty"`
	Attributes                map[string]string `json:"Attributes,omitempty"`
	Tags                      map[string]string `json:"Tags,omitempty"`
	CreatedTimestamp          int64             `json:"CreatedTimestamp"`
	LastModifiedTimestamp     int64             `json:"LastModifiedTimestamp"`
}

// SNSSubscription represents an SNS subscription.
type SNSSubscription struct {
	SubscriptionArn       string `json:"SubscriptionArn"`
	TopicArn              string `json:"TopicArn"`
	Protocol              string `json:"Protocol"`
	Endpoint              string `json:"Endpoint"`
	Owner                 string `json:"Owner"`
	RawMessageDelivery    bool   `json:"RawMessageDelivery,omitempty"`
	FilterPolicy          string `json:"FilterPolicy,omitempty"`
	FilterPolicyScope     string `json:"FilterPolicyScope,omitempty"`
	DeliveryPolicy        string `json:"DeliveryPolicy,omitempty"`
	RedrivePolicy         string `json:"RedrivePolicy,omitempty"`
	PendingConfirmation   bool   `json:"PendingConfirmation,omitempty"`
	CreatedTimestamp      int64  `json:"CreatedTimestamp"`
	LastModifiedTimestamp int64  `json:"LastModifiedTimestamp"`
}

// SNSMessageAttribute represents a publish message attribute.
type SNSMessageAttribute struct {
	DataType    string `json:"DataType"`
	StringValue string `json:"StringValue,omitempty"`
	BinaryValue string `json:"BinaryValue,omitempty"`
}
