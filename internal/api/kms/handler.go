// Package kms serves the AWS KMS JSON 1.1 API (X-Amz-Target TrentService.*).
package kms

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	kmssvc "github.com/aircwo-systems/tarn/internal/kms"
	"github.com/google/uuid"
)

const targetPrefix = "TrentService."

// IsKMSRequest reports whether r targets the KMS API.
func IsKMSRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("X-Amz-Target"), targetPrefix)
}

// Handler serves one account's KMS API.
type Handler struct {
	svc *kmssvc.Service
}

// NewHandler creates a KMS API handler.
func NewHandler(svc *kmssvc.Service) *Handler { return &Handler{svc: svc} }

type operation func(svc *kmssvc.Service, body []byte) (any, error)

// op adapts a typed service method to the dispatch table.
func op[I, O any](fn func(*kmssvc.Service, *I) (O, error)) operation {
	return func(svc *kmssvc.Service, body []byte) (any, error) {
		var in I
		if len(body) > 0 {
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, &kmssvc.Error{Code: "SerializationException", Message: err.Error()}
			}
		}
		return fn(svc, &in)
	}
}

type service = kmssvc.Service

var operations = map[string]operation{
	// Keys
	"CreateKey":            op((*service).CreateKey),
	"DescribeKey":          op((*service).DescribeKey),
	"ListKeys":             op((*service).ListKeys),
	"UpdateKeyDescription": op((*service).UpdateKeyDescription),
	"EnableKey":            op((*service).EnableKey),
	"DisableKey":           op((*service).DisableKey),
	"ScheduleKeyDeletion":  op((*service).ScheduleKeyDeletion),
	"CancelKeyDeletion":    op((*service).CancelKeyDeletion),

	// Aliases
	"CreateAlias": op((*service).CreateAlias),
	"UpdateAlias": op((*service).UpdateAlias),
	"DeleteAlias": op((*service).DeleteAlias),
	"ListAliases": op((*service).ListAliases),

	// Tags, policy and rotation
	"TagResource":          op((*service).TagResource),
	"UntagResource":        op((*service).UntagResource),
	"ListResourceTags":     op((*service).ListResourceTags),
	"GetKeyPolicy":         op((*service).GetKeyPolicy),
	"PutKeyPolicy":         op((*service).PutKeyPolicy),
	"ListKeyPolicies":      op((*service).ListKeyPolicies),
	"GetKeyRotationStatus": op((*service).GetKeyRotationStatus),
	"EnableKeyRotation":    op((*service).EnableKeyRotation),
	"DisableKeyRotation":   op((*service).DisableKeyRotation),

	// Cryptographic operations
	"Encrypt":                         op((*service).Encrypt),
	"Decrypt":                         op((*service).Decrypt),
	"ReEncrypt":                       op((*service).ReEncrypt),
	"GenerateDataKey":                 op((*service).GenerateDataKey),
	"GenerateDataKeyWithoutPlaintext": op((*service).GenerateDataKeyWithoutPlaintext),
	"GenerateRandom":                  op((*service).GenerateRandom),
}

// Dispatch routes a JSON 1.1 request by its X-Amz-Target action.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Amzn-Requestid", uuid.NewString())
	action := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), targetPrefix)
	fn, ok := operations[action]
	if !ok {
		log.Printf("[kms] unhandled action %s", action)
		writeError(w, kmssvc.NotImplemented(action))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, &kmssvc.Error{Code: "SerializationException", Message: err.Error()})
		return
	}
	out, err := fn(h.svc, body)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	_ = json.NewEncoder(w).Encode(out)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var ke *kmssvc.Error
	if !errors.As(err, &ke) {
		log.Printf("[kms] internal error: %v", err)
		ke = &kmssvc.Error{Code: "KMSInternalException", Message: err.Error()}
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.Header().Set("X-Amzn-ErrorType", ke.Code)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"__type": ke.Code, "message": ke.Message})
}
