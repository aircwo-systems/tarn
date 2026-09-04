package mcp

import "io"

// maxBodyBytes bounds any response this package reads into memory. Tool results
// are fed to a model, so an unbounded read is a context-window hazard as much
// as a memory one.
const maxBodyBytes = 1 << 20

func boolPtr(b bool) *bool { return &b }

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func defaultInt(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

// clampInt keeps a caller-supplied limit inside a sane range, substituting the
// default when unset.
func clampInt(v, fallback, max int) int {
	if v <= 0 {
		return fallback
	}
	if v > max {
		return max
	}
	return v
}

// readAllBounded reads at most maxBodyBytes from r.
func readAllBounded(r io.Reader) []byte {
	body, _ := io.ReadAll(io.LimitReader(r, maxBodyBytes))
	return body
}
