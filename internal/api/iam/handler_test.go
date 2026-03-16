package iam

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func doIAMAction(t *testing.T, h *Handler, action string, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("Action", action)
	form.Set("Version", iamVersion)
	for k, v := range params {
		form.Set(k, v)
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Dispatch(rec, req)
	return rec
}

func TestPutAndGetRolePolicyRoundTrip(t *testing.T) {
	h := NewHandler("000000000000")
	roleName := "lambda_exec_role-dev"
	policyName := "lambda_policy-dev"
	policyDoc := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"logs:*","Resource":"*"}]}`

	putRec := doIAMAction(t, h, "PutRolePolicy", map[string]string{
		"RoleName":       roleName,
		"PolicyName":     policyName,
		"PolicyDocument": policyDoc,
	})
	if putRec.Code != http.StatusOK {
		t.Fatalf("PutRolePolicy status = %d, want 200; body=%s", putRec.Code, putRec.Body.String())
	}

	getRec := doIAMAction(t, h, "GetRolePolicy", map[string]string{
		"RoleName":   roleName,
		"PolicyName": policyName,
	})
	if getRec.Code != http.StatusOK {
		t.Fatalf("GetRolePolicy status = %d, want 200; body=%s", getRec.Code, getRec.Body.String())
	}
	if !strings.Contains(getRec.Body.String(), "<PolicyName>"+policyName+"</PolicyName>") {
		t.Fatalf("expected policy name in response, got: %s", getRec.Body.String())
	}
	if !strings.Contains(getRec.Body.String(), url.QueryEscape(policyDoc)) {
		t.Fatalf("expected URL-escaped policy document in response, got: %s", getRec.Body.String())
	}
}

func TestListRolePoliciesIncludesInlinePolicy(t *testing.T) {
	h := NewHandler("000000000000")
	roleName := "lambda_exec_role-stage"

	doIAMAction(t, h, "PutRolePolicy", map[string]string{
		"RoleName":       roleName,
		"PolicyName":     "policy-a",
		"PolicyDocument": `{"Version":"2012-10-17","Statement":[]}`,
	})
	doIAMAction(t, h, "PutRolePolicy", map[string]string{
		"RoleName":       roleName,
		"PolicyName":     "policy-b",
		"PolicyDocument": `{"Version":"2012-10-17","Statement":[]}`,
	})

	listRec := doIAMAction(t, h, "ListRolePolicies", map[string]string{
		"RoleName": roleName,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("ListRolePolicies status = %d, want 200; body=%s", listRec.Code, listRec.Body.String())
	}
	if !strings.Contains(listRec.Body.String(), "<member>policy-a</member>") ||
		!strings.Contains(listRec.Body.String(), "<member>policy-b</member>") {
		t.Fatalf("expected both policy names in response, got: %s", listRec.Body.String())
	}
}

func TestDeleteRolePolicyRemovesPolicy(t *testing.T) {
	h := NewHandler("000000000000")
	roleName := "lambda_exec_role-prod"
	policyName := "lambda_policy-prod"

	doIAMAction(t, h, "PutRolePolicy", map[string]string{
		"RoleName":       roleName,
		"PolicyName":     policyName,
		"PolicyDocument": `{"Version":"2012-10-17","Statement":[]}`,
	})

	delRec := doIAMAction(t, h, "DeleteRolePolicy", map[string]string{
		"RoleName":   roleName,
		"PolicyName": policyName,
	})
	if delRec.Code != http.StatusOK {
		t.Fatalf("DeleteRolePolicy status = %d, want 200; body=%s", delRec.Code, delRec.Body.String())
	}

	getRec := doIAMAction(t, h, "GetRolePolicy", map[string]string{
		"RoleName":   roleName,
		"PolicyName": policyName,
	})
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("GetRolePolicy after delete status = %d, want 404; body=%s", getRec.Code, getRec.Body.String())
	}
}
