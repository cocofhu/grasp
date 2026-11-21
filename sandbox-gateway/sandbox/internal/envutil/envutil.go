// Package envutil reads and edits KEY=VALUE environment slices, used when
// normalizing credentials into the env an agent CLI is spawned with.
package envutil

import (
	"os"
	"strings"
)

// FirstNonEmpty returns the first non-empty value among the given process env keys.
func FirstNonEmpty(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func index(env []string, key string) int {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			return i
		}
	}
	return -1
}

// Get reads key's value from an env slice ("" if absent).
func Get(env []string, key string) string {
	idx := index(env, key)
	if idx < 0 {
		return ""
	}
	return strings.TrimPrefix(env[idx], key+"=")
}

// SetIfEmpty sets key=val only if val is non-empty and key is not already set.
func SetIfEmpty(env []string, key, val string) []string {
	if val == "" || Get(env, key) != "" {
		return env
	}
	return Upsert(env, key, val)
}

// Upsert inserts or replaces key=val in the env slice (returns a new slice
// when replacing, to avoid mutating the caller's backing array).
func Upsert(env []string, key, val string) []string {
	if idx := index(env, key); idx >= 0 {
		out := append([]string(nil), env...)
		out[idx] = key + "=" + val
		return out
	}
	return append(env, key+"="+val)
}
