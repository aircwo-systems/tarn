package secrets

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	secretssvc "github.com/aircwo-systems/tarn/internal/secrets"
	tracesvc "github.com/aircwo-systems/tarn/internal/trace"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Handler handles Secrets Manager API requests using JSON-RPC style (X-Amz-Target header).
type Handler struct {
	cfg       *config.Config
	svc       *secretssvc.Service
	collector *tracesvc.Collector
}

// NewHandler creates a new Secrets Manager API handler.
func NewHandler(cfg *config.Config, svc *secretssvc.Service) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}

// SetCollector attaches a trace collector so secrets fetches are recorded as sub-spans.
func (h *Handler) SetCollector(c *tracesvc.Collector) { h.collector = c }

// IsSecretsManagerRequest checks if a request targets the Secrets Manager service.
func IsSecretsManagerRequest(r *http.Request) bool {
	target := r.Header.Get("X-Amz-Target")
	return strings.HasPrefix(target, "secretsmanager.")
}

// Dispatch routes the request based on X-Amz-Target header.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("X-Amz-Target")
	action := strings.TrimPrefix(target, "secretsmanager.")

	switch action {
	case "CreateSecret":
		h.createSecret(w, r)
	case "GetSecretValue":
		h.getSecretValue(w, r)
	case "GetResourcePolicy":
		h.getResourcePolicy(w, r)
	case "DescribeSecret":
		h.describeSecret(w, r)
	case "UpdateSecret":
		h.updateSecret(w, r)
	case "PutSecretValue":
		h.putSecretValue(w, r)
	case "DeleteSecret":
		h.deleteSecret(w, r)
	case "ListSecrets":
		h.listSecrets(w, r)
	case "TagResource":
		h.tagResource(w, r)
	case "UntagResource":
		h.untagResource(w, r)
	default:
		writeError(w, 400, "InvalidAction", "Unsupported action: "+target)
	}
}

func (h *Handler) createSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string            `json:"Name"`
		Description  string            `json:"Description"`
		SecretString string            `json:"SecretString"`
		SecretBinary string            `json:"SecretBinary"` // base64 encoded
		Tags         []types.SecretTag `json:"Tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, 400, "InvalidParameterException", "Name is required")
		return
	}

	var binaryData []byte
	if req.SecretBinary != "" {
		var err error
		binaryData, err = base64.StdEncoding.DecodeString(req.SecretBinary)
		if err != nil {
			writeError(w, 400, "InvalidParameterException", "Invalid SecretBinary encoding")
			return
		}
	}

	secret, err := h.svc.CreateSecret(req.Name, req.Description, req.SecretString, binaryData, req.Tags)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, 400, "ResourceExistsException", err.Error())
		} else {
			writeError(w, 500, "InternalServiceError", err.Error())
		}
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"ARN":           secret.ARN,
		"Name":          secret.Name,
		"VersionId":     secret.VersionId,
		"VersionStages": secret.VersionStages,
	})
}

func (h *Handler) getSecretValue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId     string `json:"SecretId"`
		VersionId    string `json:"VersionId"`
		VersionStage string `json:"VersionStage"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	if req.SecretId == "" {
		writeError(w, 400, "InvalidParameterException", "SecretId is required")
		return
	}

	start := time.Now()
	secret, err := h.svc.GetSecretValue(req.SecretId)
	if h.collector != nil {
		status := "ok"
		if err != nil {
			status = "error"
		}
		h.collector.RecordAnon("secrets", req.SecretId, time.Since(start).Milliseconds(), status, nil)
	}
	if err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	resp := map[string]interface{}{
		"ARN":           secret.ARN,
		"Name":          secret.Name,
		"VersionId":     secret.VersionId,
		"VersionStages": secret.VersionStages,
		"CreatedDate":   float64(secret.CreatedDate.Unix()),
	}

	if secret.SecretString != "" {
		resp["SecretString"] = secret.SecretString
	}
	if secret.SecretBinary != nil {
		resp["SecretBinary"] = base64.StdEncoding.EncodeToString(secret.SecretBinary)
	}

	writeJSON(w, 200, resp)
}

