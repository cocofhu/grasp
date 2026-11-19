package codex

import (
	"fmt"
	"testing"

	"backend/internal/provider/oneshot"
)

const reconnectNotice = "Reconnecting... 2/5 (stream disconnected before completion: websocket closed by server before response.completed)"

func TestCodexReconnectIsNotTerminal(t *testing.T) {
	for _, envelope := range []string{
		`{"type":"error","message":%q}`,
		`{"msg":{"type":"error","message":%q}}`,
		`{"type":"item.completed","item":{"type":"error","message":%q}}`,
	} {
		line := fmt.Sprintf(envelope, reconnectNotice)
		got := (codec{}).ParseLine([]byte(line))
		if got.StopReason != "" || len(got.Msgs) != 0 || got.RetryDiagnostic != reconnectNotice {
			t.Errorf("retry notice became a turn result: %+v", got)
		}
	}
	message := "Reconnecting... attempts exhausted"
	line := fmt.Sprintf(`{"type":"error","message":%q}`, message)
	got := (codec{}).ParseLine([]byte(line))
	if got.StopReason != "failed" || len(got.Msgs) != 1 || got.Msgs[0].Text != message {
		t.Errorf("unrecognized error was lost: %+v", got)
	}
	line = fmt.Sprintf(`{"type":"turn.failed","error":{"message":%q}}`, reconnectNotice)
	got = (codec{}).ParseLine([]byte(line))
	if got.StopReason != "failed" || len(got.Msgs) != 1 || got.Msgs[0].Kind != oneshot.KindError || got.Msgs[0].Text != reconnectNotice {
		t.Errorf("explicit turn.failed was lost: %+v", got)
	}
}

func TestCodexReconnectTurn(t *testing.T) {
	progress := fmt.Sprintf(`printf '%%s\n' '{"type":"thread.started","thread_id":"S1"}' '{"type":"error","message":%q}'
`, reconnectNotice)
	t.Run("recovers after the result exit grace", func(t *testing.T) {
		// A retry can take longer than the five seconds allowed for a finished CLI to exit.
		checkCodexDiagnosticTurn(t, progress+`sleep 6
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"recovered"}}' '{"type":"turn.completed"}'
`, "end_turn", "recovered")
	})
	t.Run("exhausts retries", func(t *testing.T) {
		checkCodexDiagnosticTurn(t, progress+`printf '%s\n' '{"type":"error","message":"Reconnecting... 5/5"}' '{"type":"turn.failed","error":{"message":"stream retry limit exceeded"}}'
exit 1
`, "failed", "", "stream retry limit exceeded")
	})
	t.Run("exits mid-retry", func(t *testing.T) {
		checkCodexDiagnosticTurn(t, progress+`printf 'transport worker exited' >&2
exit 7
`, "failed", "", "CLI exited during network retry: "+reconnectNotice+"; exit status 7; stderr: transport worker exited")
	})
	t.Run("exits zero with only a retry notice", func(t *testing.T) {
		checkCodexDiagnosticTurn(t, progress+`printf 'no final result' >&2
`, "failed", "", "CLI exited during network retry: "+reconnectNotice+"; stderr: no final result")
	})
	t.Run("legacy reply supersedes the retry notice", func(t *testing.T) {
		checkCodexDiagnosticTurn(t, progress+`printf '%s\n' '{"msg":{"type":"agent_message","message":"recovered"}}'
`, "end_turn", "recovered")
	})
}
