package apigatewayv1

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	svc "github.com/aircwo-systems/tarn/internal/apigatewayv1"
)

func (h *Handler) PutRestAPI(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, 400, "BadRequestException", err.Error())
		return
	}
	api, err := h.svc.PutRestAPI(r.PathValue("restApiId"), r.URL.Query().Get("mode"), body)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, 200, api)
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var in svc.CreateAPIKeyInput
	if !readAccessRequest(w, r, &in) {
		return
	}
	result, err := h.svc.CreateAPIKey(in)
	writeAccessResult(w, http.StatusCreated, result, err)
}

func (h *Handler) CreateUsagePlan(w http.ResponseWriter, r *http.Request) {
	var in svc.CreateUsagePlanInput
	if !readAccessRequest(w, r, &in) {
		return
	}
	result, err := h.svc.CreateUsagePlan(in)
	writeAccessResult(w, http.StatusCreated, result, err)
}

func (h *Handler) CreateUsagePlanKey(w http.ResponseWriter, r *http.Request) {
	var in svc.CreateUsagePlanKeyInput
	if !readAccessRequest(w, r, &in) {
		return
	}
	result, err := h.svc.CreateUsagePlanKey(r.PathValue("id"), in)
	writeAccessResult(w, http.StatusCreated, result, err)
}

func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	var in accessPatchRequest
	if !readAccessRequest(w, r, &in) {
		return
	}
	result, err := h.svc.UpdateAPIKey(r.PathValue("id"), in.PatchOperations)
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) UpdateUsagePlan(w http.ResponseWriter, r *http.Request) {
	var in accessPatchRequest
	if !readAccessRequest(w, r, &in) {
		return
	}
	result, err := h.svc.UpdateUsagePlan(r.PathValue("id"), in.PatchOperations)
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetAPIKey(r.PathValue("id"), includeAPIKeyValue(r))
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) GetUsagePlan(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetUsagePlan(r.PathValue("id"))
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) GetUsagePlanKey(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetUsagePlanKey(r.PathValue("id"), r.PathValue("keyId"))
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	page, ok := readAccessPage(w, r)
	if !ok {
		return
	}
	result, err := h.svc.ListAPIKeys(svc.ListAPIKeysInput{PageOptions: page, IncludeValue: includeAPIKeyValue(r), NameQuery: r.URL.Query().Get("nameQuery")})
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) ListUsagePlans(w http.ResponseWriter, r *http.Request) {
	page, ok := readAccessPage(w, r)
	if !ok {
		return
	}
	result, err := h.svc.ListUsagePlans(page)
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) ListUsagePlanKeys(w http.ResponseWriter, r *http.Request) {
	page, ok := readAccessPage(w, r)
	if !ok {
		return
	}
	result, err := h.svc.ListUsagePlanKeys(r.PathValue("id"), page)
	writeAccessResult(w, http.StatusOK, result, err)
}

func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteAPIKey(r.PathValue("id")); err != nil {
		writeAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) DeleteUsagePlan(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteUsagePlan(r.PathValue("id")); err != nil {
		writeAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) DeleteUsagePlanKey(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteUsagePlanKey(r.PathValue("id"), r.PathValue("keyId")); err != nil {
		writeAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type accessPatchRequest struct {
	PatchOperations []svc.PatchOp `json:"patchOperations"`
}

func readAccessRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequestException", "Invalid JSON body")
		return false
	}
	return true
}

func includeAPIKeyValue(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("includeValue") == "true" || q.Get("includeValues") == "true"
}

func readAccessPage(w http.ResponseWriter, r *http.Request) (svc.PageOptions, bool) {
	q := r.URL.Query()
	page := svc.PageOptions{Position: q.Get("position")}
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "BadRequestException", "Invalid limit")
			return svc.PageOptions{}, false
		}
		page.Limit = n
	}
	return page, true
}

func writeAccessResult[T any](w http.ResponseWriter, status int, result T, err error) {
	if err != nil {
		writeAccessError(w, err)
		return
	}
	writeJSON(w, status, result)
}

func writeAccessError(w http.ResponseWriter, err error) {
	status, code := http.StatusBadRequest, "BadRequestException"
	switch {
	case errors.Is(err, svc.ErrAccessNotFound):
		status, code = http.StatusNotFound, "NotFoundException"
	case errors.Is(err, svc.ErrAPIKeyConflict):
		status, code = http.StatusConflict, "ConflictException"
	}
	writeError(w, status, code, err.Error())
}

func (h *Handler) Tags(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tags map[string]string `json:"tags"`
	}
	if r.Method == "PUT" {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, 400, "BadRequestException", "Invalid JSON body")
			return
		}
	}
	tags, err := h.svc.AccessTags(r.PathValue("resource"), r.Method, in.Tags, r.URL.Query()["tagKeys"])
	if err != nil {
		writeError(w, 404, "NotFoundException", err.Error())
		return
	}
	if r.Method == "GET" {
		writeJSON(w, 200, map[string]any{"tags": tags})
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) PatchRestAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PatchOperations []svc.PatchOp `json:"patchOperations"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, 400, "BadRequestException", err.Error())
		return
	}
	api, err := h.svc.PatchRestAPI(r.PathValue("restApiId"), req.PatchOperations)
	if err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, 200, api)
}
func (h *Handler) DeleteDeployment(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteDeployment(r.PathValue("restApiId"), r.PathValue("deploymentId")); err != nil {
		status, code := errorStatus(err)
		writeError(w, status, code, err.Error())
		return
	}
	w.WriteHeader(202)
}
