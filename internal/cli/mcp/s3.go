package mcp

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListObjectsInput selects objects to list.
type ListObjectsInput struct {
	Bucket  string `json:"bucket" jsonschema:"Bucket name."`
	Prefix  string `json:"prefix,omitempty" jsonschema:"Only list keys starting with this prefix."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum keys to return. Defaults to 100, capped at 1000."`
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// S3Object is one key in a bucket.
type S3Object struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified,omitempty"`
}

// ListObjectsOutput reports bucket contents.
type ListObjectsOutput struct {
	Bucket    string     `json:"bucket"`
	Objects   []S3Object `json:"objects"`
	Returned  int        `json:"returned"`
	Truncated bool       `json:"truncated,omitempty" jsonschema:"Whether more keys exist beyond the limit."`
}

const listObjectsDescription = `List objects in an S3 bucket on the local Tarn instance.

Returns keys, sizes, and modification times, not contents. Read one object with
tarn_get_object.

Buckets on this instance exist only on this machine; the real AWS CLI will not
see them.`

func addListObjectsTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_list_objects",
		Description: listObjectsDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "List S3 objects",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in ListObjectsInput) (
		*mcp.CallToolResult, ListObjectsOutput, error,
	) {
		if strings.TrimSpace(in.Bucket) == "" {
			return nil, ListObjectsOutput{}, errors.New("bucket is required")
		}

		limit := clampInt(in.Limit, 100, 1000)
		q := url.Values{
			"list-type": {"2"},
			"max-keys":  {strconv.Itoa(limit)},
		}
		if in.Prefix != "" {
			q.Set("prefix", in.Prefix)
		}

		resp, err := c.do(ctx, http.MethodGet,
			"/"+url.PathEscape(in.Bucket)+"?"+q.Encode(), in.Account, "", nil)
		if err != nil {
			return nil, ListObjectsOutput{}, err
		}
		defer func() { _ = resp.Body.Close() }()

		body := readAllBounded(resp.Body)
		if resp.StatusCode >= 300 {
			return nil, ListObjectsOutput{}, fmt.Errorf("list objects returned HTTP %d: %s",
				resp.StatusCode, truncate(string(body), 400))
		}

		var result struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			IsTruncated bool     `xml:"IsTruncated"`
			Contents    []struct {
				Key          string `xml:"Key"`
				Size         int64  `xml:"Size"`
				LastModified string `xml:"LastModified"`
			} `xml:"Contents"`
		}
		if err := xml.Unmarshal(body, &result); err != nil {
			return nil, ListObjectsOutput{}, fmt.Errorf("failed to parse ListBucketResult: %w", err)
		}

		out := ListObjectsOutput{Bucket: in.Bucket, Truncated: result.IsTruncated}
		for _, o := range result.Contents {
			out.Objects = append(out.Objects, S3Object{
				Key: o.Key, Size: o.Size, LastModified: o.LastModified,
			})
		}
		out.Returned = len(out.Objects)
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}

// maxObjectBytes bounds how much of an object is returned as text. Object
// bodies go straight into a model's context, so this is a budget, not a
// memory limit.
const maxObjectBytes = 64 << 10

// GetObjectInput selects one object.
type GetObjectInput struct {
	Bucket  string `json:"bucket" jsonschema:"Bucket name."`
	Key     string `json:"key" jsonschema:"Object key."`
	Account string `json:"account,omitempty" jsonschema:"Twelve-digit account ID. Omit for the default account."`
}

// GetObjectOutput carries an object's contents when they are readable as text.
type GetObjectOutput struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Size   int64  `json:"size"`

	Content   string `json:"content,omitempty" jsonschema:"Object contents, present only when the object is UTF-8 text."`
	Truncated bool   `json:"truncated,omitempty" jsonschema:"Whether content was cut at 64 KB."`
	Binary    bool   `json:"binary,omitempty" jsonschema:"True when the object is not UTF-8 text, in which case content is omitted."`

	ContentType string `json:"contentType,omitempty"`
}

const getObjectDescription = `Read one object from an S3 bucket on the local Tarn instance.

Text objects are returned inline, truncated at 64 KB. Binary objects report
their size and content type but no contents, since they are not useful to read
as text.

Find keys first with tarn_list_objects.`

func addGetObjectTool(s *mcp.Server, c *client) {
	tool := &mcp.Tool{
		Name:        "tarn_get_object",
		Description: getObjectDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Read S3 object",
			ReadOnlyHint: true,
		},
	}

	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in GetObjectInput) (
		*mcp.CallToolResult, GetObjectOutput, error,
	) {
		if strings.TrimSpace(in.Bucket) == "" || strings.TrimSpace(in.Key) == "" {
			return nil, GetObjectOutput{}, errors.New("bucket and key are required")
		}

		resp, err := c.do(ctx, http.MethodGet,
			"/"+url.PathEscape(in.Bucket)+"/"+strings.TrimPrefix(in.Key, "/"), in.Account, "", nil)
		if err != nil {
			return nil, GetObjectOutput{}, err
		}
		defer func() { _ = resp.Body.Close() }()

		body := readAllBounded(resp.Body)
		if resp.StatusCode >= 300 {
			return nil, GetObjectOutput{}, fmt.Errorf("get object returned HTTP %d: %s",
				resp.StatusCode, truncate(string(body), 400))
		}

		out := GetObjectOutput{
			Bucket:      in.Bucket,
			Key:         in.Key,
			Size:        int64(len(body)),
			ContentType: resp.Header.Get("Content-Type"),
		}

		if !utf8.Valid(body) {
			out.Binary = true
			return nil, out, nil
		}

		if len(body) > maxObjectBytes {
			body = body[:maxObjectBytes]
			out.Truncated = true
		}
		out.Content = string(body)
		return nil, out, nil
	}

	mcp.AddTool(s, tool, handler)
}
