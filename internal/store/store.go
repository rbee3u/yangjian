package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/schema"
)

type Store struct {
	Root string
}

func New(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) Init() error {
	return os.MkdirAll(s.Root, 0o755)
}

func (s *Store) Path(rel string) string {
	return filepath.Join(s.Root, rel)
}

func (s *Store) WriteJSON(rel string, v any) (string, error) {
	_, _, err := s.WriteJSONHashed(rel, v)
	if err != nil {
		return "", err
	}
	return rel, nil
}

// WriteJSONHashed writes an indented JSON document and returns the path and the
// SHA-256 hash of the exact bytes written.
func (s *Store) WriteJSONHashed(rel string, v any) (string, string, error) {
	if err := schema.Validate(v); err != nil {
		return "", "", fmt.Errorf("validate %s: %w", rel, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal %s: %w", rel, err)
	}
	full := s.Path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", "", fmt.Errorf("create directory for %s: %w", rel, err)
	}
	payload := append(data, '\n')
	if err := os.WriteFile(full, payload, 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", rel, err)
	}
	return rel, model.ContentHash(payload), nil
}

func (s *Store) ReadJSON(rel string, v any) error {
	data, err := os.ReadFile(s.Path(rel))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", rel, err)
	}
	if err := schema.Validate(v); err != nil {
		return fmt.Errorf("validate %s: %w", rel, err)
	}
	return nil
}

func (s *Store) WriteFile(rel string, data []byte) (string, error) {
	full := s.Path(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("create directory for %s: %w", rel, err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", rel, err)
	}
	return rel, nil
}

func (s *Store) WriteObservation(obs model.Observation) (model.Observation, error) {
	hashInput := struct {
		Request      map[string]any  `json:"request"`
		ResponseHash string          `json:"response_hash"`
		Block        *model.BlockRef `json:"block,omitempty"`
		Source       model.SourceRef `json:"source"`
	}{
		Request:      obs.Request,
		ResponseHash: obs.ResponseHash,
		Block:        obs.Block,
		Source:       obs.Source,
	}
	data, err := json.Marshal(hashInput)
	if err != nil {
		return model.Observation{}, fmt.Errorf("hash observation: %w", err)
	}
	sum := sha256.Sum256(data)
	hexHash := hex.EncodeToString(sum[:])
	rel := filepath.Join("observations", hexHash+".json")

	var existing model.Observation
	if err := s.ReadJSON(rel, &existing); err == nil {
		return existing, nil
	}

	obs.ID = "sha256:" + hexHash
	obs.ResponseRef = rel
	if _, err := s.WriteJSON(rel, obs); err != nil {
		return model.Observation{}, err
	}
	return obs, nil
}
