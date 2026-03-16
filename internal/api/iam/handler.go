package iam

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

const iamVersion = "2010-05-08"

// role is an in-memory IAM role record.
type role struct {
	Name                     string
	RoleID                   string
	ARN                      string
	Path                     string
	AssumeRolePolicyDocument string
	CreateDate               string
	MaxSessionDuration       int
}

// Handler is the IAM stub handler. It holds in-memory role state so that
// GetRole succeeds after CreateRole, which is required by terraform-provider-aws v6+.
type Handler struct {
	accountID string
	mu        sync.Mutex
	roles     map[string]*role
	// inlinePolicies stores IAM inline role policies by role name and policy name.
	inlinePolicies map[string]map[string]string
	seq       int
}

// NewHandler creates a new IAM stub handler.
func NewHandler(accountID string) *Handler {
	return &Handler{
		accountID: accountID,
		roles:     make(map[string]*role),
		inlinePolicies: make(map[string]map[string]string),
	}
}

// IsIAMRequest returns true when the request looks like an AWS IAM API call
// (query-protocol POST with Version=2010-05-08).
func IsIAMRequest(r *http.Request) bool {
	return r.FormValue("Version") == iamVersion
}

// Dispatch handles IAM API calls. It returns proper XML structures for the
// actions that terraform-provider-aws reads (CreateRole, GetRole, List*), and
// falls back to an empty success response for everything else.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	action := r.FormValue("Action")
	log.Printf("[iam] action: %s", action)
	w.Header().Set("Content-Type", "text/xml")

	switch action {
	case "CreateRole":
		h.createRole(w, r)
	case "GetRole":
		h.getRole(w, r)
	case "DeleteRole":
		h.deleteRole(w, r)
	case "UpdateRole", "UpdateRoleDescription", "UpdateAssumeRolePolicy":
		h.emptyOK(w, action)
	case "TagRole", "UntagRole":
		h.emptyOK(w, action)
	case "ListRoleTags":
		h.listRoleTags(w, action)
	case "AttachRolePolicy", "DetachRolePolicy":
		h.emptyOK(w, action)
	case "ListAttachedRolePolicies":
		h.listAttachedRolePolicies(w)
	case "PutRolePolicy":
		h.putRolePolicy(w, r)
	case "DeleteRolePolicy":
		h.deleteRolePolicy(w, r)
	case "GetRolePolicy":
		h.getRolePolicy(w, r)
	case "ListRolePolicies":
		h.listRolePolicies(w, r)
	case "ListInstanceProfilesForRole":
		h.listInstanceProfilesForRole(w)
	case "CreateInstanceProfile":
		h.createInstanceProfile(w, r)
	case "GetInstanceProfile":
		h.getInstanceProfile(w, r)
	case "DeleteInstanceProfile":
		h.emptyOK(w, action)
	case "AddRoleToInstanceProfile", "RemoveRoleFromInstanceProfile":
		h.emptyOK(w, action)
	case "PassRole":
		h.emptyOK(w, action)
	default:
		log.Printf("[iam] unhandled action (returning empty OK): %s", action)
		h.emptyOK(w, action)
	}
}

// ── Role handlers ─────────────────────────────────────────────────────────────

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("RoleName")
	path := r.FormValue("Path")
	if path == "" {
		path = "/"
	}
	assumeDoc := r.FormValue("AssumeRolePolicyDocument")

	h.mu.Lock()
	h.seq++
	ro := &role{
		Name:                     name,
		RoleID:                   fmt.Sprintf("AROAOPENSTACKSTUB%04d", h.seq),
		ARN:                      fmt.Sprintf("arn:aws:iam::%s:role%s%s", h.accountID, path, name),
		Path:                     path,
		AssumeRolePolicyDocument: assumeDoc,
		CreateDate:               time.Now().UTC().Format(time.RFC3339),
		MaxSessionDuration:       3600,
	}
	h.roles[name] = ro
	if _, ok := h.inlinePolicies[name]; !ok {
		h.inlinePolicies[name] = make(map[string]string)
	}
	h.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, h.roleXML("CreateRoleResponse", "CreateRoleResult", ro))
}

