package awsconnection

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func profileFiles() ([]string, []string) {
	home, _ := os.UserHomeDir()
	configFile, credentialsFile := os.Getenv("AWS_CONFIG_FILE"), os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if configFile == "" {
		configFile = filepath.Join(home, ".aws", "config")
	}
	if credentialsFile == "" {
		credentialsFile = filepath.Join(home, ".aws", "credentials")
	}
	return []string{configFile}, []string{credentialsFile}
}

// ProfileNames reads section names only. The SDK parses and validates the
// selected profile when used; credential values never enter API responses.
func ProfileNames() ([]string, error) {
	configs, credentials := profileFiles()
	names := map[string]bool{}
	for i, path := range append(configs, credentials...) {
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.New("cannot read AWS profile names; check AWS configuration file permissions")
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "[") {
				continue
			}
			end := strings.Index(line, "]")
			if end < 0 {
				continue
			}
			name := strings.TrimSpace(line[1:end])
			if i < len(configs) {
				if name != "default" && !strings.HasPrefix(name, "profile ") {
					continue
				}
				name = strings.TrimSpace(strings.TrimPrefix(name, "profile "))
			}
			if validProfileName(name) {
				names[name] = true
			}
		}
		err = scanner.Err()
		_ = f.Close()
		if err != nil {
			return nil, errors.New("cannot read AWS profile names; check AWS configuration file format")
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func validProfileName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\r\n\x00")
}
