// Package projects holds the data model + persistence for the .dev/-based
// project deployment system. See docs/superpowers/specs/2026-05-04-... .
package projects

import (
	"errors"
	"regexp"
	"strings"
)

// ErrEmptySlug is returned when a branch name slugifies to empty.
var ErrEmptySlug = errors.New("branch name slugifies to empty")

const branchSlugMaxLen = 30

var nonAlnumRe = regexp.MustCompile(`[^a-z0-9]+`)

// BranchSlug converts a branch name to a DNS-label-safe slug.
// Rules: lowercase, replace non-alphanumeric runs with "-", collapse repeats,
// trim leading/trailing "-", truncate to 30 chars.
// Returns ErrEmptySlug if the result is empty (branch was all special chars).
func BranchSlug(branch string) (string, error) {
	s := strings.ToLower(branch)
	s = nonAlnumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > branchSlugMaxLen {
		s = s[:branchSlugMaxLen]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		return "", ErrEmptySlug
	}
	return s, nil
}
