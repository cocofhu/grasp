package workspacesnapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"backend/internal/workspacejob"
)

func (e *Engine) pack(ctx context.Context, h *workspacejob.Handle, stage *os.Root, repo *liveRepository, index int, remaining int64) error {
	oids := map[string]bool{}
	if repo.value.HEAD != "" {
		oids[repo.value.HEAD] = true
	}
	for _, ref := range repo.value.Refs {
		if ref.OID != "" {
			oids[ref.OID] = true
		}
	}
	for _, entry := range repo.value.Index {
		if entry.Mode != "160000" {
			oids[entry.OID] = true
		}
	}
	for _, value := range repo.value.Operation {
		if value.Name == "MERGE_MSG" || value.Name == "MERGE_MODE" {
			continue
		}
		for _, oid := range strings.Fields(string(value.Data)) {
			oids[oid] = true
		}
	}
	if len(oids) == 0 {
		return nil // a genuinely empty, unborn repository needs no object pack
	}
	if remaining <= 0 {
		return ErrLimit
	}
	selected := make([]string, 0, len(oids))
	for oid := range oids {
		selected = append(selected, oid)
	}
	slices.Sort(selected)
	input := strings.NewReader(strings.Join(selected, "\n") + "\n")
	name := fmt.Sprintf("git/%d.pack", index)
	read, write := io.Pipe()
	hash := sha256.New()
	done := make(chan error, 1)
	go func() {
		_, err := e.git(ctx, h, repo.stagePath, input, io.MultiWriter(write, hash), nil, "pack-objects", "--stdout", "--revs", "--delta-base-offset")
		_ = write.CloseWithError(err)
		done <- err
	}()
	err := h.WriteFile(name, read, remaining)
	_ = read.CloseWithError(err)
	err = errors.Join(err, <-done)
	if err != nil {
		return err
	}
	info, err := stage.Lstat(name)
	if err != nil {
		return err
	}
	repo.value.Pack, repo.value.PackBytes, repo.value.PackSHA256 = name, info.Size(), hex.EncodeToString(hash.Sum(nil))
	return nil
}

func (e *Engine) recheckRepository(repo *liveRepository) error {
	for _, item := range []struct {
		path   string
		opened *os.Root
	}{{repo.gitPath, repo.git}, {repo.commonPath, repo.common}} {
		current, err := e.root.OpenRoot(item.path)
		if err != nil {
			return ErrChanged
		}
		before, beforeErr := item.opened.Stat(".")
		after, afterErr := current.Stat(".")
		_ = current.Close()
		if beforeErr != nil || afterErr != nil || !os.SameFile(before, after) {
			return ErrChanged
		}
	}
	gitPath, commonPath, linked, err := e.repositoryPaths(repo.value.Path, repo.work)
	if err != nil || gitPath != repo.gitPath || commonPath != repo.commonPath || linked != repo.value.Linked {
		return ErrChanged
	}
	refs, err := readRefs(repo.common, e.config.Limits.MaxEntries)
	if err != nil || !slices.Equal(refs, repo.value.Refs) {
		return ErrChanged
	}
	head, symbolic, err := resolveHEAD(repo.git, refs)
	if err != nil || head != repo.value.HEAD || symbolic != repo.value.SymbolicHEAD {
		return ErrChanged
	}
	operation, err := readGitOperation(repo.git)
	if err != nil || !slices.EqualFunc(operation, repo.value.Operation, func(a, b GitOperation) bool { return a.Name == b.Name && bytes.Equal(a.Data, b.Data) }) {
		return ErrChanged
	}
	for _, item := range []struct {
		root *os.Root
		name string
		hash string
		max  int64
	}{{repo.git, "index", repo.indexHash, e.config.Limits.MaxManifestBytes}, {repo.common, "info/exclude", repo.excludeHash, 1 << 20}} {
		data, err := readSmall(item.root, item.name, item.max)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrChanged
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != item.hash {
			return ErrChanged
		}
	}
	return nil
}
