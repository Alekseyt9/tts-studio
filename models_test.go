package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelSelectionSurvivesRestartAndSettingsChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	job := &Job{ID: "book", CreatedAt: time.Now(), TTSModel: "omni32", TranslationModel: "hy_mt2_1_8b"}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(AppSettings{TTSModel: "omni16", TranslationModel: "gemma4_direct"}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].TTSModel != "omni32" || jobs[0].TranslationModel != "hy_mt2_1_8b" {
		t.Fatalf("job selection lost: %+v", jobs)
	}
	if settings.TTSModel != "omni16" || settings.TranslationModel != "gemma4_direct" {
		t.Fatalf("settings lost: %+v", settings)
	}
}

func TestLegacyJobsKeepOriginalModelsDuringMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveJob(&Job{ID: "legacy", CreatedAt: time.Now(), Status: "paused"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(AppSettings{RefAudio: "reference_user_voice_10s.wav", SpeakerOnly: true}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"jobs", "app_settings"} {
		for _, column := range []string{"tts_model", "translation_model"} {
			if _, err := store.db.Exec("ALTER TABLE " + table + " DROP COLUMN " + column); err != nil {
				t.Fatal(err)
			}
		}
	}
	store.Close()
	store, err = NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	jobs, err := store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].TTSModel != "qwen" || jobs[0].TranslationModel != "gemma4_think" || jobs[0].Status != "paused" {
		t.Fatalf("legacy job changed: %+v", jobs[0])
	}
	if settings.RefAudio != "reference_user_voice_10s.wav" || !settings.SpeakerOnly {
		t.Fatalf("voice changed: %+v", settings)
	}
}

func TestTranslatorUsesEachJobsModelAndPrompt(t *testing.T) {
	for _, option := range translationModels {
		t.Run(option.ID, func(t *testing.T) {
			var received ollamaGenerateRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				json.NewEncoder(w).Encode(ollamaGenerateResponse{Response: "Дверь открыта.", Done: true})
			}))
			defer server.Close()
			s := &Studio{translator: &OllamaTranslator{URL: server.URL, Model: "original", Client: server.Client()}}
			translator := s.translatorForJob(&Job{TranslationModel: option.ID})
			_, err := translator.Translate(context.Background(), "The door is open.", 4000, 1, nil, func(int, int, float64, int, int) {}, func(int, int, string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if received.Model != option.Model || received.Think != option.Think {
				t.Fatalf("wrong model or thinking: %+v", received)
			}
			if received.Options["num_ctx"] != float64(16384) || !strings.Contains(received.Prompt, "The door is open.") {
				t.Fatalf("lost input/context: %+v", received)
			}
			if strings.HasPrefix(option.ID, "translategemma") && !strings.HasPrefix(received.Prompt, "You are a professional English (en) to Russian (ru) translator.") {
				t.Fatal("wrong native prompt")
			}
			if s.translator.Model != "original" || s.translator.Profile != "" {
				t.Fatal("shared translator was mutated")
			}
		})
	}
}

func TestSettingsRejectUnknownModel(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Studio{store: store, voices: []VoiceSample{{RefAudio: "voice.wav"}}}
	r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"ref_audio":"voice.wav","chunk_size":1200,"translation_chunk_size":4000,"tts_model":"not-installed","translation_model":"gemma4_direct"}`))
	w := httptest.NewRecorder()
	s.settingsHandler(w, r)
	if w.Code != 400 {
		t.Fatalf("unknown model accepted: %d", w.Code)
	}
}
