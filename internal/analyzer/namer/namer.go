package namer

import (
	"embed"
	"errors"
	"sync"
)

//go:embed vectors.w2v
var fs embed.FS

// embedded parses the bundled dictionary once per process.
//
// Ten megabytes of vectors are decoded into a map of a hundred thousand
// entries, and a run builds a Namer for every aggregation scope: the global
// view, the per-file view, and one per language and per analyzed directory. Each
// of them used to decode the same dictionary again, for a result none of them
// modifies afterwards.
var embedded = sync.OnceValues(func() (map[string][]float32, int) {
	data, err := fs.ReadFile("vectors.w2v")
	if err != nil {
		return nil, 0
	}
	vectors, dim, err := loadW2V(data)
	if err != nil {
		return nil, 0
	}
	return vectors, dim
})

// Namer derives a short label (1–3 words) for a cluster of class names
// using token embeddings (word vectors).
type Namer struct {
	vectors map[string][]float32
	dim     int
}

// Option configures a Namer.
type Option func(*Namer)

// WithDim overrides the expected embedding dimension (default: 50).
func WithDim(dim int) Option {
	return func(n *Namer) { n.dim = dim }
}

// WithVectors injects vectors instead of using the embedded vectors.json.
func WithVectors(v map[string][]float32) Option {
	return func(n *Namer) { n.vectors = v }
}

// New creates a new Namer. By default it loads embedded vectors.json and expects 50D vectors.
func NewNamer(opts ...Option) (*Namer, error) {
	n := &Namer{dim: 50}
	for _, opt := range opts {
		opt(n)
	}

	if n.vectors == nil {
		vectors, dim := embedded()
		if len(vectors) == 0 {
			return nil, errors.New("namer: cannot read the embedded vectors")
		}

		n.vectors = vectors
		n.dim = dim
	}

	if err := n.validate(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *Namer) validate() error {
	if n.dim <= 0 {
		return errors.New("namer: dim must be > 0")
	}
	if len(n.vectors) == 0 {
		return errors.New("namer: empty vectors dictionary")
	}
	checked := 0
	for _, vec := range n.vectors {
		if len(vec) != n.dim {
			return errors.New("namer: vector dimension mismatch in dictionary")
		}
		checked++
		if checked >= 16 {
			break
		}
	}
	return nil
}
