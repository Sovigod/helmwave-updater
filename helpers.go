package main

import (
	"log"
	"strings"
)

// verbose logger helper to avoid scattering `if verbose { ... }` blocks
func vlog(format string, args ...interface{}) {
	if verbose {
		log.Printf(format, args...)
	}
}

// helper to check tags (case-insensitive)
func hasTag(tags []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}
