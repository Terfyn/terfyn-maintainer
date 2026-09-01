// Package maintainer holds the pure logic behind the terfyn-maintainer CLI
// (DESIGN.md §8): parsing the repo, shaping the FixTask input, and building the
// `terfyn run` argument vector. Keeping it side-effect-free makes it unit
// testable; cmd/terfyn-maintainer wires it to the filesystem and the process.
package maintainer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Workflow is the target the CLI always runs.
const Workflow = "workflow/FixPullRequest"

// FixTask is the workflow input (matches schemas/FixTask.json).
type FixTask struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Task   string `json:"task"`
}

// ParseRepo splits an "owner/name" slug into its parts.
func ParseRepo(slug string) (owner, repo string, err error) {
	slug = strings.TrimSpace(slug)
	owner, repo, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", fmt.Errorf("maintainer: --repo must be owner/name, got %q", slug)
	}
	return owner, repo, nil
}

// BuildInput validates the parameters and returns the FixTask plus its JSON
// encoding (the file handed to `terfyn run --input-file`).
func BuildInput(repoSlug string, issue int, task string) (FixTask, []byte, error) {
	owner, repo, err := ParseRepo(repoSlug)
	if err != nil {
		return FixTask{}, nil, err
	}
	if issue <= 0 {
		return FixTask{}, nil, fmt.Errorf("maintainer: --issue must be a positive number, got %d", issue)
	}
	if strings.TrimSpace(task) == "" {
		return FixTask{}, nil, errors.New("maintainer: --task must not be empty")
	}
	ft := FixTask{Owner: owner, Repo: repo, Number: issue, Task: task}
	b, err := json.MarshalIndent(ft, "", "  ")
	if err != nil {
		return FixTask{}, nil, err
	}
	return ft, b, nil
}

// RunArgs builds the `terfyn` argument vector for a fresh run.
func RunArgs(project, inputFile string) []string {
	return []string{"run", Workflow, "--project", project, "--input-file", inputFile}
}

// ResumeArgs builds the `terfyn` argument vector to resume a suspended run with
// a human decision.
func ResumeArgs(project, runID, decision string) ([]string, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, errors.New("maintainer: --resume requires a run id")
	}
	switch decision {
	case "approve", "reject":
	default:
		return nil, fmt.Errorf("maintainer: --decision must be approve or reject, got %q", decision)
	}
	return []string{"run", Workflow, "--project", project, "--resume", runID, "--decision", decision}, nil
}
