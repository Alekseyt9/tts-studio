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

func TestSplitWholeSentencesKeepsAbbreviations(t *testing.T) {
	text := "Dr. Smith arrived at 3.14 p.m. He opened the door!\n\nThen he sat down."
	parts := splitWholeSentences(text)
	if len(parts) != 3 {
		t.Fatalf("got %d sentences, want 3: %#v", len(parts), parts)
	}
	if !strings.HasPrefix(parts[0], "Dr. Smith") {
		t.Fatalf("honorific was split from its sentence: %#v", parts)
	}
}

func TestTranslationChunksOnlySplitBetweenSentences(t *testing.T) {
	text := "First complete sentence. Second complete sentence. Third complete sentence."
	chunks := splitForTranslation(text, 32)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %#v", chunks)
	}
	for _, chunk := range chunks {
		if !strings.HasSuffix(strings.TrimSpace(chunk), ".") {
			t.Fatalf("chunk ends inside a sentence: %q", chunk)
		}
	}
}

func TestTranslatorEnablesThinkingAnd16KContext(t *testing.T) {
	var received ollamaGenerateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{Response: "Один полный перевод.", Done: true})
	}))
	defer server.Close()

	translator := &OllamaTranslator{URL: server.URL, Model: "gemma4:12b", Client: &http.Client{Timeout: time.Second}}
	translated, err := translator.Translate(
		context.Background(),
		"One complete sentence.",
		4000,
		nil,
		func(_, _ int, _ float64) {},
		func(_, _ int, _ string) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if translated != "Один полный перевод." {
		t.Fatalf("unexpected translation: %q", translated)
	}
	if !received.Think {
		t.Fatal("thinking mode is disabled")
	}
	if !received.Stream {
		t.Fatal("streaming mode is disabled")
	}
	if received.Options["num_ctx"] != float64(16384) {
		t.Fatalf("num_ctx = %#v, want 16384", received.Options["num_ctx"])
	}
}

func TestTranslatorReportsApproximateStreamingProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoder := json.NewEncoder(w)
		_ = encoder.Encode(ollamaGenerateResponse{Thinking: "Сначала анализирую смысл предложения."})
		_ = encoder.Encode(ollamaGenerateResponse{Response: "Это уже часть русского перевода, "})
		_ = encoder.Encode(ollamaGenerateResponse{Response: "а это его окончание.", Done: true})
	}))
	defer server.Close()
	translator := &OllamaTranslator{URL: server.URL, Model: "gemma4:12b", Client: &http.Client{Timeout: time.Second}}
	var updates []float64
	result, err := translator.stream(context.Background(), ollamaGenerateRequest{Model: translator.Model, Stream: true}, 60, func(progress float64) {
		updates = append(updates, progress)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "окончание") {
		t.Fatalf("streamed response was not accumulated: %q", result)
	}
	hasIntermediate := false
	for _, update := range updates {
		if update > 0 && update < 1 {
			hasIntermediate = true
		}
	}
	if !hasIntermediate || len(updates) == 0 || updates[len(updates)-1] != 1 {
		t.Fatalf("unexpected progress updates: %#v", updates)
	}
}

func TestSQLiteStoreRoundTripAndDelete(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := &Job{
		ID: "job-1", Title: "Book", SourceText: "Hello.", Text: "Привет.",
		ChunkSize: 1200, Voice: "clone", RefAudio: "voice.wav",
		SpeakerOnly: true, AutoMerge: true, Status: "running", Progress: 50,
		TranslationStatus: "ready", TranslationProgress: 100,
		TranslationChunk: 1, TranslationChunks: 1, TranslatedCharacters: 7,
		TranslationChunkSize: 4000, TranslationAttempt: 2, ErrorMessage: "temporary error",
		TranslationURL: "/audio/job-1/translation.txt", CreatedAt: time.Now(),
		TranslationParts: []string{"Привет."},
		Chunks:           []*Chunk{{ID: 1, Start: 1, End: 7, Characters: 7, Text: "Привет.", Status: "queued", SynthesisSeconds: 12.5}},
	}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Text != "Привет." || len(jobs[0].Chunks) != 1 || len(jobs[0].TranslationParts) != 1 || jobs[0].Chunks[0].SynthesisSeconds != 12.5 || jobs[0].TranslationAttempt != 2 || jobs[0].ErrorMessage != "temporary error" || jobs[0].TranslationChunkSize != 4000 {
		t.Fatalf("state was not restored: %#v", jobs)
	}
	if err := store.DeleteJob(job.ID); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("job remained in SQLite after delete: %#v", jobs)
	}
}

