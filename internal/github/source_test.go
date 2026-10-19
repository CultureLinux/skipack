package github

import "testing"

func TestParseTreeURLLeavesRefResolutionToAPI(t *testing.T) {
	s, err := ParseTreeURL("https://github.com/acme/tools/tree/feature/windows/skills/caveman")
	if err != nil {
		t.Fatal(err)
	}
	if s.Owner != "acme" || s.Repo != "tools" || s.Remainder != "feature/windows/skills/caveman" {
		t.Fatalf("unexpected: %#v", s)
	}
}

func TestChooseLongestRef(t *testing.T) {
	ref, path, err := ChooseRef("feature/windows/skills/caveman", []string{"main", "feature", "feature/windows"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "feature/windows" || path != "skills/caveman" {
		t.Fatalf("got %q %q", ref, path)
	}
}

func TestChooseRefRejectsAmbiguity(t *testing.T) {
	_, _, err := ChooseRef("main", []string{"main"})
	if err == nil {
		t.Fatal("empty skill path accepted")
	}
}
