package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	dynamodbsvc "github.com/aircwo-systems/tarn/internal/dynamodb"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const (
	defaultDynamoReadLimit   = 50
	maxDynamoReadLimit       = 500
	defaultDynamoStreamLimit = 100
	maxDynamoStreamLimit     = 500
)

type dynamoKeySchemaItem struct {
	AttributeName string `json:"attributeName"`
	KeyType       string `json:"keyType"`
}

type dynamoAttributeDefinition struct {
	AttributeName string `json:"attributeName"`
	AttributeType string `json:"attributeType"`
}

type dynamoProjection struct {
	ProjectionType   string   `json:"projectionType,omitempty"`
	NonKeyAttributes []string `json:"nonKeyAttributes,omitempty"`
}

type dynamoThroughput struct {
	ReadCapacityUnits      int64 `json:"readCapacityUnits"`
	WriteCapacityUnits     int64 `json:"writeCapacityUnits"`
	NumberOfDecreasesToday int64 `json:"numberOfDecreasesToday,omitempty"`
}

type dynamoLSIDetail struct {
	IndexName      string                `json:"indexName"`
	IndexArn       string                `json:"indexArn,omitempty"`
	KeySchema      []dynamoKeySchemaItem `json:"keySchema"`
	Projection     dynamoProjection      `json:"projection"`
	ItemCount      int64                 `json:"itemCount"`
	IndexSizeBytes int64                 `json:"indexSizeBytes"`
}

type dynamoGSIDetail struct {
	IndexName             string                `json:"indexName"`
	IndexArn              string                `json:"indexArn,omitempty"`
	IndexStatus           string                `json:"indexStatus,omitempty"`
	KeySchema             []dynamoKeySchemaItem `json:"keySchema"`
	Projection            dynamoProjection      `json:"projection"`
	ProvisionedThroughput *dynamoThroughput     `json:"provisionedThroughput,omitempty"`
	ItemCount             int64                 `json:"itemCount"`
	IndexSizeBytes        int64                 `json:"indexSizeBytes"`
}

type dynamoTTLDetail struct {
	AttributeName string `json:"attributeName,omitempty"`
	Status        string `json:"status"`
}

type dynamoBackupsDetail struct {
	Status                     string  `json:"status"`
	PointInTimeRecoveryStatus  string  `json:"pointInTimeRecoveryStatus"`
	EarliestRestorableDateTime *string `json:"earliestRestorableDateTime,omitempty"`
	LatestRestorableDateTime   *string `json:"latestRestorableDateTime,omitempty"`
}

type dynamoKinesisDestination struct {
	StreamArn         string `json:"streamArn"`
	DestinationStatus string `json:"destinationStatus"`
}

type dynamoSSEDetail struct {
	Status          string `json:"status"`
	SSEType         string `json:"sseType"`
	KMSMasterKeyArn string `json:"kmsMasterKeyArn,omitempty"`
}

type dynamoStreamShardDetail struct {
	ShardID                string `json:"shardId"`
	StartingSequenceNumber string `json:"startingSequenceNumber,omitempty"`
	EndingSequenceNumber   string `json:"endingSequenceNumber,omitempty"`
}

type dynamoStreamDetail struct {
	Arn      string                    `json:"arn"`
	Label    string                    `json:"label,omitempty"`
	Status   string                    `json:"status"`
	ViewType string                    `json:"viewType,omitempty"`
	Shards   []dynamoStreamShardDetail `json:"shards"`
}

type dynamoTriggerIntegration struct {
	UUID           string                `json:"uuid"`
	FunctionName   string                `json:"functionName"`
	State          string                `json:"state"`
	BatchSize      int                   `json:"batchSize"`
	EventSourceArn string                `json:"eventSourceArn,omitempty"`
	LastResult     string                `json:"lastResult,omitempty"`
	FilterCriteria *types.FilterCriteria `json:"filterCriteria,omitempty"`
	LogGroup       string                `json:"logGroup,omitempty"`
}

type dynamoLambdaIntegration struct {
	FunctionName string `json:"functionName"`
	Runtime      string `json:"runtime"`
	State        string `json:"state"`
	EnvKey       string `json:"envKey"`
	MatchType    string `json:"matchType"`
	LogGroup     string `json:"logGroup,omitempty"`
}

type dynamoECSIntegration struct {
	Kind              string `json:"kind"` // "service" | "task-definition"
	Name              string `json:"name"`
	Arn               string `json:"arn"`
	ClusterArn        string `json:"clusterArn,omitempty"`
	ClusterName       string `json:"clusterName,omitempty"`
	TaskDefinitionArn string `json:"taskDefinitionArn"`
	ContainerName     string `json:"containerName"`
	EnvKey            string `json:"envKey"`
	MatchType         string `json:"matchType"`
	Status            string `json:"status"`
	DesiredCount      int    `json:"desiredCount,omitempty"`
	RunningCount      int    `json:"runningCount,omitempty"`
	LogGroup          string `json:"logGroup,omitempty"`
}

