// Command terfyn-maintainer is the thin CLI over `terfyn run` for the guarded
// autonomous PR fixer (DESIGN.md §8). It shapes the FixTask input, points the
// native workspace tool at a checkout, and surfaces the suspension at the
// publication boundary. It never auto-approves.
//
//	terfyn-maintainer --repo Terfyn/terfyn --issue 316 \
//	    --task "Fix the CSRF middleware bug" \
//	    --workspace /path/to/checkout
//
//	# resume after reviewing the pending publish:
//	terfyn-maintainer --resume <run-id> --decision approve
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Terfyn/terfyn-maintainer/internal/maintainer"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "terfyn-maintainer:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	fs := flag.NewFlagSet("terfyn-maintainer", flag.ContinueOnError)
	var (
		repo        = fs.String("repo", "", "GitHub repository as owner/name")
		issue       = fs.Int("issue", 0, "issue or PR number")
		task        = fs.String("task", "", "one-line description of the fix")
		project     = fs.String("project", ".", "terfyn project root (contains project.yaml)")
		workspace   = fs.String("workspace", os.Getenv("TERFYN_WORKSPACE_ROOT"), "checkout the agents read/write (sandbox root)")
		testCommand = fs.String("test-command", envOr("TERFYN_WORKSPACE_TEST_COMMAND", "go test ./..."), "command run_tests executes in the workspace")
		remote      = fs.String("remote", envOr("TERFYN_GIT_REMOTE", "origin"), "git remote push_branch targets")
		terfynBin   = fs.String("terfyn", "terfyn", "path to the terfyn binary")
		resume      = fs.String("resume", "", "resume a suspended run by id")
		decision    = fs.String("decision", "approve", "decision for --resume: approve or reject")
	)
	if err := fs.Parse(argv); err != nil {
		return err
	}

	// The workspace sandbox and push remote are passed to terfyn (and through it
	// to the native workspace tool and terfyn-git-publish) via the environment.
	env := os.Environ()
	if *workspace != "" {
		abs, err := filepath.Abs(*workspace)
		if err != nil {
			return err
		}
		env = append(env,
			"TERFYN_WORKSPACE_ROOT="+abs,
			"TERFYN_WORKSPACE_TEST_COMMAND="+*testCommand,
			"TERFYN_GIT_REMOTE="+*remote,
		)
	}

	// Resume path: no input, just carry the human decision back into terfyn.
	if *resume != "" {
		args, err := maintainer.ResumeArgs(*project, *resume, *decision)
		if err != nil {
			return err
		}
		return exec1(*terfynBin, args, env)
	}

	// Fresh run: build and write the FixTask input, then invoke terfyn.
	if *workspace == "" {
		return fmt.Errorf("--workspace (or TERFYN_WORKSPACE_ROOT) is required to run")
	}
	_, input, err := maintainer.BuildInput(*repo, *issue, *task)
	if err != nil {
		return err
	}
	inputFile := filepath.Join(os.TempDir(), fmt.Sprintf("terfyn-maintainer-%d.json", os.Getpid()))
	if err := os.WriteFile(inputFile, input, 0o600); err != nil {
		return err
	}
	defer os.Remove(inputFile)

	fmt.Fprintf(os.Stderr, "→ terfyn run %s (project %s, workspace %s)\n", maintainer.Workflow, *project, *workspace)
	fmt.Fprintln(os.Stderr, "  at the publication boundary the run suspends; resume with:")
	fmt.Fprintf(os.Stderr, "    terfyn-maintainer --resume <run-id> --decision approve\n\n")
	return exec1(*terfynBin, maintainer.RunArgs(*project, inputFile), env)
}

// exec1 runs cmd with args and env, forwarding stdio, and mirrors the child's
// exit code so Terfyn's exit semantics (0 = success/suspended, 5 = denial, …)
// pass through unchanged.
func exec1(bin string, args, env []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		os.Exit(exit.ExitCode())
	}
	return err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
