package provider

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaterializeAttachments writes base64 image/file attachments under a unique
// /tmp directory and returns absolute paths the agent can open with ordinary
// Read tools. Within one batch the first safe basename is kept; later copies
// of that name get a numeric suffix before the extension so bytes are not
// overwritten. Caller must os.RemoveAll(dir) when the turn finishes.
func MaterializeAttachments(files []PromptImage) (dir string, paths []string, err error) {
	dir, err = os.MkdirTemp("", "sbx-attach-*")
	if err != nil {
		return "", nil, fmt.Errorf("attach: mkdir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	paths = make([]string, 0, len(files))
	used := make(map[string]struct{}, len(files))
	for i, f := range files {
		raw, derr := DecodeAttachmentData(f.Data)
		if derr != nil {
			cleanup()
			return "", nil, fmt.Errorf("attach: decode #%d: %w", i+1, derr)
		}
		name, nerr := uniqueAttachmentName(used, AttachmentFileName(i, f))
		if nerr != nil {
			cleanup()
			return "", nil, fmt.Errorf("attach: name #%d: %w", i+1, nerr)
		}
		p, jerr := underRoot(dir, name)
		if jerr != nil {
			cleanup()
			return "", nil, fmt.Errorf("attach: unsafe name %q: %w", name, jerr)
		}
		if werr := os.WriteFile(p, raw, 0o600); werr != nil {
			cleanup()
			return "", nil, fmt.Errorf("attach: write %s: %w", name, werr)
		}
		paths = append(paths, p)
	}
	return dir, paths, nil
}

// uniqueAttachmentName returns name on first use. A later duplicate becomes
// stem-N.ext (image.png, then image-2.png), skipping any suffix already taken
// in this batch. The chosen name is recorded in used.
func uniqueAttachmentName(used map[string]struct{}, name string) (string, error) {
	if _, ok := used[name]; !ok {
		used[name] = struct{}{}
		return name, nil
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	// A realistic batch is tiny. The cap only stops a pathological set of
	// names that already occupy every stem-N.ext we would try.
	const maxAttachmentNameSuffix = 10000
	for n := 2; n <= maxAttachmentNameSuffix; n++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, n, ext)
		if _, ok := used[candidate]; ok {
			continue
		}
		used[candidate] = struct{}{}
		return candidate, nil
	}
	return "", fmt.Errorf("duplicate name %q", name)
}

// DecodeAttachmentData decodes standard or raw base64 attachment payloads.
func DecodeAttachmentData(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty data")
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// AttachmentFileName picks a safe basename for an attachment.
// Explicitly rejects ".." (filepath.Base("..") == "..") to prevent escaping the
// MkdirTemp root (CodeQL #13).
func AttachmentFileName(i int, f PromptImage) string {
	n := filepath.Base(strings.TrimSpace(f.Name))
	if n != "" && n != "." && n != ".." && n != string(filepath.Separator) && !strings.Contains(n, "..") {
		return n
	}
	return fmt.Sprintf("attachment-%d%s", i+1, ExtForMIME(f.MimeType))
}

// underRoot joins root/rel and asserts the result stays within root.
func underRoot(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
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

// ExtForMIME maps a MIME type to a file extension (default .bin).
func ExtForMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/json", "text/json":
		return ".json"
	case "text/csv":
		return ".csv"
	case "text/xml", "application/xml":
		return ".xml"
	case "text/yaml", "application/yaml", "application/x-yaml":
		return ".yaml"
	default:
		return ".bin"
	}
}

// AppendAttachmentRefs appends absolute paths for the agent to read.
func AppendAttachmentRefs(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	var b strings.Builder
	if text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("用户上传了以下附件，请直接读取这些本地文件路径：\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "- %s\n", p)
	}
	return b.String()
}
