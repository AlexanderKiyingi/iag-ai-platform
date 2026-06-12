package provider

import (
	"hash/fnv"
	"math"
	"strings"
)

// EmbedModel is the identifier reported for the built-in deterministic embedder.
const EmbedModel = "iag-hash-embed-v1"

// Embed produces deterministic, L2-normalized embeddings using feature hashing
// (the hashing trick) over word tokens. It needs no external API, so services
// can wire and test semantic-similarity flows immediately; swap in a real
// embeddings provider later by extending this package. Same text + same dim
// always yields the same vector.
func Embed(inputs []string, dim int) [][]float64 {
	if dim <= 0 {
		dim = 256
	}
	out := make([][]float64, len(inputs))
	for i, text := range inputs {
		out[i] = embedOne(text, dim)
	}
	return out
}

func embedOne(text string, dim int) []float64 {
	vec := make([]float64, dim)
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		sum := h.Sum32()
		idx := int(sum % uint32(dim))
		// Sign bit spreads features across +/- to reduce collisions.
		if sum&1 == 0 {
			vec[idx] += 1
		} else {
			vec[idx] -= 1
		}
	}
	return l2normalize(vec)
}

func l2normalize(v []float64) []float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	if sum == 0 {
		return v
	}
	norm := math.Sqrt(sum)
	for i := range v {
		v[i] /= norm
	}
	return v
}
