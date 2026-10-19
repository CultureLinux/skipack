package github

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxSkillBytes int64 = 50 << 20

func ExtractSkillZip(data []byte, skillPath, dst string) error {
	if int64(len(data)) > maxSkillBytes {
		return errors.New("archive exceeds 50 MiB limit")
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	cleanSkill := strings.Trim(filepath.ToSlash(skillPath), "/")
	if cleanSkill == "" || strings.Contains(cleanSkill, "../") {
		return errors.New("invalid skill path")
	}
	type item struct {
		f   *zip.File
		rel string
	}
	var items []item
	manifest := false
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if strings.Contains(f.Name, "\\") || strings.HasPrefix(name, "/") || filepath.IsAbs(f.Name) {
			return errors.New("archive contains unsafe path")
		}
		parts := strings.Split(name, "/")
		for _, part := range parts {
			if part == "." || part == ".." {
				return errors.New("archive contains unsafe path")
			}
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
			return fmt.Errorf("archive contains unsupported entry %q", f.Name)
		}
		if len(parts) < 2 {
			continue
		}
		rest := strings.Join(parts[1:], "/")
		if rest != cleanSkill && !strings.HasPrefix(rest, cleanSkill+"/") {
			continue
		}
		rel := strings.TrimPrefix(rest, cleanSkill)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		if strings.Contains(rel, "\\") || filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
			return errors.New("archive contains unsafe path")
		}
		if rel == "SKILL.md" && mode.IsRegular() {
			manifest = true
		}
		items = append(items, item{f, rel})
	}
	if !manifest {
		return errors.New("skill does not contain SKILL.md at its root")
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	var total int64
	for _, it := range items {
		target := filepath.Join(dst, filepath.FromSlash(it.rel))
		base, _ := filepath.Abs(dst)
		abs, _ := filepath.Abs(target)
		if abs != base && !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
			return errors.New("archive path escapes destination")
		}
		if it.f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		total += int64(it.f.UncompressedSize64)
		if total > maxSkillBytes {
			return errors.New("extracted skill exceeds 50 MiB limit")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		src, err := it.f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(src, maxSkillBytes+1))
		closeErr := out.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
