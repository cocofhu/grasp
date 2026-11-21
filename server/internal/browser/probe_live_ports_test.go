package browser

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestProbeVNCReadyHappyPath(t *testing.T) {
	// Bind the fixed CDP / websockify ports used by probeVNCReady.
	cdpLn, err := net.Listen("tcp", "127.0.0.1:9222")
	if err != nil {
		t.Skipf("port 9222 busy: %v", err)
	}
	t.Cleanup(func() { _ = cdpLn.Close() })
	wsLn, err := net.Listen("tcp", "127.0.0.1:6080")
	if err != nil {
		t.Skipf("port 6080 busy: %v", err)
	}
	t.Cleanup(func() { _ = wsLn.Close() })

	cdpSrv := &http.Server{Handler: hiddenTargetProbeMux(t)}
	go func() { _ = cdpSrv.Serve(cdpLn) }()
	t.Cleanup(func() { _ = cdpSrv.Close() })

	// Accept one TCP connection for websockify readiness (probe just dials).
	go func() {
		conn, err := wsLn.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	s, _, _ := newFakeService(Config{})
	s.SetReadyProbe(nil) // use real probeVNCReady
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !s.probeVNCReady(ctx, "127.0.0.1") {
		t.Fatal("expected probe ready")
	}
	if !s.probeTabCreate(ctx, "127.0.0.1", cdpPort) {
		t.Fatal("expected tab create ok")
	}
}
