package workspacejob

import (
	"errors"
	"fmt"
	"testing"
)

type codedFixture string

func (c codedFixture) Error() string       { return "fixture failure" }
func (c codedFixture) ReceiptCode() string { return string(c) }

func TestFailureCodeKeepsStableExecutorCodesOnly(t *testing.T) {
	for err, want := range map[error]string{
		codedFixture("git_unsupported"):                            "git_unsupported",
		fmt.Errorf("wrapped: %w", codedFixture("workspace_limit")): "workspace_limit",
		errors.New("raw /root/secret path"):                        "executor_failed",
		codedFixture("Bad Code!"):                                  "executor_failed",
		codedFixture(""):                                           "executor_failed",
	} {
		if got := failureCode(err); got != want {
			t.Fatal(err, got, want)
		}
	}
}
