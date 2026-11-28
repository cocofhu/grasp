package handler

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func TestClipboardMonitorTracksX11SelectionChanges(t *testing.T) {
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		t.Skip("Xvfb is unavailable")
	}
	displayFD, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer displayFD.Close()
	defer writer.Close()
	cmd := exec.CommandContext(t.Context(), xvfb, "-displayfd", "3", "-screen", "0", "320x240x24", "-nolisten", "tcp", "-noreset")
	cmd.ExtraFiles = []*os.File{writer}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	_ = writer.Close()
	if err := displayFD.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	display, err := bufio.NewReader(displayFD).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":"+strings.TrimSpace(display))
	t.Setenv("XAUTHORITY", t.TempDir()+"/no-auth")

	var monitor clipboardMonitor
	snapshot := func(after string) clipboardState {
		t.Helper()
		state, err := monitor.snapshot(t.Context(), after)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := snapshot("")
	if before.Generation == "" || before.Owned {
		t.Fatalf("unexpected initial state: %+v", before)
	}
	if got := snapshot(before.token()); got != before {
		t.Fatalf("clipboard changed without a selection operation: %+v", got)
	}

	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	id, err := conn.NewId()
	if err != nil {
		t.Fatal(err)
	}
	win := xproto.Window(id)
	if err := xproto.CreateWindowChecked(conn, 0, win, root, 0, 0, 1, 1, 0, xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		t.Fatal(err)
	}
	atom, err := xproto.InternAtom(conn, false, uint16(len("CLIPBOARD")), "CLIPBOARD").Reply()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := xproto.SetSelectionOwnerChecked(conn, win, atom.Atom, xproto.TimeCurrentTime).Check(); err != nil {
			t.Fatal(err)
		}
		after := snapshot(before.token())
		if !after.Owned || after.Generation != before.Generation || after.Revision <= before.Revision {
			t.Fatalf("selection update was missed: before %+v, after %+v", before, after)
		}
		before = after
	}
	if err := xproto.DestroyWindowChecked(conn, win).Check(); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(before.token()); after.Owned || after.Revision <= before.Revision {
		t.Fatalf("removed selection owner was missed: %+v", after)
	}
}
