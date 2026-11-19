// Package vncws is the desktop's WebSocket front on :6080. It replaces
// websockify: "/audio" streams the PulseAudio sink as raw PCM, every other
// path bridges binary WebSocket frames to x11vnc's RFB socket.
package vncws

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// PCM format of /audio frames: signed 16-bit little-endian, interleaved.
const (
	SampleRate = 48000
	Channels   = 2
	// FrameBytes is 20 ms of audio, one WebSocket message.
	FrameBytes = SampleRate / 50 * Channels * 2
)

// SinkName is the PulseAudio null sink vnc-preview.sh loads; apps play into
// it and /audio records its monitor.
const SinkName = "grasp"

const writeTimeout = 10 * time.Second

// AudioSource opens a PCM capture in the format above. Closing the stream
// stops the capture.
type AudioSource func(ctx context.Context) (io.ReadCloser, error)

// Server serves RFB and audio on one listener.
type Server struct {
	// VNCAddr is x11vnc's host:port.
	VNCAddr string
	// Audio captures the desktop sound; nil answers /audio with 503.
	Audio AudioSource
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	// websockify's "binary" subprotocol, for clients that still offer it.
	Subprotocols: []string{"binary"},
	// The port is internal to the sandbox network; the platform proxy dials
	// it without an Origin, as it did websockify.
	CheckOrigin: func(*http.Request) bool { return true },
}

// Handler routes /audio to the sound stream and the rest to RFB.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/audio", s.serveAudio)
	mux.HandleFunc("/", s.serveRFB)
	return mux
}

func (s *Server) serveRFB(w http.ResponseWriter, r *http.Request) {
	tcp, err := net.DialTimeout("tcp", s.VNCAddr, 5*time.Second)
	if err != nil {
		http.Error(w, "vnc unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = tcp.Close() }()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	go func() {
		defer func() { _ = tcp.Close() }()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if _, err := tcp.Write(data); err != nil {
				return
			}
		}
	}()
	buf := make([]byte, 64<<10)
	for {
		n, err := tcp.Read(buf)
		if n > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) serveAudio(w http.ResponseWriter, r *http.Request) {
	if s.Audio == nil {
		http.Error(w, "audio unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	src, err := s.Audio(ctx)
	if err != nil {
		http.Error(w, "audio unavailable", http.StatusServiceUnavailable)
		return
	}
	var once sync.Once
	closeSrc := func() { once.Do(func() { _ = src.Close() }) }
	defer closeSrc()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	// The viewer never sends audio; reading only notices it leaving.
	go func() {
		defer closeSrc()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()
	frame := make([]byte, FrameBytes)
	for {
		if _, err := io.ReadFull(src, frame); err != nil {
			_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
			_ = conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "audio ended"), time.Now().Add(time.Second))
			return
		}
		// Idle sinks record digital silence; the player fills gaps itself.
		if silent(frame) {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return
		}
	}
}

func silent(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// ParecSource records the SinkName monitor with pulseaudio-utils' parec.
// lookPath is exec.LookPath outside tests.
func ParecSource(lookPath func(string) (string, error)) AudioSource {
	return func(ctx context.Context) (io.ReadCloser, error) {
		bin, err := lookPath("parec")
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, bin,
			"--raw", "--format=s16le", "--rate="+strconv.Itoa(SampleRate), "--channels="+strconv.Itoa(Channels),
			"--latency-msec=20", "--device="+SinkName+".monitor")
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &procStream{ReadCloser: out, cmd: cmd}, nil
	}
}

type procStream struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (p *procStream) Close() error {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.ReadCloser.Close()
	err := p.cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil
	}
	return err
}
