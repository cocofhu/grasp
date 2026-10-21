package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// PreviewAnnotationsArtifactName is the fixed sidecar written by the human
// gate CommentPin "写入产物" path. It is intentionally outside the primary
// SaveGateArtifact whitelist and never creates PreviewIssue rows.
const PreviewAnnotationsArtifactName = "preview_annotations.json"

// PreviewAnnotationsKind is the artifact store kind for annotation packages.
const PreviewAnnotationsKind = "preview_annotations"

// AnnotationHardScope is the Hard scope string written into every package.
const AnnotationHardScope = "仅改标中区域；越界先问"

// AnnotationArtifactDoc is the upsert payload for preview_annotations.json.
// Empty Annotations is allowed and means "clear delivery" (f4 dirty/delete-all):
// vars/outputs no longer expose a consumable package to the next node.
type AnnotationArtifactDoc struct {
	Kind        string               `json:"kind"`
	Consumer    string               `json:"consumer"`
	Route       string               `json:"route"`
	HardScope   string               `json:"hardScope"`
	Count       int                  `json:"count"`
	Annotations []AnnotationPinEntry `json:"annotations"`
	WrittenAt   string               `json:"writtenAt,omitempty"`
	GateNodeID  string               `json:"gateNodeId,omitempty"`
	GateIter    int                  `json:"gateIteration,omitempty"`
	// Status is "cleared" when Annotations is empty (explicit empty overwrite).
	Status string `json:"status,omitempty"`
}

// AnnotationPinEntry is one CommentPin in the delivered package.
// Screenshot is always "present" or "MISSING". ImageDataUrl is only kept when
// Screenshot==present and within the volume cap; oversized shots become MISSING
// with ScreenshotReason=oversized so downstream never assumes readable bytes.
type AnnotationPinEntry struct {
	Seq              int               `json:"seq"`
	Selector         string            `json:"selector"`
	Comment          string            `json:"comment"`
	CurrentText      string            `json:"currentText,omitempty"`
	Screenshot       string            `json:"screenshot"` // present | MISSING
	ScreenshotReason string            `json:"screenshotReason,omitempty"`
	ImageDataUrl     string            `json:"imageDataUrl,omitempty"`
	MarkKind         string            `json:"markKind"`
	Bounds           *AnnotationBounds `json:"bounds,omitempty"`
}

// AnnotationBounds is the pick-time element rect inside the opaque iframe.
type AnnotationBounds struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// SaveAnnotationArtifactResult is returned after a successful upsert.
type SaveAnnotationArtifactResult struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	SizeBytes int       `json:"sizeBytes"`
	UpdatedAt time.Time `json:"updatedAt"`
	ETag      string    `json:"etag"`
	NodeID    string    `json:"nodeId"`
	Content   string    `json:"content"`
	Cleared   bool      `json:"cleared,omitempty"`
}

// maxAnnotationImageBytes caps optional inline screenshots (~384KB raw ≈
// ~512KB base64). Oversized shots are degraded to screenshot=MISSING.
const maxAnnotationImageBytes = 384 * 1024

// SaveAnnotationArtifact upserts preview_annotations.json for a pending gate.
// It does not touch PreviewIssue / openPreviewIssueCount / SaveGateArtifact
// primary whitelist. Downstream reads the artifact and/or vars.preview_annotations.
// Empty annotations[] clears delivery (vars emptied, gate outputs keys removed).
func (e *Engine) SaveAnnotationArtifact(runID, gateNodeID string, doc AnnotationArtifactDoc) (*SaveAnnotationArtifactResult, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	unlock := e.lockResume(runID + ":" + gateNodeID + ":annotation-artifact")
	defer unlock()

	c, gate, node, err := e.loadPendingGate(runID, gateNodeID)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizeAnnotationDoc(doc, gateNodeID, gate.Iteration)
	if err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	content := string(raw)
	cleared := normalized.Count == 0 || normalized.Status == "cleared"

	id, err := e.store.Save(runID, gateNodeID, PreviewAnnotationsArtifactName, PreviewAnnotationsKind, content)
	if err != nil {
		return nil, err
	}

	if cleared {
		// f4: dirty/delete-all must not leave a consumable package for Pass.
		c.setVar("preview_annotations", "")
		e.persistVar(runID, "preview_annotations", "")
		c.setVar("preview_annotations_count", 0)
		e.persistVar(runID, "preview_annotations_count", 0)
		e.clearGateAnnotationOutputs(c, gateNodeID, gate.Iteration)
	} else {
		c.setVar("preview_annotations", content)
		e.persistVar(runID, "preview_annotations", content)
		c.setVar("preview_annotations_count", normalized.Count)
		e.persistVar(runID, "preview_annotations_count", normalized.Count)
		e.syncGateAnnotationOutputs(c, gateNodeID, gate.Iteration, content, normalized.Count)
	}

	detail := fmt.Sprintf(
		"annotation artifact name=%s count=%d cleared=%v route=artifact_only reviewer=operator",
		PreviewAnnotationsArtifactName, normalized.Count, cleared,
	)
	e.appendTrace(c, models.TraceEntry{NodeID: gateNodeID, Event: "annotation_artifact_write", Detail: detail})

	msg, _ := json.Marshal(map[string]any{
		"type": "annotation_artifact", "runId": runID, "nodeId": gateNodeID,
		"name": PreviewAnnotationsArtifactName, "count": normalized.Count, "cleared": cleared,
	})
	e.broker.Publish(runID, msg)

	a, ok := e.artifactMeta(runID, PreviewAnnotationsArtifactName)
	updatedAt := time.Now()
	size := len(content)
	if ok {
		if !a.UpdatedAt.IsZero() {
			updatedAt = a.UpdatedAt
		} else if !a.CreatedAt.IsZero() {
			updatedAt = a.CreatedAt
		}
		size = a.SizeBytes
		id = a.ID
	}
	etag := artifactETagWithTime(content, updatedAt)
	log.Info().Str("run_id", runID).Str("gate", gateNodeID).Int("count", normalized.Count).
		Bool("cleared", cleared).Int("size", size).
		Msg("human gate annotation artifact saved")

	_ = node // pending gate already validated
	return &SaveAnnotationArtifactResult{
		ID: id, Name: PreviewAnnotationsArtifactName, Kind: PreviewAnnotationsKind,
		SizeBytes: size, UpdatedAt: updatedAt, ETag: etag, NodeID: gateNodeID, Content: content,
		Cleared: cleared,
	}, nil
}