func TestSQLiteSettingsRoundTrip(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	defaults, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.RefAudio != "reference_audio_2.wav" || defaults.ChunkSize != 1200 || defaults.TranslationChunkSize != 4000 || !defaults.SpeakerOnly || !defaults.AutoMerge {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	want := AppSettings{RefAudio: "voice-b.wav", RefText: "Точный текст.", SpeakerOnly: false, ChunkSize: 2400, TranslationChunkSize: 2500, AutoMerge: false}
	if err := store.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("settings = %#v, want %#v", got, want)
	}
}

func TestDeleteJobRemovesQueueAndDatabaseState(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := &Job{
		ID: "delete-me", Title: "Queued", SourceText: "Hello.", ChunkSize: 1200,
		Voice: "clone", RefAudio: "voice.wav", SpeakerOnly: true,
		Status: "queued", TranslationStatus: "queued", CreatedAt: time.Now(),
		Chunks: []*Chunk{},
	}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	studio := &Studio{jobs: map[string]*Job{job.ID: job}, order: []string{job.ID}, store: store}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+job.ID, nil)
	studio.jobHandler(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}
	if len(studio.jobs) != 0 || len(studio.order) != 0 {
		t.Fatalf("job remained in memory: jobs=%d order=%d", len(studio.jobs), len(studio.order))
	}
	jobs, err := store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("job remained in SQLite: %#v", jobs)
	}
}

func TestApproximateProgressIsNotPersistedUntilChunkCompletes(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := &Job{
		ID: "translation", Title: "Translation", SourceText: "Hello.",
		ChunkSize: 1200, Voice: "clone", RefAudio: "voice.wav",
		SpeakerOnly: true, Status: "translating", TranslationStatus: "queued",
		CreatedAt: time.Now(), Chunks: []*Chunk{},
	}
	studio := &Studio{jobs: map[string]*Job{job.ID: job}, order: []string{job.ID}, store: store}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	studio.setTranslationProgress(job, 0, 1, 0.63)
	loaded, err := store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].TranslationProgress != 0 {
		t.Fatalf("approximate progress leaked into SQLite: %d", loaded[0].TranslationProgress)
	}
	if err := studio.saveTranslationPart(job, 1, 1, "Привет."); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].TranslationProgress != 100 || len(loaded[0].TranslationParts) != 1 {
		t.Fatalf("completed chunk was not persisted: %#v", loaded[0])
	}
}

func TestPauseAndResumeRestartCurrentChunk(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := &Job{
		ID: "pause-me", Title: "Paused", SourceText: "Hello.", Text: "Привет.",
		ChunkSize: 1200, Voice: "clone", RefAudio: "voice.wav", SpeakerOnly: true,
		Status: "running", Progress: 50, TranslationStatus: "ready",
		TranslationProgress: 100, TranslationChunk: 1, TranslationChunks: 1,
		TranslationParts: []string{"Привет."}, CreatedAt: time.Now(),
		Chunks: []*Chunk{{ID: 1, Text: "Привет.", Status: "running", Progress: 64, ElapsedSeconds: 20, SynthesisSeconds: 20, Path: "partial.wav", AudioURL: "/partial.wav"}},
	}
	studio := &Studio{
		jobs: map[string]*Job{job.ID: job}, order: []string{job.ID}, store: store,
		controls: map[string]context.CancelFunc{}, queued: map[string]bool{}, queue: make(chan string, 2),
	}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	pauseRecorder := httptest.NewRecorder()
	studio.jobHandler(pauseRecorder, httptest.NewRequest(http.MethodPost, "/api/jobs/"+job.ID+"/pause", nil))
	if pauseRecorder.Code != http.StatusOK || job.Status != "paused" {
		t.Fatalf("pause failed: status=%d job=%s body=%s", pauseRecorder.Code, job.Status, pauseRecorder.Body.String())
	}
	chunk := job.Chunks[0]
	if chunk.Status != "queued" || chunk.Progress != 0 || chunk.ElapsedSeconds != 0 || chunk.SynthesisSeconds != 0 || chunk.Path != "" {
		t.Fatalf("current chunk was not reset: %#v", chunk)
	}
	resumeRecorder := httptest.NewRecorder()
	studio.jobHandler(resumeRecorder, httptest.NewRequest(http.MethodPost, "/api/jobs/"+job.ID+"/resume", nil))
	if resumeRecorder.Code != http.StatusOK || job.Status != "queued" {
		t.Fatalf("resume failed: status=%d job=%s body=%s", resumeRecorder.Code, job.Status, resumeRecorder.Body.String())
	}
	select {
	case queuedID := <-studio.queue:
		if queuedID != job.ID {
			t.Fatalf("queued %q, want %q", queuedID, job.ID)
		}
	default:
		t.Fatal("resumed job was not queued")
	}
}

