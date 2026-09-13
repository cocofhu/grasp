package services

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	OrgFolderKind          = "org-folder"
	OrgFolderSchemaVersion = 1
	OrgFolderMaxBytes      = 64 << 20 // 64 MiB
)

var (
	ErrOrgFolderTooLarge        = errors.New("文件夹包超过 64MiB 上限")
	ErrOrgFolderMissingManifest = errors.New("ZIP 缺少 folder.json，无法识别为文件夹包")
	ErrOrgFolderSingleAgent     = errors.New("这是单 Agent ZIP，组级导入仅接受文件夹包，请改用顶栏「导入」")
	ErrOrgFolderInvalidKind     = errors.New("folder.json kind 无效，须为 org-folder")
	ErrOrgFolderBadSchema       = errors.New("不支持的 folder.json schemaVersion")
	ErrOrgFolderGroupNotFound   = errors.New("组不存在")
	ErrOrgFolderInvalidZip      = errors.New("ZIP 格式非法或已损坏")
	ErrOrgFolderRootAgentJSON   = errors.New("文件夹包根目录不得包含 agent.json")
	ErrOrgFolderNestedZip       = errors.New("文件夹包不得包含嵌套 ZIP")
	ErrOrgFolderTargetNotFound  = errors.New("目标组不存在")
)

// orgFolderJSON is the root folder.json inside a folder export ZIP.
type orgFolderJSON struct {
	Kind          string                        `json:"kind"`
	SchemaVersion int                           `json:"schemaVersion"`
	ExportedAt    string                        `json:"exportedAt"`
	RootGroupID   string                        `json:"rootGroupId"`
	Groups        []OrgGroup                    `json:"groups"`
	Agents        map[string]OrgAgentMembership `json:"agents"`
	AgentNames    []string                      `json:"agentNames"`
}

// GroupSubtree is a virtual-group closure (not parentAgent reporting).
type GroupSubtree struct {
	RootGroupID string
	Groups      []OrgGroup
	AgentNames  []string
	Memberships map[string]OrgAgentMembership
}

// ImportFolderMode selects batch rename vs overwrite.
type ImportFolderMode string

const (
	ImportFolderRename    ImportFolderMode = "rename"
	ImportFolderOverwrite ImportFolderMode = "overwrite"
)

// ImportFolderResult is returned after a successful folder import.
type ImportFolderResult struct {
	Org         AgentOrg          `json:"org"`
	Created     []string          `json:"created,omitempty"`
	Overwritten []string          `json:"overwritten,omitempty"`
	Renamed     map[string]string `json:"renamed,omitempty"`
}

// CollectGroupSubtree returns the target group + descendants + deduped agents.
// Empty groups are kept. Membership is clipped to the subtree.
func CollectGroupSubtree(org AgentOrg, rootGroupID string) (GroupSubtree, error) {
	rootGroupID = strings.TrimSpace(rootGroupID)
	if rootGroupID == "" {
		return GroupSubtree{}, fmt.Errorf("%w: groupId is required", ErrOrgFolderGroupNotFound)
	}
	byID := map[string]OrgGroup{}
	children := map[string][]string{}
	found := false
	for _, g := range org.Groups {
		byID[g.ID] = g
		p := strings.TrimSpace(g.ParentGroupID)
		children[p] = append(children[p], g.ID)
		if g.ID == rootGroupID {
			found = true
		}
	}
	if !found {
		return GroupSubtree{}, fmt.Errorf("%w: %s", ErrOrgFolderGroupNotFound, rootGroupID)
	}

	idSet := map[string]struct{}{rootGroupID: {}}
	queue := []string{rootGroupID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, cid := range children[cur] {
			if _, ok := idSet[cid]; ok {
				continue
			}
			idSet[cid] = struct{}{}
			queue = append(queue, cid)
		}
	}

	groups := make([]OrgGroup, 0, len(idSet))
	for id := range idSet {
		g := byID[id]
		if g.ID == rootGroupID {
			g.ParentGroupID = ""
		} else if _, ok := idSet[strings.TrimSpace(g.ParentGroupID)]; !ok {
			g.ParentGroupID = ""
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })

	agentSet := map[string]struct{}{}
	memberships := map[string]OrgAgentMembership{}
	for name, m := range org.Agents {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var gids []string
		for _, gid := range m.GroupIDs {
			if _, ok := idSet[gid]; ok {
				gids = append(gids, gid)
			}
		}
		if len(gids) == 0 {
			continue
		}
		agentSet[name] = struct{}{}
		memberships[name] = OrgAgentMembership{
			GroupIDs: uniqueNonEmpty(gids),
		}
	}
	names := make([]string, 0, len(agentSet))
	for n := range agentSet {
		names = append(names, n)
	}
	sort.Strings(names)
	return GroupSubtree{
		RootGroupID: rootGroupID,
		Groups:      groups,
		AgentNames:  names,
		Memberships: memberships,
	}, nil
}

