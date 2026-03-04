package s3

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	s3svc "github.com/openstack-project/openstack/internal/s3"
	"github.com/openstack-project/openstack/pkg/types"
)

const s3Namespace = "http://s3.amazonaws.com/doc/2006-03-01/"

// Handler serves the S3 REST XML API.
type Handler struct {
	svc *s3svc.Service
}

// NewHandler creates a new S3 API handler.
func NewHandler(svc *s3svc.Service) *Handler {
	return &Handler{svc: svc}
}

// Dispatch routes S3 API requests by method, bucket, key and query params.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	// Strip /_s3 prefix
	path := strings.TrimPrefix(r.URL.Path, "/_s3")
	if path == "" {
		path = "/"
	}

	// Parse bucket and key from path
	path = strings.TrimPrefix(path, "/")
	bucket, key, _ := strings.Cut(path, "/")

	switch {
	case bucket == "":
		// Operations on the service level
		if r.Method == http.MethodGet {
			h.listBuckets(w, r)
			return
		}
	case key == "":
		// Operations on a bucket
		switch r.Method {
		case http.MethodPut:
			if r.URL.Query().Has("notification") {
				h.putBucketNotification(w, r, bucket)
				return
			}
			if r.URL.Query().Has("policy") {
				h.putBucketPolicy(w, r, bucket)
				return
			}
			h.createBucket(w, r, bucket)
			return
		case http.MethodHead:
			h.headBucket(w, r, bucket)
			return
		case http.MethodDelete:
			if r.URL.Query().Has("policy") {
				h.deleteBucketPolicy(w, r, bucket)
				return
			}
			h.deleteBucket(w, r, bucket)
			return
		case http.MethodGet:
			if r.URL.Query().Has("notification") {
				h.getBucketNotification(w, r, bucket)
				return
			}
			if r.URL.Query().Has("policy") {
				h.getBucketPolicy(w, r, bucket)
				return
			}
			if r.URL.Query().Has("location") {
				h.getBucketLocation(w, r, bucket)
				return
			}
			h.listObjectsV2(w, r, bucket)
			return
		case http.MethodPost:
			if r.URL.Query().Has("delete") {
				h.deleteObjects(w, r, bucket)
				return
			}
		}
	default:
		// Operations on an object
		switch r.Method {
		case http.MethodPut:
			if copySource := r.Header.Get("x-amz-copy-source"); copySource != "" {
				h.copyObject(w, r, bucket, key, copySource)
				return
			}
			h.putObject(w, r, bucket, key)
			return
		case http.MethodGet:
			h.getObject(w, r, bucket, key)
			return
		case http.MethodHead:
			h.headObject(w, r, bucket, key)
			return
		case http.MethodDelete:
			h.deleteObject(w, r, bucket, key)
			return
		}
	}

	writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed against this resource.")
}

// --- Bucket operations ---

