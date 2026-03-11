package collection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LambdaDir is a detected Lambda repository on the local filesystem.
type LambdaDir struct {
	Dir        string   // absolute path
	SchemasTs  string   // absolute path to schemas.ts (may be empty if not found)
	EventFiles []string // absolute paths to events/*.json files
}

// ScanMatch pairs a Lambda function name with a detected local directory.
type ScanMatch struct {
	FunctionName string   `json:"functionName"`
	Dir          string   `json:"dir"`
	SchemasTs    string   `json:"schemasTs,omitempty"`
	EventFiles   []string `json:"eventFiles,omitempty"`
	Score        float64  `json:"score"` // 0-1 similarity score
}

// ScanResult is the response from a local source scan.
type ScanResult struct {
	Matches    []ScanMatch `json:"matches"`
	Unmatched  []string    `json:"unmatched"`  // function names with no local match
	Discovered []string    `json:"discovered"` // dirs found but not matched to any function
}

// ScanLocalSource scans baseDir for Lambda project directories and attempts to
// match them to the provided function names.
func ScanLocalSource(baseDir string, functionNames []string) (*ScanResult, error) {
	dirs, err := findLambdaDirs(baseDir)
	if err != nil {
		return nil, err
	}

	result := &ScanResult{}
	usedDirs := make(map[string]bool)
	usedFuncs := make(map[string]bool)

	// Build a candidate list: for each function, find the best matching dir.
	var candidates []scoreCandidate

	for _, fn := range functionNames {
		for _, dir := range dirs {
			score := matchScore(fn, filepath.Base(dir.Dir))
			if score > 0.3 {
				candidates = append(candidates, scoreCandidate{fn, dir, score})
			}
		}
	}

	// Greedy best-first assignment.
	sortCandidatesDesc(candidates)
	for _, c := range candidates {
		if usedFuncs[c.fn] || usedDirs[c.dir.Dir] {
			continue
		}
		result.Matches = append(result.Matches, ScanMatch{
			FunctionName: c.fn,
			Dir:          c.dir.Dir,
			SchemasTs:    c.dir.SchemasTs,
			EventFiles:   c.dir.EventFiles,
			Score:        c.score,
		})
		usedFuncs[c.fn] = true
		usedDirs[c.dir.Dir] = true
	}

	for _, fn := range functionNames {
		if !usedFuncs[fn] {
			result.Unmatched = append(result.Unmatched, fn)
		}
	}
	for _, dir := range dirs {
		if !usedDirs[dir.Dir] {
			result.Discovered = append(result.Discovered, dir.Dir)
		}
	}

	return result, nil
}

// findLambdaDirs walks baseDir up to 3 levels deep looking for directories
// that have a package.json or index.js/handler.js (Lambda indicator) and
// optionally a schemas.ts.
func findLambdaDirs(baseDir string) ([]LambdaDir, error) {
	var results []LambdaDir

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(baseDir, e.Name())
		ld := probeLambdaDir(dir)
		if ld != nil {
			results = append(results, *ld)
		} else {
			// Try one level deeper (monorepo-ish structure)
			sub, _ := os.ReadDir(dir)
			for _, se := range sub {
				if !se.IsDir() || strings.HasPrefix(se.Name(), ".") {
					continue
				}
				subDir := filepath.Join(dir, se.Name())
				ld2 := probeLambdaDir(subDir)
				if ld2 != nil {
					results = append(results, *ld2)
				}
			}
		}
	}
	return results, nil
}

// lambdaIndicators are file names whose presence suggests a Lambda project.
var lambdaIndicators = []string{
	"handler.js", "handler.ts", "index.js", "index.ts",
	"package.json", "serverless.yml", "serverless.yaml",
}

func probeLambdaDir(dir string) *LambdaDir {
	hasIndicator := false
	for _, ind := range lambdaIndicators {
		if _, err := os.Stat(filepath.Join(dir, ind)); err == nil {
			hasIndicator = true
			break
		}
	}
	if !hasIndicator {
		return nil
	}

	ld := &LambdaDir{Dir: dir}

	// Find schemas.ts anywhere under the project (skipping node_modules etc).
	// If multiple found, prefer the one whose path contains "validate" or "schema".
	allSchemas := findAllFiles(dir, "schemas.ts")
	if len(allSchemas) == 1 {
		ld.SchemasTs = allSchemas[0]
	} else if len(allSchemas) > 1 {
		// Prefer paths that contain validation-related segments.
		best := allSchemas[0]
		for _, p := range allSchemas {
			rel := strings.ToLower(p)
			if strings.Contains(rel, "validate") || strings.Contains(rel, "schema") ||
				strings.Contains(rel, "validation") {
				best = p
				break
			}
		}
		ld.SchemasTs = best
	}

	// Gather events/*.json files.
	// Search for an "events" directory anywhere in the project tree.
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Include any .json file that lives directly inside a directory named "events".
		parent := filepath.Base(filepath.Dir(path))
		if parent == "events" && strings.HasSuffix(d.Name(), ".json") {
			ld.EventFiles = append(ld.EventFiles, path)
		}
		return nil
	})

	return ld
}

// skipDirs are directory names we never descend into when searching.
var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"coverage":     true,
	".serverless":  true,
}

// findAllFiles returns all files matching name under dir.
func findAllFiles(dir, name string) []string {
	var results []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == name {
			results = append(results, path)
		}
		return nil
	})
	return results
}

// ---- fuzzy matching ---------------------------------------------------------

// nonWordRe strips everything that is not a letter or digit for normalisation.
var nonWordRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// envSuffixRe removes common environment / deploy suffixes like -dev, -prod, -staging, etc.
var envSuffixRe = regexp.MustCompile(`(?i)[-_](dev|prod|staging|uat|qa|test|local|sandbox|v\d+)$`)

// normalizeName lower-cases, strips env suffixes and splits into a word set.
func normalizeName(s string) map[string]bool {
	s = envSuffixRe.ReplaceAllString(s, "")
	s = nonWordRe.ReplaceAllString(strings.ToLower(s), " ")
	words := strings.Fields(s)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 1 { // skip single chars
			m[w] = true
		}
	}
	return m
}

// matchScore returns a Jaccard similarity score (0-1) between two names.
func matchScore(a, b string) float64 {
	wa := normalizeName(a)
	wb := normalizeName(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

type scoreCandidate struct {
	fn    string
	dir   LambdaDir
	score float64
}

// sortCandidatesDesc sorts candidates by score descending.
func sortCandidatesDesc(candidates []scoreCandidate) {
	for i := 1; i < len(candidates); i++ {
		j := i
		for j > 0 && candidates[j].score > candidates[j-1].score {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
			j--
		}
	}
}

// LoadEventFile reads an events/*.json file and returns its raw JSON body.
// If the file is a Lambda proxy event (has "httpMethod" + "body"), the inner
// body string is parsed and returned — that is what the HTTP endpoint expects,
// not the full Lambda event wrapper.
func LoadEventFile(path string) (json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	// Detect Lambda proxy event format and extract the inner HTTP body.
	var evt struct {
		HTTPMethod string `json:"httpMethod"`
		Body       string `json:"body"`
	}
	if json.Unmarshal(raw, &evt) == nil && evt.HTTPMethod != "" && evt.Body != "" {
		var inner json.RawMessage
		if json.Unmarshal([]byte(evt.Body), &inner) == nil {
			return inner, nil
		}
	}
	return raw, nil
}
