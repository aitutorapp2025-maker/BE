// Package ai implements the tutoring pipeline: text embeddings (Voyage) for
// retrieval and answer generation (Claude Messages API). Both are called over
// plain net/http so the service takes on no new module dependencies.
package ai

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
)

// Vector is a float32 embedding stored in a PostgreSQL pgvector column. It
// (de)serializes using pgvector's text format — "[0.1,0.2,...]" — so no
// third-party driver type is required. GORM stores it with the column tag
// `type:vector(N)` on the model field.
type Vector []float32

// Value renders the vector as pgvector text for the driver.
func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}

// Scan parses pgvector text ("[0.1,0.2,...]") back into a Vector.
func (v *Vector) Scan(src any) error {
	if src == nil {
		*v = nil
		return nil
	}
	var s string
	switch t := src.(type) {
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		return fmt.Errorf("vector: unsupported scan type %T", src)
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		*v = Vector{}
		return nil
	}
	parts := strings.Split(s, ",")
	out := make(Vector, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return fmt.Errorf("vector: bad element %q: %w", p, err)
		}
		out[i] = float32(f)
	}
	*v = out
	return nil
}

// Literal returns the pgvector text form for use as a query parameter in a
// cosine-distance search (the `<=>` operator).
func (v Vector) Literal() string {
	s, _ := v.Value()
	if s == nil {
		return "[]"
	}
	return s.(string)
}
