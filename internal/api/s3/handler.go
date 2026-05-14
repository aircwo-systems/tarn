package s3

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	s3svc "github.com/aircwo-systems/tarn/internal/s3"
	"github.com/aircwo-systems/tarn/pkg/types"
)

const s3Namespace = "http://s3.amazonaws.com/doc/2006-03-01/"

// multipartUpload holds in-flight part data for a multipart upload.
type multipartUpload struct {
	bucket      string
	key         string
	contentType string
	parts       map[int][]byte
}

// Handler serves the S3 REST XML API.
type Handler struct {
	svc       *s3svc.Service
	mu        sync.Mutex
	uploads   map[string]*multipartUpload
	uploadSeq int
}

// NewHandler creates a new S3 API handler.
func NewHandler(svc *s3svc.Service) *Handler {
	return &Handler{
		svc:     svc,
		uploads: make(map[string]*multipartUpload),
	}
}

func virtualHostedBucket(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, ".")
	if host == "" || net.ParseIP(host) != nil || !strings.HasSuffix(host, ".localhost") {
		return ""
	}
	bucket, _, found := strings.Cut(host, ".")
	if !found || bucket == "" {
		return ""
	}
	return bucket
}

// Dispatch routes S3 API requests by method, bucket, key and query params.
func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/_s3")
	if path == "" {
		path = "/"
	}

	// Parse bucket and key from path
	path = strings.TrimPrefix(path, "/")
	var bucket, key string
	if vhBucket := virtualHostedBucket(r.Host); vhBucket != "" {
		bucket = vhBucket
		key = path
	} else {
		bucket, key, _ = strings.Cut(path, "/")
	}

	switch {
	case bucket == "":
		if r.Method == http.MethodGet {
			h.listBuckets(w, r)
			return
		}
	case key == "":
		switch r.Method {
		case http.MethodPut:
			if r.URL.Query().Has("tagging") {
				h.putBucketTagging(w, r, bucket)
				return
			}
			q := r.URL.Query()
			switch {
			case q.Has("notification"):
				h.putBucketNotification(w, r, bucket)
			case q.Has("policy"):
				h.putBucketPolicy(w, r, bucket)
			case q.Has("versioning"):
				h.putBucketVersioning(w, r, bucket)
			case q.Has("encryption"):
				h.putBucketEncryption(w, r, bucket)
			case q.Has("cors"):
				h.putBucketCORS(w, r, bucket)
			case q.Has("lifecycle"):
				h.putBucketLifecycle(w, r, bucket)
			case q.Has("tagging"):
				h.putBucketTagging(w, r, bucket)
			case q.Has("publicAccessBlock"):
				h.putPublicAccessBlock(w, r, bucket)
			case q.Has("ownershipControls"):
				h.putBucketOwnershipControls(w, r, bucket)
			case q.Has("object-lock"):
				h.putBucketObjectLock(w, r, bucket)
			case q.Has("acl"):
				h.putBucketACL(w, r, bucket)
			case q.Has("logging"):
				h.putBucketLogging(w, r, bucket)
			case q.Has("accelerate"):
				h.putBucketAccelerate(w, r, bucket)
			case q.Has("replication"):
				h.putBucketReplication(w, r, bucket)
			case q.Has("requestPayment"):
				h.putBucketRequestPayment(w, r, bucket)
			default:
				h.createBucket(w, r, bucket)
			}
			return
		case http.MethodHead:
			h.headBucket(w, r, bucket)
			return
		case http.MethodDelete:
			if r.URL.Query().Has("tagging") {
				h.deleteBucketTagging(w, r, bucket)
				return
			}
			q := r.URL.Query()
			switch {
			case q.Has("policy"):
				h.deleteBucketPolicy(w, r, bucket)
			case q.Has("cors"):
				h.deleteBucketCORS(w, r, bucket)
			case q.Has("lifecycle"):
				h.deleteBucketLifecycle(w, r, bucket)
			case q.Has("tagging"):
				h.deleteBucketTagging(w, r, bucket)
			case q.Has("encryption"):
				h.deleteBucketEncryption(w, r, bucket)
			case q.Has("publicAccessBlock"):
				h.deletePublicAccessBlock(w, r, bucket)
			case q.Has("ownershipControls"):
				h.deleteBucketOwnershipControls(w, r, bucket)
			case q.Has("replication"):
				h.deleteBucketReplication(w, r, bucket)
			default:
				h.deleteBucket(w, r, bucket)
			}
			return
		case http.MethodGet:
			q := r.URL.Query()
			switch {
			case q.Has("notification"):
				h.getBucketNotification(w, r, bucket)
			case q.Has("policy"):
				h.getBucketPolicy(w, r, bucket)
			case q.Has("location"):
				h.getBucketLocation(w, r, bucket)
			case q.Has("versioning"):
				h.getBucketVersioning(w, r, bucket)
			case q.Has("encryption"):
				h.getBucketEncryption(w, r, bucket)
			case q.Has("cors"):
				h.getBucketCORS(w, r, bucket)
			case q.Has("logging"):
				h.getBucketLogging(w, r, bucket)
			case q.Has("acl"):
				h.getBucketACL(w, r, bucket)
			case q.Has("replication"):
				h.getBucketReplication(w, r, bucket)
			case q.Has("accelerate"):
				h.getBucketAccelerate(w, r, bucket)
			case q.Has("request-payment"):
				h.getBucketRequestPayment(w, r, bucket)
			case q.Has("object-lock"):
				h.getBucketObjectLock(w, r, bucket)
			case q.Has("tagging"):
				h.getBucketTagging(w, r, bucket)
			case q.Has("lifecycle"):
				h.getBucketLifecycle(w, r, bucket)
			case q.Has("publicAccessBlock"):
				h.getPublicAccessBlock(w, r, bucket)
			case q.Has("ownershipControls"):
				h.getBucketOwnershipControls(w, r, bucket)
			default:
				h.listObjectsV2(w, r, bucket)
			}
			return
		case http.MethodPost:
			if r.URL.Query().Has("delete") {
				h.deleteObjects(w, r, bucket)
				return
			}
		}
	default:
		switch r.Method {
		case http.MethodPut:
			if r.URL.Query().Has("partNumber") {
				h.uploadPart(w, r, bucket, key)
				return
			}
			if r.URL.Query().Has("tagging") {
				h.putObjectTagging(w, r, bucket, key)
				return
			}
			if copySource := r.Header.Get("x-amz-copy-source"); copySource != "" {
				h.copyObject(w, r, bucket, key, copySource)
				return
			}
			h.putObject(w, r, bucket, key)
			return
		case http.MethodPost:
			if r.URL.Query().Has("uploads") {
				h.createMultipartUpload(w, r, bucket, key)
				return
			}
			if r.URL.Query().Has("uploadId") {
				h.completeMultipartUpload(w, r, bucket, key)
				return
			}
		case http.MethodGet:
			if r.URL.Query().Has("tagging") {
				h.getObjectTagging(w, r, bucket, key)
				return
			}
			h.getObject(w, r, bucket, key)
			return
		case http.MethodDelete:
			if r.URL.Query().Has("uploadId") {
				h.abortMultipartUpload(w, r, bucket, key)
				return
			}
			if r.URL.Query().Has("tagging") {
				h.deleteObjectTagging(w, bucket, key)
				return
			}
			h.deleteObject(w, r, bucket, key)
			return
		case http.MethodHead:
			h.headObject(w, r, bucket, key)
			return
		}
	}

	writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed against this resource.")
}

