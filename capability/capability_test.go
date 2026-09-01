// Package capability holds guarantee tests: they run the real `terfyn plan` over
// the project in this repo and assert the capability boundary the design promises
// still holds. They run offline on the mock model and are skipped when the terfyn
// binary is not installed.
package capability

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// planDoc is the subset of `terfyn plan -o json` this test asserts on.
type planDoc struct {
	EffectBound []struct {
		RootKind string `json:"rootKind"`
		RootName string `json:"rootName"`
		Items    []struct {
			Ident        string `json:"ident"`
			Reachability string `json:"reachability"`
		} `json:"items"`
	} `json:"effectBound"`
}

// TestReviewerCannotWrite is the design's central guarantee: the Implementer may
// autonomously reach workspace.write, and the Reviewer may not. If someone adds
// tool.workspace.write_file to the Reviewer's grants, this test fails.
func TestReviewerCannotWrite(t *testing.T) {
	doc := plan(t)
	if !hasAutonomousEffect(doc, "Implementer", "workspace.write") {
		t.Fatal("Implementer should autonomously reach workspace.write (grant regressed?)")
	}
	if hasAutonomousEffect(doc, "Reviewer", "workspace.write") {
		t.Fatal("Reviewer must NOT reach workspace.write — a write grant leaked in")
	}
}

// TestPublishEffectsAreStatic checks the publication effects remain workflow-level
// static steps (so approvals.requiredFor can gate them), not autonomous agent grants.
func TestPublishEffectsAreStatic(t *testing.T) {
	doc := plan(t)
	for _, agent := range []string{"Triager", "Implementer", "Reviewer"} {
		if hasAutonomousEffect(doc, agent, "network.write") {
			t.Fatalf("%s must not autonomously reach network.write (push must stay a gated workflow step)", agent)
		}
		if hasAutonomousEffect(doc, agent, "github.write") {
			t.Fatalf("%s must not autonomously reach github.write (comment must stay a gated workflow step)", agent)
		}
	}
}

func hasAutonomousEffect(doc planDoc, agent, effect string) bool {
	for _, root := range doc.EffectBound {
		if root.RootKind != "agent" || root.RootName != agent {
			continue
		}
		for _, it := range root.Items {
			if it.Ident == effect && it.Reachability == "autonomous" {
				return true
			}
		}
	}
	return false
}

func plan(t *testing.T) planDoc {
	t.Helper()
	if _, err := exec.LookPath("terfyn"); err != nil {
		t.Skip("terfyn not installed; skipping capability guarantee test")
	}
	cmd := exec.Command("terfyn", "plan", "-o", "json", "--project", repoRoot(t))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("terfyn plan: %v", err)
	}
	var doc planDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode plan json: %v", err)
	}
	if len(doc.EffectBound) == 0 {
		t.Fatal("plan produced no effect bound")
	}
	return doc
}

// repoRoot returns the module root (the parent of this package's directory).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Dir(filepath.Dir(file))
}
