package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://github.com/CultureLinux/skipack"
	maxBinaryBytes = 100 << 20
)

type Updater struct {
	HTTP       *http.Client
	BaseURL    string
	GOOS       string
	GOARCH     string
	Executable func() (string, error)
	Replace    func(target, staged string) error
}

func New() Updater {
	h := &http.Client{Timeout: 30 * time.Second}
	h.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		switch strings.ToLower(req.URL.Hostname()) {
		case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
			return nil
		default:
			return errors.New("release redirected to an unexpected host")
		}
	}
	return Updater{HTTP: h, BaseURL: defaultBaseURL}
}

func (u Updater) Update() error {
	asset := "skipack-" + u.goos() + "-" + u.goarch()
	if u.goos() == "windows" {
		asset += ".exe"
	}
	sums, err := u.download("SHA256SUMS")
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return err
	}
	binary, err := u.download(asset)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	got := sha256.Sum256(binary)
	if !strings.EqualFold(want, hex.EncodeToString(got[:])) {
		return errors.New("downloaded binary checksum does not match SHA256SUMS")
	}
	target, err := u.executable()()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".skipack-self-update-*")
	if err != nil {
		return fmt.Errorf("create update file: %w", err)
	}
	staged := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(staged)
		}
	}()
	if _, err = tmp.Write(binary); err == nil {
		err = tmp.Chmod(0755)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write update file: %w", err)
	}
	if err = u.replacer()(target, staged); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	cleanup = false
	return nil
}

func Replace(target, staged string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(staged, target)
	}
	backup := target + ".skipack-backup"
	_ = os.Remove(backup)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := os.Rename(target, backup); err == nil {
			if err = os.Rename(staged, target); err == nil {
				return os.Remove(backup)
			}
			_ = os.Rename(backup, target)
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for the running executable to close")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (u Updater) download(name string) ([]byte, error) {
	base := strings.TrimRight(u.BaseURL, "/")
	req, err := http.NewRequest(http.MethodGet, base+"/releases/latest/download/"+name, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBinaryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBinaryBytes {
		return nil, errors.New("download exceeds 100 MiB limit")
	}
	return b, nil
}

func checksumFor(data []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset && len(fields[0]) == sha256.Size*2 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("SHA256SUMS does not contain %s", asset)
}

func (u Updater) goos() string {
	if u.GOOS != "" {
		return u.GOOS
	}
	return runtime.GOOS
}

func (u Updater) goarch() string {
	if u.GOARCH != "" {
		return u.GOARCH
	}
	return runtime.GOARCH
}

func (u Updater) httpClient() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return New().HTTP
}

func (u Updater) executable() func() (string, error) {
	if u.Executable != nil {
		return u.Executable
	}
	return os.Executable
}

func (u Updater) replacer() func(string, string) error {
	if u.Replace != nil {
		return u.Replace
	}
	if runtime.GOOS != "windows" {
		return Replace
	}
	return scheduleWindowsReplacement
}

func scheduleWindowsReplacement(target, staged string) error {
	helper := filepath.Join(os.TempDir(), "skipack-self-update-helper.exe")
	current, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	if err = os.WriteFile(helper, data, 0700); err != nil {
		return err
	}
	return exec.Command(helper, "self-update", "--replace", target, staged).Start()
}
