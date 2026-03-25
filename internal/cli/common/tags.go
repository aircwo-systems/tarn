package common

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// ParseTagMap parses a comma-separated KEY=VALUE string into a tag map.
func ParseTagMap(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	tags := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid tag %q: expected KEY=VALUE", entry)
		}
		tags[key] = value
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

// ToSecretTags converts a string map into Secrets Manager tag entries.
func ToSecretTags(tags map[string]string) []types.SecretTag {
	if len(tags) == 0 {
		return nil
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]types.SecretTag, 0, len(keys))
	for _, key := range keys {
		out = append(out, types.SecretTag{Key: key, Value: tags[key]})
	}
	return out
}
