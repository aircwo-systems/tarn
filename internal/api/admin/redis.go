package admin

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	infrasvc "github.com/aircwo-systems/tarn/internal/infrastructure"
	"github.com/aircwo-systems/tarn/internal/redisviewer"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

type redisSettingsPayload struct {
	Enabled bool `json:"enabled"`
}

type redisAuthPayload struct {
	DB       int    `json:"db"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	TLS      bool   `json:"tls,omitempty"`
}

type redisTargetSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	Host      string  `json:"host"`
	Port      int     `json:"port"`
	Status    string  `json:"status"`
	LatencyMs float64 `json:"latencyMs"`
	Version   string  `json:"version,omitempty"`
	Error     string  `json:"error,omitempty"`
	ProbedAt  string  `json:"probedAt"`
	Source    string  `json:"source,omitempty"`
	TLS       bool    `json:"tls,omitempty"`
}

type redisLambdaIntegration struct {
	FunctionName string `json:"functionName"`
	Runtime      string `json:"runtime"`
	State        string `json:"state"`
	Evidence     string `json:"evidence"`
	Source       string `json:"source"`
	LogGroup     string `json:"logGroup,omitempty"`
}

type redisECSIntegration struct {
	Kind              string `json:"kind"` // "service" | "task-definition"
	Name              string `json:"name"`
	Arn               string `json:"arn"`
	ClusterArn        string `json:"clusterArn,omitempty"`
	ClusterName       string `json:"clusterName,omitempty"`
	TaskDefinitionArn string `json:"taskDefinitionArn"`
	ContainerName     string `json:"containerName"`
	EnvKey            string `json:"envKey"`
	Status            string `json:"status"`
	LogGroup          string `json:"logGroup,omitempty"`
}

type redisSecretIntegration struct {
	Name        string `json:"name"`
	Arn         string `json:"arn"`
	Description string `json:"description,omitempty"`
}

type redisTraceIntegration struct {
	ID         string `json:"id"`
	StartedAt  string `json:"startedAt"`
	DurationMs int64  `json:"durationMs"`
	Status     int    `json:"status"`
	Method     string `json:"method,omitempty"`
	Path       string `json:"path,omitempty"`
	Reason     string `json:"reason"` // "redis-span" | "linked-function"
}

type redisIntegrations struct {
	Lambdas   []redisLambdaIntegration `json:"lambdas"`
	ECS       []redisECSIntegration    `json:"ecs"`
	Secrets   []redisSecretIntegration `json:"secrets"`
	Traces    []redisTraceIntegration  `json:"traces"`
	LogGroups []string                 `json:"logGroups"`
}

type redisTargetDetailResponse struct {
	Target       redisTargetSummary      `json:"target"`
	Info         *redisviewer.ServerInfo `json:"info,omitempty"`
	InfoError    string                  `json:"infoError,omitempty"`
	Integrations redisIntegrations       `json:"integrations"`
}

func (h *Handler) redisViewerEnabled() bool {
	if h.redisViewer != nil {
		return h.redisViewer.Enabled()
	}
	return h.cfg != nil && h.cfg.RedisViewerEnabled
}

func (h *Handler) requireRedisViewer(w http.ResponseWriter) bool {
	if !h.redisViewerEnabled() {
		writeError(w, http.StatusForbidden, "Redis viewer is disabled. Enable it in Settings or start Tarn with --redis-viewer / --rv.")
		return false
	}
	return true
}

func (h *Handler) invalidateOverviewCache() {
	h.overview.mu.Lock()
	h.overview.computed = time.Time{}
	h.overview.mu.Unlock()
}

// RedisViewerSettings returns whether the conditional Redis viewer is enabled.
func (h *Handler) RedisViewerSettings(w http.ResponseWriter, _ *http.Request) {
	writeAdminJSON(w, http.StatusOK, redisSettingsPayload{Enabled: h.redisViewerEnabled()})
}

// SetRedisViewerSettings enables or disables the Redis viewer at runtime.
func (h *Handler) SetRedisViewerSettings(w http.ResponseWriter, r *http.Request) {
	var req redisSettingsPayload
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	if h.redisViewer != nil {
		if err := h.redisViewer.SetEnabled(req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if h.cfg != nil {
		h.cfg.RedisViewerEnabled = req.Enabled
	}
	h.invalidateOverviewCache()
	writeAdminJSON(w, http.StatusOK, redisSettingsPayload{Enabled: h.redisViewerEnabled()})
}

func isRedisProbe(p infrasvc.ProbeResult) bool {
	k := strings.ToLower(strings.TrimSpace(p.Kind))
	return k == "redis" || k == "rediss"
}

// resolveRedisProbe finds a probed or user-registered Redis target strictly from
// Tarn's infrastructure registry so callers cannot dial arbitrary hosts (SSRF).
func (h *Handler) resolveRedisProbe(w http.ResponseWriter, r *http.Request) (infrasvc.ProbeResult, bool) {
	if !h.requireRedisViewer(w) {
		return infrasvc.ProbeResult{}, false
	}
	if h.infra == nil {
		writeError(w, http.StatusServiceUnavailable, "infrastructure service unavailable")
		return infrasvc.ProbeResult{}, false
	}
	rawID := r.PathValue("id")
	if decoded, err := url.PathUnescape(rawID); err == nil {
		rawID = decoded
	}
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		writeError(w, http.StatusBadRequest, "redis target id is required")
		return infrasvc.ProbeResult{}, false
	}

	for _, p := range h.infra.ResultsForAccount(h.cfg.AccountID) {
		if !isRedisProbe(p) {
			continue
		}
		if infraProbeID(p.Kind, p.Host, p.Port) == rawID {
			return p, true
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("redis target %q not found", rawID))
	return infrasvc.ProbeResult{}, false
}

func redisConnFromProbe(p infrasvc.ProbeResult, auth redisAuthPayload) redisviewer.ConnConfig {
	useTLS := auth.TLS || strings.EqualFold(p.Kind, "rediss")
	return redisviewer.ConnConfig{
		Host:     p.Host,
		Port:     p.Port,
		DB:       auth.DB,
		Username: auth.Username,
		Password: auth.Password,
		TLS:      useTLS,
	}
}

func parseRedisAuthFromQuery(r *http.Request) redisAuthPayload {
	q := r.URL.Query()
	db, _ := strconv.Atoi(strings.TrimSpace(q.Get("db")))
	if db < 0 || db > 15 {
		db = 0
	}
	useTLS, _ := strconv.ParseBool(strings.TrimSpace(q.Get("tls")))
	return redisAuthPayload{
		DB:       db,
		Username: r.Header.Get("X-Tarn-Redis-Username"),
		Password: r.Header.Get("X-Tarn-Redis-Password"),
		TLS:      useTLS,
	}
}

// RedisTargetDetail returns live Redis INFO, keyspace stats, and Tarn integrations
// (Lambdas, ECS services, Secrets, Traces, and Log Groups) for one Redis target.
func (h *Handler) RedisTargetDetail(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}

	auth := parseRedisAuthFromQuery(r)
	cfg := redisConnFromProbe(probe, auth)

	resp := redisTargetDetailResponse{
		Target: redisTargetSummary{
			ID:        infraProbeID(probe.Kind, probe.Host, probe.Port),
			Name:      probe.Name,
			Kind:      probe.Kind,
			Host:      probe.Host,
			Port:      probe.Port,
			Status:    probe.Status,
			LatencyMs: probe.LatencyMs,
			Version:   probe.Version,
			Error:     probe.Error,
			ProbedAt:  probe.ProbedAt,
			Source:    probe.Source,
			TLS:       cfg.TLS,
		},
		Integrations: h.collectRedisIntegrations(probe),
	}

	info, err := redisviewer.FetchServerInfo(r.Context(), cfg)
	if err != nil {
		resp.InfoError = err.Error()
	} else {
		resp.Info = info
		if info.Version != "" && resp.Target.Version == "" {
			if info.Engine == "valkey" {
				resp.Target.Version = "Valkey " + info.Version
			} else {
				resp.Target.Version = info.Version
			}
		}
		if resp.Target.Status != "connected" {
			resp.Target.Status = "connected"
			resp.Target.Error = ""
		}
	}

	writeAdminJSON(w, http.StatusOK, resp)
}

func (h *Handler) collectRedisIntegrations(probe infrasvc.ProbeResult) redisIntegrations {
	out := redisIntegrations{
		Lambdas:   []redisLambdaIntegration{},
		ECS:       []redisECSIntegration{},
		Secrets:   []redisSecretIntegration{},
		Traces:    []redisTraceIntegration{},
		LogGroups: []string{},
	}

	logGroupSet := map[string]struct{}{}
	linkedFunctions := map[string]struct{}{}

	// 1. Secrets referencing this Redis target
	var secretsRaw []*types.Secret
	if h.secrets != nil {
		secretsRaw = h.secrets.ListSecrets()
		for _, sec := range secretsRaw {
			if val, err := h.secrets.GetSecretValue(sec.Name); err == nil && val != nil && val.SecretString != "" {
				matched := false
				for _, hint := range inferSecretConnectionHints(sec.Name, val.SecretString) {
					if probeMatchesHint(probe, hint) {
						matched = true
						break
					}
				}
				if !matched {
					if kind, host, port, ok := parseDBConnectionValue(val.SecretString, "redis"); ok {
						if probeMatchesHint(probe, connectionHint{kind: kind, host: host, port: port}) {
							matched = true
						}
					}
				}
				if matched {
					out.Secrets = append(out.Secrets, redisSecretIntegration{
						Name:        sec.Name,
						Arn:         sec.ARN,
						Description: sec.Description,
					})
				}
			}
		}
		sort.Slice(out.Secrets, func(i, j int) bool { return out.Secrets[i].Name < out.Secrets[j].Name })
	}

	// 2. Lambda functions referencing this Redis target (env vars or secrets)
	if h.lambda != nil {
		if fns, err := h.lambda.ListFunctions(); err == nil {
			fnMeta := make(map[string]struct{ runtime, state string }, len(fns))
			for _, fn := range fns {
				fnMeta[fn.FunctionName] = struct{ runtime, state string }{string(fn.Runtime), string(fn.State)}
			}
			conns := inferInfraConnections(fns, []infrasvc.ProbeResult{probe}, h.secretValueLookup(secretsRaw))
			seen := map[string]struct{}{}
			for _, c := range conns {
				dedupe := c.SourceFunction + "|" + c.Source
				if _, ok := seen[dedupe]; ok {
					continue
				}
				seen[dedupe] = struct{}{}
				meta := fnMeta[c.SourceFunction]
				lg := "/aws/lambda/" + c.SourceFunction
				logGroupSet[lg] = struct{}{}
				linkedFunctions[c.SourceFunction] = struct{}{}
				out.Lambdas = append(out.Lambdas, redisLambdaIntegration{
					FunctionName: c.SourceFunction,
					Runtime:      meta.runtime,
					State:        meta.state,
					Evidence:     c.Evidence,
					Source:       c.Source,
					LogGroup:     lg,
				})
			}
			sort.Slice(out.Lambdas, func(i, j int) bool {
				if out.Lambdas[i].FunctionName == out.Lambdas[j].FunctionName {
					return out.Lambdas[i].Source < out.Lambdas[j].Source
				}
				return out.Lambdas[i].FunctionName < out.Lambdas[j].FunctionName
			})
		}
	}

	// 3. ECS services & task definitions referencing this Redis target
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

		for tdArn := range inspectTDs {
			described, err := h.ecs.DescribeTaskDefinition(tdArn, nil)
			if err != nil || described == nil || described.TaskDefinition == nil {
				continue
			}
			td := described.TaskDefinition
			for _, cd := range td.ContainerDefinitions {
				envMap := make(map[string]string, len(cd.Environment))
				for _, e := range cd.Environment {
					envMap[e.Name] = e.Value
				}
				hints := inferEnvConnectionHints(envMap)
				logGroup := "/ecs/" + td.Family
				if cd.LogConfiguration != nil {
					if g := strings.TrimSpace(cd.LogConfiguration.Options["awslogs-group"]); g != "" {
						logGroup = g
					}
				}
				for _, hint := range hints {
					if !probeMatchesHint(probe, hint) {
						continue
					}
					logGroupSet[logGroup] = struct{}{}
					svcs := servicesByTD[td.TaskDefinitionArn]
					if len(svcs) > 0 {
						for _, svc := range svcs {
							out.ECS = append(out.ECS, redisECSIntegration{
								Kind:              "service",
								Name:              svc.Name,
								Arn:               svc.Arn,
								ClusterArn:        svc.ClusterArn,
								ClusterName:       clusterNamesByArn[svc.ClusterArn],
								TaskDefinitionArn: td.TaskDefinitionArn,
								ContainerName:     cd.Name,
								EnvKey:            hint.source,
								Status:            svc.Status,
								LogGroup:          logGroup,
							})
						}
					} else {
						out.ECS = append(out.ECS, redisECSIntegration{
							Kind:              "task-definition",
							Name:              td.Family + ":" + strconv.Itoa(td.Revision),
							Arn:               td.TaskDefinitionArn,
							TaskDefinitionArn: td.TaskDefinitionArn,
							ContainerName:     cd.Name,
							EnvKey:            hint.source,
							Status:            td.Status,
							LogGroup:          logGroup,
						})
					}
				}
			}
		}
		sort.Slice(out.ECS, func(i, j int) bool {
			if out.ECS[i].Name == out.ECS[j].Name {
				return out.ECS[i].EnvKey < out.ECS[j].EnvKey
			}
			return out.ECS[i].Name < out.ECS[j].Name
		})
	}

	// 4. Recent traces containing a redis span or invoking a linked function
	if h.traceStore != nil {
		const maxRedisTraces = 15
		for _, tr := range h.recentTraces() {
			reason := matchRedisTrace(tr, linkedFunctions)
			if reason == "" {
				continue
			}
			out.Traces = append(out.Traces, redisTraceIntegration{
				ID:         tr.ID,
				StartedAt:  tr.StartedAt.UTC().Format(time.RFC3339),
				DurationMs: tr.DurationMs,
				Status:     tr.Status,
				Method:     tr.Method,
				Path:       tr.Path,
				Reason:     reason,
			})
			if len(out.Traces) >= maxRedisTraces {
				break
			}
		}
	}

	for lg := range logGroupSet {
		out.LogGroups = append(out.LogGroups, lg)
	}
	sort.Strings(out.LogGroups)
	return out
}

func matchRedisTrace(tr *tracesvc.Trace, linkedFunctions map[string]struct{}) string {
	if tr == nil {
		return ""
	}
	linkedMatch := false
	for _, span := range tr.Spans {
		if strings.EqualFold(span.Kind, "redis") {
			return "redis-span"
		}
		if strings.EqualFold(span.Kind, "lambda") {
			if _, ok := linkedFunctions[span.Name]; ok {
				linkedMatch = true
			}
		}
	}
	if linkedMatch {
		return "linked-function"
	}
	return ""
}

type redisScanAPIRequest struct {
	redisAuthPayload
	Pattern string `json:"pattern"`
	Type    string `json:"type"`
	Cursor  string `json:"cursor"`
	Count   int    `json:"count"`
}

// RedisScanKeys performs a cursor-based SCAN on a probed Redis target.
func (h *Handler) RedisScanKeys(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}
	var req redisScanAPIRequest
	if !decodeJSONBody(w, r, &req, true) {
		return
	}
	cfg := redisConnFromProbe(probe, req.redisAuthPayload)
	res, err := redisviewer.ScanKeys(r.Context(), cfg, redisviewer.ScanRequest{
		Pattern: req.Pattern,
		Type:    req.Type,
		Cursor:  req.Cursor,
		Count:   req.Count,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, res)
}

type redisGetKeyAPIRequest struct {
	redisAuthPayload
	Key    string `json:"key"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
	Offset int64  `json:"offset,omitempty"`
	Match  string `json:"match,omitempty"`
}

