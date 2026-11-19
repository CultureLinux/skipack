package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUpdateDownloadsAndVerifiesPlatformBinary(t *testing.T) {
	const asset = "skipack-linux-arm64"
	binary := []byte("new binary")
	sum := sha256.Sum256(binary)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest/download/SHA256SUMS":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"))
		case "/releases/latest/download/" + asset:
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "skipack")
	if err := os.WriteFile(target, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	replaced := false
	u := Updater{HTTP: server.Client(), BaseURL: server.URL, GOOS: "linux", GOARCH: "arm64", Executable: func() (string, error) { return target, nil }, Replace: func(gotTarget, staged string) error {
		replaced = true
		if gotTarget != target {
			t.Fatalf("target=%q", gotTarget)
		}
		b, err := os.ReadFile(staged)
		if err != nil {
			return err
		}
		if string(b) != "new binary" {
			t.Fatalf("staged binary=%q", b)
		}
		return nil
	}}
	if err := u.Update(); err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("binary was not replaced")
	}
}

func TestUpdateRejectsInvalidChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest/download/SHA256SUMS":
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  skipack-linux-amd64\n"))
		case "/releases/latest/download/skipack-linux-amd64":
			_, _ = w.Write([]byte("tampered"))
		}
	}))
	defer server.Close()

	replaced := false
	u := Updater{HTTP: server.Client(), BaseURL: server.URL, GOOS: "linux", GOARCH: "amd64", Executable: func() (string, error) { return filepath.Join(t.TempDir(), "skipack"), nil }, Replace: func(_, _ string) error {
		replaced = true
		return nil
	}}
	if err := u.Update(); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if replaced {
		t.Fatal("replacement ran despite checksum mismatch")
	}
}

func TestReplaceSwapsTheExecutableAndRemovesTheBackup(t *testing.T) {
	dir := t.TempDir()
	target, staged := filepath.Join(dir, "skipack"), filepath.Join(dir, "staged")
	os.WriteFile(target, []byte("old"), 0755)
	os.WriteFile(staged, []byte("new"), 0755)
	if err := Replace(target, staged); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Fatalf("target=%q", b)
	}
	if _, err := os.Stat(target + ".skipack-backup"); err == nil {
		t.Fatal("backup remains")
	}
}

func TestFinishReplaceLogsFailureAndRemovesStagedFile(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)
	old := replaceTimeout
	replaceTimeout = 200 * time.Millisecond
	defer func() { replaceTimeout = old }()
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	os.WriteFile(staged, []byte("new"), 0755)
	if err := FinishReplace(filepath.Join(dir, "no-such-dir", "skipack"), staged); err == nil {
		t.Fatal("replacing into a missing directory succeeded")
	}
	if _, err := os.Stat(staged); err == nil {
		t.Fatal("staged file remains after failure")
	}
	b, err := os.ReadFile(LogPath())
	if err != nil || !strings.Contains(string(b), "replace ") {
		t.Fatalf("log=%q err=%v", b, err)
	}
}

func TestDeferredOnlyForTheDefaultWindowsReplacement(t *testing.T) {
	if (Updater{Replace: func(string, string) error { return nil }}).Deferred() {
		t.Fatal("custom replacer reported as deferred")
	}
	if got, want := (Updater{}).Deferred(), runtime.GOOS == "windows"; got != want {
		t.Fatalf("Deferred=%v want %v", got, want)
	}
}

func TestChecksumForFindsTheAssetAndRejectsBadLines(t *testing.T) {
	good := strings.Repeat("ab", 32)
	sums := []byte("zz  other\n" + good + "  skipack-linux-amd64\n")
	if got, err := checksumFor(sums, "skipack-linux-amd64"); err != nil || got != good {
		t.Fatalf("got=%q err=%v", got, err)
	}
	for _, bad := range []string{"", "short  skipack-linux-amd64\n", strings.Repeat("g", 64) + "  skipack-linux-amd64\n"} {
		if _, err := checksumFor([]byte(bad), "skipack-linux-amd64"); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestDownloadRejectsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	u := Updater{HTTP: server.Client(), BaseURL: server.URL}
	if _, err := u.download("SHA256SUMS"); err == nil {
		t.Fatal("404 accepted")
	}
}

func TestDefaultsAndRedirectPolicy(t *testing.T) {
	u := Updater{}
	if u.goos() != runtime.GOOS || u.goarch() != runtime.GOARCH || u.httpClient() == nil || u.executable() == nil || u.replacer() == nil {
		t.Fatal("defaults not applied")
	}
	check := New().HTTP.CheckRedirect
	bad, _ := http.NewRequest("GET", "https://evil.example/x", nil)
	good, _ := http.NewRequest("GET", "https://objects.githubusercontent.com/x", nil)
	if check(bad, nil) == nil {
		t.Fatal("redirect to an unexpected host accepted")
	}
	if err := check(good, nil); err != nil {
		t.Fatalf("GitHub asset host rejected: %v", err)
	}
}

func TestRemoveSelfLaterNeverTouchesARegularExecutable(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	removeSelfLater()
	if _, err := os.Stat(self); err != nil {
		t.Fatalf("test binary was removed: %v", err)
	}
}
