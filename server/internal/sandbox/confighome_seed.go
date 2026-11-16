package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// EnsureConfigHome copies a host-side ConfigHome tree (mcp.json, rules/, …) into
// the sandbox at configRoot over SSH. Remote K8s gateways ignore config.hostPath
// (docker same-host bind-mount only), so SSH sync is what actually lands
// mcp.json on the pod. Safe on Attach/reconnect; no-op when InstallHelpers is
// off or hostDir is empty. Best-effort; never fatal.
func (m *Manager) EnsureConfigHome(ctx context.Context, sb *Sandbox, hostDir, configRoot string) {
	if m == nil || !m.installHelpers || sb == nil {
		return
	}
	hostDir = strings.TrimSpace(hostDir)
	if hostDir == "" {
		return
	}
	if configRoot == "" {
		configRoot = sb.ConfigRoot
	}
	if configRoot == "" {
		configRoot = "/root/.cursor"
	}
	m.seedConfigHome(ctx, sb, hostDir, configRoot)
}

func (m *Manager) seedConfigHome(ctx context.Context, sb *Sandbox, hostDir, configRoot string) {
	creds := sb.creds()
	// Gateway marks running before sshd is up; 25s was losing the boot race
	// on cold images and left agent MCP mcp.json missing.
	rctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := creds.waitReady(rctx, 90*time.Second); err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Msg("seed config home: ssh not ready; mcp.json unavailable")
		return
	}
	payload, err := packConfigHomeTar(hostDir)
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("dir", hostDir).
			Msg("seed config home: pack tar failed")
		return
	}
	mkdirCmd, err := newSafeCmd("mkdir", "-p", configRoot)
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("root", configRoot).
			Msg("seed config home: invalid config root")
		return
	}
	if out, err := creds.run(ctx, 30*time.Second, mkdirCmd); err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("out", strings.TrimSpace(string(out))).
			Str("root", configRoot).Msg("seed config home: mkdir failed")
		return
	}
	tarCmd, err := newSafeCmd("tar", "-C", configRoot, "-xf", "-")
	if err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("root", configRoot).
			Msg("seed config home: invalid config root")
		return
	}
	if out, err := creds.runInput(ctx, 60*time.Second, tarCmd, bytes.NewReader(payload)); err != nil {
		log.Warn().Str("id", sb.ID).Err(err).Str("out", strings.TrimSpace(string(out))).
			Str("root", configRoot).Msg("seed config home: extract failed")
		return
	}
	log.Info().Str("id", sb.ID).Str("root", configRoot).Int("bytes", len(payload)).
		Msg("seeded config home over SSH (mcp.json/rules)")
}

// packConfigHomeTar builds a ustar archive of hostDir (relative paths).
func packConfigHomeTar(hostDir string) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeConfigHomeTar(&buf, hostDir); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeConfigHomeTar writes a ustar archive of hostDir (relative paths) to w
// and closes the archive; w itself stays open.
func writeConfigHomeTar(w io.Writer, hostDir string) error {
	info, err := os.Stat(hostDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("config home %q is not a directory", hostDir)
	}
	tw := tar.NewWriter(w)
	err = filepath.WalkDir(hostDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(hostDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		// Skip symlinks — config homes are plain files/dirs only.
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
			return tw.WriteHeader(hdr)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		_ = f.Close()
		return copyErr
	})
	if err != nil {
		_ = tw.Close()
		return err
	}
	return tw.Close()
}