func (h *Handler) getRole(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("RoleName")

	h.mu.Lock()
	ro := h.roles[name]
	if ro == nil {
		// Auto-stub so TF reads don't 404 for roles created outside our session.
		h.seq++
		ro = &role{
			Name:                     name,
			RoleID:                   fmt.Sprintf("AROAOPENSTACKSTUB%04d", h.seq),
			ARN:                      fmt.Sprintf("arn:aws:iam::%s:role/%s", h.accountID, name),
			Path:                     "/",
			AssumeRolePolicyDocument: "{}",
			CreateDate:               time.Now().UTC().Format(time.RFC3339),
			MaxSessionDuration:       3600,
		}
			h.roles[name] = ro
			if _, ok := h.inlinePolicies[name]; !ok {
				h.inlinePolicies[name] = make(map[string]string)
			}
		}
	h.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, h.roleXML("GetRoleResponse", "GetRoleResult", ro))
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("RoleName")
	h.mu.Lock()
	delete(h.roles, name)
	delete(h.inlinePolicies, name)
	h.mu.Unlock()
	h.emptyOK(w, "DeleteRole")
}

// roleXML renders the common Role element wrapped in response/result tags.
// The AssumeRolePolicyDocument is URL-encoded as the real IAM API does.
func (h *Handler) roleXML(response, result string, ro *role) string {
	policyEncoded := url.QueryEscape(ro.AssumeRolePolicyDocument)
	return fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<%s xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
			`<%s>`+
			`<Role>`+
			`<RoleName>%s</RoleName>`+
			`<RoleId>%s</RoleId>`+
			`<Arn>%s</Arn>`+
			`<Path>%s</Path>`+
			`<CreateDate>%s</CreateDate>`+
			`<AssumeRolePolicyDocument>%s</AssumeRolePolicyDocument>`+
			`<MaxSessionDuration>%d</MaxSessionDuration>`+
			`</Role>`+
			`</%s>`+
			`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
			`</%s>`,
		response, result,
		ro.Name, ro.RoleID, ro.ARN, ro.Path, ro.CreateDate, policyEncoded, ro.MaxSessionDuration,
		result, response,
	)
}

// ── List/policy handlers ──────────────────────────────────────────────────────

func (h *Handler) listAttachedRolePolicies(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<ListAttachedRolePoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<ListAttachedRolePoliciesResult>`+
		`<AttachedPolicies/>`+
		`<IsTruncated>false</IsTruncated>`+
		`</ListAttachedRolePoliciesResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</ListAttachedRolePoliciesResponse>`)
}

func (h *Handler) putRolePolicy(w http.ResponseWriter, r *http.Request) {
	roleName := r.FormValue("RoleName")
	policyName := r.FormValue("PolicyName")
	policyDoc := r.FormValue("PolicyDocument")

	if roleName == "" || policyName == "" {
		h.noSuchEntity(w, "Role or policy not found.")
		return
	}

	h.mu.Lock()
	// Match getRole behavior: auto-stub missing roles to keep Terraform flows resilient.
	if _, ok := h.roles[roleName]; !ok {
		h.seq++
		h.roles[roleName] = &role{
			Name:                     roleName,
			RoleID:                   fmt.Sprintf("AROAOPENSTACKSTUB%04d", h.seq),
			ARN:                      fmt.Sprintf("arn:aws:iam::%s:role/%s", h.accountID, roleName),
			Path:                     "/",
			AssumeRolePolicyDocument: "{}",
			CreateDate:               time.Now().UTC().Format(time.RFC3339),
			MaxSessionDuration:       3600,
		}
	}
	if _, ok := h.inlinePolicies[roleName]; !ok {
		h.inlinePolicies[roleName] = make(map[string]string)
	}
	h.inlinePolicies[roleName][policyName] = policyDoc
	h.mu.Unlock()

	h.emptyOK(w, "PutRolePolicy")
}

