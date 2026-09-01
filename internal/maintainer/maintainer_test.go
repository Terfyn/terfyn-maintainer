package maintainer

import (
	"encoding/json"
	"testing"
)

func TestParseRepo(t *testing.T) {
	owner, repo, err := ParseRepo("Terfyn/terfyn-maintainer")
	if err != nil || owner != "Terfyn" || repo != "terfyn-maintainer" {
		t.Fatalf("ParseRepo = (%q,%q,%v)", owner, repo, err)
	}
	for _, bad := range []string{"", "noslash", "a/b/c", "/x", "y/"} {
		if _, _, err := ParseRepo(bad); err == nil {
			t.Fatalf("ParseRepo(%q) should fail", bad)
		}
	}
}

func TestBuildInput(t *testing.T) {
	ft, raw, err := BuildInput("Terfyn/terfyn", 316, "Fix the CSRF bug")
	if err != nil {
		t.Fatal(err)
	}
	if ft.Owner != "Terfyn" || ft.Repo != "terfyn" || ft.Number != 316 || ft.Task != "Fix the CSRF bug" {
		t.Fatalf("FixTask = %+v", ft)
	}
	var round FixTask
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("input JSON does not round-trip: %v", err)
	}
	if round != ft {
		t.Fatalf("round-trip mismatch: %+v vs %+v", round, ft)
	}
}

func TestBuildInputRejectsBadArgs(t *testing.T) {
	if _, _, err := BuildInput("Terfyn/terfyn", 0, "x"); err == nil {
		t.Fatal("issue 0 should be rejected")
	}
	if _, _, err := BuildInput("Terfyn/terfyn", 1, "  "); err == nil {
		t.Fatal("empty task should be rejected")
	}
}

func TestRunArgs(t *testing.T) {
	got := RunArgs(".", "/tmp/issue.json")
	want := []string{"run", "workflow/FixPullRequest", "--project", ".", "--input-file", "/tmp/issue.json"}
	if len(got) != len(want) {
		t.Fatalf("RunArgs = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RunArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResumeArgs(t *testing.T) {
	got, err := ResumeArgs(".", "018f-abc", "approve")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "workflow/FixPullRequest", "--project", ".", "--resume", "018f-abc", "--decision", "approve"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ResumeArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := ResumeArgs(".", "", "approve"); err == nil {
		t.Fatal("empty run id should fail")
	}
	if _, err := ResumeArgs(".", "x", "maybe"); err == nil {
		t.Fatal("bad decision should fail")
	}
}