func (h *Handler) listBuckets(w http.ResponseWriter, _ *http.Request) {
	buckets := h.svc.ListBuckets()

	type xmlBucket struct {
		Name         string `xml:"Name"`
		CreationDate string `xml:"CreationDate"`
	}
	type xmlResponse struct {
		XMLName xml.Name    `xml:"ListAllMyBucketsResult"`
		Xmlns   string      `xml:"xmlns,attr"`
		Owner   xmlOwner    `xml:"Owner"`
		Buckets []xmlBucket `xml:"Buckets>Bucket"`
	}

	resp := xmlResponse{
		Xmlns: s3Namespace,
		Owner: defaultOwner(),
	}
	for _, b := range buckets {
		resp.Buckets = append(resp.Buckets, xmlBucket{
			Name:         b.Name,
			CreationDate: b.CreationDate.Format(time.RFC3339),
		})
	}
	if resp.Buckets == nil {
		resp.Buckets = []xmlBucket{}
	}

	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) createBucket(w http.ResponseWriter, _ *http.Request, bucket string) {
	_, err := h.svc.CreateBucket(bucket)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "BucketAlreadyOwnedByYou"):
			writeS3Error(w, http.StatusConflict, "BucketAlreadyOwnedByYou", "Your previous request to create the named bucket succeeded and you already own it.")
		case strings.Contains(msg, "InvalidBucketName"):
			writeS3Error(w, http.StatusBadRequest, "InvalidBucketName", msg)
		default:
			writeS3Error(w, http.StatusInternalServerError, "InternalError", msg)
		}
		return
	}
	w.Header().Set("Location", "/"+bucket)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) headBucket(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) deleteBucket(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.DeleteBucket(bucket); err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "NoSuchBucket"):
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		case strings.Contains(msg, "BucketNotEmpty"):
			writeS3Error(w, http.StatusConflict, "BucketNotEmpty", "The bucket you tried to delete is not empty")
		default:
			writeS3Error(w, http.StatusInternalServerError, "InternalError", msg)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getBucketLocation(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type locationResponse struct {
		XMLName  xml.Name `xml:"LocationConstraint"`
		Xmlns    string   `xml:"xmlns,attr"`
		Location string   `xml:",chardata"`
	}
	writeXML(w, http.StatusOK, locationResponse{Xmlns: s3Namespace, Location: "us-east-1"})
}

func (h *Handler) getBucketPolicy(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	writeS3Error(w, http.StatusNotFound, "NoSuchBucketPolicy", "The bucket policy does not exist")
}

func (h *Handler) putBucketPolicy(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketPolicy(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Object operations ---

func (h *Handler) listObjectsV2(w http.ResponseWriter, r *http.Request, bucket string) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	delimiter := q.Get("delimiter")
	contToken := q.Get("continuation-token")
	maxKeys := 1000
	if v := q.Get("max-keys"); v != "" {
		fmt.Sscanf(v, "%d", &maxKeys)
	}

	result, err := h.svc.ListObjects(bucket, prefix, delimiter, contToken, maxKeys)
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchBucket") {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		} else {
			writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return
	}

	type xmlObject struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
		ETag         string `xml:"ETag"`
		Size         int64  `xml:"Size"`
		StorageClass string `xml:"StorageClass"`
	}
	type xmlPrefix struct {
		Prefix string `xml:"Prefix"`
	}
	type xmlResponse struct {
		XMLName               xml.Name    `xml:"ListBucketResult"`
		Xmlns                 string      `xml:"xmlns,attr"`
		Name                  string      `xml:"Name"`
		Prefix                string      `xml:"Prefix"`
		Delimiter             string      `xml:"Delimiter,omitempty"`
		MaxKeys               int         `xml:"MaxKeys"`
		IsTruncated           bool        `xml:"IsTruncated"`
		KeyCount              int         `xml:"KeyCount"`
		Contents              []xmlObject `xml:"Contents"`
		CommonPrefixes        []xmlPrefix `xml:"CommonPrefixes,omitempty"`
		NextContinuationToken string      `xml:"NextContinuationToken,omitempty"`
	}

	resp := xmlResponse{
		Xmlns:                 s3Namespace,
		Name:                  result.Name,
		Prefix:                result.Prefix,
		Delimiter:             result.Delimiter,
		MaxKeys:               result.MaxKeys,
		IsTruncated:           result.IsTruncated,
		KeyCount:              result.KeyCount,
		NextContinuationToken: result.NextContinuationToken,
	}
	for _, obj := range result.Contents {
		resp.Contents = append(resp.Contents, xmlObject{
			Key:          obj.Key,
			LastModified: obj.LastModified.Format(time.RFC3339),
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: "STANDARD",
		})
	}
	for _, cp := range result.CommonPrefixes {
		resp.CommonPrefixes = append(resp.CommonPrefixes, xmlPrefix{Prefix: cp})
	}

	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) putObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	metadata := extractMetadata(r)

	obj, err := h.svc.PutObject(bucket, key, contentType, r.Body, metadata)
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchBucket") {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		} else {
			writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		}
		return
	}

	w.Header().Set("ETag", obj.ETag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getObject(w http.ResponseWriter, _ *http.Request, bucket, key string) {
	obj, reader, err := h.svc.GetObject(bucket, key)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "NoSuchBucket"):
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		case strings.Contains(msg, "NoSuchKey"):
			writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		default:
			writeS3Error(w, http.StatusInternalServerError, "InternalError", msg)
		}
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	for k, v := range obj.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

