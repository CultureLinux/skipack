package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
