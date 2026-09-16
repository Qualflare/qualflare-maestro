package gitdetect

import (
	"errors"
	"strings"
	"testing"
)

func TestRepoRoot_ReturnsTheWorkingTreeGitReports(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) {
		if got := strings.Join(args, " "); got != "rev-parse --show-toplevel" {
			t.Fatalf("unexpected git call %q", got)
		}
		return "/work/app\n", nil
	})
	if got := RepoRoot(); got != "/work/app" {
		t.Fatalf("RepoRoot() = %q, want /work/app", got)
	}
}

func TestRepoRoot_IsEmptyOutsideARepository(t *testing.T) {
	SetRunnerForTest(t, func(args ...string) (string, error) {
		return "", errors.New("fatal: not a git repository")
	})
	if got := RepoRoot(); got != "" {
		t.Fatalf("RepoRoot() = %q, want empty", got)
	}
}
