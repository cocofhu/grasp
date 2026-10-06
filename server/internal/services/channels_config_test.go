package services

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
)

func setChannelKey(t *testing.T) {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(k))
}

func newChannelSvc(t *testing.T) (*ChannelConfigService, string) {
	t.Helper()
	db := newTestDB(t)
	p, err := NewProjectService(db).Create("ChanProj", "", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return NewChannelConfigService(db), p.ID
}

func validInput(projectID string) ChannelConfigInput {
	return ChannelConfigInput{
		Type:      "qq",
		Name:      "QQ Bot",
		Enabled:   true,
		ProjectID: projectID,
		AgentName: "pm-agent",
		AppID:     "app-123",
		AppSecret: "the-secret",
	}
}

func TestChannelCreateAndList(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)

	var changed int
	svc.SetOnChange(func() { changed++ })

	dto, err := svc.Create(validInput(pid))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !dto.AppSecretSet {
		t.Error("AppSecretSet should be true after create")
	}
	if dto.ProjectID != pid || dto.AppID != "app-123" {
		t.Errorf("unexpected dto: %+v", dto)
	}

	// Raw row stores encrypted secret, decryptable back to plaintext.
	raw, err := svc.ListRaw()
	if err != nil || len(raw) != 1 {
		t.Fatalf("ListRaw: %v (n=%d)", err, len(raw))
	}
	if raw[0].AppSecretEnc == "the-secret" || raw[0].AppSecretEnc == "" {
		t.Fatalf("secret not encrypted at rest: %q", raw[0].AppSecretEnc)
	}
	dec, err := crypto.Decrypt(raw[0].AppSecretEnc)
	if err != nil || dec != "the-secret" {
		t.Fatalf("decrypt stored secret: %q err=%v", dec, err)
	}
}

func TestChannelCreateRejectsBadInput(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)

	bad := validInput(pid)
	bad.Type = "slack"
	if _, err := svc.Create(bad); err != ErrChannelTypeUnsupported {
		t.Errorf("unsupported type: got %v", err)
	}

	noProj := validInput(pid)
	noProj.ProjectID = ""
	if _, err := svc.Create(noProj); err != ErrChannelProjectRequired {
		t.Errorf("missing project: got %v", err)
	}

	missingProj := validInput(pid)
	missingProj.ProjectID = "does-not-exist"
	if _, err := svc.Create(missingProj); err != ErrProjectNotFound {
		t.Errorf("nonexistent project: got %v", err)
	}

	noApp := validInput(pid)
	noApp.AppID = ""
	if _, err := svc.Create(noApp); err != ErrChannelAppIDRequired {
		t.Errorf("missing appId: got %v", err)
	}

	noSecret := validInput(pid)
	noSecret.AppSecret = ""
	if _, err := svc.Create(noSecret); err != ErrChannelSecretRequired {
		t.Errorf("missing secret: got %v", err)
	}

	blankName := validInput(pid)
	blankName.Name = "   "
	if _, err := svc.Create(blankName); err != ErrChannelNameRequired {
		t.Errorf("blank name: got %v", err)
	}
}

func TestChannelCreateWeComAndFreeze(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)
	in := validInput(pid)
	in.Type = "wecom"
	in.AppID = "aibot_omega_01"
	dto, err := svc.Create(in)
	if err != nil {
		t.Fatalf("create wecom: %v", err)
	}
	if dto.Type != "wecom" || dto.AppID != "aibot_omega_01" || dto.AppSecretSet != true {
		t.Fatalf("dto=%+v", dto)
	}

	upd := in
	upd.Type = "qq"
	if _, err := svc.Update(dto.ID, upd); err != ErrChannelTypeFrozen {
		t.Fatalf("type change: got %v want ErrChannelTypeFrozen", err)
	}
	upd.Type = "wecom"
	upd.AppID = "aibot_other"
	if _, err := svc.Update(dto.ID, upd); err != ErrChannelBotIDFrozen {
		t.Fatalf("botid change: got %v want ErrChannelBotIDFrozen", err)
	}
	upd.AppID = "aibot_omega_01"
	upd.Name = "renamed-wecom"
	upd.AppSecret = ""
	got, err := svc.Update(dto.ID, upd)
	if err != nil {
		t.Fatalf("allowed update: %v", err)
	}
	if got.Name != "renamed-wecom" || got.Type != "wecom" || got.AppID != "aibot_omega_01" {
		t.Fatalf("after update %+v", got)
	}
}

