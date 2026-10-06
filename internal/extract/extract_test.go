package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractArticle(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "article.html"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	passages, err := Extract(f, "https://example.com/how-plants-make-food", Options{})
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

func TestSentences(t *testing.T) {
	got := Sentences("One. Two! Three? Four")
	want := []string{"One.", "Two!", "Three?", "Four"}

	if len(got) != len(want) {
		t.Fatalf("Sentences = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sentence %d = %q, want %q", i, got[i], want[i])
		}
	}

	multiline := Sentences("First one.\n\nSecond one!  Third?")
	wantMultiline := []string{"First one.", "Second one!", "Third?"}
	if len(multiline) != len(wantMultiline) {
		t.Fatalf("Sentences = %q, want %q", multiline, wantMultiline)
	}
	for i := range wantMultiline {
		if multiline[i] != wantMultiline[i] {
			t.Errorf("sentence %d = %q, want %q", i, multiline[i], wantMultiline[i])
		}
	}
}

func TestPassagesRespectsBudget(t *testing.T) {
	text := "aaaa. bbbb. cccc. dddd. eeee."

	passages := Passages(text, Options{MaxPassageChars: 12})
	if len(passages) < 2 {
		t.Fatalf("got %d passages, want at least 2", len(passages))
	}

	want := strings.Join(Sentences(text), " ")
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

func TestPassagesKeepsLongSentenceWhole(t *testing.T) {
	long := "This single sentence is far longer than the tiny budget allows."
	passages := Passages(long, Options{MaxPassageChars: 10})

	if len(passages) != 1 {
		t.Fatalf("got %d passages, want 1", len(passages))
	}
	if passages[0].Text != long {
		t.Errorf("passage = %q, want %q", passages[0].Text, long)
	}
}

func TestPassagesAppliesMaxPassages(t *testing.T) {
	text := "aaaa. bbbb. cccc. dddd. eeee."
	passages := Passages(text, Options{MaxPassageChars: 12, MaxPassages: 1})

	if len(passages) != 1 {
		t.Fatalf("got %d passages, want 1", len(passages))
	}
}

func TestPassagesEmptyInput(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\t  \n"} {
		if passages := Passages(text, Options{}); len(passages) != 0 {
			t.Errorf("Passages(%q) returned %d passages, want none", text, len(passages))
		}
	}
}
