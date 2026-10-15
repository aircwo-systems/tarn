package logs

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var logGroupNamePattern = regexp.MustCompile(`^[.\-_/#A-Za-z0-9]{1,512}$`)
var retentionDays = []int{1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653}

// DispatchAWS serves the CloudWatch Logs log group lifecycle used by Terraform.
// Log data and metadata share the account's existing in-memory log store.
func (s *Service) DispatchAWS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.Header().Set("X-Amzn-RequestId", uuid.NewString())
	fail := func(code, message string) {
		w.Header().Set("X-Amzn-ErrorType", code)
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"__type": code, "message": message})
	}
	var req struct {
		LogGroupName        string          `json:"logGroupName"`
		LogGroupNamePrefix  string          `json:"logGroupNamePrefix"`
		LogGroupNamePattern string          `json:"logGroupNamePattern"`
		LogGroupClass       string          `json:"logGroupClass"`
		ResourceArn         string          `json:"resourceArn"`
		RetentionInDays     int             `json:"retentionInDays"`
		Tags                json.RawMessage `json:"tags"`
		TagKeys             []string        `json:"tagKeys"`
		Limit               int             `json:"limit"`
		NextToken           string          `json:"nextToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail("InvalidParameterException", "Invalid JSON body")
		return
	}
	action := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "Logs_20140328.")
	tags := map[string]string{}
	if len(req.Tags) > 0 {
		if action == "UntagLogGroup" {
			if err := json.Unmarshal(req.Tags, &req.TagKeys); err != nil {
				fail("InvalidParameterException", "tags must be an array of keys")
				return
			}
		} else {
			if err := json.Unmarshal(req.Tags, &tags); err != nil {
				fail("InvalidParameterException", "tags must be a map")
				return
			}
		}
	}
	arnPrefix := fmt.Sprintf("arn:aws:logs:%s:%s:log-group:", s.cfg.Region, s.cfg.AccountID)
	name := req.LogGroupName
	if req.ResourceArn != "" {
		if !strings.HasPrefix(req.ResourceArn, arnPrefix) {
			fail("ResourceNotFoundException", "Log group not found")
			return
		}
		name = strings.TrimSuffix(strings.TrimPrefix(req.ResourceArn, arnPrefix), ":*")
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	group := s.store.groups[name]
	reply := map[string]any{}
	switch action {
	case "CreateLogGroup":
		if !logGroupNamePattern.MatchString(name) || strings.HasPrefix(name, "aws/") {
			fail("InvalidParameterException", "Invalid log group name")
			return
		}
		if group != nil {
			fail("ResourceAlreadyExistsException", "Log group already exists")
			return
		}
		if req.LogGroupClass != "" && req.LogGroupClass != "STANDARD" {
			fail("InvalidParameterException", "Only STANDARD log groups are supported")
			return
		}
		s.store.groups[name] = &logGroup{name: name, createdAt: time.Now().UTC(), streams: map[string]*LogStream{}, maxEvents: s.store.maxEvents, tags: maps.Clone(tags)}
	case "DescribeLogGroups":
		if req.LogGroupNamePrefix != "" && req.LogGroupNamePattern != "" {
			fail("InvalidParameterException", "Prefix and pattern are mutually exclusive")
			return
		}
		limit := req.Limit
		if limit == 0 {
			limit = 50
		}
		if limit < 1 || limit > 50 {
			fail("InvalidParameterException", "limit must be between 1 and 50")
			return
		}
		offset := 0
		if req.NextToken != "" {
			var err error
			offset, err = strconv.Atoi(req.NextToken)
			if err != nil || offset < 0 {
				fail("InvalidParameterException", "Invalid nextToken")
				return
			}
		}
		names := []string{}
		for n := range s.store.groups {
			if strings.HasPrefix(n, req.LogGroupNamePrefix) && strings.Contains(n, req.LogGroupNamePattern) && (req.LogGroupClass == "" || req.LogGroupClass == "STANDARD") {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		groups := []any{}
		if offset > len(names) {
			fail("InvalidParameterException", "Invalid nextToken")
			return
		}
		end := min(offset+limit, len(names))
		for _, n := range names[offset:end] {
			g := s.store.groups[n]
			item := map[string]any{"logGroupName": n, "arn": arnPrefix + n + ":*", "creationTime": g.createdAt.UnixMilli()}
			if req.LogGroupNamePattern == "" {
				item["logGroupArn"] = arnPrefix + n
				item["logGroupClass"] = "STANDARD"
				item["metricFilterCount"] = 0
				item["storedBytes"] = 0
				if g.retentionInDays > 0 {
					item["retentionInDays"] = g.retentionInDays
				}
			}
			groups = append(groups, item)
		}
		reply["logGroups"] = groups
		if end < len(names) {
			reply["nextToken"] = strconv.Itoa(end)
		}
	case "DeleteLogGroup", "PutRetentionPolicy", "DeleteRetentionPolicy", "TagResource", "UntagResource", "ListTagsForResource", "TagLogGroup", "UntagLogGroup", "ListTagsLogGroup":
		if group == nil {
			fail("ResourceNotFoundException", "Log group not found")
			return
		}
		switch action {
		case "DeleteLogGroup":
			delete(s.store.groups, name)
		case "PutRetentionPolicy":
			if !slices.Contains(retentionDays, req.RetentionInDays) {
				fail("InvalidParameterException", "Invalid retentionInDays")
				return
			}
			group.retentionInDays = req.RetentionInDays
		case "DeleteRetentionPolicy":
			group.retentionInDays = 0
		case "TagResource", "TagLogGroup":
			if group.tags == nil {
				group.tags = map[string]string{}
			}
			maps.Copy(group.tags, tags)
		case "UntagResource", "UntagLogGroup":
			for _, key := range req.TagKeys {
				delete(group.tags, key)
			}
		case "ListTagsForResource", "ListTagsLogGroup":
			tags := maps.Clone(group.tags)
			if tags == nil {
				tags = map[string]string{}
			}
			reply["tags"] = tags
		}
	default:
		fail("UnknownOperationException", "Unsupported CloudWatch Logs operation: "+action)
		return
	}
	json.NewEncoder(w).Encode(reply)
}