func TestChannelWeComBotIDConflict(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)
	in := validInput(pid)
	in.Type = "wecom"
	in.AppID = "aibot_taken_global"
	in.AgentName = "agent-a"
	if _, err := svc.Create(in); err != nil {
		t.Fatalf("first: %v", err)
	}
	in2 := validInput(pid)
	in2.Type = "wecom"
	in2.AppID = "aibot_taken_global"
	in2.AgentName = "agent-b"
	if _, err := svc.Create(in2); err != ErrChannelBotIDExists {
		t.Fatalf("dup botid: got %v want ErrChannelBotIDExists", err)
	}
	if !strings.Contains(ErrChannelBotIDExists.Error(), "BotID 已被其它项目/渠道占用") {
		t.Fatalf("copy=%q", ErrChannelBotIDExists.Error())
	}
}

func TestChannelDuplicateAppID(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)
	if _, err := svc.Create(validInput(pid)); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(validInput(pid)); err != ErrChannelAppIDExists {
		t.Fatalf("duplicate appId: got %v want ErrChannelAppIDExists", err)
	}
}

func TestChannelUpdateKeepsSecretWhenBlank(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)
	dto, err := svc.Create(validInput(pid))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, _ := svc.ListRaw()
	origEnc := before[0].AppSecretEnc

	upd := validInput(pid)
	upd.Name = "Renamed"
	upd.AppSecret = "" // keep existing
	if _, err := svc.Update(dto.ID, upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	after, _ := svc.ListRaw()
	if after[0].AppSecretEnc != origEnc {
		t.Error("blank AppSecret on update should preserve stored secret")
	}
	if after[0].Name != "Renamed" {
		t.Errorf("name not updated: %q", after[0].Name)
	}

	// Providing a new secret rotates it.
	upd.AppSecret = "rotated"
	if _, err := svc.Update(dto.ID, upd); err != nil {
		t.Fatalf("Update rotate: %v", err)
	}
	after2, _ := svc.ListRaw()
	dec, _ := crypto.Decrypt(after2[0].AppSecretEnc)
	if dec != "rotated" {
		t.Errorf("secret not rotated: %q", dec)
	}
}

func TestChannelCreateWithoutKeyFails(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, "")
	svc, pid := newChannelSvc(t)
	if _, err := svc.Create(validInput(pid)); err != ErrChannelSecretKeyMissing {
		t.Fatalf("without secrets key: got %v want ErrChannelSecretKeyMissing", err)
	}
}

func TestChannelCreateInvalidKeyFails(t *testing.T) {
	t.Setenv(crypto.SecretsKeyEnv, "not-base64!!")
	svc, pid := newChannelSvc(t)
	if _, err := svc.Create(validInput(pid)); err != ErrChannelSecretKeyInvalid {
		t.Fatalf("invalid secrets key: got %v want ErrChannelSecretKeyInvalid", err)
	}
}

func TestChannelCronDeliverTargetRequired(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)

	in := validInput(pid)
	in.CronDeliver = true
	in.CronDeliverTarget = ""
	if _, err := svc.Create(in); err != ErrChannelCronTargetRequired {
		t.Fatalf("empty target: got %v want ErrChannelCronTargetRequired", err)
	}

	in.CronDeliverTarget = "slack:x"
	if _, err := svc.Create(in); err != ErrChannelCronTargetInvalid {
		t.Fatalf("bad scene: got %v want ErrChannelCronTargetInvalid", err)
	}

	in.CronDeliverTarget = "group:"
	if _, err := svc.Create(in); err != ErrChannelCronTargetInvalid {
		t.Fatalf("empty id: got %v want ErrChannelCronTargetInvalid", err)
	}

	in.CronDeliverTarget = "group:openid-1"
	if _, err := svc.Create(in); err != nil {
		t.Fatalf("valid target: %v", err)
	}
}

