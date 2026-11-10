package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const KeyWorkflowsEnabled = "workflows_enabled"

type WorkflowFeature struct {
	Enabled bool   `json:"enabled"`
	Locked  bool   `json:"locked"`
	Source  string `json:"source"`
}

func (s *SettingsService) WorkflowFeature() (WorkflowFeature, error) { return WorkflowFeatureFor(s.db) }

// AdmissionError is stable across Web, external API and PM callers.
type AdmissionError struct {
	Code    string
	Message string
	Status  int
}

func (e *AdmissionError) Error() string { return e.Message }
func admissionError(code, message string, status int) error {
	return &AdmissionError{code, message, status}
}

func WorkflowFeatureFor(db *gorm.DB) (WorkflowFeature, error) {
	result := WorkflowFeature{Enabled: true, Source: "config"}
	if cfg := config.GetConfig(); cfg != nil && cfg.Features.WorkflowsEnabled != nil {
		result.Enabled = *cfg.Features.WorkflowsEnabled
	}
	if value := os.Getenv("GRASP_WORKFLOWS_ENABLED"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return result, errors.New("invalid GRASP_WORKFLOWS_ENABLED")
		}
		return WorkflowFeature{Enabled: parsed, Locked: true, Source: "env"}, nil
	}
	var setting models.Setting
	err := db.First(&setting, "key = ?", KeyWorkflowsEnabled).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Enabled, err = strconv.ParseBool(setting.Value)
	result.Source = "db"
	return result, err
}

func CheckWorkflowsEnabled(db *gorm.DB) error {
	feature, err := WorkflowFeatureFor(db)
	if err != nil {
		return err
	}
	if !feature.Enabled {
		return admissionError("workflow_disabled", "工作流功能已关闭，现有任务仍可查看和管理", http.StatusForbidden)
	}
	return nil
}

// LockWorkflowAdmission serializes creation, quotas and feature changes across
// replicas. The update is a write lock on SQLite and an ordinary row lock on
// MySQL, held until the caller's transaction commits.
func LockWorkflowAdmission(tx *gorm.DB) error {
	row := models.Setting{Key: "workflow_admission_lock", Value: "1"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	return tx.Model(&models.Setting{}).Where("key = ?", row.Key).Update("updated_at", time.Now().UTC()).Error
}

func (s *SettingsService) SetWorkflowFeature(enabled bool) error {
	feature, err := WorkflowFeatureFor(s.db)
	if err != nil {
		return err
	}
	if feature.Locked {
		return admissionError("workflow_setting_locked", "工作流开关由环境配置锁定", http.StatusConflict)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := LockWorkflowAdmission(tx); err != nil {
			return err
		}
		return setSetting(tx, KeyWorkflowsEnabled, strconv.FormatBool(enabled))
	})
}

// RunFirstMessage is the caller's opening message before references are
// resolved or attachment bodies are moved into object storage.
type RunFirstMessage struct {
	Text       string               `json:"text"`
	Images     []models.PromptImage `json:"images,omitempty"`
	References []ReferenceInput     `json:"references,omitempty"`
}

// RunStart is created by authenticated server adapters, never decoded from a
// client body. Trigger remains a display label and grants no authority.
type RunStart struct {
	WorkflowID               string
	Source                   string // web | api | pm | solo
	ActorID                  string
	SoloID                   string
	IdempotencyKey           string
	Confirmed                bool
	Published                bool
	ExpectedVersion          int
	Inputs                   map[string]any
	Trigger, Priority, Title string
	Tags                     []string
	Env                      []models.EnvEntry
	FirstMessage             *models.CompositeText
	FirstMessageInput        *RunFirstMessage
	// PrepareFirstMessage resolves references only after an existing launch
	// receipt was checked. Replaying a receipt must not depend on mutable
	// external metadata or on a target that has since been deleted.
	PrepareFirstMessage                       func() (*models.CompositeText, error)
	ModelSet                                  bool
	ConfigurationID, ModelID, ReasoningEffort string
	intentFingerprint                         string
}

