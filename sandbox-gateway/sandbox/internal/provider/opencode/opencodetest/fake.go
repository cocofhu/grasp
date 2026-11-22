// Package opencodetest is a scripted stand-in for `opencode serve` used by
// tests: the HTTP API and event stream Grasp drives, shaped like the real
// server's, without any model service.
//
// The prompt text selects the script: "hang" keeps the run going until it is
// aborted or steered, "fail" ends it with a model error, "subagent" leaves a
// child session busy for a few status polls, "stubborn" leaves one busy until
// it is aborted, "background" starts a child that reports busy only after a
// moment, "replay" re-publishes an earlier turn's failed answer. Any other
// prompt gets Reply. A prompt containing "receipt" is answered a second
// late, and not taken at all when it contains "lostreceipt"; one containing
// "cutoff" is taken but its connection drops before the answer.
package opencodetest

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Options configure one fake server process.
type Options struct {
	Version   string
	Reply     string
	Reasoning string
	// RequestLog, when set, receives one JSON line per API request.
	RequestLog string
	// SessionStore, when set, keeps session ids across server processes.
	SessionStore string
	// IgnoreAbort plays a server that never answers an abort.
	IgnoreAbort bool
	// SlowStart plays a server still starting: it never answers the first
	// health check.
	SlowStart bool
	// DropPromptReceipt plays a connection lost after the server took a
	// prompt: the run starts, the response never arrives.
	DropPromptReceipt bool
	// ExitOnStatus and FailStatus, when positive, make the server exit at, or
	// answer 503 to, that numbered session status read.
	ExitOnStatus, FailStatus int
	// OnPrompt, when set, sees each prompt first; an error rejects it.
	OnPrompt func(Prompt) error
}

// Request is one RequestLog line.
type Request struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// Prompt is the body of prompt_async.
type Prompt struct {
	Parts []struct {
		Text string `json:"text"`
	} `json:"parts"`
	Model *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Variant   string `json:"variant"`
	MessageID string `json:"messageID"`
}

// Text joins the prompt's text parts.
func (p Prompt) Text() string {
	var sb strings.Builder
	for _, part := range p.Parts {
		sb.WriteString(part.Text)
	}
	return sb.String()
}

// Serve runs the server for the arguments after `opencode`. It returns only
// on a usage error.
func Serve(args []string, o Options) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("unexpected native command %q", args)
	}
	host, port := "127.0.0.1", "4096"
	for i := 1; i+1 < len(args); i++ {
		switch args[i] {
		case "--hostname":
			host = args[i+1]
		case "--port":
			port = args[i+1]
		}
	}
	f := &fake{o: o, password: os.Getenv("OPENCODE_SERVER_PASSWORD"), sessions: map[string]*fakeSession{}, subs: map[chan []byte]bool{},
		messages: map[string]bool{}}
	f.loadSessions()
	l, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	return http.Serve(l, f)
}

type fake struct {
	o        Options
	password string

	mu          sync.Mutex
	seq         int
	statusReads int
	healthReads int
	sessions    map[string]*fakeSession
	subs        map[chan []byte]bool
	// messages are the prompt message ids the server took.
	messages map[string]bool
}

