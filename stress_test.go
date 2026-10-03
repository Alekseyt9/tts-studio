package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStressCleanupPreservesSpellingAndMeaningfulMarks(t *testing.T) {
	input := "О́н во́шёл в ста́рый за́мок. Хло́пок — хлопо́к; двери́́, дверь́, ё́лка, па́ро́м."
	want := "Он вошёл в ста́рый за́мок. Хло́пок — хлопо́к; двери, дверь, ёлка, паром."
	if got := cleanRussianStress(input); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.ReplaceAll(cleanRussianStress(input), "\u0301", "") != strings.ReplaceAll(input, "\u0301", "") {
		t.Fatal("cleanup changed letters or punctuation")
	}
}

func TestTTSChunkBoundaryKeepsStressWithVowel(t *testing.T) {
	chunks := splitText("за́мок", 2)
	if len(chunks) < 2 || chunks[0].Text != "за́" {
		t.Fatalf("accent separated from vowel: %+v", chunks)
	}
	for _, chunk := range chunks {
		if strings.HasPrefix(chunk.Text, "\u0301") {
			t.Fatal("chunk starts with a detached accent")
		}
	}
}

func TestStressTranslationIsScopedToJobAndSavedWithMarks(t *testing.T) {
	for _, profile := range []string{"gemma4_direct", "gemma4_think", "translategemma_12b", "hy_mt2_7b"} {
		t.Run(profile, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req ollamaGenerateRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if req.Prompt == "" {
					json.NewEncoder(w).Encode(ollamaGenerateResponse{Done: true})
					return
				}
				calls++
				if !strings.Contains(req.Prompt, "U+0301") || !strings.Contains(req.Prompt, "A castle and a lock.") {
					t.Errorf("lost stress instruction or source: %s", req.Prompt)
				}
				if profile == "gemma4_think" && calls == 1 {
					json.NewEncoder(w).Encode(ollamaGenerateResponse{Done: true})
					return
				}
				json.NewEncoder(w).Encode(ollamaGenerateResponse{Response: "За́мок и замо́к. О́н во́шёл.", Done: true})
			}))
			defer server.Close()
			s := &Studio{translator: &OllamaTranslator{URL: server.URL, Model: "gemma4:12b", Client: server.Client()}}
			translator := s.translatorForJob(&Job{TTSModel: "faster_stress", TranslationModel: profile})
			var saved string
			text, err := translator.Translate(context.Background(), "A castle and a lock.", 4000, 1, nil,
				func(int, int, float64, int, int) {}, func(_, _ int, part string) error { saved = part; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if text != "За́мок и замо́к. Он вошёл." || saved != text {
				t.Fatalf("marks not saved: %q %q", text, saved)
			}
			if s.translator.AddStress || s.translatorForJob(&Job{TTSModel: "faster"}).AddStress {
				t.Fatal("stress leaked to another job")
			}
		})
	}
}
