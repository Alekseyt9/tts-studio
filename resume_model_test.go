package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resumeTestStudio(t *testing.T) (*Studio, *Job) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	job := &Job{ID: "book", CreatedAt: time.Now(), Status: "paused", TTSModel: "qwen", TranslationModel: "gemma4_direct", Text: "Готовый перевод.", TranslationStatus: "ready", SpeakerOnly: true, RefAudio: "voice.wav", Chunks: []*Chunk{
		{ID: 1, Status: "ready", Path: "keep.wav", AudioURL: "/keep.wav", Duration: 60, SynthesisSeconds: 200},
		{ID: 2, Status: "queued", Text: "Остаток книги."},
	}}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	return &Studio{store: store, jobs: map[string]*Job{job.ID: job}, queued: map[string]bool{}, queue: make(chan string, 2), controls: map[string]context.CancelFunc{}}, job
}

func TestResumeChangesOnlyRemainingTTSAndPersistsSelection(t *testing.T) {
	for _, engine := range []string{"faster", "omni32", "omni16"} {
		t.Run(engine, func(t *testing.T) {
			s, job := resumeTestStudio(t)
			w := httptest.NewRecorder()
			s.jobHandler(w, httptest.NewRequest(http.MethodPost, "/api/jobs/book/resume", strings.NewReader(`{"tts_model":"`+engine+`"}`)))
			if w.Code != 200 {
				t.Fatalf("resume: %d %s", w.Code, w.Body.String())
			}
			if job.TTSModel != engine || job.TranslationModel != "gemma4_direct" || job.Text != "Готовый перевод." || job.RefAudio != "voice.wav" {
				t.Fatalf("wrong resumed settings: %+v", job)
			}
			ready := job.Chunks[0]
			if ready.Status != "ready" || ready.Path != "keep.wav" || ready.AudioURL != "/keep.wav" || ready.Duration != 60 || ready.SynthesisSeconds != 200 || ready.TTSModel != "qwen" {
				t.Fatalf("finished audio changed: %+v", ready)
			}
			if job.Chunks[1].Status != "queued" {
				t.Fatal("unfinished audio was skipped")
			}
			loaded, err := s.store.LoadJobs()
			if err != nil {
				t.Fatal(err)
			}
			if loaded[0].TTSModel != engine || loaded[0].Chunks[0].TTSModel != "qwen" {
				t.Fatal("model selection/history lost on reload")
			}
			select {
			case id := <-s.queue:
				if id != "book" {
					t.Fatal(id)
				}
			default:
				t.Fatal("resume not queued")
			}
		})
	}
}

func TestResumeRejectsInvalidModelOrRunningJobWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		status, body string
		code         int
	}{
		{"paused", `{"tts_model":"unknown"}`, 400},
		{"paused", `{"tts_model":`, 400},
		{"running", `{"tts_model":"omni16"}`, 409},
	} {
		t.Run(tc.status+tc.body, func(t *testing.T) {
			s, job := resumeTestStudio(t)
			job.Status = tc.status
			w := httptest.NewRecorder()
			s.jobHandler(w, httptest.NewRequest(http.MethodPost, "/api/jobs/book/resume", strings.NewReader(tc.body)))
			if w.Code != tc.code || job.TTSModel != "qwen" || job.Status != tc.status || len(s.queue) != 0 {
				t.Fatalf("invalid resume mutated job: %d %+v", w.Code, job)
			}
		})
	}
}

func TestResumePersistenceFailureLeavesJobPaused(t *testing.T) {
	s, job := resumeTestStudio(t)
	s.store.Close()
	w := httptest.NewRecorder()
	s.jobHandler(w, httptest.NewRequest(http.MethodPost, "/api/jobs/book/resume", strings.NewReader(`{"tts_model":"faster"}`)))
	if w.Code != 500 || job.Status != "paused" || job.TTSModel != "qwen" || len(s.queue) != 0 {
		t.Fatal("failed persistence changed in-memory job")
	}
}
