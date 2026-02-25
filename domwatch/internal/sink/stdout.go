// CLAUDE:SUMMARY Writes mutation events as JSON lines to an io.Writer (defaults to stdout).
package sink

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/hazyhaar/chrc/domwatch/mutation"
)

// Stdout writes JSON lines to an io.Writer (default os.Stdout).
type Stdout struct {
	mu sync.Mutex
	w  io.Writer
	enc *json.Encoder
}

// NewStdout creates a Stdout sink. If w is nil, os.Stdout is used.
func NewStdout(w io.Writer) *Stdout {
	if w == nil {
		w = os.Stdout
	}
	return &Stdout{w: w, enc: json.NewEncoder(w)}
}

func (s *Stdout) Send(_ context.Context, batch mutation.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(envelope{Type: "batch", Data: batch})
}

func (s *Stdout) SendSnapshot(_ context.Context, snap mutation.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(envelope{Type: "snapshot", Data: snap})
}

func (s *Stdout) SendProfile(_ context.Context, prof mutation.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enc.Encode(envelope{Type: "profile", Data: prof})
}

func (s *Stdout) Close() error { return nil }

type envelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
