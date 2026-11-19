package github

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipFixture(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestExtractSkillExtractsOnlyRequestedFolder(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out")
	err := ExtractSkillZip(zipFixture(t, map[string]string{"repo-sha/skills/qualité/SKILL.md": "# qualité", "repo-sha/other.txt": "no"}), "skills/qualité", dst)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# qualité" {
		t.Fatalf("content = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "other.txt")); !os.IsNotExist(err) {
		t.Fatal("file outside skill extracted")
	}
}

func TestExtractSkillRejectsTraversal(t *testing.T) {
	err := ExtractSkillZip(zipFixture(t, map[string]string{"repo-sha/skills/x/SKILL.md": "ok", "repo-sha/skills/x/../../evil": "bad"}), "skills/x", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("traversal accepted")
	}
}

func TestExtractSkillRequiresRootManifest(t *testing.T) {
	err := ExtractSkillZip(zipFixture(t, map[string]string{"repo-sha/skills/x/readme.md": "no"}), "skills/x", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("missing SKILL.md accepted")
	}
}

func TestExtractSkillRejectsUnsafePathAnywhereInArchive(t *testing.T) {
	err := ExtractSkillZip(zipFixture(t, map[string]string{
		"repo-sha/skills/x/SKILL.md":    "ok",
		"repo-sha/unrelated/../../evil": "bad",
	}), "skills/x", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("unsafe path outside selected skill accepted")
	}
}

func TestExtractSkillIgnoresSymlinkOutsideSelectedFolder(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	manifest, _ := w.Create("repo-sha/skills/x/SKILL.md")
	_, _ = manifest.Write([]byte("ok"))
	header := &zip.FileHeader{Name: "repo-sha/unrelated/link", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0777)
	link, _ := w.CreateHeader(header)
	_, _ = link.Write([]byte("../../outside"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractSkillZip(b.Bytes(), "skills/x", filepath.Join(t.TempDir(), "out")); err != nil {
		t.Fatalf("unrelated symlink blocked extraction: %v", err)
	}
}

func TestExtractSkillRejectsSymlinkInsideSelectedFolder(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	manifest, _ := w.Create("repo-sha/skills/x/SKILL.md")
	_, _ = manifest.Write([]byte("ok"))
	header := &zip.FileHeader{Name: "repo-sha/skills/x/link", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0777)
	link, _ := w.CreateHeader(header)
	_, _ = link.Write([]byte("../../outside"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractSkillZip(b.Bytes(), "skills/x", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("symlink inside selected skill accepted")
	}
}

func TestExtractSkillRejectsOversizedExpandedContent(t *testing.T) {
	err := ExtractSkillZip(zipFixture(t, map[string]string{
		"repo-sha/skills/x/SKILL.md": "ok",
		"repo-sha/skills/x/large":    strings.Repeat("x", int(maxSkillBytes)+1),
	}), "skills/x", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("oversized expanded content accepted")
	}
}

func TestExtractRejectsNamesThatAreSpecialOnWindows(t *testing.T) {
	for _, name := range []string{"SKILL.md::$DATA", "con.txt", "NUL", "dir/COM1.md", "a/b.", "a/b ", "x<y", "LPT9"} {
		z := archive(map[string]string{"repo-sha/skills/x/SKILL.md": "ok", "repo-sha/skills/x/" + name: "bad"})
		if err := ExtractSkillZip(z, "skills/x", t.TempDir()+"/out"); err == nil {
			t.Fatalf("%q was accepted", name)
		}
	}
}

func TestExtractAcceptsOrdinaryNamesIncludingDotfiles(t *testing.T) {
	z := archive(map[string]string{"repo-sha/skills/x/SKILL.md": "ok", "repo-sha/skills/x/.hidden": "1", "repo-sha/skills/x/console.md": "2", "repo-sha/skills/x/refs/guide.md": "3"})
	if err := ExtractSkillZip(z, "skills/x", t.TempDir()+"/out"); err != nil {
		t.Fatal(err)
	}
}

func TestExtractIgnoresSizeOfTheRestOfTheRepository(t *testing.T) {
	big := strings.Repeat("x", int(maxSkillBytes)+1)
	z := archive(map[string]string{"repo-sha/skills/x/SKILL.md": "ok", "repo-sha/assets/huge.bin": big})
	if err := ExtractSkillZip(z, "skills/x", t.TempDir()+"/out"); err != nil {
		t.Fatalf("large unrelated files must not block the skill: %v", err)
	}
}
