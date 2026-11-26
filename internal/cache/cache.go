package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	dir string
}

type entry struct {
	SavedAt time.Time       `json:"saved_at"`
	Data    json.RawMessage `json:"data"`
}

func New() (*Store, error) {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "hop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) Load(key string, dst any) (time.Time, bool, error) {
	data, err := os.ReadFile(s.path(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return time.Time{}, false, err
	}
	if err := json.Unmarshal(e.Data, dst); err != nil {
		return time.Time{}, false, err
	}
	return e.SavedAt, true, nil
}

func (s *Store) Save(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry{SavedAt: time.Now(), Data: data})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(key))
}

func (s *Store) Clear() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range entries {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, item.Name())); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Store) path(key string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, key)
	return filepath.Join(s.dir, safe+".json")
}
