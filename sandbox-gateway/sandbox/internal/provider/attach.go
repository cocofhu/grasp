package provider

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// attachmentRoot holds every materialized attachment for the sandbox's
// lifetime. Paths must stay valid after the turn: follow-up turns and a
// switched coding agent reopen the same files.
func attachmentRoot() string { return filepath.Join(os.TempDir(), "sbx-attach") }

// Attachment is one materialized file referenced from the prompt.
type Attachment struct {
	Path     string
	MimeType string
	History  bool
}

// MaterializeAttachments writes base64 image/file attachments to stable
// absolute paths the agent can open with ordinary Read tools. Each attachment
// lives in its own directory keyed by its blob id (or content hash), so the
// same upload always lands on the same path and names never collide. Files are
// kept after the turn; an existing file is reused rather than rewritten.
func MaterializeAttachments(files []PromptImage) ([]Attachment, error) {
	out := make([]Attachment, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	for i, f := range files {
		raw, err := DecodeAttachmentData(f.Data)
		if err != nil {
			return nil, fmt.Errorf("attach: decode #%d: %w", i+1, err)
		}
		name := AttachmentFileName(i, f)
		p, err := underRoot(attachmentRoot(), filepath.Join(attachmentKey(f.Ref, raw), name))
		if err != nil {
			return nil, fmt.Errorf("attach: unsafe name %q: %w", name, err)
		}
		if _, dup := seen[p]; dup {
			continue
		}
		if err := writeAttachment(p, raw); err != nil {
			return nil, fmt.Errorf("attach: write %s: %w", name, err)
		}
		seen[p] = struct{}{}
		out = append(out, Attachment{Path: p, MimeType: f.MimeType, History: f.History})
	}
	return out, nil
}

// attachmentKey is the blob id when ref is a well-formed blob:{id}, otherwise
// a content hash.
func attachmentKey(ref string, raw []byte) string {
	if id, ok := strings.CutPrefix(strings.TrimSpace(ref), "blob:"); ok && id != "" && len(id) <= 128 {
		valid := true
		for _, r := range id {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
				valid = false
				break
			}
		}
		if valid {
			return id
		}
	}
	sum := sha256.Sum256(raw)
	return "sha256-" + hex.EncodeToString(sum[:16])
}

// writeAttachment publishes raw at p via rename so a concurrent reader never
// sees a partial file.
func writeAttachment(p string, raw []byte) error {
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return nil
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
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
// attachment root (CodeQL #13).
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

// AppendAttachmentRefs appends a tagged block listing attachment paths. The
// tags keep the injected note apart from the user's own words, so a request
// like "rewrite this" is not applied to the note itself.
func AppendAttachmentRefs(text string, files []Attachment) string {
	if len(files) == 0 {
		return text
	}
	var current, history []Attachment
	for _, f := range files {
		if f.History {
			history = append(history, f)
		} else {
			current = append(current, f)
		}
	}
	var b strings.Builder
	if text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("<attached_files>\n")
	b.WriteString("（系统附加说明，不属于用户消息正文，不要改写或复述本段。需要了解附件内容时，用读文件工具打开下列本地路径；这些文件在后续对话中仍保留在原路径。）\n")
	writeAttachmentList(&b, "本轮用户上传的附件：", current)
	writeAttachmentList(&b, "此前对话中上传的附件：", history)
	b.WriteString("</attached_files>\n")
	return b.String()
}

func writeAttachmentList(b *strings.Builder, title string, files []Attachment) {
	if len(files) == 0 {
		return
	}
	b.WriteString(title)
	b.WriteString("\n")
	for _, f := range files {
		if mime := strings.TrimSpace(f.MimeType); mime != "" {
			fmt.Fprintf(b, "- %s (%s)\n", f.Path, mime)
		} else {
			fmt.Fprintf(b, "- %s\n", f.Path)
		}
	}
}
