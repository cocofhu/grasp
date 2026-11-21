package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSteerResult(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		status sandbox.SteerStatus
		err    error
		want   chatsession.SteerResult
	}{
		{sandbox.SteerDelivered, nil, chatsession.SteerJoined},
		{sandbox.SteerNoTurn, nil, chatsession.SteerTurnEnded},
		{sandbox.SteerUnsupported, nil, chatsession.SteerRejected},
		{sandbox.SteerFailed, boom, chatsession.SteerRejected},
		{sandbox.SteerDelivered, boom, chatsession.SteerRejected},
	} {
		if got := SteerResult(tc.status, tc.err); got != tc.want {
			t.Errorf("SteerResult(%q, %v) = %v, want %v", tc.status, tc.err, got, tc.want)
		}
	}
}

type steerProvider struct {
	ExecProvider
	status sandbox.SteerStatus
	calls  int
}

func (p *steerProvider) SteerSessionTurn(context.Context, string, string, string, string, []models.PromptImage) (sandbox.SteerStatus, error) {
	p.calls++
	return p.status, nil
}

func TestRegistrySteerSessionTurnFindsOwner(t *testing.T) {
	ctx := context.Background()
	idle := &steerProvider{status: sandbox.SteerNoTurn}
	owner := &steerProvider{status: sandbox.SteerDelivered}
	reg := &ProviderRegistry{providers: map[AcpBackend]ExecProvider{
		BackendCursor: idle, BackendCodex: owner, BackendOpenCode: &acpProvider{},
	}}
	if st, err := reg.SteerSessionTurn(ctx, "r", "n", "", "x", nil); st != sandbox.SteerDelivered || err != nil {
		t.Fatalf("owner answer = %q %v", st, err)
	}
	if owner.calls != 1 {
		t.Fatalf("owner calls = %d", owner.calls)
	}
	owner.status = sandbox.SteerNoTurn
	if st, err := reg.SteerSessionTurn(ctx, "r", "n", "lane", "x", nil); st != sandbox.SteerNoTurn || err != nil {
		t.Fatalf("nobody holds the turn = %q %v", st, err)
	}
}

func TestTranscriptsReplaySteers(t *testing.T) {
	history := []models.ReactMessage{
		{Role: "human", Text: "build it"},
		{Role: "agent", Text: "built, with tests", Parts: []models.AcpPart{
			{Kind: "message", Text: "building"},
			{Kind: models.AcpPartSteer, Text: "add tests too"},
		}},
	}
	want := "human: build it\nhuman: add tests too\nagent: built, with tests\n"
	if got := renderTranscript(history); got != want {
		t.Fatalf("transcript = %q", got)
	}
	if got := renderHandoff(history); !strings.Contains(got, "[#1 用户] build it\n[#2 用户] add tests too\n  （回合进行中补充）\n[#3 Agent] built, with tests\n") {
		t.Fatalf("handoff = %q", got)
	}
}

func TestACPProviderSteerSessionTurnWithoutSession(t *testing.T) {
	p := &acpProvider{sessions: map[string]*reactSession{"r|n": {}}, visitors: map[string]*visitorLane{}}
	for _, lane := range []string{"", "guest"} {
		if st, err := p.SteerSessionTurn(context.Background(), "r", "n", lane, "x", nil); st != sandbox.SteerNoTurn || err != nil {
			t.Fatalf("lane %q = %q %v", lane, st, err)
		}
	}
}