// RedisGetKey inspects a single key's metadata and paginated type-aware value.
func (h *Handler) RedisGetKey(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}
	var req redisGetKeyAPIRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	cfg := redisConnFromProbe(probe, req.redisAuthPayload)
	detail, err := redisviewer.InspectKeyPage(r.Context(), cfg, redisviewer.InspectOptions{
		Key:    req.Key,
		Limit:  req.Limit,
		Cursor: req.Cursor,
		Offset: req.Offset,
		Match:  req.Match,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, detail)
}

type redisMutateKeyAPIRequest struct {
	redisAuthPayload
	redisviewer.MutateKeyRequest
}

// RedisMutateKey applies a targeted item-level or TTL mutation without replacing the entire key.
func (h *Handler) RedisMutateKey(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}
	var req redisMutateKeyAPIRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	cfg := redisConnFromProbe(probe, req.redisAuthPayload)
	if err := redisviewer.MutateKey(r.Context(), cfg, req.MutateKeyRequest); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"ok": true, "key": req.Key, "action": req.Action})
}

type redisSetKeyAPIRequest struct {
	redisAuthPayload
	redisviewer.SetKeyRequest
}

// RedisSetKey creates or updates a Redis key.
func (h *Handler) RedisSetKey(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}
	var req redisSetKeyAPIRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	cfg := redisConnFromProbe(probe, req.redisAuthPayload)
	if err := redisviewer.SetKey(r.Context(), cfg, req.SetKeyRequest); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"ok": true, "key": req.Key})
}

type redisDeleteKeysAPIRequest struct {
	redisAuthPayload
	Keys []string `json:"keys"`
}

// RedisDeleteKeys deletes one or more Redis keys.
func (h *Handler) RedisDeleteKeys(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}
	var req redisDeleteKeysAPIRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	cfg := redisConnFromProbe(probe, req.redisAuthPayload)
	deleted, err := redisviewer.DeleteKeys(r.Context(), cfg, req.Keys)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

type redisCommandAPIRequest struct {
	redisAuthPayload
	Command string `json:"command"`
}

// RedisExecCommand runs a single guarded Redis command and returns the formatted output.
func (h *Handler) RedisExecCommand(w http.ResponseWriter, r *http.Request) {
	probe, ok := h.resolveRedisProbe(w, r)
	if !ok {
		return
	}
	var req redisCommandAPIRequest
	if !decodeJSONBody(w, r, &req, false) {
		return
	}
	cfg := redisConnFromProbe(probe, req.redisAuthPayload)
	res, err := redisviewer.ExecCommand(r.Context(), cfg, req.Command)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAdminJSON(w, http.StatusOK, res)
}
