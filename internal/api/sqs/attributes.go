package sqs

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// attributeMD5 implements SQS's length-prefixed encoding, independent of wire encoding.
func attributeMD5(attrs map[string]*types.MessageAttribute) string {
	if len(attrs) == 0 {
		return ""
	}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	h := md5.New()
	write := func(value []byte) {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(value)))
		h.Write(size[:])
		h.Write(value)
	}
	for _, name := range names {
		attr := attrs[name]
		write([]byte(name))
		write([]byte(attr.DataType))
		if strings.SplitN(attr.DataType, ".", 2)[0] == "Binary" {
			h.Write([]byte{2})
			write(attr.BinaryValue)
		} else {
			h.Write([]byte{1})
			write([]byte(attr.StringValue))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func selectMessageAttributes(attrs map[string]*types.MessageAttribute, names []string) map[string]*types.MessageAttribute {
	selected := make(map[string]*types.MessageAttribute)
	for name, attr := range attrs {
		for _, pattern := range names {
			if pattern == "All" || pattern == ".*" || pattern == name || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))) {
				selected[name] = attr
				break
			}
		}
	}
	return selected
}

func attributeMD5XML(attrs map[string]*types.MessageAttribute) string {
	if digest := attributeMD5(attrs); digest != "" {
		return "<MD5OfMessageAttributes>" + digest + "</MD5OfMessageAttributes>"
	}
	return ""
}
