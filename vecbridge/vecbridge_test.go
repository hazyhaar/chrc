package vecbridge

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/hazyhaar/pkg/dbopen"
	"github.com/hazyhaar/pkg/horosvec"
)

func TestServiceRoundTrip(t *testing.T) {
	db := dbopen.OpenMemory(t)

	svc, err := NewFromDB(db, horosvec.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if svc.Index.Count() != 0 {
		t.Fatalf("expected empty index, got %d", svc.Index.Count())
	}

	// Build a small index.
	dim := 32
	n := 200
	vecs := make([][]float32, n)
	ids := make([][]byte, n)
	for i := range vecs {
		v := make([]float32, dim)
		for j := range v {
			v[j] = rand.Float32() - 0.5
		}
		vecs[i] = v
		ids[i] = []byte{byte(i >> 8), byte(i & 0xff)}
	}

	iter := &sliceIter{vecs: vecs, ids: ids}
	if err := svc.Index.Build(context.Background(), iter); err != nil {
		t.Fatal(err)
	}

	if svc.Index.Count() != n {
		t.Fatalf("expected %d nodes, got %d", n, svc.Index.Count())
	}

	// Search for a known vector.
	results, err := svc.Index.Search(vecs[0], 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	// loadVector should work.
	vec, err := svc.loadVector(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != dim {
		t.Fatalf("expected %d dims, got %d", dim, len(vec))
	}
}

// sliceIter implements horosvec.VectorIterator for testing.
type sliceIter struct {
	vecs [][]float32
	ids  [][]byte
	pos  int
}

func (s *sliceIter) Next() ([]byte, []float32, bool) {
	if s.pos >= len(s.vecs) {
		return nil, nil, false
	}
	id := s.ids[s.pos]
	vec := s.vecs[s.pos]
	s.pos++
	return id, vec, true
}

func (s *sliceIter) Reset() error {
	s.pos = 0
	return nil
}