// ExportFolderZIP builds a folder package for groupID. Caller-visible errors.
func (o *OrgService) ExportFolderZIP(groupID string) ([]byte, string, error) {
	if o == nil || o.skill == nil {
		return nil, "", fmt.Errorf("org service unavailable")
	}
	o.skill.mu.Lock()
	defer o.skill.mu.Unlock()
	o.mu.Lock()
	defer o.mu.Unlock()

	org, _, err := o.loadLocked()
	if err != nil {
		return nil, "", err
	}
	sub, err := CollectGroupSubtree(org, groupID)
	if err != nil {
		return nil, "", err
	}

	// Only export agents that still exist on disk.
	alive := make([]string, 0, len(sub.AgentNames))
	for _, name := range sub.AgentNames {
		if o.skill.Exists(name) {
			alive = append(alive, name)
		} else {
			delete(sub.Memberships, name)
		}
	}
	sub.AgentNames = alive

	rootName := groupID
	for _, g := range sub.Groups {
		if g.ID == sub.RootGroupID {
			rootName = g.Name
			break
		}
	}
	downloadName := sanitizeDownloadFilename(rootName) + ".zip"

	manifest := orgFolderJSON{
		Kind:          OrgFolderKind,
		SchemaVersion: OrgFolderSchemaVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		RootGroupID:   sub.RootGroupID,
		Groups:        sub.Groups,
		Agents:        sub.Memberships,
		AgentNames:    sub.AgentNames,
	}
	meta, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", err
	}

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	hdr := &zip.FileHeader{Name: "folder.json", Method: zip.Store}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		_ = zw.Close()
		return nil, "", err
	}
	if _, err := w.Write(meta); err != nil {
		_ = zw.Close()
		return nil, "", err
	}

	for _, name := range sub.AgentNames {
		prefix := "agents/" + name + "/"
		if err := o.skill.writeAgentToZip(zw, name, prefix); err != nil {
			_ = zw.Close()
			return nil, "", err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	out := buf.Bytes()
	if len(out) > OrgFolderMaxBytes {
		return nil, "", ErrOrgFolderTooLarge
	}
	return out, downloadName, nil
}

// ImportFolderZIP imports a folder package. targetGroupID empty → new root group.
func (o *OrgService) ImportFolderZIP(raw []byte, targetGroupID string, mode ImportFolderMode) (result ImportFolderResult, err error) {
	if o == nil || o.skill == nil {
		return ImportFolderResult{}, fmt.Errorf("org service unavailable")
	}
	if int64(len(raw)) > OrgFolderMaxBytes {
		return ImportFolderResult{}, ErrOrgFolderTooLarge
	}
	switch mode {
	case ImportFolderRename, ImportFolderOverwrite:
	default:
		return ImportFolderResult{}, fmt.Errorf("invalid import mode %q", mode)
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return ImportFolderResult{}, fmt.Errorf("%w: %v", ErrOrgFolderInvalidZip, err)
	}

	hasFolder, hasRootAgent := false, false
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		name = strings.TrimPrefix(name, "./")
		if strings.HasSuffix(name, "/") {
			continue
		}
		if name == "folder.json" {
			hasFolder = true
		}
		if name == "agent.json" {
			hasRootAgent = true
		}
		if strings.HasSuffix(strings.ToLower(name), ".zip") && strings.HasPrefix(name, "agents/") {
			return ImportFolderResult{}, ErrOrgFolderNestedZip
		}
	}
	if !hasFolder {
		if hasRootAgent {
			return ImportFolderResult{}, ErrOrgFolderSingleAgent
		}
		return ImportFolderResult{}, ErrOrgFolderMissingManifest
	}
	if hasRootAgent {
		return ImportFolderResult{}, ErrOrgFolderRootAgentJSON
	}

	manifest, err := readFolderManifest(zr)
	if err != nil {
		return ImportFolderResult{}, err
	}

	agentExports, err := parseFolderAgents(raw, manifest.AgentNames)
	if err != nil {
		return ImportFolderResult{}, err
	}

	o.skill.mu.Lock()
	defer o.skill.mu.Unlock()
	o.mu.Lock()
	defer o.mu.Unlock()

	org, _, err := o.loadLocked()
	if err != nil {
		return ImportFolderResult{}, err
	}

	targetGroupID = strings.TrimSpace(targetGroupID)
	if targetGroupID != "" {
		found := false
		for _, g := range org.Groups {
			if g.ID == targetGroupID {
				found = true
				break
			}
		}
		if !found {
			return ImportFolderResult{}, fmt.Errorf("%w: %s", ErrOrgFolderTargetNotFound, targetGroupID)
		}
	}

	idMap := map[string]string{}
	for _, g := range manifest.Groups {
		idMap[strings.TrimSpace(g.ID)] = NewGroupID()
	}

	newGroups := make([]OrgGroup, 0, len(manifest.Groups))
	for _, g := range manifest.Groups {
		oldID := strings.TrimSpace(g.ID)
		ng := OrgGroup{
			ID:   idMap[oldID],
			Name: strings.TrimSpace(g.Name),
		}
		if oldID == manifest.RootGroupID || strings.TrimSpace(g.ParentGroupID) == "" {
			ng.ParentGroupID = targetGroupID
		} else if mapped, ok := idMap[strings.TrimSpace(g.ParentGroupID)]; ok {
			ng.ParentGroupID = mapped
		}
		newGroups = append(newGroups, ng)
	}

	existingNames := map[string]struct{}{}
	for _, a := range o.skill.List() {
		existingNames[a.Name] = struct{}{}
	}

	nameMap := map[string]string{}
	created := []string{}
	overwritten := []string{}
	renamed := map[string]string{}

	for _, origName := range manifest.AgentNames {
		origName = strings.TrimSpace(origName)
		if origName == "" {
			continue
		}
		_, exists := existingNames[origName]
		finalName := origName
		if exists {
			if mode == ImportFolderRename {
				candidate := SuggestAgentRename(origName, existingNames)
				normalized, nerr := NormalizeAndValidateAgentName(candidate)
				if nerr != nil {
					return ImportFolderResult{}, fmt.Errorf("无法为 %q 生成合法重命名：%w", origName, nerr)
				}
				finalName = normalized
				renamed[origName] = finalName
				created = append(created, finalName)
				existingNames[finalName] = struct{}{}
			} else {
				overwritten = append(overwritten, origName)
			}
		} else {
			normalized, nerr := NormalizeAndValidateAgentName(origName)
			if nerr != nil {
				return ImportFolderResult{}, fmt.Errorf("包内 Agent 名称无效 %q：%w", origName, nerr)
			}
			finalName = normalized
			created = append(created, finalName)
			existingNames[finalName] = struct{}{}
		}
		nameMap[origName] = finalName
	}

	snapDir, err := os.MkdirTemp("", "org-folder-import-*")
	if err != nil {
		return ImportFolderResult{}, err
	}
	defer os.RemoveAll(snapDir)

	orgSnap, orgSnapErr := os.ReadFile(o.path())
	orgExisted := orgSnapErr == nil
	if orgSnapErr != nil && !os.IsNotExist(orgSnapErr) {
		return ImportFolderResult{}, orgSnapErr
	}

	for _, name := range overwritten {
		src := filepath.Join(o.skill.root, sanitize(name))
		dst := filepath.Join(snapDir, "agents", sanitize(name))
		if err := copyDir(src, dst); err != nil {
			return ImportFolderResult{}, fmt.Errorf("快照 Agent %q 失败：%w", name, err)
		}
	}

	var importErr error
	defer func() {
		if importErr == nil {
			return
		}
		for _, name := range created {
			_ = o.skill.deleteUnlocked(name)
		}
		for _, name := range overwritten {
			dst := filepath.Join(o.skill.root, sanitize(name))
			src := filepath.Join(snapDir, "agents", sanitize(name))
			_ = os.RemoveAll(dst)
			_ = copyDir(src, dst)
		}
		if orgExisted {
			_ = os.WriteFile(o.path(), orgSnap, 0o644)
		} else {
			_ = os.Remove(o.path())
		}
	}()

	for origName, finalName := range nameMap {
		parsed, ok := agentExports[origName]
		if !ok {
			importErr = fmt.Errorf("包内缺少 Agent %q 的快照", origName)
			return ImportFolderResult{}, fmt.Errorf("导入失败，已整次回滚：%w", importErr)
		}
		zipMode := ImportZIPCreate
		if mode == ImportFolderOverwrite && containsString(overwritten, finalName) {
			zipMode = ImportZIPOverwrite
		}
		if _, err := o.skill.applyAgentExport(parsed.export, parsed.files, finalName, zipMode); err != nil {
			importErr = err
			return ImportFolderResult{}, fmt.Errorf("导入失败，已整次回滚：%w", err)
		}
	}

	merged := org
	merged.Groups = append(append([]OrgGroup{}, org.Groups...), newGroups...)
	if merged.Agents == nil {
		merged.Agents = map[string]OrgAgentMembership{}
	}
	for origName, m := range manifest.Agents {
		finalName, ok := nameMap[strings.TrimSpace(origName)]
		if !ok {
			continue
		}
		gids := make([]string, 0, len(m.GroupIDs))
		for _, gid := range m.GroupIDs {
			if nid, ok := idMap[strings.TrimSpace(gid)]; ok {
				gids = append(gids, nid)
			}
		}
		nm := OrgAgentMembership{GroupIDs: uniqueNonEmpty(gids)}
		if len(nm.GroupIDs) == 0 {
			if mode == ImportFolderOverwrite && containsString(overwritten, finalName) {
				delete(merged.Agents, finalName)
			}
			continue
		}
		merged.Agents[finalName] = nm
	}
	// Overwrite with empty package membership still clears other-group affiliation.
	if mode == ImportFolderOverwrite {
		for _, name := range overwritten {
			if _, ok := manifest.Agents[name]; !ok {
				delete(merged.Agents, name)
			}
		}
	}

	normalized, err := o.validateAndNormalize(merged)
	if err != nil {
		importErr = err
		return ImportFolderResult{}, fmt.Errorf("导入失败，已整次回滚：%w", err)
	}
	normalized.Revision = org.Revision + 1
	if err := o.saveLocked(normalized); err != nil {
		importErr = err
		return ImportFolderResult{}, fmt.Errorf("导入失败，已整次回滚：%w", err)
	}

	return ImportFolderResult{
		Org:         normalized,
		Created:     created,
		Overwritten: overwritten,
		Renamed:     renamed,
	}, nil
}

