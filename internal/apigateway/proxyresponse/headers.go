// Package proxyresponse handles Lambda proxy response fields shared by HTTP and REST APIs.
package proxyresponse

import (
	"net/http"
	"slices"
)

// Headers merges Lambda proxy headers, keeping repeated values and preferring
// multiValueHeaders for any name-value pair present in both fields.
func Headers(single map[string]string, multi map[string][]string) http.Header {
	headers := make(http.Header, len(single)+len(multi))
	for name, values := range multi {
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	for name, value := range single {
		if !slices.Contains(headers.Values(name), value) {
			headers.Add(name, value)
		}
	}
	return headers
}