type dynamoStepFunctionIntegration struct {
	Name       string   `json:"name"`
	Arn        string   `json:"arn"`
	Type       string   `json:"type"`
	Status     string   `json:"status"`
	MatchType  string   `json:"matchType"`
	StateNames []string `json:"stateNames"`
}

type dynamoKMSIntegration struct {
	KeyID       string   `json:"keyId"`
	Arn         string   `json:"arn"`
	Aliases     []string `json:"aliases"`
	Description string   `json:"description,omitempty"`
	State       string   `json:"state"`
}

type dynamoIntegrations struct {
	Triggers      []dynamoTriggerIntegration      `json:"triggers"`
	Lambdas       []dynamoLambdaIntegration       `json:"lambdas"`
	ECS           []dynamoECSIntegration          `json:"ecs"`
	StepFunctions []dynamoStepFunctionIntegration `json:"stepFunctions"`
	KMSKey        *dynamoKMSIntegration           `json:"kmsKey,omitempty"`
}

type dynamoTableDetailResponse struct {
	Table struct {
		Name                      string                      `json:"name"`
		Arn                       string                      `json:"arn"`
		ID                        string                      `json:"id"`
		Status                    string                      `json:"status"`
		CreatedDate               string                      `json:"createdDate"`
		BillingMode               string                      `json:"billingMode,omitempty"`
		TableClass                string                      `json:"tableClass,omitempty"`
		ItemCount                 int64                       `json:"itemCount"`
		TableSizeBytes            int64                       `json:"tableSizeBytes"`
		DeletionProtectionEnabled bool                        `json:"deletionProtectionEnabled"`
		AttributeDefinitions      []dynamoAttributeDefinition `json:"attributeDefinitions"`
		KeySchema                 []dynamoKeySchemaItem       `json:"keySchema"`
		ProvisionedThroughput     *dynamoThroughput           `json:"provisionedThroughput,omitempty"`
		LocalSecondaryIndexes     []dynamoLSIDetail           `json:"localSecondaryIndexes"`
		GlobalSecondaryIndexes    []dynamoGSIDetail           `json:"globalSecondaryIndexes"`
		StreamEnabled             bool                        `json:"streamEnabled"`
		StreamViewType            string                      `json:"streamViewType,omitempty"`
		LatestStreamArn           string                      `json:"latestStreamArn,omitempty"`
		LatestStreamLabel         string                      `json:"latestStreamLabel,omitempty"`
		SSEDescription            *dynamoSSEDetail            `json:"sseDescription,omitempty"`
	} `json:"table"`
	TTL                 dynamoTTLDetail            `json:"ttl"`
	ContinuousBackups   dynamoBackupsDetail        `json:"continuousBackups"`
	KinesisDestinations []dynamoKinesisDestination `json:"kinesisDestinations"`
	Tags                map[string]string          `json:"tags"`
	Stream              *dynamoStreamDetail        `json:"stream,omitempty"`
	Integrations        dynamoIntegrations         `json:"integrations"`
}