func (h *Handler) headObject(w http.ResponseWriter, _ *http.Request, bucket, key string) {
	obj, err := h.svc.HeadObject(bucket, key)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	for k, v := range obj.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) deleteObject(w http.ResponseWriter, _ *http.Request, bucket, key string) {
	h.svc.DeleteObject(bucket, key)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	type deleteRequest struct {
		XMLName xml.Name `xml:"Delete"`
		Objects []struct {
			Key string `xml:"Key"`
		} `xml:"Object"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed")
		return
	}

	var req deleteRequest
	if err := xml.Unmarshal(body, &req); err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed")
		return
	}

	keys := make([]string, len(req.Objects))
	for i, obj := range req.Objects {
		keys[i] = obj.Key
	}

	errs := h.svc.DeleteObjects(bucket, keys)

	type xmlDeleted struct {
		Key string `xml:"Key"`
	}
	type xmlError struct {
		Key     string `xml:"Key"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	type xmlResponse struct {
		XMLName xml.Name     `xml:"DeleteResult"`
		Xmlns   string       `xml:"xmlns,attr"`
		Deleted []xmlDeleted `xml:"Deleted"`
		Errors  []xmlError   `xml:"Error,omitempty"`
	}

	errKeys := make(map[string]bool)
	resp := xmlResponse{Xmlns: s3Namespace}
	for _, e := range errs {
		errKeys[e.Key] = true
		resp.Errors = append(resp.Errors, xmlError{Key: e.Key, Code: e.Code, Message: e.Message})
	}
	for _, key := range keys {
		if !errKeys[key] {
			resp.Deleted = append(resp.Deleted, xmlDeleted{Key: key})
		}
	}

	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) copyObject(w http.ResponseWriter, _ *http.Request, dstBucket, dstKey, copySource string) {
	// Parse x-amz-copy-source: /bucket/key
	copySource = strings.TrimPrefix(copySource, "/")
	srcBucket, srcKey, ok := strings.Cut(copySource, "/")
	if !ok || srcBucket == "" || srcKey == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "Invalid x-amz-copy-source header")
		return
	}

	obj, err := h.svc.CopyObject(srcBucket, srcKey, dstBucket, dstKey)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "NoSuchBucket"):
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		case strings.Contains(msg, "NoSuchKey"):
			writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		default:
			writeS3Error(w, http.StatusInternalServerError, "InternalError", msg)
		}
		return
	}

	type copyResult struct {
		XMLName      xml.Name `xml:"CopyObjectResult"`
		ETag         string   `xml:"ETag"`
		LastModified string   `xml:"LastModified"`
	}
	writeXML(w, http.StatusOK, copyResult{
		ETag:         obj.ETag,
		LastModified: obj.LastModified.Format(time.RFC3339),
	})
}

// --- Notification operations ---

