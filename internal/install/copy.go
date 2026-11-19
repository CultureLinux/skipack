package install

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copyTree copies regular files and directories only, so a local copy can never
// introduce symlinks or special entries.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported local entry %q", rel)
		}
		mode := os.FileMode(0644)
		if info.Mode()&0111 != 0 {
			mode = 0755
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		in.Close()
		if closeErr := out.Close(); copyErr == nil {
			copyErr = closeErr
		}
		return copyErr
	})
}

// stageCopy copies src into a fresh temporary folder inside parent and returns it.
func stageCopy(src, parent, pattern string) (string, error) {
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	// MkdirTemp creates 0700 folders on Unix; installed skills use 0755 like the
	// ones extracted from an archive.
	if err = os.Chmod(tmp, 0755); err == nil {
		err = copyTree(src, tmp)
	}
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

type fileSnapshot struct {
	directory bool
	digest    [sha256.Size]byte
}

func directoriesEqual(left, right string) (bool, error) {
	leftFiles, err := snapshotDirectory(left)
	if err != nil {
		return false, err
	}
	rightFiles, err := snapshotDirectory(right)
	if err != nil {
		return false, err
	}
	if len(leftFiles) != len(rightFiles) {
		return false, nil
	}
	for path, leftFile := range leftFiles {
		rightFile, ok := rightFiles[path]
		if !ok || leftFile != rightFile {
			return false, nil
		}
	}
	return true, nil
}

func snapshotDirectory(root string) (map[string]fileSnapshot, error) {
	files := map[string]fileSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		if entry.IsDir() {
			files[key] = fileSnapshot{directory: true}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported local entry %q", relative)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		var digest [sha256.Size]byte
		copy(digest[:], hash.Sum(nil))
		files[key] = fileSnapshot{digest: digest}
		return nil
	})
	return files, err
}
