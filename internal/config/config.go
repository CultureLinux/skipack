package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Source string `yaml:"source"`
	Skill  string `yaml:"skill,omitempty"`
	Path   string `yaml:"-"`
}
type Config struct {
	Version int                `yaml:"version"`
	Groups  map[string][]Skill `yaml:"groups"`
}

func Parse(data []byte) (Config, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) {
		return Config{}, errors.New("configuration is not valid UTF-8")
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("parse YAML: %w", err)
	}
	if c.Version != 1 {
		return c, fmt.Errorf("unsupported configuration version %d (expected 1)", c.Version)
	}
	if c.Groups == nil {
		return c, errors.New("configuration must define groups")
	}
	for group, skills := range c.Groups {
		if group == "" {
			return c, errors.New("group name cannot be empty")
		}
		for i, skill := range skills {
			if skill.Source == "" {
				return c, fmt.Errorf("group %q item %d has no source", group, i+1)
			}
		}
	}
	return c, nil
}

type Store struct{ Path string }

func DefaultStore() (Store, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Path: filepath.Join(d, "skipack", "config.json")}, nil
}
func (s Store) Set(source string) error {
	if source == "" {
		return errors.New("configuration source cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	b, _ := json.Marshal(struct {
		Source string `json:"source"`
	}{source})
	b = append(b, '\n')
	return os.WriteFile(s.Path, b, 0600)
}
func (s Store) Get() (string, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errors.New("no configuration source set; run 'skipack config set <source>'")
		}
		return "", err
	}
	var v struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", fmt.Errorf("read skipack settings: %w", err)
	}
	if v.Source == "" {
		return "", errors.New("configuration source is empty")
	}
	return v.Source, nil
}
