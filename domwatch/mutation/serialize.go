package mutation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// MarshalBatch serialises a Batch to JSON.
func MarshalBatch(b *Batch) ([]byte, error) {
	return json.Marshal(b)
}

// UnmarshalBatch deserialises a Batch from JSON.
func UnmarshalBatch(data []byte) (*Batch, error) {
	var b Batch
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// MarshalSnapshot serialises a Snapshot to JSON.
func MarshalSnapshot(s *Snapshot) ([]byte, error) {
	return json.Marshal(s)
}

// UnmarshalSnapshot deserialises a Snapshot from JSON.
func UnmarshalSnapshot(data []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// MarshalProfile serialises a Profile to JSON.
func MarshalProfile(p *Profile) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalProfile deserialises a Profile from JSON.
func UnmarshalProfile(data []byte) (*Profile, error) {
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// HashHTML returns the SHA-256 hex digest of raw HTML bytes.
func HashHTML(html []byte) string {
	h := sha256.Sum256(html)
	return fmt.Sprintf("%x", h)
}
