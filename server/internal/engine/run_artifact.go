package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	gatenode "github.com/cocofhu/grasp/internal/models/nodereg"

	"github.com/rs/zerolog/log"
)

// Artifact edit reasons / error codes shared by ArtifactEditState and
// SaveRunArtifact. The web client keys its copy on these strings.
const (
	ArtifactEditReasonRunning     = "run_running"
	ArtifactEditReasonEnded       = "run_ended"
	ArtifactEditReasonSessionBusy = "session_busy"

	ArtifactEditCodeRunNotEditable = "run_not_editable"
	ArtifactEditCodeSessionBusy    = "session_busy"
	ArtifactEditCodeConflict       = "artifact_conflict"
	ArtifactEditCodeNotFound       = "artifact_not_found"
	ArtifactEditCodeInvalid        = "invalid"
)

// ArtifactEditState tells whether a run's text artifacts can be edited by a human.
type ArtifactEditState struct {
	Editable bool   `json:"editable"`
	Reason   string `json:"reason"`
}

// ArtifactEditError is a SaveRunArtifact failure carrying a stable code.
// Revision is the current live revision on conflict; Field is the schema path
// the validation message names, when one could be recognized.
type ArtifactEditError struct {
	Code     string
	Msg      string
	Revision int
	Field    string
}

func (e *ArtifactEditError) Error() string { return e.Msg }

