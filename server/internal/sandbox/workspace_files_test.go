package sandbox

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWorkspaceFilesClientUsesAuthenticatedBinaryFencedProtocol(t *testing.T) {
	var reads, writes, finishes atomic.Int32
	fence := WorkspaceJobRenew{SHA256: strings.Repeat("a", 64), Lineage: "lineage-original", Fence: WorkspaceJobFence{Owner: "private-file-owner", Generation: 7}}
	body := []byte("binary\x00\xffbytes")
	digest := sha256.Sum256(body)
	client := workspaceJobTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "files-authenticated"})
			return
		}
		cookie, err := r.Cookie(acpSessionCookieName)
		if err != nil || cookie.Value != "files-authenticated" {
			t.Error("file call bypassed bridge auth")
		}
		if r.URL.Path == "/api/workspace/files/capabilities" {
			_, _ = io.WriteString(w, `{"version":1,"capture":true,"read":true,"restore":true,"processMemory":false}`)
			return
		}
		data, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Grasp-Workspace-Fence"))
		var actual WorkspaceJobRenew
		if err != nil || json.Unmarshal(data, &actual) != nil || actual.SHA256 != fence.SHA256 || actual.Lineage != fence.Lineage || actual.Fence.Owner != fence.Fence.Owner || actual.Fence.Generation != fence.Fence.Generation {
			t.Error("private file capability changed")
		}
		switch r.Method {
		case http.MethodGet:
			reads.Add(1)
			w.Header().Set("X-Grasp-Snapshot-Offset", "0")
			w.Header().Set("X-Grasp-Snapshot-EOF", "true")
			w.Header().Set("X-Grasp-Snapshot-SHA256", hex.EncodeToString(digest[:]))
			_, _ = w.Write(body)
		case http.MethodPut:
			writes.Add(1)
			data, err := io.ReadAll(r.Body)
			if err != nil || string(data) != string(body) || r.Header.Get("X-Grasp-Snapshot-SHA256") != hex.EncodeToString(digest[:]) || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Error("binary upload was corrupted")
			}
			_, _ = io.WriteString(w, `{"offset":`+strconv.Itoa(len(body))+`}`)
		case http.MethodPost:
			finishes.Add(1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"accepted":true}`)
		}
	})
	if caps, err := client.FileCapabilities(t.Context()); err != nil || !caps.Capture || caps.ProcessMemory {
		t.Fatal(caps, err)
	}
	if data, last, err := client.ReadWorkspaceChunk(t.Context(), "read-original", fence, "archive", 0, len(body)); err != nil || !last || string(data) != string(body) {
		t.Fatal("binary range receipt rejected", err)
	}
	if err := client.WriteWorkspaceChunk(t.Context(), "restore-original", fence, "archive", 0, body); err != nil {
		t.Fatal(err)
	}
	if err := client.FinishWorkspaceTransfer(t.Context(), "restore-original", fence); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ReadWorkspaceChunk(t.Context(), "read-original", fence, "../private", 0, 10); err == nil {
		t.Fatal("arbitrary artifact path admitted")
	}
	if err := client.WriteWorkspaceChunk(t.Context(), "restore-original", fence, "manifest", 0, make([]byte, WorkspaceFileChunkBytes+1)); err == nil {
		t.Fatal("oversized binary request admitted")
	}
	if reads.Load() != 1 || writes.Load() != 1 || finishes.Load() != 1 {
		t.Fatal("client replayed a mutation or sent invalid ranges")
	}
}

func TestWorkspaceFilesClientRejectsUnverifiedRangeReceipts(t *testing.T) {
	for _, scenario := range []string{"digest", "offset", "eof", "oversized", "unknown_finish"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			client := workspaceJobTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/login" {
					http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "session"})
					return
				}
				calls.Add(1)
				if scenario == "unknown_finish" {
					w.WriteHeader(http.StatusBadGateway)
					_, _ = io.WriteString(w, `{"code":"response_lost"}`)
					return
				}
				body := []byte("bytes")
				digest := sha256.Sum256(body)
				w.Header().Set("X-Grasp-Snapshot-Offset", "0")
				w.Header().Set("X-Grasp-Snapshot-EOF", "true")
				w.Header().Set("X-Grasp-Snapshot-SHA256", hex.EncodeToString(digest[:]))
				switch scenario {
				case "digest":
					w.Header().Set("X-Grasp-Snapshot-SHA256", strings.Repeat("0", 64))
				case "offset":
					w.Header().Set("X-Grasp-Snapshot-Offset", "1")
				case "eof":
					w.Header().Set("X-Grasp-Snapshot-EOF", "invalid")
				case "oversized":
					body = append(body, 'x')
				}
				_, _ = w.Write(body)
			})
			if scenario == "unknown_finish" {
				if err := client.FinishWorkspaceTransfer(t.Context(), "restore-original", WorkspaceJobRenew{}); err == nil {
					t.Fatal("uncertain mutation claimed success")
				}
			} else {
				if _, _, err := client.ReadWorkspaceChunk(t.Context(), "read-original", WorkspaceJobRenew{}, "archive", 0, 5); err == nil {
					t.Fatal("unverified chunk was accepted")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("ambiguous mutation was repeated")
			}
		})
	}
}
