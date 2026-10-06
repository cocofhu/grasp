package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// profileNamePattern restricts agent_profile directory names to a single safe
// segment: Unicode letters/digits plus `_-` (same charset as Agent names).
var profileNamePattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)

// safeProfileName returns a single-segment profile directory name or "".
func safeProfileName(profile string) string {
	base := filepath.Base(strings.TrimSpace(profile))
	if !profileNamePattern.MatchString(base) {
		return ""
	}
	return base
}

// underRoot joins root/rel and asserts the result stays within root.
func underRoot(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("non-local path %q", rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(absRoot, rel)
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+sep) {
		return "", fmt.Errorf("path %q escapes root", rel)
	}
	return absFull, nil
}

// profileDir resolves <profilesRoot>/<safeProfile> under-root (CodeQL #14–#16).
func profileDir(profilesRoot, profile string) (string, error) {
	name := safeProfileName(profile)
	if name == "" || strings.TrimSpace(profilesRoot) == "" {
		return "", fmt.Errorf("invalid agent_profile")
	}
	return underRoot(profilesRoot, name)
}
