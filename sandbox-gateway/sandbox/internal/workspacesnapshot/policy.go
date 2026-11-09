package workspacesnapshot

import (
	"backend/internal/workspacebinding"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

func safePath(name string) bool {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || path.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	if name == "." {
		return true
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func beneath(name, parent string) bool {
	return parent == "." || name == parent || strings.HasPrefix(name, parent+"/")
}

func mandatory(name string, additional []string) string {
	for _, item := range additional {
		if beneath(name, item) {
			return "platform_private"
		}
	}
	gitMetadata := false
	for _, part := range strings.Split(name, "/") {
		switch strings.ToLower(part) {
		case ".git":
			gitMetadata = true
		case ".ssh", ".gnupg", ".codex", ".claude", ".cursor", ".opencode", ".trae", ".qwen", ".gemini", ".codeium", ".grasp", ".grasp-workspace-control", ".grasp-runtime", ".grasp-credentials":
			return "platform_private"
		case ".cache":
			return "private_cache"
		}
	}
	// These files belong to the platform's authorization/config injection,
	// rather than ordinary project source such as an application .env file.
	switch strings.ToLower(path.Base(name)) {
	case ".git-credentials", ".gitconfig", ".netrc", ".npmrc", ".pypirc":
		return "authorization_private"
	}
	if gitMetadata {
		return "git_metadata_rebuilt"
	}
	return ""
}

func normalizeSpec(spec Spec, private []string) (Spec, error) {
	if spec.Binding != nil && !workspacebinding.ValidReference(*spec.Binding) {
		return spec, ErrUnsafe
	}
	if spec.PolicyVersion != PolicyVersion || len(spec.Paths) == 0 || len(spec.Paths) > 64 || len(spec.IncludeIgnored) > 1024 {
		return spec, ErrUnsafe
	}
	for _, list := range [][]string{spec.Paths, spec.IncludeIgnored} {
		for _, name := range list {
			if !safePath(name) || mandatory(name, private) != "" {
				return spec, ErrUnsafe
			}
		}
	}
	spec.Paths = slices.Clone(spec.Paths)
	spec.IncludeIgnored = slices.Clone(spec.IncludeIgnored)
	slices.Sort(spec.Paths)
	slices.Sort(spec.IncludeIgnored)
	for i, name := range spec.Paths {
		if slices.ContainsFunc(spec.Paths[:i], func(parent string) bool { return beneath(name, parent) }) {
			return spec, ErrUnsafe
		}
	}
	for _, name := range spec.IncludeIgnored {
		if !slices.ContainsFunc(spec.Paths, func(parent string) bool { return beneath(name, parent) }) {
			return spec, ErrUnsafe
		}
	}
	return spec, nil
}

func validLimits(l Limits) bool {
	for _, size := range []int64{l.MaxFileBytes, l.MaxBytes, l.MaxGitBytes, l.MaxArchiveBytes, l.MaxManifestBytes, l.MaxSpoolBytes} {
		if size <= 0 || size > 1<<50 {
			return false
		}
	}
	return l.MaxBytes >= l.MaxFileBytes && l.MaxSpoolBytes >= l.MaxArchiveBytes && l.MaxEntries > 0 && l.MaxEntries <= 1000000 && l.MaxRepositories > 0 && l.MaxRepositories <= 256 && l.MaxScanEntries >= l.MaxEntries && l.MaxScanEntries <= 10000000
}

func ignoredAncestor(name string, choices []string) bool {
	return slices.ContainsFunc(choices, func(choice string) bool { return beneath(choice, name) || beneath(name, choice) })
}

func safeLink(name, target string) bool {
	if target == "" || len(target) > 4096 || !utf8.ValidString(target) || path.IsAbs(target) || strings.ContainsAny(target, "\\\x00") {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}