func (r RunStart) Identity() (key, fingerprint string, err error) {
	if r.IdempotencyKey == "" {
		return "", "", nil
	}
	if r.intentFingerprint != "" {
		hash := sha256.Sum256([]byte(r.Source + "\x00" + r.ActorID + "\x00" + r.SoloID + "\x00" + r.IdempotencyKey))
		return hex.EncodeToString(hash[:]), r.intentFingerprint, nil
	}
	// Keep this whitelist separate from the execution envelope. In particular,
	// resolved reference IDs, timestamps, access and titles are not user intent.
	// New server-only fields in RunStart must never silently change this hash.
	message := r.FirstMessageInput
	if message == nil && r.FirstMessage != nil {
		message = &RunFirstMessage{Text: r.FirstMessage.Text, Images: r.FirstMessage.Images}
		for _, ref := range r.FirstMessage.References {
			message.References = append(message.References, ReferenceInput{Kind: ref.Kind, Target: ref.Target})
		}
	}
	if message != nil {
		normalized := &RunFirstMessage{Text: message.Text, Images: message.Images}
		for _, ref := range message.References {
			normalized.References = append(normalized.References, ReferenceInput{Kind: strings.TrimSpace(ref.Kind), Target: strings.TrimSpace(ref.Target)})
		}
		message = normalized
	}
	data, err := json.Marshal(struct {
		WorkflowID                                string
		Confirmed, Published                      bool
		ExpectedVersion                           int
		Inputs                                    map[string]any
		Trigger, Priority, Title                  string
		Tags                                      []string
		Env                                       []models.EnvEntry
		FirstMessage                              *RunFirstMessage
		ModelSet                                  bool
		ConfigurationID, ModelID, ReasoningEffort string
	}{r.WorkflowID, r.Confirmed, r.Published, r.ExpectedVersion, r.Inputs,
		r.Trigger, r.Priority, r.Title, r.Tags, r.Env, message, r.ModelSet,
		r.ConfigurationID, r.ModelID, r.ReasoningEffort})
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(data)
	fingerprint = hex.EncodeToString(hash[:])
	hash = sha256.Sum256([]byte(r.Source + "\x00" + r.ActorID + "\x00" + r.SoloID + "\x00" + r.IdempotencyKey))
	return hex.EncodeToString(hash[:]), fingerprint, nil
}

type AdmittedRunStarter interface {
	StartAdmittedRun(RunStart) (*models.Run, error)
}
type RunAdmission struct {
	db      *gorm.DB
	starter AdmittedRunStarter
}

func NewRunAdmission(db *gorm.DB, starter AdmittedRunStarter) *RunAdmission {
	return &RunAdmission{db, starter}
}

func (s *RunAdmission) Start(input RunStart) (*models.Run, error) {
	if input.ActorID == "" {
		return nil, admissionError("unauthorized", "启动工作流需要已认证的身份", http.StatusUnauthorized)
	}
	switch input.Source {
	case "web", "api", "pm":
		if input.SoloID != "" {
			return nil, admissionError("invalid_origin", "此启动渠道不能指定私人 Solo", http.StatusBadRequest)
		}
	case "solo":
		if !input.Confirmed || input.IdempotencyKey == "" {
			return nil, admissionError("manual_confirmation_required", "请在 Solo 的启动卡中手动启动工作流", http.StatusBadRequest)
		}
		var solo models.SoloSession
		if err := s.db.Select("id").Where("id = ? AND user_id = ?", input.SoloID, input.ActorID).First(&solo).Error; err != nil {
			return nil, admissionError("solo_not_found", "Solo 不存在或无权访问", http.StatusNotFound)
		}
	default:
		return nil, admissionError("invalid_source", "不支持的启动渠道", http.StatusForbidden)
	}
	if len(input.IdempotencyKey) > 128 || strings.IndexFunc(input.IdempotencyKey, unicode.IsControl) >= 0 {
		return nil, admissionError("invalid_idempotency_key", "启动标识长度或格式无效", http.StatusBadRequest)
	}
	key, fingerprint, err := input.Identity()
	if err != nil {
		return nil, err
	}
	if key != "" {
		var previous models.WorkflowLaunch
		err := s.db.First(&previous, "key = ?", key).Error
		if err == nil {
			if previous.Fingerprint != fingerprint {
				return nil, admissionError("idempotency_conflict", "同一启动标识不能用于不同的工作流配置", http.StatusConflict)
			}
			var run models.Run
			if err := s.db.First(&run, "id = ?", previous.RunID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, admissionError("launch_deleted", "该次启动的任务已删除，请使用新的启动标识", http.StatusGone)
				}
				return nil, err
			}
			return &run, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	// A receipt acknowledges work accepted before the feature was disabled;
	// returning it does not launch new work. Authorization and intent matching
	// above still apply, and the engine repeats the feature check transactionally
	// for genuinely new launches.
	if err := CheckWorkflowsEnabled(s.db); err != nil {
		return nil, err
	}
	input.intentFingerprint = fingerprint
	if input.PrepareFirstMessage != nil {
		input.FirstMessage, err = input.PrepareFirstMessage()
		if err != nil {
			return nil, err
		}
	}
	return s.starter.StartAdmittedRun(input)
}