// getResourcePolicy returns an empty policy for existing secrets.
// Terraform's AWS provider probes this during refresh and expects a successful read.
func (h *Handler) getResourcePolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId string `json:"SecretId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}
	if req.SecretId == "" {
		writeError(w, 400, "InvalidParameterException", "SecretId is required")
		return
	}

	secret, err := h.svc.DescribeSecret(req.SecretId)
	if err != nil {
		writeError(w, 400, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"ARN":  secret.ARN,
		"Name": secret.Name,
		"ResourcePolicy": `{
  "Version":"2012-10-17",
  "Statement":[
    {
      "Sid":"TarnDefaultSecretPolicy",
      "Effect":"Allow",
      "Principal":{"AWS":"*"},
      "Action":["secretsmanager:GetSecretValue","secretsmanager:DescribeSecret"],
      "Resource":"*"
    }
  ]
}`,
	})
}

func (h *Handler) describeSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId string `json:"SecretId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	secret, err := h.svc.DescribeSecret(req.SecretId)
	if err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	resp := map[string]interface{}{
		"ARN":         secret.ARN,
		"Name":        secret.Name,
		"Description": secret.Description,
		"VersionIdsToStages": map[string][]string{
			secret.VersionId: secret.VersionStages,
		},
		"Tags":             secret.Tags,
		"CreatedDate":      float64(secret.CreatedDate.Unix()),
		"LastChangedDate":  float64(secret.LastChangedDate.Unix()),
		"LastAccessedDate": float64(secret.LastAccessedDate.Unix()),
	}

	writeJSON(w, 200, resp)
}

func (h *Handler) updateSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId     string `json:"SecretId"`
		SecretString string `json:"SecretString"`
		SecretBinary string `json:"SecretBinary"`
		Description  string `json:"Description"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	var binaryData []byte
	if req.SecretBinary != "" {
		var err error
		binaryData, err = base64.StdEncoding.DecodeString(req.SecretBinary)
		if err != nil {
			writeError(w, 400, "InvalidParameterException", "Invalid SecretBinary encoding")
			return
		}
	}

	secret, err := h.svc.UpdateSecret(req.SecretId, req.SecretString, binaryData)
	if err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"ARN":           secret.ARN,
		"Name":          secret.Name,
		"VersionId":     secret.VersionId,
		"VersionStages": secret.VersionStages,
	})
}

func (h *Handler) putSecretValue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId     string `json:"SecretId"`
		SecretString string `json:"SecretString"`
		SecretBinary string `json:"SecretBinary"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	var binaryData []byte
	if req.SecretBinary != "" {
		var err error
		binaryData, err = base64.StdEncoding.DecodeString(req.SecretBinary)
		if err != nil {
			writeError(w, 400, "InvalidParameterException", "Invalid SecretBinary encoding")
			return
		}
	}

	secret, err := h.svc.PutSecretValue(req.SecretId, req.SecretString, binaryData)
	if err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"ARN":           secret.ARN,
		"Name":          secret.Name,
		"VersionId":     secret.VersionId,
		"VersionStages": secret.VersionStages,
	})
}

func (h *Handler) deleteSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId                   string `json:"SecretId"`
		ForceDeleteWithoutRecovery bool   `json:"ForceDeleteWithoutRecovery"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	// Get ARN and name before deleting
	secret, err := h.svc.DescribeSecret(req.SecretId)
	if err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	if err := h.svc.DeleteSecret(req.SecretId); err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"ARN":          secret.ARN,
		"Name":         secret.Name,
		"DeletionDate": float64(secret.LastChangedDate.Unix()),
	})
}

func (h *Handler) listSecrets(w http.ResponseWriter, r *http.Request) {
	// Consume body even if empty
	io.ReadAll(r.Body)

	secrets := h.svc.ListSecrets()

	secretList := make([]map[string]interface{}, 0, len(secrets))
	for _, s := range secrets {
		entry := map[string]interface{}{
			"ARN":             s.ARN,
			"Name":            s.Name,
			"Description":     s.Description,
			"CreatedDate":     float64(s.CreatedDate.Unix()),
			"LastChangedDate": float64(s.LastChangedDate.Unix()),
			"Tags":            s.Tags,
		}
		secretList = append(secretList, entry)
	}

	writeJSON(w, 200, map[string]interface{}{
		"SecretList": secretList,
	})
}

func (h *Handler) tagResource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId string            `json:"SecretId"`
		Tags     []types.SecretTag `json:"Tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	if err := h.svc.TagResource(req.SecretId, req.Tags); err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, 200, map[string]interface{}{})
}

func (h *Handler) untagResource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecretId string   `json:"SecretId"`
		TagKeys  []string `json:"TagKeys"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "InvalidParameterException", err.Error())
		return
	}

	if err := h.svc.UntagResource(req.SecretId, req.TagKeys); err != nil {
		writeError(w, 404, "ResourceNotFoundException", err.Error())
		return
	}

	writeJSON(w, 200, map[string]interface{}{})
}

// --- Helpers ---

func readJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"__type":  code,
		"Message": message,
	})
}
