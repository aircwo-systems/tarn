package lambda

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseFunctionReference accepts a name, qualified name, partial ARN, or ARN.
// It preserves the qualifier rather than silently redirecting it to $LATEST.
func ParseFunctionReference(reference, qualifier string) (string, string, error) {
	reference = strings.TrimSpace(reference)
	if strings.Contains(reference, ":function:") {
		prefix, tail, _ := strings.Cut(reference, ":function:")
		if strings.HasPrefix(prefix, "arn:") {
			parts := strings.Split(prefix, ":")
			if len(parts) != 5 || parts[2] != "lambda" {
				return "", "", invalid("invalid function ARN")
			}
		} else if strings.Contains(prefix, ":") || prefix == "" {
			return "", "", invalid("invalid partial function ARN")
		}
		reference = tail
	}
	name, embedded, qualified := strings.Cut(reference, ":")
	if err := validName(name, 64); err != nil {
		return "", "", err
	}
	if qualified {
		if embedded == "" || strings.Contains(embedded, ":") {
			return "", "", invalid("invalid function qualifier")
		}
		if qualifier != "" && qualifier != embedded {
			return "", "", invalid("conflicting function qualifiers")
		}
		qualifier = embedded
	}
	if qualifier != "" && qualifier != "$LATEST" {
		if err := validName(qualifier, 128); err != nil {
			return "", "", err
		}
	}
	return name, qualifier, nil
}

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalidParameter, message) }

func validName(name string, maximum int) error {
	if name == "" || len(name) > maximum || name == "." || name == ".." {
		return invalid("invalid resource name: " + name)
	}
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '-', ch == '.':
		default:
			return invalid("invalid resource name: " + name)
		}
	}
	return nil
}

// checkedPath rejects traversal and symlinks within a configured storage root.
// The configured DataDir itself may legitimately resolve through a symlink.
func checkedPath(root string, parts ...string) (string, error) {
	path := filepath.Join(append([]string{root}, parts...)...)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", invalid("storage path escapes its root")
	}
	current := root
	components := []string{root}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		components = append(components, current)
	}
	for _, component := range components {
		info, err := os.Lstat(component)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", invalid("symlink in storage path")
		}
	}
	return path, nil
}
