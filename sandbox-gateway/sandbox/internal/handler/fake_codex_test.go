package handler

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeCodexAppServer answers the app-server handshake with shell builtins
// only (tests run with PATH limited to the fake's directory).
const fakeCodexAppServer = `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    '{"id":'*) rest=${line#'{"id":'}; id=${rest%%,*} ;;
    *) continue ;;
  esac
  case "$line" in
    *'"method":"thread/start"'*|*'"method":"thread/resume"'*)
      printf '{"id":%s,"result":{"thread":{"id":"fake-thread"},"model":"fake"}}\n' "$id" ;;
    *) printf '{"id":%s,"result":{}}\n' "$id" ;;
  esac
done
`

// installFakeCodex puts a fake codex app-server first on an isolated PATH.
func installFakeCodex(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(fakeCodexAppServer), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
}