// --- Bucket CRUD ---

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

	resp := xmlResponse{Xmlns: s3Namespace, Owner: defaultOwner()}
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

func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request, bucket string) {
	region := ""
	tags := map[string]string{}
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		var cfg struct {
			XMLName            xml.Name `xml:"CreateBucketConfiguration"`
			LocationConstraint string   `xml:"LocationConstraint"`
			Tags               []struct {
				Key   string `xml:"Key"`
				Value string `xml:"Value"`
			} `xml:"Tags>Tag"`
			TagSet []struct {
				Key   string `xml:"Key"`
				Value string `xml:"Value"`
			} `xml:"Tags>TagSet>Tag"`
		}
		if err := xml.Unmarshal(body, &cfg); err == nil && cfg.LocationConstraint != "" {
			if cfg.LocationConstraint == "us-east-1" {
				writeS3Error(w, http.StatusBadRequest, "InvalidLocationConstraint", "The specified location-constraint is not valid")
				return
			}
			region = cfg.LocationConstraint
		}
		for _, tag := range cfg.Tags {
			if tag.Key != "" {
				tags[tag.Key] = tag.Value
			}
		}
		for _, tag := range cfg.TagSet {
			if tag.Key != "" {
				tags[tag.Key] = tag.Value
			}
		}
	}
	if region == "" {
		region = signingRegion(r)
	}

	_, err := h.svc.CreateBucketInRegion(bucket, region)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "BucketAlreadyOwnedByYou"):
			w.Header().Set("Location", "/"+bucket)
			w.WriteHeader(http.StatusOK)
		case strings.Contains(msg, "InvalidBucketName"):
			writeS3Error(w, http.StatusBadRequest, "InvalidBucketName", msg)
		default:
			writeS3Error(w, http.StatusInternalServerError, "InternalError", msg)
		}
		return
	}
	if len(tags) > 0 {
		if err := h.svc.PutBucketTagging(bucket, tags); err != nil {
			writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
	}
	w.Header().Set("Location", "/"+bucket)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) headBucket(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if region, err := h.svc.GetBucketRegion(bucket); err == nil && region != "" {
		w.Header().Set("x-amz-bucket-region", region)
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

// --- Bucket config GET handlers ---

func (h *Handler) getBucketLocation(w http.ResponseWriter, _ *http.Request, bucket string) {
	region, err := h.svc.GetBucketRegion(bucket)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type locationResponse struct {
		XMLName  xml.Name `xml:"LocationConstraint"`
		Xmlns    string   `xml:"xmlns,attr"`
		Location string   `xml:",chardata"`
	}
	writeXML(w, http.StatusOK, locationResponse{Xmlns: s3Namespace, Location: region})
}

func (h *Handler) getBucketVersioning(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type versioningResponse struct {
		XMLName   xml.Name `xml:"VersioningConfiguration"`
		Xmlns     string   `xml:"xmlns,attr"`
		Status    string   `xml:"Status,omitempty"`
		MFADelete string   `xml:"MfaDelete,omitempty"`
	}
	resp := versioningResponse{Xmlns: s3Namespace}
	if v := h.svc.GetBucketVersioning(bucket); v != nil {
		resp.Status = v.Status
		resp.MFADelete = v.MFADelete
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketEncryption(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	enc := h.svc.GetBucketEncryption(bucket)
	if enc == nil || len(enc.Rules) == 0 {
		writeS3Error(w, http.StatusNotFound, "ServerSideEncryptionConfigurationNotFoundError", "The server side encryption configuration was not found")
		return
	}
	type xmlApplySSE struct {
		SSEAlgorithm   string `xml:"SSEAlgorithm"`
		KMSMasterKeyID string `xml:"KMSMasterKeyID,omitempty"`
	}
	type xmlSSERule struct {
		ApplySSE         xmlApplySSE `xml:"ApplyServerSideEncryptionByDefault"`
		BucketKeyEnabled bool        `xml:"BucketKeyEnabled"`
	}
	type xmlEncryption struct {
		XMLName xml.Name     `xml:"ServerSideEncryptionConfiguration"`
		Xmlns   string       `xml:"xmlns,attr"`
		Rules   []xmlSSERule `xml:"Rule"`
	}
	resp := xmlEncryption{Xmlns: s3Namespace}
	for _, rule := range enc.Rules {
		resp.Rules = append(resp.Rules, xmlSSERule{
			ApplySSE:         xmlApplySSE{SSEAlgorithm: rule.Algorithm, KMSMasterKeyID: rule.KMSMasterKeyID},
			BucketKeyEnabled: rule.BucketKeyEnabled,
		})
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketCORS(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	rules := h.svc.GetBucketCORS(bucket)
	if len(rules) == 0 {
		writeS3Error(w, http.StatusNotFound, "NoSuchCORSConfiguration", "The CORS configuration does not exist")
		return
	}
	type xmlCORSRule struct {
		ID             string   `xml:"ID,omitempty"`
		AllowedHeaders []string `xml:"AllowedHeader,omitempty"`
		AllowedMethods []string `xml:"AllowedMethod"`
		AllowedOrigins []string `xml:"AllowedOrigin"`
		ExposeHeaders  []string `xml:"ExposeHeader,omitempty"`
		MaxAgeSeconds  int      `xml:"MaxAgeSeconds,omitempty"`
	}
	type xmlCORSConfig struct {
		XMLName xml.Name      `xml:"CORSConfiguration"`
		Xmlns   string        `xml:"xmlns,attr"`
		Rules   []xmlCORSRule `xml:"CORSRule"`
	}
	resp := xmlCORSConfig{Xmlns: s3Namespace}
	for _, cr := range rules {
		resp.Rules = append(resp.Rules, xmlCORSRule{
			ID:             cr.ID,
			AllowedHeaders: cr.AllowedHeaders,
			AllowedMethods: cr.AllowedMethods,
			AllowedOrigins: cr.AllowedOrigins,
			ExposeHeaders:  cr.ExposeHeaders,
			MaxAgeSeconds:  cr.MaxAgeSeconds,
		})
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketLogging(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type xmlLoggingEnabled struct {
		TargetBucket string `xml:"TargetBucket"`
		TargetPrefix string `xml:"TargetPrefix,omitempty"`
	}
	type xmlLogging struct {
		XMLName        xml.Name           `xml:"BucketLoggingStatus"`
		Xmlns          string             `xml:"xmlns,attr"`
		LoggingEnabled *xmlLoggingEnabled `xml:"LoggingEnabled,omitempty"`
	}
	resp := xmlLogging{Xmlns: s3Namespace}
	if l := h.svc.GetBucketLogging(bucket); l != nil {
		resp.LoggingEnabled = &xmlLoggingEnabled{TargetBucket: l.TargetBucket, TargetPrefix: l.TargetPrefix}
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketACL(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type grant struct {
		Grantee struct {
			XmlnsXSI string `xml:"xmlns:xsi,attr"`
			Type     string `xml:"xsi:type,attr"`
			ID       string `xml:"ID"`
		} `xml:"Grantee"`
		Permission string `xml:"Permission"`
	}
	type aclResponse struct {
		XMLName           xml.Name `xml:"AccessControlPolicy"`
		Xmlns             string   `xml:"xmlns,attr"`
		Owner             xmlOwner `xml:"Owner"`
		AccessControlList []grant  `xml:"AccessControlList>Grant"`
	}

	g := grant{Permission: "FULL_CONTROL"}
	g.Grantee.XmlnsXSI = "http://www.w3.org/2001/XMLSchema-instance"
	g.Grantee.Type = "CanonicalUser"
	g.Grantee.ID = defaultOwner().ID

	writeXML(w, http.StatusOK, aclResponse{
		Xmlns:             s3Namespace,
		Owner:             defaultOwner(),
		AccessControlList: []grant{g},
	})
}

func (h *Handler) getBucketReplication(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	writeS3Error(w, http.StatusNotFound, "ReplicationConfigurationNotFoundError", "The replication configuration was not found")
}

func (h *Handler) getBucketAccelerate(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type accelerateResponse struct {
		XMLName xml.Name `xml:"AccelerateConfiguration"`
		Xmlns   string   `xml:"xmlns,attr"`
		Status  string   `xml:"Status"`
	}
	writeXML(w, http.StatusOK, accelerateResponse{Xmlns: s3Namespace, Status: "Suspended"})
}

func (h *Handler) getBucketRequestPayment(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	type requestPaymentResponse struct {
		XMLName xml.Name `xml:"RequestPaymentConfiguration"`
		Xmlns   string   `xml:"xmlns,attr"`
		Payer   string   `xml:"Payer"`
	}
	writeXML(w, http.StatusOK, requestPaymentResponse{Xmlns: s3Namespace, Payer: "BucketOwner"})
}

func (h *Handler) getBucketObjectLock(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	ol := h.svc.GetBucketObjectLock(bucket)
	if ol == nil {
		writeS3Error(w, http.StatusNotFound, "ObjectLockConfigurationNotFoundError", "Object Lock configuration does not exist for this bucket")
		return
	}
	type xmlRetention struct {
		Mode  string `xml:"Mode"`
		Days  int    `xml:"Days,omitempty"`
		Years int    `xml:"Years,omitempty"`
	}
	type xmlOLRule struct {
		DefaultRetention xmlRetention `xml:"DefaultRetention"`
	}
	type xmlObjectLock struct {
		XMLName           xml.Name   `xml:"ObjectLockConfiguration"`
		Xmlns             string     `xml:"xmlns,attr"`
		ObjectLockEnabled string     `xml:"ObjectLockEnabled"`
		Rule              *xmlOLRule `xml:"Rule,omitempty"`
	}
	resp := xmlObjectLock{Xmlns: s3Namespace, ObjectLockEnabled: ol.ObjectLockEnabled}
	if ol.Rule != nil {
		resp.Rule = &xmlOLRule{
			DefaultRetention: xmlRetention{
				Mode:  ol.Rule.DefaultRetention.Mode,
				Days:  ol.Rule.DefaultRetention.Days,
				Years: ol.Rule.DefaultRetention.Years,
			},
		}
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketTagging(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	tags := h.svc.GetBucketTagging(bucket)
	if len(tags) == 0 {
		writeS3Error(w, http.StatusNotFound, "NoSuchTagSet", "The TagSet does not exist")
		return
	}

	type xmlTag struct {
		Key   string `xml:"Key"`
		Value string `xml:"Value"`
	}
	type taggingResponse struct {
		XMLName xml.Name `xml:"Tagging"`
		Xmlns   string   `xml:"xmlns,attr"`
		TagSet  []xmlTag `xml:"TagSet>Tag"`
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	resp := taggingResponse{Xmlns: s3Namespace, TagSet: make([]xmlTag, 0, len(keys))}
	for _, key := range keys {
		resp.TagSet = append(resp.TagSet, xmlTag{Key: key, Value: tags[key]})
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketLifecycle(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	rules := h.svc.GetBucketLifecycle(bucket)
	if len(rules) == 0 {
		writeS3Error(w, http.StatusNotFound, "NoSuchLifecycleConfiguration", "The lifecycle configuration does not exist")
		return
	}
	type xmlExpiration struct {
		Days                      int    `xml:"Days,omitempty"`
		Date                      string `xml:"Date,omitempty"`
		ExpiredObjectDeleteMarker bool   `xml:"ExpiredObjectDeleteMarker,omitempty"`
	}
	type xmlNoncurrentExp struct {
		NoncurrentDays int `xml:"NoncurrentDays"`
	}
	type xmlAbortMPU struct {
		DaysAfterInitiation int `xml:"DaysAfterInitiation"`
	}
	type xmlFilter struct {
		Prefix string `xml:"Prefix,omitempty"`
	}
	type xmlLCRule struct {
		ID                             string            `xml:"ID,omitempty"`
		Filter                         *xmlFilter        `xml:"Filter,omitempty"`
		Status                         string            `xml:"Status"`
		Expiration                     *xmlExpiration    `xml:"Expiration,omitempty"`
		NoncurrentVersionExpiration    *xmlNoncurrentExp `xml:"NoncurrentVersionExpiration,omitempty"`
		AbortIncompleteMultipartUpload *xmlAbortMPU      `xml:"AbortIncompleteMultipartUpload,omitempty"`
	}
	type xmlLifecycle struct {
		XMLName xml.Name    `xml:"LifecycleConfiguration"`
		Xmlns   string      `xml:"xmlns,attr"`
		Rules   []xmlLCRule `xml:"Rule"`
	}
	resp := xmlLifecycle{Xmlns: s3Namespace}
	for _, rule := range rules {
		xr := xmlLCRule{ID: rule.ID, Status: rule.Status}
		if rule.Prefix != "" {
			xr.Filter = &xmlFilter{Prefix: rule.Prefix}
		}
		if rule.Expiration != nil {
			xr.Expiration = &xmlExpiration{Days: rule.Expiration.Days, Date: rule.Expiration.Date, ExpiredObjectDeleteMarker: rule.Expiration.ExpiredObjectDeleteMarker}
		}
		if rule.NoncurrentVersionExpiration != nil {
			xr.NoncurrentVersionExpiration = &xmlNoncurrentExp{NoncurrentDays: rule.NoncurrentVersionExpiration.NoncurrentDays}
		}
		if rule.AbortIncompleteMultipartUpload != nil {
			xr.AbortIncompleteMultipartUpload = &xmlAbortMPU{DaysAfterInitiation: rule.AbortIncompleteMultipartUpload.DaysAfterInitiation}
		}
		resp.Rules = append(resp.Rules, xr)
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *Handler) getBucketPolicy(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	policy := h.svc.GetBucketPolicy(bucket)
	if policy == "" {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucketPolicy", "The bucket policy does not exist")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(policy))
}

func (h *Handler) getPublicAccessBlock(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	pab := h.svc.GetPublicAccessBlock(bucket)
	if pab == nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchPublicAccessBlockConfiguration", "The public access block configuration was not found")
		return
	}
	type xmlPAB struct {
		XMLName               xml.Name `xml:"PublicAccessBlockConfiguration"`
		Xmlns                 string   `xml:"xmlns,attr"`
		BlockPublicAcls       bool     `xml:"BlockPublicAcls"`
		IgnorePublicAcls      bool     `xml:"IgnorePublicAcls"`
		BlockPublicPolicy     bool     `xml:"BlockPublicPolicy"`
		RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets"`
	}
	writeXML(w, http.StatusOK, xmlPAB{
		Xmlns:                 s3Namespace,
		BlockPublicAcls:       pab.BlockPublicAcls,
		IgnorePublicAcls:      pab.IgnorePublicAcls,
		BlockPublicPolicy:     pab.BlockPublicPolicy,
		RestrictPublicBuckets: pab.RestrictPublicBuckets,
	})
}

func (h *Handler) getBucketOwnershipControls(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	oc := h.svc.GetBucketOwnershipControls(bucket)
	if oc == "" {
		writeS3Error(w, http.StatusNotFound, "OwnershipControlsNotFoundError", "The bucket ownership controls were not found")
		return
	}
	type xmlOC struct {
		XMLName         xml.Name `xml:"OwnershipControls"`
		Xmlns           string   `xml:"xmlns,attr"`
		ObjectOwnership string   `xml:"Rule>ObjectOwnership"`
	}
	writeXML(w, http.StatusOK, xmlOC{Xmlns: s3Namespace, ObjectOwnership: oc})
}

// --- Bucket config PUT handlers ---

func (h *Handler) putBucketVersioning(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlVersioning struct {
		XMLName   xml.Name `xml:"VersioningConfiguration"`
		Status    string   `xml:"Status"`
		MFADelete string   `xml:"MfaDelete"`
	}
	var v xmlVersioning
	_ = xml.Unmarshal(body, &v)
	if err := h.svc.PutBucketVersioning(bucket, &types.BucketVersioning{Status: v.Status, MFADelete: v.MFADelete}); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketEncryption(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlApplySSE struct {
		SSEAlgorithm   string `xml:"SSEAlgorithm"`
		KMSMasterKeyID string `xml:"KMSMasterKeyID"`
	}
	type xmlSSERule struct {
		ApplySSE         xmlApplySSE `xml:"ApplyServerSideEncryptionByDefault"`
		BucketKeyEnabled bool        `xml:"BucketKeyEnabled"`
	}
	type xmlEncryption struct {
		XMLName xml.Name     `xml:"ServerSideEncryptionConfiguration"`
		Rules   []xmlSSERule `xml:"Rule"`
	}
	var enc xmlEncryption
	_ = xml.Unmarshal(body, &enc)
	cfg := &types.BucketEncryption{}
	for _, rule := range enc.Rules {
		cfg.Rules = append(cfg.Rules, types.SSERule{
			Algorithm:        rule.ApplySSE.SSEAlgorithm,
			KMSMasterKeyID:   rule.ApplySSE.KMSMasterKeyID,
			BucketKeyEnabled: rule.BucketKeyEnabled,
		})
	}
	if err := h.svc.PutBucketEncryption(bucket, cfg); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketCORS(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlCORSRule struct {
		ID             string   `xml:"ID"`
		AllowedHeaders []string `xml:"AllowedHeader"`
		AllowedMethods []string `xml:"AllowedMethod"`
		AllowedOrigins []string `xml:"AllowedOrigin"`
		ExposeHeaders  []string `xml:"ExposeHeader"`
		MaxAgeSeconds  int      `xml:"MaxAgeSeconds"`
	}
	type xmlCORSConfig struct {
		XMLName xml.Name      `xml:"CORSConfiguration"`
		Rules   []xmlCORSRule `xml:"CORSRule"`
	}
	var xmlCfg xmlCORSConfig
	if err := xml.Unmarshal(body, &xmlCfg); err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed")
		return
	}
	rules := make([]types.CORSRule, 0, len(xmlCfg.Rules))
	for _, cr := range xmlCfg.Rules {
		rules = append(rules, types.CORSRule{
			ID:             cr.ID,
			AllowedHeaders: cr.AllowedHeaders,
			AllowedMethods: cr.AllowedMethods,
			AllowedOrigins: cr.AllowedOrigins,
			ExposeHeaders:  cr.ExposeHeaders,
			MaxAgeSeconds:  cr.MaxAgeSeconds,
		})
	}
	if err := h.svc.PutBucketCORS(bucket, rules); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketLifecycle(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlExpiration struct {
		Days                      int    `xml:"Days"`
		Date                      string `xml:"Date"`
		ExpiredObjectDeleteMarker bool   `xml:"ExpiredObjectDeleteMarker"`
	}
	type xmlNoncurrentExp struct {
		NoncurrentDays int `xml:"NoncurrentDays"`
	}
	type xmlAbortMPU struct {
		DaysAfterInitiation int `xml:"DaysAfterInitiation"`
	}
	type xmlFilter struct {
		Prefix string `xml:"Prefix"`
	}
	type xmlLCRule struct {
		ID                             string            `xml:"ID"`
		Status                         string            `xml:"Status"`
		Filter                         *xmlFilter        `xml:"Filter"`
		Prefix                         string            `xml:"Prefix"`
		Expiration                     *xmlExpiration    `xml:"Expiration"`
		NoncurrentVersionExpiration    *xmlNoncurrentExp `xml:"NoncurrentVersionExpiration"`
		AbortIncompleteMultipartUpload *xmlAbortMPU      `xml:"AbortIncompleteMultipartUpload"`
	}
	type xmlLifecycle struct {
		XMLName xml.Name    `xml:"LifecycleConfiguration"`
		Rules   []xmlLCRule `xml:"Rule"`
	}
	var xmlCfg xmlLifecycle
	if err := xml.Unmarshal(body, &xmlCfg); err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed")
		return
	}
	rules := make([]types.LifecycleRule, 0, len(xmlCfg.Rules))
	for _, xr := range xmlCfg.Rules {
		rule := types.LifecycleRule{ID: xr.ID, Status: xr.Status, Prefix: xr.Prefix}
		if xr.Filter != nil && xr.Filter.Prefix != "" {
			rule.Prefix = xr.Filter.Prefix
		}
		if xr.Expiration != nil {
			rule.Expiration = &types.LifecycleExpiration{Days: xr.Expiration.Days, Date: xr.Expiration.Date, ExpiredObjectDeleteMarker: xr.Expiration.ExpiredObjectDeleteMarker}
		}
		if xr.NoncurrentVersionExpiration != nil {
			rule.NoncurrentVersionExpiration = &types.NoncurrentVersionExpiration{NoncurrentDays: xr.NoncurrentVersionExpiration.NoncurrentDays}
		}
		if xr.AbortIncompleteMultipartUpload != nil {
			rule.AbortIncompleteMultipartUpload = &types.AbortIncompleteMultipartUpload{DaysAfterInitiation: xr.AbortIncompleteMultipartUpload.DaysAfterInitiation}
		}
		rules = append(rules, rule)
	}
	if err := h.svc.PutBucketLifecycle(bucket, rules); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketTagging(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlTag struct {
		Key   string `xml:"Key"`
		Value string `xml:"Value"`
	}
	type xmlTagging struct {
		XMLName xml.Name `xml:"Tagging"`
		Tags    []xmlTag `xml:"TagSet>Tag"`
	}
	var t xmlTagging
	if err := xml.Unmarshal(body, &t); err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed")
		return
	}
	tags := make(map[string]string, len(t.Tags))
	for _, tag := range t.Tags {
		tags[tag.Key] = tag.Value
	}
	if err := h.svc.PutBucketTagging(bucket, tags); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putPublicAccessBlock(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlPAB struct {
		XMLName               xml.Name `xml:"PublicAccessBlockConfiguration"`
		BlockPublicAcls       bool     `xml:"BlockPublicAcls"`
		IgnorePublicAcls      bool     `xml:"IgnorePublicAcls"`
		BlockPublicPolicy     bool     `xml:"BlockPublicPolicy"`
		RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets"`
	}
	var pab xmlPAB
	_ = xml.Unmarshal(body, &pab)
	cfg := &types.PublicAccessBlockConfig{
		BlockPublicAcls:       pab.BlockPublicAcls,
		IgnorePublicAcls:      pab.IgnorePublicAcls,
		BlockPublicPolicy:     pab.BlockPublicPolicy,
		RestrictPublicBuckets: pab.RestrictPublicBuckets,
	}
	if err := h.svc.PutPublicAccessBlock(bucket, cfg); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketOwnershipControls(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlOC struct {
		XMLName         xml.Name `xml:"OwnershipControls"`
		ObjectOwnership string   `xml:"Rule>ObjectOwnership"`
	}
	var oc xmlOC
	_ = xml.Unmarshal(body, &oc)
	if oc.ObjectOwnership == "" {
		oc.ObjectOwnership = "BucketOwnerEnforced"
	}
	if err := h.svc.PutBucketOwnershipControls(bucket, oc.ObjectOwnership); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketObjectLock(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlRetention struct {
		Mode  string `xml:"Mode"`
		Days  int    `xml:"Days"`
		Years int    `xml:"Years"`
	}
	type xmlObjectLock struct {
		XMLName           xml.Name      `xml:"ObjectLockConfiguration"`
		ObjectLockEnabled string        `xml:"ObjectLockEnabled"`
		DefaultRetention  *xmlRetention `xml:"Rule>DefaultRetention"`
	}
	var ol xmlObjectLock
	_ = xml.Unmarshal(body, &ol)
	cfg := &types.ObjectLockConfig{ObjectLockEnabled: ol.ObjectLockEnabled}
	if ol.DefaultRetention != nil {
		cfg.Rule = &types.ObjectLockRule{
			DefaultRetention: types.ObjectLockRetention{
				Mode:  ol.DefaultRetention.Mode,
				Days:  ol.DefaultRetention.Days,
				Years: ol.DefaultRetention.Years,
			},
		}
	}
	if err := h.svc.PutBucketObjectLock(bucket, cfg); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketACL(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	acl := r.Header.Get("x-amz-acl")
	if acl == "" {
		acl = "private"
	}
	_, _ = io.Copy(io.Discard, r.Body)
	if err := h.svc.PutBucketACL(bucket, acl); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketLogging(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, _ := io.ReadAll(r.Body)
	type xmlLoggingEnabled struct {
		TargetBucket string `xml:"TargetBucket"`
		TargetPrefix string `xml:"TargetPrefix"`
	}
	type xmlLogging struct {
		XMLName        xml.Name           `xml:"BucketLoggingStatus"`
		LoggingEnabled *xmlLoggingEnabled `xml:"LoggingEnabled"`
	}
	var l xmlLogging
	_ = xml.Unmarshal(body, &l)
	var cfg *types.BucketLogging
	if l.LoggingEnabled != nil && l.LoggingEnabled.TargetBucket != "" {
		cfg = &types.BucketLogging{TargetBucket: l.LoggingEnabled.TargetBucket, TargetPrefix: l.LoggingEnabled.TargetPrefix}
	}
	if err := h.svc.PutBucketLogging(bucket, cfg); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketPolicy(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedPolicy", "Unable to read policy body")
		return
	}
	if err := h.svc.PutBucketPolicy(bucket, string(body)); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) putBucketAccelerate(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketReplication(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putBucketRequestPayment(w http.ResponseWriter, r *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusOK)
}

// --- Bucket config DELETE handlers ---

func (h *Handler) deleteBucketPolicy(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeleteBucketPolicy(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketCORS(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeleteBucketCORS(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketLifecycle(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeleteBucketLifecycle(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketTagging(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeleteBucketTagging(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketEncryption(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeleteBucketEncryption(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deletePublicAccessBlock(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeletePublicAccessBlock(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketOwnershipControls(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	h.svc.DeleteBucketOwnershipControls(bucket)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteBucketReplication(w http.ResponseWriter, _ *http.Request, bucket string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

	obj, err := h.svc.PutObject(bucket, key, contentType, r.Body, extractMetadata(r))
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
	_, _ = io.Copy(w, reader)
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

// --- Object tagging ---

func (h *Handler) getObjectTagging(w http.ResponseWriter, _ *http.Request, bucket, key string) {
	if _, err := h.svc.HeadObject(bucket, key); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	type xmlTag struct {
		Key   string `xml:"Key"`
		Value string `xml:"Value"`
	}
	type taggingResponse struct {
		XMLName xml.Name `xml:"Tagging"`
		Xmlns   string   `xml:"xmlns,attr"`
		TagSet  struct {
			Tags []xmlTag `xml:"Tag"`
		} `xml:"TagSet"`
	}
	writeXML(w, http.StatusOK, taggingResponse{Xmlns: s3Namespace})
}

func (h *Handler) putObjectTagging(w http.ResponseWriter, _ *http.Request, bucket, key string) {
	if _, err := h.svc.HeadObject(bucket, key); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteObjectTagging(w http.ResponseWriter, bucket, key string) {
	if _, err := h.svc.HeadObject(bucket, key); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
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
	copySource = strings.TrimPrefix(copySource, "/")
	srcBucket, srcKey, ok := strings.Cut(copySource, "/")
	if !ok || srcBucket == "" || srcKey == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "Invalid x-amz-copy-source header")
		return
	}
	if decoded, err := url.PathUnescape(srcKey); err == nil {
		srcKey = decoded
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

// --- Multipart upload ---

func (h *Handler) createMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if err := h.svc.HeadBucket(bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	h.mu.Lock()
	h.uploadSeq++
	uploadID := fmt.Sprintf("tarn-mpu-%d", h.uploadSeq)
	h.uploads[uploadID] = &multipartUpload{
		bucket:      bucket,
		key:         key,
		contentType: contentType,
		parts:       make(map[int][]byte),
	}
	h.mu.Unlock()

	type xmlResponse struct {
		XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
		Xmlns    string   `xml:"xmlns,attr"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		UploadID string   `xml:"UploadId"`
	}
	writeXML(w, http.StatusOK, xmlResponse{
		Xmlns:    s3Namespace,
		Bucket:   bucket,
		Key:      key,
		UploadID: uploadID,
	})
}

func (h *Handler) uploadPart(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")
	partNumStr := r.URL.Query().Get("partNumber")

	h.mu.Lock()
	upload := h.uploads[uploadID]
	h.mu.Unlock()

	if upload == nil || upload.bucket != bucket || upload.key != key {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist")
		return
	}

	partNum, _ := strconv.Atoi(partNumStr)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to read part body")
		return
	}

	h.mu.Lock()
	upload.parts[partNum] = data
	h.mu.Unlock()

	etag := fmt.Sprintf("\"part-%d-%d\"", partNum, len(data))
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) completeMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")

	h.mu.Lock()
	upload := h.uploads[uploadID]
	h.mu.Unlock()

	if upload == nil || upload.bucket != bucket || upload.key != key {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist")
		return
	}

	type xmlPart struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	}
	type xmlComplete struct {
		XMLName xml.Name  `xml:"CompleteMultipartUpload"`
		Parts   []xmlPart `xml:"Part"`
	}
	body, _ := io.ReadAll(r.Body)
	var req xmlComplete
	_ = xml.Unmarshal(body, &req)

	h.mu.Lock()
	var assembled bytes.Buffer
	for _, p := range req.Parts {
		if data, ok := upload.parts[p.PartNumber]; ok {
			assembled.Write(data)
		}
	}
	delete(h.uploads, uploadID)
	h.mu.Unlock()

	obj, err := h.svc.PutObject(bucket, key, upload.contentType, bytes.NewReader(assembled.Bytes()), nil)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	type xmlResponse struct {
		XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
		Xmlns    string   `xml:"xmlns,attr"`
		Location string   `xml:"Location"`
		Bucket   string   `xml:"Bucket"`
		Key      string   `xml:"Key"`
		ETag     string   `xml:"ETag"`
	}
	writeXML(w, http.StatusOK, xmlResponse{
		Xmlns:    s3Namespace,
		Location: fmt.Sprintf("/%s/%s", bucket, key),
		Bucket:   bucket,
		Key:      key,
		ETag:     obj.ETag,
	})
}

func (h *Handler) abortMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key string) {
	uploadID := r.URL.Query().Get("uploadId")
	h.mu.Lock()
	delete(h.uploads, uploadID)
	h.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// --- Helpers ---

type xmlOwner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

func defaultOwner() xmlOwner {
	return xmlOwner{ID: "tarn", DisplayName: "Tarn"}
}

func extractMetadata(r *http.Request) map[string]string {
	meta := make(map[string]string)
	for key, vals := range r.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-amz-meta-") && len(vals) > 0 {
			meta[strings.TrimPrefix(lower, "x-amz-meta-")] = vals[0]
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
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(v)
}

// signingRegion extracts the AWS region from the Authorization header credential scope.
func signingRegion(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if idx := strings.Index(auth, "Credential="); idx >= 0 {
		cred := auth[idx+len("Credential="):]
		if end := strings.IndexByte(cred, ','); end >= 0 {
			cred = cred[:end]
		}
		// Credential=AKID/date/region/service/aws4_request
		parts := strings.Split(cred, "/")
		if len(parts) >= 3 {
			return parts[2]
		}
	}
	return ""
}

func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	type errorResponse struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
		Message string   `xml:"Message"`
	}
	writeXML(w, status, errorResponse{Code: code, Message: message})
}