// DynamoTableDetail returns schema, indexes, TTL, backups, stream shards, tags,
// and deployed-infrastructure integrations for a single DynamoDB table.
func (h *Handler) DynamoTableDetail(w http.ResponseWriter, r *http.Request) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}

	table, err := h.dynamodb.DescribeTable(name)
	if err != nil {
		writeDynamoError(w, err)
		return
	}

	var resp dynamoTableDetailResponse
	resp.Table.Name = table.TableName
	resp.Table.Arn = table.TableArn
	resp.Table.ID = table.TableId
	resp.Table.Status = table.TableStatus
	resp.Table.CreatedDate = formatUnixFloatRFC3339(table.CreationDateTime)
	if table.BillingModeSummary != nil {
		resp.Table.BillingMode = table.BillingModeSummary.BillingMode
	}
	if table.TableClassSummary != nil {
		resp.Table.TableClass = table.TableClassSummary.TableClass
	}
	resp.Table.ItemCount = table.ItemCount
	resp.Table.TableSizeBytes = table.TableSizeBytes
	resp.Table.DeletionProtectionEnabled = table.DeletionProtectionEnabled

	resp.Table.AttributeDefinitions = make([]dynamoAttributeDefinition, 0, len(table.AttributeDefinitions))
	for _, ad := range table.AttributeDefinitions {
		resp.Table.AttributeDefinitions = append(resp.Table.AttributeDefinitions, dynamoAttributeDefinition{
			AttributeName: ad.AttributeName,
			AttributeType: ad.AttributeType,
		})
	}
	resp.Table.KeySchema = toDynamoKeySchema(table.KeySchema)
	if table.ProvisionedThroughput != nil {
		resp.Table.ProvisionedThroughput = &dynamoThroughput{
			ReadCapacityUnits:      table.ProvisionedThroughput.ReadCapacityUnits,
			WriteCapacityUnits:     table.ProvisionedThroughput.WriteCapacityUnits,
			NumberOfDecreasesToday: table.ProvisionedThroughput.NumberOfDecreasesToday,
		}
	}

	resp.Table.LocalSecondaryIndexes = make([]dynamoLSIDetail, 0, len(table.LocalSecondaryIndexes))
	for _, lsi := range table.LocalSecondaryIndexes {
		resp.Table.LocalSecondaryIndexes = append(resp.Table.LocalSecondaryIndexes, dynamoLSIDetail{
			IndexName: lsi.IndexName,
			IndexArn:  lsi.IndexArn,
			KeySchema: toDynamoKeySchema(lsi.KeySchema),
			Projection: dynamoProjection{
				ProjectionType:   lsi.Projection.ProjectionType,
				NonKeyAttributes: lsi.Projection.NonKeyAttributes,
			},
			ItemCount:      lsi.ItemCount,
			IndexSizeBytes: lsi.IndexSizeBytes,
		})
	}

	resp.Table.GlobalSecondaryIndexes = make([]dynamoGSIDetail, 0, len(table.GlobalSecondaryIndexes))
	for _, gsi := range table.GlobalSecondaryIndexes {
		var pt *dynamoThroughput
		if gsi.ProvisionedThroughput != nil {
			pt = &dynamoThroughput{
				ReadCapacityUnits:      gsi.ProvisionedThroughput.ReadCapacityUnits,
				WriteCapacityUnits:     gsi.ProvisionedThroughput.WriteCapacityUnits,
				NumberOfDecreasesToday: gsi.ProvisionedThroughput.NumberOfDecreasesToday,
			}
		}
		resp.Table.GlobalSecondaryIndexes = append(resp.Table.GlobalSecondaryIndexes, dynamoGSIDetail{
			IndexName:   gsi.IndexName,
			IndexArn:    gsi.IndexArn,
			IndexStatus: gsi.IndexStatus,
			KeySchema:   toDynamoKeySchema(gsi.KeySchema),
			Projection: dynamoProjection{
				ProjectionType:   gsi.Projection.ProjectionType,
				NonKeyAttributes: gsi.Projection.NonKeyAttributes,
			},
			ProvisionedThroughput: pt,
			ItemCount:             gsi.ItemCount,
			IndexSizeBytes:        gsi.IndexSizeBytes,
		})
	}

	if table.StreamSpecification != nil {
		resp.Table.StreamEnabled = table.StreamSpecification.StreamEnabled
		resp.Table.StreamViewType = table.StreamSpecification.StreamViewType
	}
	resp.Table.LatestStreamArn = table.LatestStreamArn
	resp.Table.LatestStreamLabel = table.LatestStreamLabel
	if table.SSEDescription != nil {
		resp.Table.SSEDescription = &dynamoSSEDetail{
			Status:          table.SSEDescription.Status,
			SSEType:         table.SSEDescription.SSEType,
			KMSMasterKeyArn: table.SSEDescription.KMSMasterKeyArn,
		}
	}

	// TTL
	resp.TTL = dynamoTTLDetail{Status: "DISABLED"}
	if ttlDesc, ttlErr := h.dynamodb.DescribeTimeToLive(table.TableName); ttlErr == nil {
		if inner, ok := ttlDesc["TimeToLiveDescription"].(map[string]any); ok {
			if status, _ := inner["TimeToLiveStatus"].(string); status != "" {
				resp.TTL.Status = status
			}
			if attr, _ := inner["AttributeName"].(string); attr != "" {
				resp.TTL.AttributeName = attr
			}
		}
	}

	// Continuous backups / PITR
	resp.ContinuousBackups = dynamoBackupsDetail{
		Status:                    "DISABLED",
		PointInTimeRecoveryStatus: "DISABLED",
	}
	if cbDesc, cbErr := h.dynamodb.DescribeContinuousBackups(table.TableName); cbErr == nil {
		if inner, ok := cbDesc["ContinuousBackupsDescription"].(map[string]any); ok {
			if status, _ := inner["ContinuousBackupsStatus"].(string); status != "" {
				resp.ContinuousBackups.Status = status
			}
			if pitr, ok := inner["PointInTimeRecoveryDescription"].(map[string]any); ok {
				if ps, _ := pitr["PointInTimeRecoveryStatus"].(string); ps != "" {
					resp.ContinuousBackups.PointInTimeRecoveryStatus = ps
				}
				if earliest, ok := pitr["EarliestRestorableDateTime"].(float64); ok && earliest > 0 {
					s := formatUnixFloatRFC3339(earliest)
					resp.ContinuousBackups.EarliestRestorableDateTime = &s
				}
				if latest, ok := pitr["LatestRestorableDateTime"].(float64); ok && latest > 0 {
					s := formatUnixFloatRFC3339(latest)
					resp.ContinuousBackups.LatestRestorableDateTime = &s
				}
			}
		}
	}

	// Kinesis streaming destinations
	resp.KinesisDestinations = []dynamoKinesisDestination{}
	if kinDesc, kinErr := h.dynamodb.DescribeKinesisStreamingDestination(table.TableName); kinErr == nil {
		if dests, ok := kinDesc["KinesisDataStreamDestinations"].([]map[string]any); ok {
			for _, d := range dests {
				streamArn, _ := d["StreamArn"].(string)
				status, _ := d["DestinationStatus"].(string)
				if streamArn != "" {
					resp.KinesisDestinations = append(resp.KinesisDestinations, dynamoKinesisDestination{
						StreamArn:         streamArn,
						DestinationStatus: status,
					})
				}
			}
		}
	}

	// Tags
	resp.Tags = map[string]string{}
	if tags, tagErr := h.dynamodb.ListTagsOfResource(table.TableArn); tagErr == nil {
		for _, t := range tags {
			if t.Key != "" {
				resp.Tags[t.Key] = t.Value
			}
		}
	}

	// Stream details
	if table.LatestStreamArn != "" {
		if desc, streamErr := h.dynamodb.DescribeStream(table.LatestStreamArn, 100, ""); streamErr == nil {
			if sd, ok := desc["StreamDescription"].(map[string]any); ok {
				stream := &dynamoStreamDetail{
					Arn:      table.LatestStreamArn,
					Label:    table.LatestStreamLabel,
					Status:   stringField(sd, "StreamStatus"),
					ViewType: stringField(sd, "StreamViewType"),
					Shards:   []dynamoStreamShardDetail{},
				}
				if shards, ok := sd["Shards"].([]map[string]any); ok {
					for _, sh := range shards {
						shard := dynamoStreamShardDetail{ShardID: stringField(sh, "ShardId")}
						if sr, ok := sh["SequenceNumberRange"].(map[string]any); ok {
							shard.StartingSequenceNumber = stringField(sr, "StartingSequenceNumber")
							shard.EndingSequenceNumber = stringField(sr, "EndingSequenceNumber")
						}
						stream.Shards = append(stream.Shards, shard)
					}
				}
				resp.Stream = stream
			}
		}
	}

	resp.Integrations = h.collectDynamoTableIntegrations(table)
	writeAdminJSON(w, http.StatusOK, resp)
}