func (h *Handler) putBucketNotification(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "Unable to read request body")
		return
	}

	type xmlFilter struct {
		Name  string `xml:"Name"`
		Value string `xml:"Value"`
	}
	type xmlLambdaConfig struct {
		ID       string   `xml:"Id"`
		Function string   `xml:"CloudFunction"`
		Events   []string `xml:"Event"`
		Filter   struct {
			Rules []xmlFilter `xml:"S3Key>FilterRule"`
		} `xml:"Filter"`
	}
	type xmlNotificationConfig struct {
		XMLName       xml.Name          `xml:"NotificationConfiguration"`
		LambdaConfigs []xmlLambdaConfig `xml:"CloudFunctionConfiguration"`
	}

	var xmlCfg xmlNotificationConfig
	if err := xml.Unmarshal(body, &xmlCfg); err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed")
		return
	}

	cfg := &types.BucketNotificationConfiguration{
		LambdaConfigurations: make([]types.S3LambdaNotification, 0, len(xmlCfg.LambdaConfigs)),
	}
	for _, lc := range xmlCfg.LambdaConfigs {
		fnName := extractFunctionName(lc.Function)
		nc := types.S3LambdaNotification{
			ID:                 lc.ID,
			LambdaFunctionArn:  lc.Function,
			LambdaFunctionName: fnName,
		}
		for _, evt := range lc.Events {
			nc.Events = append(nc.Events, types.S3EventName(evt))
		}
		for _, rule := range lc.Filter.Rules {
			switch rule.Name {
			case "prefix":
				nc.FilterPrefix = rule.Value
			case "suffix":
				nc.FilterSuffix = rule.Value
			}
		}
		cfg.LambdaConfigurations = append(cfg.LambdaConfigurations, nc)
	}

	if err := h.svc.PutBucketNotificationConfiguration(bucket, cfg); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) getBucketNotification(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}

	cfg := h.svc.GetBucketNotificationConfiguration(bucket)

	type xmlFilter struct {
		Name  string `xml:"Name"`
		Value string `xml:"Value"`
	}
	type xmlFilterWrapper struct {
		Rules []xmlFilter `xml:"S3Key>FilterRule"`
	}
	type xmlLambdaConfig struct {
		ID       string           `xml:"Id"`
		Function string           `xml:"CloudFunction"`
		Events   []string         `xml:"Event"`
		Filter   xmlFilterWrapper `xml:"Filter,omitempty"`
	}
	type xmlResponse struct {
		XMLName       xml.Name          `xml:"NotificationConfiguration"`
		Xmlns         string            `xml:"xmlns,attr"`
		LambdaConfigs []xmlLambdaConfig `xml:"CloudFunctionConfiguration,omitempty"`
	}

	resp := xmlResponse{Xmlns: s3Namespace}
	if cfg != nil {
		for _, lc := range cfg.LambdaConfigurations {
			xlc := xmlLambdaConfig{
				ID:       lc.ID,
				Function: lc.LambdaFunctionArn,
			}
			for _, evt := range lc.Events {
				xlc.Events = append(xlc.Events, string(evt))
			}
			if lc.FilterPrefix != "" {
				xlc.Filter.Rules = append(xlc.Filter.Rules, xmlFilter{Name: "prefix", Value: lc.FilterPrefix})
			}
			if lc.FilterSuffix != "" {
				xlc.Filter.Rules = append(xlc.Filter.Rules, xmlFilter{Name: "suffix", Value: lc.FilterSuffix})
			}
			resp.LambdaConfigs = append(resp.LambdaConfigs, xlc)
		}
	}

	writeXML(w, http.StatusOK, resp)
}

func extractFunctionName(arnOrName string) string {
	if idx := strings.Index(arnOrName, ":function:"); idx != -1 {
		tail := arnOrName[idx+len(":function:"):]
		if parts := strings.Split(tail, ":"); len(parts) > 0 {
			return parts[0]
		}
	}
	return arnOrName
}

// --- Helpers ---

type xmlOwner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

func defaultOwner() xmlOwner {
	return xmlOwner{ID: "openstack", DisplayName: "OpenStack"}
}

func extractMetadata(r *http.Request) map[string]string {
	meta := make(map[string]string)
	for key, vals := range r.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-amz-meta-") && len(vals) > 0 {
			metaKey := strings.TrimPrefix(lower, "x-amz-meta-")
			meta[metaKey] = vals[0]
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func writeXML(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(v)
}

func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	type errorResponse struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
		Message string   `xml:"Message"`
	}
	writeXML(w, status, errorResponse{Code: code, Message: message})
}
