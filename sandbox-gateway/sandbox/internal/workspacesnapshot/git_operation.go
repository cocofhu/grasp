package workspacesnapshot

import (
	"errors"
	"os"
	"slices"
	"strings"
)

var gitOperationNames = []string{"MERGE_HEAD", "MERGE_MODE", "MERGE_MSG", "ORIG_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_AUTOSTASH"}

func readGitOperation(root *os.Root) ([]GitOperation, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply", "sequencer", "index.lock", "HEAD.lock"} {
		if _, err := root.Lstat(name); err == nil {
			// A rebase todo can contain executable commands and external
			// state. Preserve the original disk instead of replaying it.
			return nil, ErrGitUnsupported
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	values := []GitOperation{}
	for _, name := range gitOperationNames {
		data, err := readSmall(root, name, 1<<20)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		value := GitOperation{Name: name, Data: data}
		if !validGitOperation(value) {
			return nil, ErrGitUnsupported
		}
		values = append(values, value)
	}
	return values, nil
}

func validGitOperation(value GitOperation) bool {
	if !slices.Contains(gitOperationNames, value.Name) || len(value.Data) > 1<<20 {
		return false
	}
	switch value.Name {
	case "MERGE_MSG":
		return true // opaque bounded commit-message bytes; never a command
	case "MERGE_MODE":
		mode := strings.TrimSpace(string(value.Data))
		return mode == "" || mode == "no-ff"
	default:
		fields := strings.Fields(string(value.Data))
		if len(fields) == 0 || (value.Name != "MERGE_HEAD" && len(fields) != 1) || len(fields) > 64 {
			return false
		}
		for _, oid := range fields {
			if !oidPattern.MatchString(oid) {
				return false
			}
		}
		return true
	}
}