func TestRetryFailedTranslationKeepsCompletedChunks(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := &Job{
		ID: "retry-me", Title: "Retry", SourceText: "One. Two. Three.",
		ChunkSize: 1200, TranslationChunkSize: 4000, Voice: "clone", RefAudio: "voice.wav",
		SpeakerOnly: true, Status: "failed", Progress: 26, TranslationStatus: "failed",
		TranslationProgress: 66, TranslationChunk: 2, TranslationChunks: 3,
		TranslationAttempt: 3, ErrorMessage: "context exhausted",
		TranslationParts: []string{"Один.", "Два."}, CreatedAt: time.Now(), Chunks: []*Chunk{},
	}
	studio := &Studio{
		jobs: map[string]*Job{job.ID: job}, order: []string{job.ID}, store: store,
		controls: map[string]context.CancelFunc{}, queued: map[string]bool{}, queue: make(chan string, 2),
	}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	studio.jobHandler(recorder, httptest.NewRequest(http.MethodPost, "/api/jobs/"+job.ID+"/retry", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("retry status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if job.Status != "queued" || job.TranslationStatus != "queued" || job.TranslationChunk != 2 || len(job.TranslationParts) != 2 || job.ErrorMessage != "" || job.TranslationAttempt != 0 {
		t.Fatalf("unexpected retried job: %#v", job)
	}
	select {
	case id := <-studio.queue:
		if id != job.ID {
			t.Fatalf("queued %q, want %q", id, job.ID)
		}
	default:
		t.Fatal("retried job was not queued")
	}
}

func TestTranslationRetriesUnfinishedChunkThreeTimes(t *testing.T) {
	oldDelay := translationRetryBaseDelay
	translationRetryBaseDelay = time.Millisecond
	defer func() { translationRetryBaseDelay = oldDelay }()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		encoder := json.NewEncoder(w)
		if attempts < 3 {
			_ = encoder.Encode(ollamaGenerateResponse{Thinking: "Думаю слишком долго"})
			return
		}
		_ = encoder.Encode(ollamaGenerateResponse{Response: "Перевод.", Done: true})
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	job := &Job{ID: "auto-retry", Title: "Retry", SourceText: "Translation.", ChunkSize: 1200, TranslationChunkSize: 4000, Status: "translating", TranslationStatus: "queued", CreatedAt: time.Now(), Chunks: []*Chunk{}}
	studio := &Studio{
		jobs: map[string]*Job{job.ID: job}, order: []string{job.ID}, store: store,
		translator: &OllamaTranslator{URL: server.URL, Model: "gemma4:12b", Client: &http.Client{Timeout: time.Second}},
	}
	if err := store.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	translated, err := studio.translateWithRetries(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if translated != "Перевод." || attempts != 3 || len(job.TranslationParts) != 1 {
		t.Fatalf("translated=%q attempts=%d parts=%#v", translated, attempts, job.TranslationParts)
	}
}
