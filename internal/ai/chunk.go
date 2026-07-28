package ai

import "strings"

// Chunk splits book text into overlapping passages sized for embedding. Sizes
// are measured in words (a cheap, dependency-free proxy for tokens): ~350 words
// ≈ ~500 tokens, with ~50 words of overlap so a sentence spanning a boundary
// still appears whole in one chunk. Paragraph breaks are preferred split points.
func Chunk(text string) []string {
	const (
		size    = 350
		overlap = 50
	)
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	if len(words) <= size {
		return []string{strings.Join(words, " ")}
	}
	var chunks []string
	for start := 0; start < len(words); start += size - overlap {
		end := start + size
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.TrimSpace(strings.Join(words[start:end], " "))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(words) {
			break
		}
	}
	return chunks
}
