package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gavrh/sense/internal/extract"
)

func TestExtractArticle(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "article.html"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	passages, err := extract.Extract(f, "https://example.com/how-plants-make-food", extract.Options{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(passages) == 0 {
		t.Fatal("Extract returned no passages")
	}

	var text strings.Builder
	for i, p := range passages {
		if p.Index != i {
			t.Errorf("passage %d has Index %d, want %d", i, p.Index, i)
		}
		text.WriteString(p.Text)
		text.WriteByte(' ')
	}
	got := text.String()

	const articleSentence = "Photosynthesis is the process plants use to turn light into chemical energy."
	if !strings.Contains(got, articleSentence) {
		t.Errorf("article text missing %q", articleSentence)
	}

	excluded := []string{
		"SCRIPT_SENTINEL_TOKEN",
		"STYLE_SENTINEL_TOKEN",
		"NAV_SENTINEL_TOKEN",
		"HEADER_SENTINEL_TOKEN",
		"ASIDE_SENTINEL_TOKEN",
		"FORM_SENTINEL_TOKEN",
		"FOOTER_SENTINEL_TOKEN",
	}
	for _, sentinel := range excluded {
		if strings.Contains(got, sentinel) {
			t.Errorf("extracted text contains %q", sentinel)
		}
	}
}

func TestExtractSentences(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"One. Two! Three? Four", []string{"One.", "Two!", "Three?", "Four"}},
		{"First one.\n\nSecond one!  Third?", []string{"First one.", "Second one!", "Third?"}},
	}

	for _, tc := range cases {
		got := extract.Sentences(tc.text)
		if len(got) != len(tc.want) {
			t.Fatalf("Sentences(%q) = %q, want %q", tc.text, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("Sentences(%q)[%d] = %q, want %q", tc.text, i, got[i], tc.want[i])
			}
		}
	}
}

func TestExtractPassagesBudget(t *testing.T) {
	text := "aaaa. bbbb. cccc. dddd. eeee."

	passages := extract.Passages(text, extract.Options{MaxPassageChars: 12})
	if len(passages) < 2 {
		t.Fatalf("got %d passages, want at least 2", len(passages))
	}

	want := strings.Join(extract.Sentences(text), " ")
	got := make([]string, 0, len(passages))
	for _, p := range passages {
		if len([]rune(p.Text)) > 12 {
			t.Errorf("passage %q exceeds budget", p.Text)
		}
		got = append(got, p.Text)
	}
	if joined := strings.Join(got, " "); joined != want {
		t.Errorf("passages = %q, want %q", joined, want)
	}
}

func TestExtractPassagesLimits(t *testing.T) {
	t.Run("max passages", func(t *testing.T) {
		passages := extract.Passages("aaaa. bbbb. cccc. dddd. eeee.", extract.Options{MaxPassageChars: 12, MaxPassages: 1})
		if len(passages) != 1 {
			t.Fatalf("got %d passages, want 1", len(passages))
		}
	})

	t.Run("long sentence stays whole", func(t *testing.T) {
		long := "This single sentence is far longer than the tiny budget allows."
		passages := extract.Passages(long, extract.Options{MaxPassageChars: 10})
		if len(passages) != 1 {
			t.Fatalf("got %d passages, want 1", len(passages))
		}
		if passages[0].Text != long {
			t.Errorf("passage = %q, want %q", passages[0].Text, long)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		for _, text := range []string{"", "   ", "\n\t  \n"} {
			if passages := extract.Passages(text, extract.Options{}); len(passages) != 0 {
				t.Errorf("Passages(%q) returned %d passages, want none", text, len(passages))
			}
		}
	})
}