func (h *Handler) collectDynamoTableIntegrations(table *types.DynamoDBTable) dynamoIntegrations {
	out := dynamoIntegrations{
		Triggers:      []dynamoTriggerIntegration{},
		Lambdas:       []dynamoLambdaIntegration{},
		ECS:           []dynamoECSIntegration{},
		StepFunctions: []dynamoStepFunctionIntegration{},
	}
	if table == nil {
		return out
	}

	tableNameLower := strings.ToLower(strings.TrimSpace(table.TableName))
	tableArnLower := strings.ToLower(strings.TrimSpace(table.TableArn))
	streamArnLower := strings.ToLower(strings.TrimSpace(table.LatestStreamArn))

	matchesRef := func(key, value string, requireDynamoKey bool) (string, bool) {
		val := strings.TrimSpace(value)
		if val == "" {
			return "", false
		}
		lower := strings.ToLower(val)
		if streamArnLower != "" && lower == streamArnLower {
			return "stream-arn", true
		}
		if tableArnLower != "" && lower == tableArnLower {
			return "table-arn", true
		}
		if strings.HasPrefix(lower, "arn:aws:dynamodb:") {
			if parsed := dynamoTableNameFromArn(val); strings.EqualFold(parsed, table.TableName) {
				if strings.Contains(lower, ":stream/") {
					return "stream-arn", true
				}
				return "table-arn", true
			}
			return "", false
		}
		if requireDynamoKey && !looksLikeDynamoEnvKey(strings.ToUpper(strings.TrimSpace(key))) {
			return "", false
		}
		if lower == tableNameLower {
			return "table-name", true
		}
		return "", false
	}

	// 1. Event Source Mappings (DynamoDB Streams -> Lambda)
	if h.esm != nil {
		for _, m := range h.esm.ListMappings("", "") {
			if normalizeEventSourceType(m) != "dynamodb-stream" {
				continue
			}
			sourceName := eventSourceDisplayName(m)
			srcLower := strings.ToLower(strings.TrimSpace(m.EventSourceArn))
			if !strings.EqualFold(sourceName, table.TableName) &&
				(streamArnLower == "" || srcLower != streamArnLower) &&
				(tableArnLower == "" || !strings.HasPrefix(srcLower, tableArnLower)) {
				continue
			}
			out.Triggers = append(out.Triggers, dynamoTriggerIntegration{
				UUID:           m.UUID,
				FunctionName:   m.FunctionName,
				State:          m.State,
				BatchSize:      m.BatchSize,
				EventSourceArn: m.EventSourceArn,
				LastResult:     m.LastProcessingResult,
				FilterCriteria: m.FilterCriteria,
				LogGroup:       "/aws/lambda/" + m.FunctionName,
			})
		}
		sort.Slice(out.Triggers, func(i, j int) bool {
			if out.Triggers[i].FunctionName == out.Triggers[j].FunctionName {
				return out.Triggers[i].UUID < out.Triggers[j].UUID
			}
			return out.Triggers[i].FunctionName < out.Triggers[j].FunctionName
		})
	}

	// 2. Lambda environment variable references
	if h.lambda != nil {
		if fns, err := h.lambda.ListFunctions(); err == nil {
			seenLambda := map[string]struct{}{}
			for _, fn := range fns {
				for k, v := range fn.Environment {
					matchType, ok := matchesRef(k, v, true)
					if !ok {
						continue
					}
					dedupe := fn.FunctionName + "|" + k
					if _, exists := seenLambda[dedupe]; exists {
						continue
					}
					seenLambda[dedupe] = struct{}{}
					out.Lambdas = append(out.Lambdas, dynamoLambdaIntegration{
						FunctionName: fn.FunctionName,
						Runtime:      string(fn.Runtime),
						State:        string(fn.State),
						EnvKey:       k,
						MatchType:    matchType,
						LogGroup:     "/aws/lambda/" + fn.FunctionName,
					})
				}
			}
			sort.Slice(out.Lambdas, func(i, j int) bool {
				if out.Lambdas[i].FunctionName == out.Lambdas[j].FunctionName {
					return out.Lambdas[i].EnvKey < out.Lambdas[j].EnvKey
				}
				return out.Lambdas[i].FunctionName < out.Lambdas[j].FunctionName
			})
		}
	}

	// 3. ECS Task Definitions & Services referencing the table
	if h.ecs != nil {
		ecsOverview := h.listECSOverview()
		servicesByTD := map[string][]ecsServiceSummary{}
		clusterNamesByArn := map[string]string{}
		for _, c := range ecsOverview.Clusters {
			clusterNamesByArn[c.Arn] = c.Name
		}
		for _, svc := range ecsOverview.Services {
			servicesByTD[svc.TaskDefinitionArn] = append(servicesByTD[svc.TaskDefinitionArn], svc)
		}

		// Inspect the latest revision per family plus any task definition actively bound to a service.
		inspectTDs := map[string]struct{}{}
		latestByFamily := map[string]ecsTaskDefinitionSummary{}
		for _, td := range ecsOverview.TaskDefinitions {
			prev, exists := latestByFamily[td.Family]
			if !exists || td.Revision > prev.Revision {
				latestByFamily[td.Family] = td
			}
			if len(servicesByTD[td.TaskDefinitionArn]) > 0 {
				inspectTDs[td.TaskDefinitionArn] = struct{}{}
			}
		}
		for _, td := range latestByFamily {
			inspectTDs[td.TaskDefinitionArn] = struct{}{}
		}

		tdArns := make([]string, 0, len(inspectTDs))
		for arn := range inspectTDs {
			tdArns = append(tdArns, arn)
		}
		sort.Strings(tdArns)

		for _, tdArn := range tdArns {
			described, err := h.ecs.DescribeTaskDefinition(tdArn, nil)
			if err != nil || described == nil || described.TaskDefinition == nil {
				continue
			}
			td := described.TaskDefinition
			for _, cd := range td.ContainerDefinitions {
				logGroup := "/ecs/" + td.Family
				if cd.LogConfiguration != nil {
					if g := strings.TrimSpace(cd.LogConfiguration.Options["awslogs-group"]); g != "" {
						logGroup = g
					}
				}
				for _, env := range cd.Environment {
					matchType, ok := matchesRef(env.Name, env.Value, true)
					if !ok {
						continue
					}
					svcs := servicesByTD[td.TaskDefinitionArn]
					if len(svcs) > 0 {
						for _, svc := range svcs {
							out.ECS = append(out.ECS, dynamoECSIntegration{
								Kind:              "service",
								Name:              svc.Name,
								Arn:               svc.Arn,
								ClusterArn:        svc.ClusterArn,
								ClusterName:       clusterNamesByArn[svc.ClusterArn],
								TaskDefinitionArn: td.TaskDefinitionArn,
								ContainerName:     cd.Name,
								EnvKey:            env.Name,
								MatchType:         matchType,
								Status:            svc.Status,
								DesiredCount:      svc.DesiredCount,
								RunningCount:      svc.RunningCount,
								LogGroup:          logGroup,
							})
						}
					} else {
						out.ECS = append(out.ECS, dynamoECSIntegration{
							Kind:              "task-definition",
							Name:              td.Family + ":" + strconv.Itoa(td.Revision),
							Arn:               td.TaskDefinitionArn,
							TaskDefinitionArn: td.TaskDefinitionArn,
							ContainerName:     cd.Name,
							EnvKey:            env.Name,
							MatchType:         matchType,
							Status:            td.Status,
							LogGroup:          logGroup,
						})
					}
				}
			}
		}
		sort.Slice(out.ECS, func(i, j int) bool {
			if out.ECS[i].Kind != out.ECS[j].Kind {
				return out.ECS[i].Kind < out.ECS[j].Kind
			}
			if out.ECS[i].Name != out.ECS[j].Name {
				return out.ECS[i].Name < out.ECS[j].Name
			}
			return out.ECS[i].EnvKey < out.ECS[j].EnvKey
		})
	}

	// 4. Step Functions state machines referencing DynamoDB APIs & this table
	if h.stepfunctions != nil {
		for _, sm := range h.stepfunctions.ListStateMachines() {
			matchType, stateNames := matchDynamoInASL(sm.Definition, table.TableName, table.TableArn)
			if matchType == "" {
				continue
			}
			out.StepFunctions = append(out.StepFunctions, dynamoStepFunctionIntegration{
				Name:       sm.Name,
				Arn:        sm.Arn,
				Type:       sm.Type,
				Status:     sm.Status,
				MatchType:  matchType,
				StateNames: stateNames,
			})
		}
		sort.Slice(out.StepFunctions, func(i, j int) bool {
			return out.StepFunctions[i].Name < out.StepFunctions[j].Name
		})
	}

	// 5. KMS key for SSE-KMS encrypted tables
	if h.kms != nil && table.SSEDescription != nil && table.SSEDescription.KMSMasterKeyArn != "" {
		if keyID, ok := h.kms.LookupKeyID(table.SSEDescription.KMSMasterKeyArn); ok {
			for _, ks := range h.kms.KeySummaries() {
				if ks.ID == keyID {
					aliases := ks.Aliases
					if aliases == nil {
						aliases = []string{}
					}
					out.KMSKey = &dynamoKMSIntegration{
						KeyID:       ks.ID,
						Arn:         ks.Arn,
						Aliases:     aliases,
						Description: ks.Description,
						State:       ks.State,
					}
					break
				}
			}
		}
	}

	return out
}

