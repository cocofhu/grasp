package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkspaceRuntimeEnvOnlyUsesPlatformTupleAndPersistsRemoteRootsOnPVC(t *testing.T) {
	cfg := &WorkspaceRuntimeConfig{Lineage: "fixed-startup-lineage"}
	manager := &Manager{workspaceOperations: copyWorkspaceRuntimeConfig(cfg)}
	cfg.Lineage = "user-mutated-after-construction"
	env := map[string]string{"GRASP_EXECUTION_LINEAGE": "attacker", "GRASP_SANDBOX_RESOURCE_ID": "invented-resource", "GRASP_WORKSPACE_CONTROL_ROOT": "/user/path", "GRASP_WORKSPACE_RESTORE_ROOT": "/user/path2", "GRASP_WORKSPACE_FILE_LIMITS": "unbounded", "ordinary": "preserved"}
	if err := manager.prepareWorkspaceRuntimeEnv(env, "", ""); err != nil {
		t.Fatal(err)
	}
	if env["GRASP_EXECUTION_LINEAGE"] != "fixed-startup-lineage" || env["ordinary"] != "preserved" || env["GRASP_SANDBOX_RESOURCE_ID"] != "" || env["GRASP_WORKSPACE_CONTROL_ROOT"] != "" || env["GRASP_WORKSPACE_RESTORE_ROOT"] != "" {
		t.Fatal(env)
	}
	var limits WorkspaceFileLimits
	if json.Unmarshal([]byte(env["GRASP_WORKSPACE_FILE_LIMITS"]), &limits) != nil || limits != defaultWorkspaceRuntimeLimits() {
		t.Fatal("trusted finite default limits missing")
	}
	resource := qualifiedGatewayID("k8s", "native-resource")
	if err := manager.prepareWorkspaceRuntimeEnv(env, resource, ""); err != nil {
		t.Fatal(err)
	}
	if env["GRASP_WORKSPACE_CONTROL_ROOT"] != filepath.Join("/tmp/grasp-private-workspace", resource, "control") || env["GRASP_WORKSPACE_RESTORE_ROOT"] != filepath.Join("/tmp/grasp-private-workspace", resource, "restores") {
		t.Fatal("roots not on K8s /tmp PVC", env)
	}
	if err := manager.prepareWorkspaceRuntimeEnv(env, "local-resident-one", "/owned/local/worktree"); err != nil {
		t.Fatal(err)
	}
	if env["GRASP_WORKSPACE_CONTROL_ROOT"] != "/owned/local/.grasp-private-workspace/local-resident-one/control" {
		t.Fatal(env)
	}
	manager.workspaceOperations = nil
	if err := manager.prepareWorkspaceRuntimeEnv(env, "", ""); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"GRASP_EXECUTION_LINEAGE", "GRASP_SANDBOX_RESOURCE_ID", "GRASP_WORKSPACE_CONTROL_ROOT", "GRASP_WORKSPACE_RESTORE_ROOT", "GRASP_WORKSPACE_FILE_LIMITS"} {
		if env[key] != "" {
			t.Fatal("disabled feature retained user tuple", key)
		}
	}
}
func TestWorkspaceRuntimeEnvRejectsInvalidPlatformConfiguration(t *testing.T) {
	for _, kind := range []string{"lineage", "relative_root", "newline_root", "file_limit", "zero_limit", "resource"} {
		t.Run(kind, func(t *testing.T) {
			cfg := copyWorkspaceRuntimeConfig(&WorkspaceRuntimeConfig{Lineage: "valid-original-lineage", Limits: defaultWorkspaceRuntimeLimits()})
			resource := "actual-provider-resource"
			switch kind {
			case "lineage":
				cfg.Lineage = "../../other"
			case "relative_root":
				cfg.PrivateRoot = "relative"
			case "newline_root":
				cfg.PrivateRoot = "/tmp/line\nsecret"
			case "file_limit":
				cfg.Limits.MaxFileBytes = cfg.Limits.MaxBytes + 1
			case "zero_limit":
				cfg.Limits.MaxEntries = 0
			case "resource":
				resource = "../other"
			}
			manager := &Manager{workspaceOperations: cfg}
			if err := manager.prepareWorkspaceRuntimeEnv(map[string]string{}, resource, ""); err == nil {
				t.Fatal("bad platform tuple accepted", kind)
			}
		})
	}
}
func TestWorkspaceRuntimeAlreadyConfiguredOnlyReadsAndVerifiesOriginalHead(t *testing.T) {
	limits := defaultWorkspaceRuntimeLimits()
	head := WorkspaceBindingHead{Version: 1, Resource: "actual-provider-resource", Lineage: "fixed-startup-lineage", Reference: WorkspaceBindingReference{ID: "original-native-head", Revision: 1}, Target: WorkspaceBindingSelector{Kind: "original"}}
	canonical, _ := json.Marshal(head)
	hash := sha256.Sum256(canonical)
	head.Reference.Fingerprint = hex.EncodeToString(hash[:])
	for _, kind := range []string{"ready", "pending", "wrong_resource", "wrong_lineage", "bad_reference", "bad_limits", "unexpected_error"} {
		t.Run(kind, func(t *testing.T) {
			methods := []string{}
			view := WorkspaceBindingView{Head: head, Path: "/persistent/original", Ready: true}
			caps := WorkspaceBindingCapabilities{Version: 1, Resource: head.Resource, Lineage: head.Lineage, Activate: true, Return: true, DurableHead: true, PortOwnership: true}
			fileLimits := limits
			switch kind {
			case "pending":
				view.Ready = false
				view.PendingOperationID = "original-unconfirmed-operation"
			case "wrong_resource":
				caps.Resource = "different-resource"
			case "wrong_lineage":
				caps.Lineage = "different-lineage"
			case "bad_reference":
				view.Head.Reference.Fingerprint = strings.Repeat("a", 64)
			case "bad_limits":
				fileLimits.MaxEntries++
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/login" {
					var input map[string]string
					if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&input) != nil || input["password"] != "synthetic-test-password" {
						t.Error("invalid authentication request")
						w.WriteHeader(401)
						return
					}
					http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "synthetic-session"})
					w.WriteHeader(200)
					return
				}
				methods = append(methods, r.Method)
				w.Header().Set("Content-Type", "application/json")
				if kind == "unexpected_error" {
					w.WriteHeader(409)
					_ = json.NewEncoder(w).Encode(map[string]string{"code": "different_error"})
					return
				}
				switch r.URL.Path {
				case "/api/workspace/binding/capabilities":
					_ = json.NewEncoder(w).Encode(caps)
				case "/api/workspace/binding/head":
					_ = json.NewEncoder(w).Encode(view)
				case "/api/workspace/files/capabilities":
					_ = json.NewEncoder(w).Encode(WorkspaceFileCapabilities{Version: 1, Limits: fileLimits})
				default:
					t.Error("configuration performed an unexpected request", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			host, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			manager := &Manager{workspaceOperations: copyWorkspaceRuntimeConfig(&WorkspaceRuntimeConfig{Lineage: head.Lineage, Limits: limits})}
			sb := &Sandbox{ID: head.Resource, Host: host, Port: port, Password: "synthetic-test-password", WorkspaceDir: "/persistent/original"}
			err := manager.ConfigureWorkspaceRuntime(context.Background(), sb)
			success := kind == "ready" || kind == "pending"
			if success && err != nil || !success && err == nil {
				t.Fatal(kind, err)
			}
			for _, method := range methods {
				if method != "GET" {
					t.Fatal("already configured runtime was restarted", method)
				}
			}
			if kind == "ready" && sb.ActiveWorkspaceFingerprint() != head.Reference.Fingerprint {
				t.Fatal("canonical head not installed")
			}
			if kind == "pending" && sb.ActiveWorkspaceFingerprint() != "" {
				t.Fatal("uncertain head published as applied")
			}
		})
	}
}
