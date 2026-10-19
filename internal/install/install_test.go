package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cyklodev/skipack/internal/config"
	projectstate "github.com/cyklodev/skipack/internal/state"
)

type fakeFetcher struct {
	bodies map[string]string
	fail   string
}

type stateSabotageFetcher struct{ root string }

func (f stateSabotageFetcher) Fetch(s config.Skill, dst string) (Origin, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return Origin{}, err
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("ok"), 0644); err != nil {
		return Origin{}, err
	}
	if err := os.MkdirAll(filepath.Join(f.root, ".agents", "skipack-state.json"), 0755); err != nil {
		return Origin{}, err
	}
	return Origin{Source: s.Source, Path: "skills/" + s.Skill}, nil
}

type pathRecordingFetcher struct{ paths []string }

func (f *pathRecordingFetcher) Fetch(s config.Skill, dst string) (Origin, error) {
	f.paths = append(f.paths, s.Path)
	if err := os.MkdirAll(dst, 0755); err != nil {
		return Origin{}, err
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("new"), 0644); err != nil {
		return Origin{}, err
	}
	return Origin{Source: s.Source, Path: s.Path}, nil
}

type partialFailureFetcher struct{}

func (partialFailureFetcher) Fetch(_ config.Skill, dst string) (Origin, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return Origin{}, err
	}
	if err := os.WriteFile(filepath.Join(dst, "partial"), []byte("incomplete"), 0644); err != nil {
		return Origin{}, err
	}
	return Origin{}, errors.New("download interrupted")
}

func (f fakeFetcher) Fetch(s config.Skill, dst string) (Origin, error) {
	if s.Skill == f.fail {
		return Origin{}, errors.New("network down")
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return Origin{}, err
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte(f.bodies[s.Skill]), 0644); err != nil {
		return Origin{}, err
	}
	return Origin{Source: s.Source, Path: "skills/" + s.Skill}, nil
}

func TestAddSkipsExistingFolderWithoutChangingIt(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, ".agents", "skills", "one")
	os.MkdirAll(existing, 0755)
	os.WriteFile(filepath.Join(existing, "keep"), []byte("mine"), 0644)
	m := Manager{Root: root, Fetcher: fakeFetcher{bodies: map[string]string{"one": "new", "two": "two"}}}
	r, err := m.Add([]config.Skill{{Source: "a/r", Skill: "one"}, {Source: "a/r", Skill: "two"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skipped) != 1 || r.Skipped[0] != "one" {
		t.Fatalf("skipped=%v", r.Skipped)
	}
	b, _ := os.ReadFile(filepath.Join(existing, "keep"))
	if string(b) != "mine" {
		t.Fatal("existing folder changed")
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "two", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Skills) != 1 || st.Skills[0].Name != "two" {
		t.Fatalf("state=%#v", st)
	}
}

func TestUpdateDownloadFailurePreservesEveryOldSkill(t *testing.T) {
	root := t.TempDir()
	m := Manager{Root: root, Fetcher: fakeFetcher{bodies: map[string]string{"one": "old1", "two": "old2"}}}
	_, err := m.Add([]config.Skill{{Source: "a/r", Skill: "one"}, {Source: "a/r", Skill: "two"}})
	if err != nil {
		t.Fatal(err)
	}
	m.Fetcher = fakeFetcher{bodies: map[string]string{"one": "new1", "two": "new2"}, fail: "two"}
	if err := m.Update(); err == nil {
		t.Fatal("update succeeded despite fetch failure")
	}
	for _, tc := range []struct{ name, want string }{{"one", "old1"}, {"two", "old2"}} {
		b, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", tc.name, "SKILL.md"))
		if string(b) != tc.want {
			t.Fatalf("%s changed to %q", tc.name, b)
		}
	}
}

func TestUpdateDoesNotTouchUntrackedSkill(t *testing.T) {
	root := t.TempDir()
	manual := filepath.Join(root, ".agents", "skills", "manual")
	os.MkdirAll(manual, 0755)
	os.WriteFile(filepath.Join(manual, "SKILL.md"), []byte("manual"), 0644)
	m := Manager{Root: root, Fetcher: fakeFetcher{bodies: map[string]string{"one": "old"}}}
	if _, err := m.Add([]config.Skill{{Source: "a/r", Skill: "one"}}); err != nil {
		t.Fatal(err)
	}
	m.Fetcher = fakeFetcher{bodies: map[string]string{"one": "new"}}
	if err := m.Update(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(manual, "SKILL.md"))
	if string(b) != "manual" {
		t.Fatal("manual skill changed")
	}
}

func TestAddRemovesPublishedSkillWhenStateCannotBeSaved(t *testing.T) {
	root := t.TempDir()
	m := Manager{Root: root, Fetcher: stateSabotageFetcher{root: root}}
	if _, err := m.Add([]config.Skill{{Source: "a/r", Skill: "one"}}); err == nil {
		t.Fatal("add succeeded despite state write failure")
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "one")); !os.IsNotExist(err) {
		t.Fatal("published skill remained after state write failure")
	}
}

func TestUpdateFetchesTheExactTrackedPath(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, ".agents", "skills", "one")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := projectstate.Save(root, projectstate.File{Version: 1, Skills: []projectstate.Skill{{Name: "one", Source: "a/r", Path: ".agents/skills/one"}}}); err != nil {
		t.Fatal(err)
	}
	fetcher := &pathRecordingFetcher{}
	if err := (Manager{Root: root, Fetcher: fetcher}).Update(); err != nil {
		t.Fatal(err)
	}
	if len(fetcher.paths) != 1 || fetcher.paths[0] != ".agents/skills/one" {
		t.Fatalf("fetched paths = %v", fetcher.paths)
	}
}

func TestAddFetchFailureLeavesNoPartialFolder(t *testing.T) {
	root := t.TempDir()
	m := Manager{Root: root, Fetcher: partialFailureFetcher{}}
	if _, err := m.Add([]config.Skill{{Source: "a/r", Skill: "one"}}); err == nil {
		t.Fatal("add succeeded despite interrupted download")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial installation remains: %v", entries)
	}
}