func TestValidCronDeliverTarget(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"c2c:u", true},
		{"group:g", true},
		{"guild:123", true},
		{"c2c:user:with:colons", true},
		{"", false},
		{"malformed", false},
		{"group:", false},
		{"slack:x", false},
	}
	for _, c := range cases {
		if got := validCronDeliverTarget(c.in); got != c.want {
			t.Errorf("validCronDeliverTarget(%q) = %v want %v", c.in, got, c.want)
		}
	}
}

func TestChannelCreateFeishuTypeAndRegion(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)

	in := validInput(pid)
	in.Type = "feishu"
	in.Name = "飞书 Channel"
	in.IsPrimary = false
	in.Config = map[string]any{"region": "lark", "token": "should-drop"}
	dto, err := svc.Create(in)
	if err != nil {
		t.Fatalf("create feishu: %v", err)
	}
	if dto.Type != "feishu" {
		t.Fatalf("type=%q", dto.Type)
	}
	if dto.IsPrimary {
		t.Fatal("explicit IsPrimary=false must stay secondary even with no primary")
	}
	if dto.Config["region"] != "lark" {
		t.Fatalf("region=%v", dto.Config["region"])
	}
	if _, ok := dto.Config["token"]; ok {
		t.Fatal("webhook token must not be stored")
	}
}

func TestChannelCreateDefaultFeishuRegionCN(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)
	in := validInput(pid)
	in.Type = "feishu"
	in.Name = "飞书 Channel"
	dto, err := svc.Create(in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if dto.Config["region"] != "cn" {
		t.Fatalf("default region=%v want cn", dto.Config["region"])
	}
}

func TestChannelCreateDingTalkStripsWebhookAndRobotCode(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)
	in := validInput(pid)
	in.Type = "dingtalk"
	in.Name = "钉钉 Channel"
	in.AppID = "ding-app-1"
	in.IsPrimary = false
	in.Config = map[string]any{
		"token":            "should-drop",
		"robotCode":        "should-drop",
		"allowMemoryWrite": true,
	}
	dto, err := svc.Create(in)
	if err != nil {
		t.Fatalf("create dingtalk: %v", err)
	}
	if dto.Type != "dingtalk" {
		t.Fatalf("type=%q", dto.Type)
	}
	if _, ok := dto.Config["token"]; ok {
		t.Fatal("webhook token must not be stored")
	}
	if _, ok := dto.Config["robotCode"]; ok {
		t.Fatal("independent robotCode must not be stored")
	}
	if dto.Config["allowMemoryWrite"] != true {
		t.Fatalf("session caps must remain: %v", dto.Config)
	}
}

func TestChannelMultiPrimarySecondary(t *testing.T) {
	setChannelKey(t)
	svc, pid := newChannelSvc(t)

	primaryIn := validInput(pid)
	primaryIn.AgentName = "agent-primary"
	primaryIn.IsPrimary = true
	primary, err := svc.Create(primaryIn)
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if !primary.IsPrimary {
		t.Fatalf("channel created with isPrimary should be primary")
	}

	secIn := validInput(pid)
	secIn.AppID = "app-456"
	secIn.AgentName = "agent-secondary"
	sec, err := svc.Create(secIn)
	if err != nil {
		t.Fatalf("create secondary: %v", err)
	}
	if sec.IsPrimary {
		t.Fatalf("second channel should be secondary")
	}

	list, err := svc.ListByProject(pid)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v n=%d", err, len(list))
	}
	if !list[0].IsPrimary {
		t.Fatalf("list should put primary first")
	}

	// Cannot bind taken agent.
	secIn2 := validInput(pid)
	secIn2.AppID = "app-789"
	secIn2.AgentName = "agent-primary"
	if _, err := svc.Create(secIn2); err != ErrChannelAgentTaken {
		t.Fatalf("taken agent: got %v", err)
	}

	// Delete primary without ack fails.
	if err := svc.Delete(primary.ID, ChannelDeleteOpts{}); err != ErrChannelDeletePrimaryNeedsAck {
		t.Fatalf("delete primary no ack: %v", err)
	}
	// Promote secondary then delete.
	if err := svc.Delete(primary.ID, ChannelDeleteOpts{NewPrimaryID: sec.ID}); err != nil {
		t.Fatalf("delete primary with new: %v", err)
	}
	got, err := svc.GetByID(sec.ID)
	if err != nil || !got.IsPrimary {
		t.Fatalf("secondary should be primary now: %+v err=%v", got, err)
	}
}