type parsedFolderAgent struct {
	export agentExportJSON
	files  []AgentFile
}

func readFolderManifest(zr *zip.Reader) (orgFolderJSON, error) {
	var manifest orgFolderJSON
	found := false
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		name = strings.TrimPrefix(name, "./")
		if name != "folder.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return orgFolderJSON{}, fmt.Errorf("无法读取 folder.json：%v", err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return orgFolderJSON{}, fmt.Errorf("无法读取 folder.json：%v", err)
		}
		if err := json.Unmarshal(b, &manifest); err != nil {
			return orgFolderJSON{}, fmt.Errorf("folder.json 格式无效：%v", err)
		}
		found = true
		break
	}
	if !found {
		return orgFolderJSON{}, ErrOrgFolderMissingManifest
	}
	if strings.TrimSpace(manifest.Kind) != OrgFolderKind {
		return orgFolderJSON{}, ErrOrgFolderInvalidKind
	}
	if manifest.SchemaVersion != OrgFolderSchemaVersion {
		return orgFolderJSON{}, fmt.Errorf("%w：%d", ErrOrgFolderBadSchema, manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.RootGroupID) == "" {
		return orgFolderJSON{}, fmt.Errorf("%w: folder.json 缺少 rootGroupId", ErrOrgFolderInvalidKind)
	}
	if manifest.Agents == nil {
		manifest.Agents = map[string]OrgAgentMembership{}
	}
	if len(manifest.AgentNames) == 0 {
		for name := range manifest.Agents {
			manifest.AgentNames = append(manifest.AgentNames, name)
		}
		sort.Strings(manifest.AgentNames)
	}
	return manifest, nil
}

func parseFolderAgents(raw []byte, names []string) (map[string]parsedFolderAgent, error) {
	out := map[string]parsedFolderAgent{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		prefix := "agents/" + name + "/"
		export, files, err := parseZipAgent(bytes.NewReader(raw), int64(len(raw)), prefix, WorkspaceFileMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("Agent %q：%w", name, err)
		}
		out[name] = parsedFolderAgent{export: export, files: files}
	}
	return out, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// SanitizeDownloadFilename sanitizes names for Content-Disposition and zip paths
// (ASCII word chars, CJK unified, hyphen, space); unsafe separators become '_'.
func SanitizeDownloadFilename(name string) string {
	return sanitizeDownloadFilename(name)
}

// sanitizeDownloadFilename mirrors web/src/lib/workflowIO.ts sanitizeFilename
// (ASCII word chars, CJK unified, hyphen, space) without the .json suffix.
func sanitizeDownloadFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == ' ':
			b.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF:
			b.WriteRune(r)
		default:
			if unicode.Is(unicode.Han, r) {
				b.WriteRune(r)
				continue
			}
			b.WriteRune('_')
		}
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "folder"
	}
	return s
}
