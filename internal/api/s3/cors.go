package s3

import (
	"net/http"

	"github.com/aircwo-systems/tarn/internal/cors"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// preflight answers an OPTIONS request against the bucket's CORS rules, the
// way S3 does: the first rule that allows the origin, method and headers wins.
func (h *Handler) preflight(w http.ResponseWriter, r *http.Request, bucket string) {
	if r.Header.Get("Origin") == "" {
		writeS3Error(w, http.StatusBadRequest, "BadRequest", "Insufficient information. Origin request header needed.")
		return
	}
	req, _ := cors.FromHTTP(r.Method, r.Header)
	if !req.Preflight {
		writeS3Error(w, http.StatusBadRequest, "BadRequest", "Invalid Access-Control-Request-Method: null")
		return
	}
	if bucket == "" || h.svc.HeadBucket(bucket) != nil {
		writeS3Error(w, http.StatusForbidden, "AccessForbidden", "CORSResponse: Bucket not found")
		return
	}
	rules := h.svc.GetBucketCORS(bucket)
	if len(rules) == 0 {
		writeS3Error(w, http.StatusForbidden, "AccessForbidden", "CORSResponse: CORS is not enabled for this bucket.")
		return
	}
	for _, rule := range rules {
		p := corsPolicy(rule)
		if p.AllowsPreflight(req) {
			copyHeaders(w.Header(), p.ResponseHeaders(req))
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	writeS3Error(w, http.StatusForbidden, "AccessForbidden", "CORSResponse: This CORS request is not allowed. This is usually because the evalution of Origin, request method / Access-Control-Request-Method or Access-Control-Request-Headers are not whitelisted by the resource's CORS spec.")
}

// applyCORS adds Access-Control-* headers to a cross-origin request's response
// when one of the bucket's rules allows its origin and method. It runs before
// the operation so error responses carry the headers too, letting the browser
// surface the real S3 error instead of a CORS failure.
func (h *Handler) applyCORS(w http.ResponseWriter, req cors.Request, bucket string) {
	for _, rule := range h.svc.GetBucketCORS(bucket) {
		p := corsPolicy(rule)
		if p.AllowsOrigin(req.Origin) && p.AllowsMethod(req.Method) {
			copyHeaders(w.Header(), p.ResponseHeaders(req))
			return
		}
	}
}

// corsPolicy converts an S3 CORS rule. S3 allows credentials for every origin
// it names explicitly and answers "*" only for a bare wildcard rule.
func corsPolicy(rule types.CORSRule) cors.Policy {
	p := cors.Policy{
		AllowOrigins:  rule.AllowedOrigins,
		AllowMethods:  rule.AllowedMethods,
		AllowHeaders:  rule.AllowedHeaders,
		ExposeHeaders: rule.ExposeHeaders,
		MaxAge:        rule.MaxAgeSeconds,
	}
	p.AllowCredentials = len(rule.AllowedOrigins) != 1 || rule.AllowedOrigins[0] != "*"
	return p
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, v := range values {
			dst.Add(name, v)
		}
	}
}
