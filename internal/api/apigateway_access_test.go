package api

import (
	"encoding/json"
	"testing"
)

// Exercise the public routes so service refactors preserve the AWS response contract.
func TestAPIGatewayAccessLifecycle(t *testing.T) {
	h := newTestHTTPHandler(t)
	type accessResponse struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Value       string `json:"value"`
		Type        string `json:"type"`
		Enabled     bool   `json:"enabled"`
		Position    string `json:"position"`
		ErrorType   string `json:"__type"`
		Items       []struct {
			ID    string `json:"id"`
			Value string `json:"value"`
		} `json:"item"`
	}
	request := func(method, path, body string, want int) accessResponse {
		t.Helper()
		w := compatibilityRequest(h, method, path, body, "")
		if w.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, w.Code, want, w.Body)
		}
		var result accessResponse
		if want == 202 {
			if w.Body.Len() != 0 {
				t.Fatalf("%s %s: delete returned body %s", method, path, w.Body)
			}
		} else if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("%s %s: invalid JSON: %v", method, path, err)
		}
		wantError := map[int]string{400: "BadRequestException", 404: "NotFoundException", 409: "ConflictException"}[want]
		if result.ErrorType != wantError {
			t.Fatalf("%s %s: error type=%q want=%q", method, path, result.ErrorType, wantError)
		}
		return result
	}

	key := request("POST", "/apikeys", `{"name":"messages-primary","value":"generic-primary-key","enabled":true}`, 201)
	if key.ID == "" || key.Value != "generic-primary-key" || !key.Enabled {
		t.Fatalf("created key: %+v", key)
	}
	keyPath := "/apikeys/" + key.ID
	request("POST", "/apikeys", `{"name":"duplicate","value":"generic-primary-key"}`, 409)
	request("POST", "/apikeys", `{}`, 400)
	request("POST", "/apikeys", `{`, 400)
	if got := request("GET", keyPath, "", 200); got.ID != key.ID || got.Value != "" {
		t.Fatalf("default key readback exposed value or lost ID: %+v", got)
	}
	if got := request("GET", keyPath+"?includeValue=true", "", 200); got.Value != key.Value {
		t.Fatalf("explicit key value missing: %+v", got)
	}
	updated := request("PATCH", keyPath, `{"patchOperations":[{"op":"replace","path":"/enabled","value":"false"}]}`, 200)
	if updated.Enabled || updated.Value != key.Value {
		t.Fatalf("key update lost value or left key enabled: %+v", updated)
	}
	request("PATCH", keyPath, `{"patchOperations":[{"op":"replace","path":"/name","value":"uncommitted"},{"op":"replace","path":"/enabled","value":"invalid"}]}`, 400)
	if got := request("GET", keyPath, "", 200); got.Name != key.Name {
		t.Fatalf("failed patch changed key: %+v", got)
	}
	second := request("POST", "/apikeys", `{"name":"messages-secondary"}`, 201)
	page := request("GET", "/apikeys?nameQuery=messages-&includeValues=true&limit=1", "", 200)
	if len(page.Items) != 1 || page.Items[0].Value == "" || page.Position == "" {
		t.Fatalf("first key page: %+v", page)
	}
	next := request("GET", "/apikeys?nameQuery=messages-&limit=1&position="+page.Position, "", 200)
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID || next.Items[0].Value != "" || next.Position != "" {
		t.Fatalf("second key page: %+v", next)
	}
	for _, query := range []string{"limit=invalid", "limit=501", "position=invalid", "position=3"} {
		request("GET", "/apikeys?"+query, "", 400)
	}

	plan := request("POST", "/usageplans", `{"name":"messages-plan"}`, 201)
	if plan.ID == "" {
		t.Fatal("created usage plan missing ID")
	}
	planPath := "/usageplans/" + plan.ID
	request("POST", "/usageplans", `{"name":"invalid","apiStages":[{"apiId":"missing","stage":"v1"}]}`, 400)
	if got := request("GET", planPath, "", 200); got.Name != plan.Name {
		t.Fatalf("usage plan readback: %+v", got)
	}
	request("PATCH", planPath, `{"patchOperations":[{"op":"replace","path":"/description","value":"Generic regression"}]}`, 200)
	if got := request("GET", planPath, "", 200); got.Description != "Generic regression" {
		t.Fatalf("usage plan update missing: %+v", got)
	}
	if got := request("GET", "/usageplans", "", 200); len(got.Items) != 1 || got.Items[0].ID != plan.ID {
		t.Fatalf("usage plan listing: %+v", got)
	}

	keysPath := planPath + "/keys"
	request("POST", keysPath, `{"keyId":"missing","keyType":"API_KEY"}`, 404)
	request("POST", keysPath, `{"keyId":"`+key.ID+`","keyType":"invalid"}`, 400)
	attached := request("POST", keysPath, `{"keyId":"`+key.ID+`","keyType":"API_KEY"}`, 201)
	if attached.ID != key.ID || attached.Type != "API_KEY" || attached.Value != key.Value {
		t.Fatalf("attached key: %+v", attached)
	}
	if got := request("GET", keysPath+"/"+key.ID, "", 200); got.Value != key.Value || got.Type != "API_KEY" {
		t.Fatalf("usage plan key readback: %+v", got)
	}
	if got := request("GET", keysPath, "", 200); len(got.Items) != 1 || got.Items[0].ID != key.ID {
		t.Fatalf("usage plan key listing: %+v", got)
	}
	request("DELETE", keysPath+"/"+key.ID, "", 202)
	request("GET", keysPath+"/"+key.ID, "", 404)
	request("POST", keysPath, `{"keyId":"`+second.ID+`","keyType":"API_KEY"}`, 201)
	request("DELETE", "/apikeys/"+second.ID, "", 202)
	if got := request("GET", keysPath, "", 200); got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("deleting key must remove plan binding and return empty array: %+v", got)
	}
	request("DELETE", planPath, "", 202)
	request("GET", planPath, "", 404)
	request("DELETE", keyPath, "", 202)
	request("GET", keyPath, "", 404)
}
