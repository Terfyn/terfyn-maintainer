// Package gitpublish implements the two branch-publication operations that
// Terfyn v0.2.0 does not provide natively (DESIGN.md §9): create_branch and
// push_branch. It is deliberately tiny — there is no push_main, force_push,
// delete_branch, merge, or general shell — so the capability boundary the plan
// advertises is honest by construction.
package gitpublish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Publisher runs git publish operations against a single checkout.
type Publisher struct {
	dir    string // the repository working directory
	remote string // push remote, e.g. "origin"
}

// New returns a Publisher rooted at dir (which must be an existing directory)
// pushing to remote (defaulting to "origin" when empty).
func New(dir, remote string) (*Publisher, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("gitpublish: empty root directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("gitpublish: root %q is not a directory", abs)
	}
	if remote = strings.TrimSpace(remote); remote == "" {
		remote = "origin"
	}
	return &Publisher{dir: abs, remote: remote}, nil
}

// CreateBranch creates and switches to a new local branch.
//
// Effect: repository.write (local only).
func (p *Publisher) CreateBranch(ctx context.Context, name string) (map[string]any, error) {
	if err := validateBranchName(name); err != nil {
		return nil, err
	}
	if _, stderr, err := p.git(ctx, "switch", "-c", name); err != nil {
		return nil, fmt.Errorf("gitpublish: create branch %q: %w: %s", name, err, stderr)
	}
	return map[string]any{"branch": name, "created": true}, nil
}

// PushBranch pushes branch to the configured remote. This is the publication
// boundary; Terfyn gates it behind human approval.
//
// Effect: repository.write + network.write.
func (p *Publisher) PushBranch(ctx context.Context, branch string) (map[string]any, error) {
	if err := validateBranchName(branch); err != nil {
		return nil, err
	}
	if _, stderr, err := p.git(ctx, "push", "--set-upstream", p.remote, branch); err != nil {
		return nil, fmt.Errorf("gitpublish: push branch %q to %q: %w: %s", branch, p.remote, err, stderr)
	}
	return map[string]any{"branch": branch, "remote": p.remote, "pushed": true}, nil
}

// git runs a git subcommand with the checkout as the working directory.
func (p *Publisher) git(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = p.dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), strings.TrimSpace(errBuf.String()), err
}

// validateBranchName rejects unsafe branch names. Names are simple refs, never
// flags or paths — in particular a leading '-' (which git would read as a flag)
// and any ".." or whitespace are refused.
func validateBranchName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("gitpublish: empty branch name")
	}
	if strings.HasPrefix(name, "-") {
		return errors.New("gitpublish: branch name may not start with '-'")
	}
	if strings.ContainsAny(name, " \t\n:?*[\\~^") || strings.Contains(name, "..") {
		return errors.New("gitpublish: branch name contains invalid characters")
	}
	return nil
}