func matchDynamoInASL(definition, tableName, tableArn string) (string, []string) {
	if strings.TrimSpace(definition) == "" {
		return "", nil
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(definition), &doc); err != nil {
		return "", nil
	}
	states, ok := doc["States"].(map[string]any)
	if !ok {
		return "", nil
	}

	var matchedStates []string
	matchType := ""
	var walk func(v any) bool
	walk = func(v any) bool {
		switch node := v.(type) {
		case map[string]any:
			for k, val := range node {
				if strings.EqualFold(k, "TableName") {
					if str, ok := val.(string); ok {
						if strings.EqualFold(str, tableName) || (tableArn != "" && strings.EqualFold(str, tableArn)) {
							if matchType == "" {
								matchType = "asl-table-name"
							}
							return true
						}
					}
				}
				if walk(val) {
					return true
				}
			}
		case []any:
			for _, elem := range node {
				if walk(elem) {
					return true
				}
			}
		case string:
			if tableArn != "" && strings.Contains(strings.ToLower(node), strings.ToLower(tableArn)) {
				matchType = "asl-table-arn"
				return true
			}
		}
		return false
	}

	for stateName, rawState := range states {
		stateMap, ok := rawState.(map[string]any)
		if !ok {
			continue
		}
		res, _ := stateMap["Resource"].(string)
		hasDynamoResource := strings.Contains(strings.ToLower(res), ":dynamodb:")
		if walk(stateMap) || (hasDynamoResource && containsCaseInsensitive(rawState, tableName)) {
			if hasDynamoResource && matchType == "" {
				matchType = "asl-dynamodb-task"
			}
			matchedStates = append(matchedStates, stateName)
		}
	}
	sort.Strings(matchedStates)
	return matchType, matchedStates
}