func (h *Handler) getRolePolicy(w http.ResponseWriter, r *http.Request) {
	roleName := r.FormValue("RoleName")
	policyName := r.FormValue("PolicyName")

	h.mu.Lock()
	policies := h.inlinePolicies[roleName]
	policyDoc, ok := policies[policyName]
	h.mu.Unlock()
	if !ok {
		h.noSuchEntity(w, "The policy was not found.")
		return
	}

	encoded := url.QueryEscape(policyDoc)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<GetRolePolicyResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<GetRolePolicyResult>`+
		`<RoleName>%s</RoleName>`+
		`<PolicyName>%s</PolicyName>`+
		`<PolicyDocument>%s</PolicyDocument>`+
		`</GetRolePolicyResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</GetRolePolicyResponse>`,
		roleName, policyName, encoded)
}

func (h *Handler) deleteRolePolicy(w http.ResponseWriter, r *http.Request) {
	roleName := r.FormValue("RoleName")
	policyName := r.FormValue("PolicyName")

	h.mu.Lock()
	if policies, ok := h.inlinePolicies[roleName]; ok {
		delete(policies, policyName)
	}
	h.mu.Unlock()

	h.emptyOK(w, "DeleteRolePolicy")
}

func (h *Handler) listRolePolicies(w http.ResponseWriter, r *http.Request) {
	roleName := r.FormValue("RoleName")

	h.mu.Lock()
	policies := h.inlinePolicies[roleName]
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	h.mu.Unlock()
	sort.Strings(names)

	var policyNamesXML string
	if len(names) == 0 {
		policyNamesXML = "<PolicyNames/>"
	} else {
		policyNamesXML = "<PolicyNames>"
		for _, name := range names {
			policyNamesXML += fmt.Sprintf("<member>%s</member>", name)
		}
		policyNamesXML += "</PolicyNames>"
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<ListRolePoliciesResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<ListRolePoliciesResult>`+
		policyNamesXML+
		`<IsTruncated>false</IsTruncated>`+
		`</ListRolePoliciesResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</ListRolePoliciesResponse>`)
}

func (h *Handler) listRoleTags(w http.ResponseWriter, action string) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<%sResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<%sResult>`+
		`<Tags/>`+
		`<IsTruncated>false</IsTruncated>`+
		`</%sResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</%sResponse>`, action, action, action, action)
}

// ── Instance profile handlers ─────────────────────────────────────────────────

func (h *Handler) createInstanceProfile(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("InstanceProfileName")
	path := r.FormValue("Path")
	if path == "" {
		path = "/"
	}
	arn := fmt.Sprintf("arn:aws:iam::%s:instance-profile%s%s", h.accountID, path, name)
	now := time.Now().UTC().Format(time.RFC3339)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<CreateInstanceProfileResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<CreateInstanceProfileResult>`+
		`<InstanceProfile>`+
		`<InstanceProfileName>%s</InstanceProfileName>`+
		`<InstanceProfileId>AIPAOPENSTACKSTUB0001</InstanceProfileId>`+
		`<Arn>%s</Arn>`+
		`<Path>%s</Path>`+
		`<Roles/>`+
		`<CreateDate>%s</CreateDate>`+
		`</InstanceProfile>`+
		`</CreateInstanceProfileResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</CreateInstanceProfileResponse>`,
		name, arn, path, now)
}

func (h *Handler) getInstanceProfile(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("InstanceProfileName")
	arn := fmt.Sprintf("arn:aws:iam::%s:instance-profile/%s", h.accountID, name)
	now := time.Now().UTC().Format(time.RFC3339)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<GetInstanceProfileResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<GetInstanceProfileResult>`+
		`<InstanceProfile>`+
		`<InstanceProfileName>%s</InstanceProfileName>`+
		`<InstanceProfileId>AIPAOPENSTACKSTUB0001</InstanceProfileId>`+
		`<Arn>%s</Arn>`+
		`<Path>/</Path>`+
		`<Roles/>`+
		`<CreateDate>%s</CreateDate>`+
		`</InstanceProfile>`+
		`</GetInstanceProfileResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</GetInstanceProfileResponse>`,
		name, arn, now)
}

func (h *Handler) listInstanceProfilesForRole(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<ListInstanceProfilesForRoleResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<ListInstanceProfilesForRoleResult>`+
		`<InstanceProfiles/>`+
		`<IsTruncated>false</IsTruncated>`+
		`</ListInstanceProfilesForRoleResult>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</ListInstanceProfilesForRoleResponse>`)
}

// ── Generic helpers ───────────────────────────────────────────────────────────

func (h *Handler) emptyOK(w http.ResponseWriter, action string) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w,
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<%sResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
			`<%sResult/>`+
			`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
			`</%sResponse>`,
		action, action, action)
}

func (h *Handler) noSuchEntity(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w,
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<ErrorResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
			`<Error><Type>Sender</Type><Code>NoSuchEntity</Code><Message>%s</Message></Error>`+
			`<RequestId>openstack-iam-stub</RequestId>`+
			`</ErrorResponse>`,
		message)
}