// AsArtifactEditError unwraps err into an *ArtifactEditError.
func AsArtifactEditError(err error) (*ArtifactEditError, bool) {
	var ae *ArtifactEditError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// RunArtifactSaveResult is returned after a human run-level artifact save.
type RunArtifactSaveResult struct {
	GateArtifactSaveResult
	Revision int  `json:"revision"`
	Changed  bool `json:"changed"`
}

// ArtifactEditState reports whether humans may edit the run's artifacts now:
// failed runs always; waiting_human runs only while every paused node's
// clarify / review session is idle (no turn in flight, empty queue).
func (e *Engine) ArtifactEditState(runID string) ArtifactEditState {
	var run models.Run
	if err := e.db.Select("id", "status").First(&run, "id = ?", runID).Error; err != nil {
		return ArtifactEditState{Reason: ArtifactEditReasonEnded}
	}
	return e.artifactEditStateFor(run)
}

func (e *Engine) artifactEditStateFor(run models.Run) ArtifactEditState {
	switch run.Status {
	case "failed":
		return ArtifactEditState{Editable: true}
	case "waiting_human":
		for _, nodeID := range e.pausedNodeIDs(run.ID) {
			if e.artifactSessionBusy(run.ID, nodeID) {
				return ArtifactEditState{Reason: ArtifactEditReasonSessionBusy}
			}
		}
		return ArtifactEditState{Editable: true}
	case "completed", "cancelled":
		return ArtifactEditState{Reason: ArtifactEditReasonEnded}
	default:
		return ArtifactEditState{Reason: ArtifactEditReasonRunning}
	}
}

func (e *Engine) pausedNodeIDs(runID string) []string {
	var ids []string
	e.db.Model(&models.StateRun{}).Where("run_id = ? AND status = ?", runID, "waiting_human").
		Distinct().Pluck("node_id", &ids)
	return ids
}

func (e *Engine) artifactSessionBusy(runID, nodeID string) bool {
	if waiting, thinking := e.ReviewSessionState(runID, nodeID); waiting > 0 || thinking {
		return true
	}
	return e.ManualSessionBusy(runID, nodeID)
}

// SaveRunArtifact stores a human edit of any text artifact in the run. The
// artifact keeps its original writer node; when content changes the store
// archives the previous body as an ArtifactVersion and bumps Revision.
//
// ifMatch, when non-empty, must equal the current ETag; a mismatch returns an
// artifact_conflict error carrying the live revision.
func (e *Engine) SaveRunArtifact(runID, name, content, ifMatch, actor string) (*RunArtifactSaveResult, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	unlock := e.lockResume(runID + ":artifact")
	defer unlock()

	c, err := e.loadCtx(runID)
	if err != nil {
		return nil, &ArtifactEditError{Code: ArtifactEditCodeNotFound, Msg: "运行不存在"}
	}
	switch st := e.artifactEditStateFor(*c.run); {
	case st.Editable:
	case st.Reason == ArtifactEditReasonSessionBusy:
		return nil, &ArtifactEditError{Code: ArtifactEditCodeSessionBusy, Msg: "Agent 正在回复，结束后再保存"}
	case st.Reason == ArtifactEditReasonEnded:
		return nil, &ArtifactEditError{Code: ArtifactEditCodeRunNotEditable, Msg: "运行已结束，产物只读"}
	default:
		return nil, &ArtifactEditError{Code: ArtifactEditCodeRunNotEditable, Msg: "运行正在执行，暂停或失败后才能编辑产物"}
	}

	name = strings.TrimSpace(name)
	switch {
	case name == "" || strings.ContainsAny(name, "/\\#") || strings.Contains(name, ".."):
		return nil, &ArtifactEditError{Code: ArtifactEditCodeInvalid, Msg: "产物名非法"}
	case mcp.IsFeedbackArtifactName(name):
		return nil, &ArtifactEditError{Code: ArtifactEditCodeNotFound, Msg: fmt.Sprintf("产物 %q 不存在", name)}
	}
	cur, ok := e.artifactMeta(runID, name)
	if !ok {
		return nil, &ArtifactEditError{Code: ArtifactEditCodeNotFound, Msg: fmt.Sprintf("产物 %q 不存在", name)}
	}
	if gatenode.IsNonTextPrimary(name, cur.Kind) || gatenode.IsReadonlyArtifactKind(cur.Kind) {
		return nil, &ArtifactEditError{Code: ArtifactEditCodeInvalid, Msg: fmt.Sprintf("产物 %q 为非文本产物，只读不可保存", name)}
	}

	norm, err := mcp.ValidateHumanArtifactContent(name, content)
	if err != nil {
		return nil, &ArtifactEditError{Code: ArtifactEditCodeInvalid, Msg: err.Error(), Field: artifactErrorField(err.Error())}
	}

	curETag := ArtifactETag(cur.Content, cur.SizeBytes, cur.UpdatedAt)
	if ifMatch != "" && ifMatch != curETag {
		return nil, &ArtifactEditError{Code: ArtifactEditCodeConflict, Msg: "产物已被更新，请载入最新或仍然保存", Revision: liveRevision(cur)}
	}

	if norm.Content == cur.Content {
		return &RunArtifactSaveResult{
			GateArtifactSaveResult: GateArtifactSaveResult{
				ID: cur.ID, Name: cur.Name, Kind: cur.Kind, SizeBytes: cur.SizeBytes,
				UpdatedAt: cur.UpdatedAt, ETag: curETag, NodeID: cur.NodeID, Content: cur.Content,
			},
			Revision: liveRevision(cur),
		}, nil
	}

	writer := cur.NodeID
	if _, err := e.store.Save(runID, writer, norm.Name, cur.Kind, norm.Content); err != nil {
		return nil, err
	}
	saved, ok := e.artifactMeta(runID, name)
	if !ok {
		return nil, errors.New("artifact vanished after save")
	}

	e.markHumanVisitWrite(runID, writer, name)
	e.syncWriterOutputs(c, writer, norm)

	actorName := strings.TrimSpace(actor)
	if actorName == "" {
		actorName = "operator"
	}
	detail := fmt.Sprintf("人改产物 name=%s revision=%d size=%d reviewer=%s", norm.Name, saved.Revision, len(norm.Content), actorName)
	e.appendTrace(c, models.TraceEntry{NodeID: writer, Event: "artifact_edit", Detail: detail})
	e.broker.Publish(runID, artifactEditMsg(runID, writer, norm.Name, ""))
	if writer != "" {
		e.refreshPendingGatesForProducer(c, writer)
	}

	updatedAt := saved.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	log.Info().Str("run_id", runID).Str("artifact", name).Int("revision", saved.Revision).
		Str("actor", actorName).Msg("human run artifact saved")
	return &RunArtifactSaveResult{
		GateArtifactSaveResult: GateArtifactSaveResult{
			ID: saved.ID, Name: saved.Name, Kind: saved.Kind, SizeBytes: saved.SizeBytes,
			UpdatedAt: updatedAt, ETag: ArtifactETag(saved.Content, saved.SizeBytes, saved.UpdatedAt),
			NodeID: saved.NodeID, Content: saved.Content,
		},
		Revision: liveRevision(saved),
		Changed:  true,
	}, nil
}

func liveRevision(a models.Artifact) int {
	if a.Revision < 1 {
		return 1
	}
	return a.Revision
}

// markHumanVisitWrite counts the edit as this visit's write when the writer is
// the node currently paused for a human, so confirming the review does not
// fail the "本次执行未写入" contract check.
func (e *Engine) markHumanVisitWrite(runID, writer, name string) {
	if writer == "" {
		return
	}
	var sr models.StateRun
	if err := e.db.Select("id", "status").Where("run_id = ? AND node_id = ?", runID, writer).
		Order("iteration desc, id desc").First(&sr).Error; err != nil || sr.Status != "waiting_human" {
		return
	}
	e.noteArtifactVisitWrite(runID, writer, name)
	if e.host != nil {
		e.host.MarkVisitWrite(runID, writer, name)
	}
}

// syncWriterOutputs mirrors the edit into the writer's latest execution
// outputs so previews and {{nodes.*.outputs.*}} read the new body.
func (e *Engine) syncWriterOutputs(c *execCtx, writer string, norm mcp.HumanArtifactNormalized) {
	if writer == "" {
		return
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", c.run.ID, writer).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return
	}
	outs := sr.Outputs
	if outs == nil {
		outs = map[string]any{}
	}
	applyHumanArtifactOutputs(outs, norm)
	sr.Outputs = outs
	logDB(e.db.Omit(stateRunTakeoverCols...).Save(&sr), c.run.ID, "sync writer outputs after human artifact edit")
	c.nodeOutputs[writer] = outs
}

// applyHumanArtifactOutputs writes a normalized human edit into an outputs map.
func applyHumanArtifactOutputs(outs map[string]any, norm mcp.HumanArtifactNormalized) {
	outKey := norm.OutKey
	if outKey == "" {
		outKey = gatenode.ArtifactToOutputKey[norm.Name]
	}
	switch {
	case outKey == "page" || norm.Name == "page.html":
		outs["page"] = norm.Content
	case outKey != "":
		outs[outKey] = norm.Rendered
		jsonKey := norm.JSONKey
		if jsonKey == "" {
			jsonKey = outKey + "_json"
		}
		outs[jsonKey] = norm.Content
	default:
		outs["artifact_"+norm.Name] = norm.Content
	}
}

var artifactFieldRe = regexp.MustCompile(`^([a-z_][a-z0-9_]*(?:\[[^\]]*\])?(?:\.[a-z_][a-z0-9_]*(?:\[[^\]]*\])?)*)\s`)

// artifactErrorField extracts the leading schema path ("summary",
// "data_design.entities[0].name") from a Parse* message, or "".
func artifactErrorField(msg string) string {
	m := artifactFieldRe.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return m[1]
}
