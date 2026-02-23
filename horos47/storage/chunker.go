package storage

import (
	"strings"
	"unicode"
)

// ChunkText splits text into chunks with word overlap for context.
// chunkSize: approximate word count per chunk.
// overlap: word overlap between consecutive chunks.
func ChunkText(text string, chunkSize int, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 200
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 4
	}

	text = normalizeWhitespace(text)
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{}
	}

	var chunks []string
	stride := chunkSize - overlap

	for i := 0; i < len(words); i += stride {
		end := i + chunkSize
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		chunks = append(chunks, chunk)
		if end == len(words) {
			break
		}
	}
	return chunks
}

// ChunkBySentences splits text respecting sentence boundaries.
// maxChunkSize: maximum word count per chunk.
func ChunkBySentences(text string, maxChunkSize int) []string {
	if maxChunkSize <= 0 {
		maxChunkSize = 200
	}

	text = normalizeWhitespace(text)
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return []string{}
	}

	var chunks []string
	var currentChunk strings.Builder
	currentWords := 0

	for _, sentence := range sentences {
		words := strings.Fields(sentence)
		sentenceWords := len(words)

		if currentWords == 0 && sentenceWords > maxChunkSize {
			chunks = append(chunks, sentence)
			continue
		}

		if currentWords+sentenceWords > maxChunkSize && currentWords > 0 {
			chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
			currentChunk.Reset()
			currentWords = 0
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
		currentWords += sentenceWords
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
	}
	return chunks
}

func normalizeWhitespace(text string) string {
	var result strings.Builder
	prevSpace := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			if !prevSpace {
				result.WriteRune(' ')
				prevSpace = true
			}
		} else {
			result.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(result.String())
}

func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder
	runes := []rune(text)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
				sentence := strings.TrimSpace(current.String())
				if len(sentence) > 0 {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if len(sentence) > 0 {
			sentences = append(sentences, sentence)
		}
	}
	return sentences
}
