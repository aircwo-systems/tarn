package iam

import (
	"fmt"
	"log"
	"net/http"
)

const iamVersion = "2010-05-08"

// IsIAMRequest returns true when the request looks like an AWS IAM API call
// (query-protocol POST with Version=2010-05-08).
func IsIAMRequest(r *http.Request) bool {
	return r.FormValue("Version") == iamVersion
}

// Dispatch logs the IAM action and returns a minimal stub success response.
// Full IAM emulation is not required; callers just need a non-error reply.
func Dispatch(w http.ResponseWriter, r *http.Request) {
	action := r.FormValue("Action")
	log.Printf("[iam] ignoring action: %s", action)

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+
		`<%sResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/">`+
		`<%sResult/>`+
		`<ResponseMetadata><RequestId>openstack-iam-stub</RequestId></ResponseMetadata>`+
		`</%sResponse>`, action, action, action)
}
