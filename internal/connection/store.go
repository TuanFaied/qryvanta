package connection

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/TuanFaied/qryvanta/internal/fsutil"
)

// StoreSchemaVersion is the current connections file format version.
const StoreSchemaVersion = 1

// FileName is the connections file name, stored next to config.json.
const FileName = "connections.json"

type fileFormat struct {
	SchemaVersion int       `json:"schemaVersion"`
	Profiles      []Profile `json:"profiles"`
}

// Store persists profiles to a JSON file. It holds no secrets.
type Store struct {
	path string
}

// NewStore returns a store backed by the file at path.
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the file path.
func (s *Store) Path() string { return s.path }

// Load reads all profiles. A missing file means no profiles.
func (s *Store) Load() ([]Profile, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read connections: %w", err)
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if f.SchemaVersion > StoreSchemaVersion {
		return nil, fmt.Errorf("%s was written by a newer Qryvanta (schema %d, supported %d)",
			s.path, f.SchemaVersion, StoreSchemaVersion)
	}
	if f.Profiles == nil {
		f.Profiles = []Profile{}
	}
	return f.Profiles, nil
}

// Save replaces all profiles atomically.
func (s *Store) Save(profiles []Profile) error {
	if profiles == nil {
		profiles = []Profile{}
	}
	data, err := json.MarshalIndent(fileFormat{SchemaVersion: StoreSchemaVersion, Profiles: profiles}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode connections: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("save connections: %w", err)
	}
	return nil
}