func containsCaseInsensitive(v any, needle string) bool {
	if needle == "" {
		return false
	}
	switch node := v.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(node), needle)
	case map[string]any:
		for _, val := range node {
			if containsCaseInsensitive(val, needle) {
				return true
			}
		}
	case []any:
		for _, elem := range node {
			if containsCaseInsensitive(elem, needle) {
				return true
			}
		}
	}
	return false
}

type dynamoReadRequest struct {
	IndexName                 string            `json:"indexName"`
	KeyConditionExpression    string            `json:"keyConditionExpression"`
	FilterExpression          string            `json:"filterExpression"`
	ProjectionExpression      string            `json:"projectionExpression"`
	ExpressionAttributeNames  map[string]string `json:"expressionAttributeNames"`
	ExpressionAttributeValues map[string]any    `json:"expressionAttributeValues"`
	Limit                     int               `json:"limit"`
	ExclusiveStartKey         map[string]any    `json:"exclusiveStartKey"`
	ScanIndexForward          *bool             `json:"scanIndexForward"`
	Select                    string            `json:"select"`
}

type dynamoReadResponse struct {
	Items            []map[string]any `json:"items"`
	Count            int              `json:"count"`
	ScannedCount     int              `json:"scannedCount"`
	LastEvaluatedKey map[string]any   `json:"lastEvaluatedKey,omitempty"`
}

