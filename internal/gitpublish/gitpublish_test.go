package gitpublish

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateBranch(t *testing.T) {
	p := newTestPublisher(t)
	if _, err := p.CreateBranch(context.Background(), "terfyn/fix-123"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	out, _, err := p.git(context.Background(), "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "terfyn/fix-123" {
		t.Fatalf("HEAD = %q, want terfyn/fix-123", out)
	}
}

func TestPushBranch(t *testing.T) {
	p := newTestPublisher(t)
	ctx := context.Background()
	if _, err := p.CreateBranch(ctx, "terfyn/fix-9"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PushBranch(ctx, "terfyn/fix-9"); err != nil {
		t.Fatalf("PushBranch: %v", err)
	}
	// The bare remote must now have the branch.
	out, _, err := p.git(ctx, "ls-remote", "--heads", p.remote, "terfyn/fix-9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "refs/heads/terfyn/fix-9") {
		t.Fatalf("remote missing pushed branch; ls-remote=%q", out)
	}
}

func TestBranchNameValidation(t *testing.T) {
	p := newTestPublisher(t)
	ctx := context.Background()
	for _, bad := range []string{"", "--force", "-D", "a b", "a..b", "re:mote", "a\tb"} {
		if _, err := p.CreateBranch(ctx, bad); err == nil {
			t.Fatalf("CreateBranch(%q) should have been rejected", bad)
		}
		if _, err := p.PushBranch(ctx, bad); err == nil {
			t.Fatalf("PushBranch(%q) should have been rejected", bad)
		}
	}
}

func TestNewRejectsNonDir(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "does-not-exist"), ""); err == nil {
		t.Fatal("New on a missing path should fail")
	}
}

// newTestPublisher builds a git repo with one commit and a bare remote named
// "origin", and returns a Publisher rooted at the working repo.
func newTestPublisher(t *testing.T) *Publisher {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare", "-q")

	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")
	runGit(t, dir, "remote", "add", "origin", bare)

	p, err := New(dir, "origin")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
