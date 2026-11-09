package workspacebinding

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Port proves the observed listener belongs to the current canonical root.
// It never kills, restarts or adopts a user's background server. Old preview
// registrations cannot be relabeled after a switch merely by reusing a port.
func (m *Manager) Port(ctx context.Context, port int) (PortView, error) {
	if port < 1 || port > 65535 {
		return PortView{}, ErrInvalid
	}
	view, release, err := m.Acquire(nil, false)
	if err != nil {
		return PortView{}, err
	}
	defer release()
	if err := listenerRoot(ctx, "/proc", view.Path, port); err != nil {
		return PortView{}, err
	}
	return PortView{Version: Version, Reference: view.Head.Reference, Port: port, Verified: true}, nil
}

func listenerRoot(ctx context.Context, proc, root string, port int) error {
	inodes := map[string]bool{}
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(proc, "net", name))
		if errors.Is(err, os.ErrNotExist) && name == "tcp6" {
			continue
		}
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (4<<20)+1))
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if len(data) > 4<<20 {
			return ErrBusy
		}
		err = listenerInodes(ctx, bytes.NewReader(data), port, inodes)
		if err != nil {
			return err
		}
	}
	if len(inodes) == 0 {
		return ErrConflict
	}
	dir, err := os.Open(proc)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(100001)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 100000 {
		return ErrBusy
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		if _, err := strconv.ParseUint(entry.Name(), 10, 32); err != nil {
			continue
		}
		base := filepath.Join(proc, entry.Name())
		fd, err := os.Open(filepath.Join(base, "fd"))
		if err != nil {
			continue
		}
		files, err := fd.ReadDir(65537)
		_ = fd.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			continue
		}
		if len(files) > 65536 {
			return ErrBusy
		}
		for _, file := range files {
			target, err := os.Readlink(filepath.Join(base, "fd", file.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			if _, wanted := inodes[inode]; !wanted {
				continue
			}
			cwd, err := os.Readlink(filepath.Join(base, "cwd"))
			if err != nil || strings.HasSuffix(cwd, " (deleted)") || !withinRoot(root, cwd) {
				return ErrConflict
			}
			inodes[inode] = true
		}
	}
	for _, seen := range inodes {
		if !seen {
			return ErrConflict
		}
	}
	return nil
}
func withinRoot(root, name string) bool {
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func listenerInodes(ctx context.Context, reader io.Reader, port int, inodes map[string]bool) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var count int
	for scanner.Scan() {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		count++
		if count > 100000 {
			return ErrBusy
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		_, value, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		parsed, err := strconv.ParseInt(value, 16, 32)
		if err != nil || parsed != int64(port) {
			continue
		}
		if _, err := strconv.ParseUint(fields[9], 10, 64); err != nil {
			return ErrInvalid
		}
		inodes[fields[9]] = false
	}
	return scanner.Err()
}