// DynamoScan scans a table or secondary index for the built-in viewer.
func (h *Handler) DynamoScan(w http.ResponseWriter, r *http.Request) {
	h.handleDynamoRead(w, r, false)
}

// DynamoQuery queries a table or secondary index for the built-in viewer.
func (h *Handler) DynamoQuery(w http.ResponseWriter, r *http.Request) {
	h.handleDynamoRead(w, r, true)
}

func (h *Handler) handleDynamoRead(w http.ResponseWriter, r *http.Request, isQuery bool) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}
	var req dynamoReadRequest
	if !decodeJSONBody(w, r, &req, !isQuery) {
		return
	}

	input := &dynamodbsvc.ReadInput{
		TableName:                 name,
		IndexName:                 strings.TrimSpace(req.IndexName),
		KeyConditionExpression:    req.KeyConditionExpression,
		FilterExpression:          req.FilterExpression,
		ProjectionExpression:      req.ProjectionExpression,
		ExpressionAttributeNames:  req.ExpressionAttributeNames,
		ExpressionAttributeValues: req.ExpressionAttributeValues,
		Limit:                     clampDynamoLimit(req.Limit, defaultDynamoReadLimit, maxDynamoReadLimit),
		ExclusiveStartKey:         req.ExclusiveStartKey,
		ScanIndexForward:          req.ScanIndexForward,
		Select:                    strings.TrimSpace(req.Select),
	}
	var (
		res *dynamodbsvc.ReadResult
		err error
	)
	if isQuery {
		res, err = h.dynamodb.Query(input)
	} else {
		res, err = h.dynamodb.Scan(input)
	}
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	items := res.Items
	if items == nil {
		items = []map[string]any{}
	}
	writeAdminJSON(w, http.StatusOK, dynamoReadResponse{
		Items:            items,
		Count:            res.Count,
		ScannedCount:     res.ScannedCount,
		LastEvaluatedKey: res.LastEvaluatedKey,
	})
}

// DynamoGetItem retrieves a single item by primary key.
func (h *Handler) DynamoGetItem(w http.ResponseWriter, r *http.Request) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}
	var req struct {
		Key                      map[string]any    `json:"key"`
		ProjectionExpression     string            `json:"projectionExpression"`
		ExpressionAttributeNames map[string]string `json:"expressionAttributeNames"`
	}
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	item, err := h.dynamodb.GetItem(name, req.Key, req.ProjectionExpression, req.ExpressionAttributeNames)
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"item":  item,
		"found": item != nil,
	})
}

type dynamoWriteItemRequest struct {
	Item                      map[string]any    `json:"item"`
	Key                       map[string]any    `json:"key"`
	UpdateExpression          string            `json:"updateExpression"`
	ConditionExpression       string            `json:"conditionExpression"`
	ExpressionAttributeNames  map[string]string `json:"expressionAttributeNames"`
	ExpressionAttributeValues map[string]any    `json:"expressionAttributeValues"`
	ReturnValues              string            `json:"returnValues"`
}

// DynamoPutItem creates or replaces an item in a table.
func (h *Handler) DynamoPutItem(w http.ResponseWriter, r *http.Request) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}
	var req dynamoWriteItemRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	attrs, err := h.dynamodb.PutItem(&dynamodbsvc.WriteInput{
		TableName:                 name,
		Item:                      req.Item,
		ConditionExpression:       req.ConditionExpression,
		ExpressionAttributeNames:  req.ExpressionAttributeNames,
		ExpressionAttributeValues: req.ExpressionAttributeValues,
		ReturnValues:              req.ReturnValues,
	})
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"attributes": attrs,
	})
}

// DynamoUpdateItem updates an item in a table using UpdateExpression.
func (h *Handler) DynamoUpdateItem(w http.ResponseWriter, r *http.Request) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}
	var req dynamoWriteItemRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	attrs, err := h.dynamodb.UpdateItem(&dynamodbsvc.WriteInput{
		TableName:                 name,
		Key:                       req.Key,
		UpdateExpression:          req.UpdateExpression,
		ConditionExpression:       req.ConditionExpression,
		ExpressionAttributeNames:  req.ExpressionAttributeNames,
		ExpressionAttributeValues: req.ExpressionAttributeValues,
		ReturnValues:              req.ReturnValues,
	})
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"attributes": attrs,
	})
}