type fakeSession struct {
	id, parent string
	busy       bool
	statusLeft int // status polls a busy child stays busy for
	abort      chan struct{}
	steer      chan string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if f.password == "" || !ok || user != "opencode" || subtle.ConstantTimeCompare([]byte(pass), []byte(f.password)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body json.RawMessage
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	f.record(Request{Method: r.Method, Path: r.URL.Path, Body: body})
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/global/health":
		f.mu.Lock()
		f.healthReads++
		first := f.healthReads == 1
		f.mu.Unlock()
		if first && f.o.SlowStart {
			<-r.Context().Done()
			return
		}
		writeJSON(w, map[string]any{"healthy": true, "version": f.o.Version})
	case r.Method == http.MethodGet && r.URL.Path == "/event":
		f.stream(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/session":
		writeJSON(w, map[string]string{"id": f.create("").id})
	case r.Method == http.MethodGet && r.URL.Path == "/session/status":
		f.mu.Lock()
		f.statusReads++
		exit := f.o.ExitOnStatus == f.statusReads
		fail := f.o.FailStatus == f.statusReads
		f.mu.Unlock()
		if exit {
			os.Exit(0)
		}
		if fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, f.status())
	case len(parts) == 2 && parts[0] == "session" && r.Method == http.MethodGet:
		if f.get(parts[1]) == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"id": parts[1]})
	case len(parts) == 3 && parts[0] == "session" && parts[2] == "children":
		writeJSON(w, f.children(parts[1]))
	case len(parts) == 3 && parts[0] == "session" && parts[2] == "prompt_async":
		var p Prompt
		if json.Unmarshal(body, &p) != nil || f.get(parts[1]) == nil {
			http.Error(w, "bad prompt", http.StatusBadRequest)
			return
		}
		if f.o.OnPrompt != nil {
			if err := f.o.OnPrompt(p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		text := p.Text()
		if !strings.Contains(text, "lostreceipt") {
			f.mu.Lock()
			f.messages[p.MessageID] = true
			f.mu.Unlock()
			f.prompt(parts[1], text)
		}
		if strings.Contains(text, "receipt") {
			time.Sleep(time.Second)
		}
		if f.o.DropPromptReceipt || strings.Contains(text, "cutoff") {
			if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 4 && parts[0] == "session" && parts[2] == "message" && r.Method == http.MethodGet:
		f.mu.Lock()
		taken := f.messages[parts[3]]
		f.mu.Unlock()
		if !taken {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"info": map[string]string{"id": parts[3], "role": "user"}})
	case len(parts) == 3 && parts[0] == "session" && parts[2] == "abort":
		if f.o.IgnoreAbort {
			<-r.Context().Done()
			return
		}
		f.abort(parts[1])
		writeJSON(w, true)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (f *fake) record(req Request) {
	if f.o.RequestLog == "" {
		return
	}
	line, _ := json.Marshal(req)
	file, err := os.OpenFile(f.o.RequestLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write(append(line, '\n'))
}

func (f *fake) stream(w http.ResponseWriter, r *http.Request) {
	ch := make(chan []byte, 4096)
	f.mu.Lock()
	f.subs[ch] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	_, _ = fmt.Fprint(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (f *fake) publish(typ string, props map[string]any) {
	data, _ := json.Marshal(map[string]any{"type": typ, "properties": props})
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs {
		select {
		case ch <- data:
		default: // a subscriber this far behind has gone
		}
	}
}

// background starts a child session the way a background task does: it is
// created at once but reports busy only a moment later.
func (f *fake) background(parent string) {
	child := f.create(parent)
	f.publish("session.created", map[string]any{"info": map[string]string{"id": child.id, "parentID": parent}})
	go func() {
		time.Sleep(100 * time.Millisecond)
		f.mu.Lock()
		child.statusLeft = 2
		f.mu.Unlock()
		f.publish("session.status", map[string]any{"sessionID": child.id, "status": map[string]string{"type": "busy"}})
	}()
}

func (f *fake) nextID(prefix string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return fmt.Sprintf("%s_%d_%d", prefix, os.Getpid(), f.seq)
}

func (f *fake) create(parent string) *fakeSession {
	s := &fakeSession{id: f.nextID("ses"), parent: parent}
	f.mu.Lock()
	f.sessions[s.id] = s
	f.mu.Unlock()
	if parent == "" && f.o.SessionStore != "" {
		file, err := os.OpenFile(f.o.SessionStore, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(file, s.id)
			_ = file.Close()
		}
	}
	return s
}

func (f *fake) loadSessions() {
	if f.o.SessionStore == "" {
		return
	}
	data, _ := os.ReadFile(f.o.SessionStore)
	for _, id := range strings.Fields(string(data)) {
		f.sessions[id] = &fakeSession{id: id}
	}
}

func (f *fake) get(id string) *fakeSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[id]
}

func (f *fake) children(id string) []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []map[string]string{}
	for _, s := range f.sessions {
		if s.parent == id {
			out = append(out, map[string]string{"id": s.id, "parentID": id})
		}
	}
	return out
}

func (f *fake) status() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{}
	for _, s := range f.sessions {
		if s.parent != "" && s.statusLeft > 0 {
			s.statusLeft--
			out[s.id] = map[string]string{"type": "busy"}
		} else if s.busy {
			out[s.id] = map[string]string{"type": "busy"}
		}
	}
	return out
}

// prompt posts a user message. A busy session takes it at its next step, as
// the real server does; an idle one starts a run.
func (f *fake) prompt(sid, text string) {
	f.mu.Lock()
	s := f.sessions[sid]
	busy := s.busy
	if !busy {
		s.busy, s.abort, s.steer = true, make(chan struct{}), make(chan string, 8)
	}
	f.mu.Unlock()
	if busy {
		s.steer <- text
		return
	}
	go f.run(s, text)
}

func (f *fake) abort(sid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[sid]
	if s != nil && s.busy && s.abort != nil {
		close(s.abort)
		s.abort = nil
	}
	if s != nil && s.parent != "" {
		s.statusLeft = 0
	}
}

func (f *fake) userMessage(sid, text string) string {
	id := f.nextID("msg")
	f.publish("message.updated", map[string]any{"sessionID": sid, "info": map[string]any{
		"id": id, "sessionID": sid, "role": "user", "time": map[string]int64{"created": time.Now().UnixMilli()},
	}})
	f.publish("message.part.updated", map[string]any{"sessionID": sid, "part": map[string]any{
		"id": f.nextID("prt"), "sessionID": sid, "messageID": id, "type": "text", "text": text,
	}})
	return id
}

func (f *fake) run(s *fakeSession, text string) {
	sid := s.id
	user := f.userMessage(sid, text)
	f.publish("session.status", map[string]any{"sessionID": sid, "status": map[string]string{"type": "busy"}})
	pending := []string{user}
	for len(pending) > 0 {
		parent := pending[0]
		pending = pending[1:]
		msg := f.nextID("msg")
		f.assistant(sid, msg, parent, nil, false)
		switch {
		case strings.Contains(text, "hang"):
			call := f.nextID("call")
			f.tool(sid, msg, call, "running")
			select {
			case <-f.abortCh(s):
				errInfo := map[string]any{"name": "MessageAbortedError", "data": map[string]string{"message": "Aborted"}}
				f.publish("session.error", map[string]any{"sessionID": sid, "error": errInfo})
				f.finish(s)
				// The real server reports the killed tool after the idle.
				f.tool(sid, msg, call, "completed")
				f.assistant(sid, msg, parent, errInfo, true)
				return
			case steer := <-s.steer:
				pending = append(pending, f.userMessage(sid, steer))
				f.tool(sid, msg, call, "completed")
				text = steer
			}
		case strings.Contains(text, "fail"):
			errInfo := map[string]any{"name": "APIError", "data": map[string]string{"message": "upstream model failed"}}
			f.assistant(sid, msg, parent, errInfo, true)
			f.publish("session.error", map[string]any{"sessionID": sid, "error": errInfo})
			f.finish(s)
			return
		default:
			if strings.Contains(text, "background") {
				f.background(sid)
			}
			if strings.Contains(text, "replay") {
				f.assistant(sid, "msg_earlier", "msg_earlier_user", map[string]any{"name": "APIError", "data": map[string]string{"message": "an earlier turn failed"}}, true)
			}
			if strings.Contains(text, "subagent") || strings.Contains(text, "stubborn") {
				child := f.create(sid)
				f.mu.Lock()
				child.statusLeft = 2
				if strings.Contains(text, "stubborn") {
					child.statusLeft = 1 << 30
				}
				f.mu.Unlock()
			}
			if f.o.Reasoning != "" {
				f.text(sid, msg, "reasoning", f.o.Reasoning)
			}
			f.text(sid, msg, "text", f.o.Reply)
		}
		f.publish("message.part.updated", map[string]any{"sessionID": sid, "part": map[string]any{
			"id": f.nextID("prt"), "sessionID": sid, "messageID": msg, "type": "step-finish",
			"tokens": map[string]any{"input": 10, "output": 3, "reasoning": 2, "cache": map[string]int{"read": 4, "write": 1}},
		}})
		f.assistant(sid, msg, parent, nil, true)
		select {
		case steer := <-s.steer:
			pending = append(pending, f.userMessage(sid, steer))
			text = steer
		default:
		}
	}
	f.finish(s)
}

func (f *fake) abortCh(s *fakeSession) <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.abort == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.abort
}

func (f *fake) finish(s *fakeSession) {
	f.mu.Lock()
	s.busy = false
	f.mu.Unlock()
	f.publish("session.status", map[string]any{"sessionID": s.id, "status": map[string]string{"type": "idle"}})
	f.publish("session.idle", map[string]any{"sessionID": s.id})
}

func (f *fake) assistant(sid, id, parent string, errInfo map[string]any, done bool) {
	info := map[string]any{"id": id, "sessionID": sid, "role": "assistant", "parentID": parent,
		"time": map[string]int64{"created": time.Now().UnixMilli()}}
	if errInfo != nil {
		info["error"] = errInfo
	}
	if done {
		info["time"] = map[string]int64{"created": time.Now().UnixMilli(), "completed": time.Now().UnixMilli()}
	}
	f.publish("message.updated", map[string]any{"sessionID": sid, "info": info})
}

func (f *fake) text(sid, msg, kind, text string) {
	id := f.nextID("prt")
	part := map[string]any{"id": id, "sessionID": sid, "messageID": msg, "type": kind, "text": ""}
	f.publish("message.part.updated", map[string]any{"sessionID": sid, "part": part})
	half := len(text) / 2
	for _, delta := range []string{text[:half], text[half:]} {
		f.publish("message.part.delta", map[string]any{"sessionID": sid, "messageID": msg, "partID": id, "field": "text", "delta": delta})
	}
	part["text"] = text
	f.publish("message.part.updated", map[string]any{"sessionID": sid, "part": part})
}

func (f *fake) tool(sid, msg, call, status string) {
	state := map[string]any{"status": status, "input": map[string]string{"command": "sleep 30"}}
	if status == "completed" {
		state["output"] = "done"
	}
	f.publish("message.part.updated", map[string]any{"sessionID": sid, "part": map[string]any{
		"id": "prt_" + call, "sessionID": sid, "messageID": msg, "type": "tool", "tool": "bash", "callID": call, "state": state,
	}})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
