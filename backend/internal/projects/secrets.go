package projects

import (
	"bufio"
	"bytes"
	"strings"
)

// ParseSecretsExample extracts the ordered list of secret keys declared in a
// .env-style template file. Values are ignored (they're examples, not real
// secrets). Comments (#) and blank lines are skipped. Lines using `export X=Y`
// shell syntax are tolerated. Duplicate keys keep the first occurrence.
//
// Returns nil (not empty slice) when no keys are found, matching how callers
// handle "no secrets needed" via len(keys) == 0.
func ParseSecretsExample(data []byte) []string {
	var keys []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue // no '=' or starts with '=' — not a key=value line
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}