// DynamoDeleteItem deletes an item from a table by primary key.
func (h *Handler) DynamoDeleteItem(w http.ResponseWriter, r *http.Request) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}
	var req dynamoWriteItemRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	attrs, err := h.dynamodb.DeleteItem(&dynamodbsvc.WriteInput{
		TableName:                 name,
		Key:                       req.Key,
		ConditionExpression:       req.ConditionExpression,
		ExpressionAttributeNames:  req.ExpressionAttributeNames,
		ExpressionAttributeValues: req.ExpressionAttributeValues,
		ReturnValues:              req.ReturnValues,
	})
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"attributes": attrs,
	})
}

// DynamoStreamRecords fetches stream records for a table's latest stream.
func (h *Handler) DynamoStreamRecords(w http.ResponseWriter, r *http.Request) {
	name, ok := h.requireDynamoTable(w, r)
	if !ok {
		return
	}

	table, err := h.dynamodb.DescribeTable(name)
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	if table.LatestStreamArn == "" {
		writeError(w, http.StatusBadRequest, "dynamodb stream is not enabled on table "+table.TableName)
		return
	}

	q := r.URL.Query()
	limit := defaultDynamoStreamLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil {
			limit = clampDynamoLimit(parsed, defaultDynamoStreamLimit, maxDynamoStreamLimit)
		}
	}

	shardID := strings.TrimSpace(q.Get("shardId"))
	if shardID == "" {
		if desc, descErr := h.dynamodb.DescribeStream(table.LatestStreamArn, 10, ""); descErr == nil {
			if sd, ok := desc["StreamDescription"].(map[string]any); ok {
				if shards, ok := sd["Shards"].([]map[string]any); ok && len(shards) > 0 {
					shardID = stringField(shards[0], "ShardId")
				}
			}
		}
	}
	if shardID == "" {
		shardID = "shardId-000000000000"
	}

	iterator := strings.TrimSpace(q.Get("shardIterator"))
	if iterator == "" {
		iteratorType := strings.ToUpper(strings.TrimSpace(q.Get("iteratorType")))
		if iteratorType == "" {
			iteratorType = "TRIM_HORIZON"
		}
		seqNum := strings.TrimSpace(q.Get("sequenceNumber"))
		iterator, err = h.dynamodb.GetShardIterator(table.LatestStreamArn, shardID, iteratorType, seqNum)
		if err != nil {
			writeDynamoError(w, err)
			return
		}
	}

	records, nextIterator, err := h.dynamodb.GetRecords(iterator, limit)
	if err != nil {
		writeDynamoError(w, err)
		return
	}
	if records == nil {
		records = []*types.StreamRecord{}
	}
	viewType := ""
	if table.StreamSpecification != nil {
		viewType = table.StreamSpecification.StreamViewType
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"streamArn":         table.LatestStreamArn,
		"streamViewType":    viewType,
		"shardId":           shardID,
		"records":           records,
		"nextShardIterator": nextIterator,
	})
}

func (h *Handler) requireDynamoTable(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.dynamodb == nil {
		writeError(w, http.StatusServiceUnavailable, "dynamodb service unavailable")
		return "", false
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "table name is required")
		return "", false
	}
	return name, true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) bool {
	if allowEmpty && (r.Body == nil || r.ContentLength == 0) {
		return true
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request JSON")
		return false
	}
	return true
}

func toDynamoKeySchema(in []types.DynamoDBKeySchemaElement) []dynamoKeySchemaItem {
	out := make([]dynamoKeySchemaItem, 0, len(in))
	for _, ks := range in {
		out = append(out, dynamoKeySchemaItem{
			AttributeName: ks.AttributeName,
			KeyType:       ks.KeyType,
		})
	}
	return out
}

func clampDynamoLimit(value, def, max int) int {
	if value <= 0 {
		return def
	}
	if value > max {
		return max
	}
	return value
}

func formatUnixFloatRFC3339(ts float64) string {
	if ts <= 0 {
		return ""
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * float64(time.Second))
	return time.Unix(sec, nsec).UTC().Format(time.RFC3339)
}

func writeDynamoError(w http.ResponseWriter, err error) {
	var svcErr *dynamodbsvc.ServiceError
	if errors.As(err, &svcErr) {
		status := svcErr.StatusCode()
		if svcErr.Code == "ResourceNotFoundException" {
			status = http.StatusNotFound
		}
		writeAdminJSON(w, status, map[string]string{
			"error": svcErr.Message,
			"code":  svcErr.Code,
		})
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeAdminJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
