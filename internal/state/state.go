package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Skill struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Path   string `json:"path"`
}
type File struct {
	Version int     `json:"version"`
	Skills  []Skill `json:"skills"`
}

func path(root string) string { return filepath.Join(root, ".agents", "skipack-state.json") }
func Load(root string) (File, error) {
	b, err := os.ReadFile(path(root))
	if err != nil {
		if os.IsNotExist(err) {
			return File{Version: 1, Skills: []Skill{}}, nil
		}
		return File{}, err
	}
	var f File
	if err = json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	if f.Version != 1 {
		return f, errors.New("unsupported state version")
	}
	return f, nil
}
func Save(root string, f File) error {
	f.Version = 1
	if f.Skills == nil {
		f.Skills = []Skill{}
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Join(root, ".agents")
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "skipack-state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0644); err == nil {
		_, err = tmp.Write(b)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path(root))
}
