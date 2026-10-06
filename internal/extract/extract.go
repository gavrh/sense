package extract

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	readability "github.com/go-shiori/go-readability"
)

const defaultMaxPassageChars = 1200

type Options struct {
	MaxPassageChars int // if <= 0 use 1200 (roughly 300 tokens)
	MaxPassages     int // if <= 0, no cap
}

type Passage struct {
	Index int
	Text  string
}

// Extract renders readable text from HTML and splits it into passages.
func Extract(r io.Reader, pageURL string, opts Options) ([]Passage, error) {
	var parsed *url.URL
	if pageURL != "" {
		u, err := url.Parse(pageURL)
		if err != nil {
			return nil, fmt.Errorf("extract: parse page url: %w", err)
		}
		parsed = u
	}

	article, err := readability.FromReader(r, parsed)
	if err != nil {
		return nil, fmt.Errorf("extract: readability: %w", err)
	}

	return Passages(article.TextContent, opts), nil
}

// Passages splits already-plain text into passages at sentence boundaries.
func Passages(text string, opts Options) []Passage {
	maxChars := opts.MaxPassageChars
	if maxChars <= 0 {
		maxChars = defaultMaxPassageChars
	}

	var (
		passages   []Passage
		current    []string
		currentLen int
	)

	flush := func() {
		joined := strings.TrimSpace(strings.Join(current, " "))
		if joined != "" {
			passages = append(passages, Passage{Index: len(passages), Text: joined})
		}
		current = nil
		currentLen = 0
	}

	for _, sentence := range Sentences(text) {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		sep := 0
		if len(current) > 0 {
			sep = 1
		}
		size := utf8.RuneCountInString(sentence)

		if currentLen+sep+size > maxChars {
			flush()
			sep = 0
		}
		current = append(current, sentence)
		currentLen += sep + size
	}
	flush()

	if opts.MaxPassages > 0 && len(passages) > opts.MaxPassages {
		passages = passages[:opts.MaxPassages]
	}
	return passages
}

// Sentences splits plain text into individual sentences. It is a simple
// heuristic: a sentence ends at ".", "!", or "?" followed by whitespace.
func Sentences(text string) []string {
	var (
		sentences []string
		start     int
	)

	for i := 0; i < len(text); i++ {
		if !isSentenceEnd(text[i]) {
			continue
		}
		if i+1 < len(text) && !isSpace(text[i+1]) {
			continue
		}

		if sentence := strings.TrimSpace(text[start : i+1]); sentence != "" {
			sentences = append(sentences, sentence)
		}
		start = i + 1
	}

	if tail := strings.TrimSpace(text[start:]); tail != "" {
		sentences = append(sentences, tail)
	}
	return sentences
}

func isSentenceEnd(b byte) bool {
	return b == '.' || b == '!' || b == '?'
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
