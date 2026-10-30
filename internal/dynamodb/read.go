package dynamodb

import (
	"hash/fnv"
	"slices"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// maxPageBytes is the most data one Scan or Query page reads.
const maxPageBytes = 1024 * 1024

// ReadInput is the request shape shared by Scan and Query.
type ReadInput struct {
	TableName                 string            `json:"TableName"`
	IndexName                 string            `json:"IndexName"`
	KeyConditionExpression    string            `json:"KeyConditionExpression"`
	FilterExpression          string            `json:"FilterExpression"`
	ProjectionExpression      string            `json:"ProjectionExpression"`
	ExpressionAttributeNames  map[string]string `json:"ExpressionAttributeNames"`
	ExpressionAttributeValues map[string]any    `json:"ExpressionAttributeValues"`
	Limit                     int               `json:"Limit"`
	ExclusiveStartKey         map[string]any    `json:"ExclusiveStartKey"`
	ScanIndexForward          *bool             `json:"ScanIndexForward"`
	Select                    string            `json:"Select"`
	Segment                   *int              `json:"Segment"`
	TotalSegments             int               `json:"TotalSegments"`
}

// ReadResult is one page of a Scan or Query. Items is nil for Select COUNT.
type ReadResult struct {
	Items            []map[string]any
	Count            int
	ScannedCount     int
	LastEvaluatedKey map[string]any
}

// Scan reads a page of the table or index in key order.
func (s *Store) Scan(in *ReadInput) (*ReadResult, error) {
	if in.KeyConditionExpression != "" || in.ScanIndexForward != nil {
		return nil, validationError("KeyConditionExpression and ScanIndexForward are not valid for Scan")
	}
	if (in.Segment == nil) != (in.TotalSegments == 0) {
		return nil, validationError("The TotalSegments and Segment parameters must be supplied together")
	}
	if in.Segment != nil && (in.TotalSegments < 1 || in.TotalSegments > 1000000 || *in.Segment < 0 || *in.Segment >= in.TotalSegments) {
		return nil, validationError("Segment must be less than TotalSegments, which must be between 1 and 1000000")
	}
	return s.read(in, false)
}

// Query reads a page of one partition of the table or index.
func (s *Store) Query(in *ReadInput) (*ReadResult, error) {
	if in.KeyConditionExpression == "" {
		return nil, validationError("Either the KeyConditions or KeyConditionExpression parameter must be specified in the request.")
	}
	if in.Segment != nil || in.TotalSegments != 0 {
		return nil, validationError("Segment and TotalSegments are not valid for Query")
	}
	return s.read(in, true)
}

// read walks items in index order. Limit and the 1 MB cap count items
// evaluated, before FilterExpression, as DynamoDB does.
func (s *Store) read(in *ReadInput, query bool) (*ReadResult, error) {
	switch in.Select {
	case "", "ALL_ATTRIBUTES", "ALL_PROJECTED_ATTRIBUTES", "COUNT":
	case "SPECIFIC_ATTRIBUTES":
		if in.ProjectionExpression == "" {
			return nil, validationError("SPECIFIC_ATTRIBUTES requires a ProjectionExpression")
		}
	default:
		return nil, validationError("Unsupported Select value: %s", in.Select)
	}
	if in.Select != "" && in.Select != "SPECIFIC_ATTRIBUTES" && in.ProjectionExpression != "" {
		return nil, validationError("Cannot specify the ProjectionExpression with a Select value other than SPECIFIC_ATTRIBUTES")
	}
	if in.Limit < 0 {
		return nil, validationError("Limit must be greater than or equal to 1")
	}
	values, err := canonicalizeItem(in.ExpressionAttributeValues)
	if err != nil {
		return nil, err
	}
	names := in.ExpressionAttributeNames

	var (
		tableKeys  []types.DynamoDBKeySchemaElement
		indexKeys  []types.DynamoDBKeySchemaElement
		projection *types.DynamoDBProjection
		views      []itemView
	)
	reverse := in.ScanIndexForward != nil && !*in.ScanIndexForward
	err = s.readTable(in.TableName, func(state *tableState) error {
		tableKeys = slices.Clone(state.Table.KeySchema)
		var err error
		if indexKeys, projection, err = lookupIndex(state.Table, in.IndexName); err != nil {
			return err
		}
		views, err = s.collectSortedItems(state, in.IndexName, reverse)
		return err
	})
	if err != nil {
		return nil, err
	}
	projectedKeys := unionKeySchema(tableKeys, indexKeys)

	out := &ReadResult{}
	if in.Select != "COUNT" {
		out.Items = make([]map[string]any, 0)
	}
	limit := in.Limit
	if limit == 0 {
		limit = len(views) + 1
	}
	start, err := findExclusiveStart(views, projectedKeys, in.ExclusiveStartKey, reverse)
	if err != nil {
		return nil, err
	}
	bytes := 0
	for i := start; i < len(views); i++ {
		view := views[i]
		if in.Segment != nil && segmentOf(view.key, tableKeys, in.TotalSegments) != *in.Segment {
			continue
		}
		if query {
			ok, err := evaluateConditionExpression(in.KeyConditionExpression, view.item, names, values)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		out.ScannedCount++
		bytes += itemSize(view.item)
		ok, err := evaluateConditionExpression(in.FilterExpression, view.item, names, values)
		if err != nil {
			return nil, err
		}
		if ok {
			out.Count++
			if out.Items != nil {
				item := view.item
				if projection != nil {
					item = applyIndexProjection(item, *projection, projectedKeys)
				}
				if in.ProjectionExpression != "" {
					if item, err = applyProjectionExpression(item, in.ProjectionExpression, names); err != nil {
						return nil, err
					}
				}
				out.Items = append(out.Items, item)
			}
		}
		if (out.ScannedCount >= limit || bytes >= maxPageBytes) && i+1 < len(views) {
			out.LastEvaluatedKey = cloneItem(view.key)
			break
		}
	}
	return out, nil
}

// segmentOf assigns an item to one parallel-scan segment by its primary key.
func segmentOf(key map[string]any, schema []types.DynamoDBKeySchemaElement, total int) int {
	id, _ := serializeKey(key, schema)
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return int(h.Sum32() % uint32(total))
}