func normalizeAnnotationDoc(doc AnnotationArtifactDoc, gateNodeID string, gateIter int) (AnnotationArtifactDoc, error) {
	out := AnnotationArtifactDoc{
		Kind:       PreviewAnnotationsKind,
		Consumer:   "next_node",
		Route:      "artifact_only",
		HardScope:  AnnotationHardScope,
		WrittenAt:  time.Now().UTC().Format(time.RFC3339),
		GateNodeID: gateNodeID,
		GateIter:   gateIter,
	}
	if len(doc.Annotations) == 0 {
		out.Count = 0
		out.Annotations = []AnnotationPinEntry{}
		out.Status = "cleared"
		return out, nil
	}
	seenSeq := map[int]bool{}
	for i, a := range doc.Annotations {
		sel := strings.TrimSpace(a.Selector)
		comment := strings.TrimSpace(a.Comment)
		if sel == "" {
			return out, fmt.Errorf("annotations[%d].selector 不能为空", i)
		}
		if comment == "" {
			return out, fmt.Errorf("annotations[%d].comment 不能为空", i)
		}
		if a.Seq <= 0 {
			return out, fmt.Errorf("annotations[%d].seq 必须为正整数", i)
		}
		if seenSeq[a.Seq] {
			return out, fmt.Errorf("annotations[%d].seq %d 重复", i, a.Seq)
		}
		seenSeq[a.Seq] = true

		shot := strings.TrimSpace(a.Screenshot)
		switch strings.ToUpper(shot) {
		case "MISSING", "":
			shot = "MISSING"
		case "PRESENT":
			shot = "present"
		default:
			if strings.EqualFold(shot, "present") {
				shot = "present"
			} else {
				return out, fmt.Errorf("annotations[%d].screenshot 须为 present 或 MISSING", i)
			}
		}

		mark := strings.TrimSpace(a.MarkKind)
		if mark == "" {
			mark = "click"
		}

		entry := AnnotationPinEntry{
			Seq:         a.Seq,
			Selector:    sel,
			Comment:     comment,
			CurrentText: strings.TrimSpace(a.CurrentText),
			Screenshot:  shot,
			MarkKind:    mark,
			Bounds:      a.Bounds,
		}
		if shot == "present" {
			img := strings.TrimSpace(a.ImageDataUrl)
			if img != "" {
				// Volume policy: keep data URL when reasonably small; otherwise
				// degrade to MISSING + reason=oversized (never present-without-bytes).
				if len(img) <= maxAnnotationImageBytes*4/3+64 {
					entry.ImageDataUrl = img
				} else {
					entry.Screenshot = "MISSING"
					entry.ScreenshotReason = "oversized"
					entry.ImageDataUrl = ""
				}
			}
		}
		out.Annotations = append(out.Annotations, entry)
	}
	out.Count = len(out.Annotations)
	return out, nil
}

func (e *Engine) syncGateAnnotationOutputs(c *execCtx, gateNodeID string, gateIter int, content string, count int) {
	sr, ok := e.loadGateStateRun(c, gateNodeID, gateIter)
	if !ok {
		return
	}
	outs := sr.Outputs
	if outs == nil {
		outs = map[string]any{}
	}
	outs["preview_annotations"] = content
	outs["preview_annotations_json"] = content
	outs["preview_annotations_count"] = count
	sr.Outputs = outs
	logDB(e.db.Omit(stateRunTakeoverCols...).Save(&sr), c.run.ID, "sync gate outputs after annotation artifact write")
	c.nodeOutputs[gateNodeID] = outs
}

// clearGateAnnotationOutputs removes delivery keys so Pass does not hand a
// stale package to the next node after an empty overwrite / dirty invalidate.
func (e *Engine) clearGateAnnotationOutputs(c *execCtx, gateNodeID string, gateIter int) {
	sr, ok := e.loadGateStateRun(c, gateNodeID, gateIter)
	if !ok {
		return
	}
	outs := sr.Outputs
	if outs == nil {
		outs = map[string]any{}
	}
	delete(outs, "preview_annotations")
	delete(outs, "preview_annotations_json")
	delete(outs, "preview_annotations_count")
	sr.Outputs = outs
	logDB(e.db.Omit(stateRunTakeoverCols...).Save(&sr), c.run.ID, "clear gate outputs after annotation artifact clear")
	c.nodeOutputs[gateNodeID] = outs
}

func (e *Engine) loadGateStateRun(c *execCtx, gateNodeID string, gateIter int) (models.StateRun, bool) {
	if gateIter <= 0 {
		gateIter = c.iter[gateNodeID]
	}
	q := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, gateNodeID)
	if gateIter > 0 {
		q = q.Where("iteration = ?", gateIter)
	} else {
		q = q.Order("iteration desc, id desc")
	}
	var sr models.StateRun
	if err := q.First(&sr).Error; err != nil {
		// Pending gates may not yet have a StateRun row; vars + artifact suffice.
		return models.StateRun{}, false
	}
	return sr, true
}
