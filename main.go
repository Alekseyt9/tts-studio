package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed all:web/dist
var webFiles embed.FS

type Chunk struct {
	ID               int     `json:"id"`
	Start            int     `json:"start"`
	End              int     `json:"end"`
	Characters       int     `json:"characters"`
	Text             string  `json:"-"`
	Status           string  `json:"status"`
	Progress         int     `json:"progress"`
	Duration         float64 `json:"duration"`
	SynthesisSeconds float64 `json:"synthesis_seconds"`
	ElapsedSeconds   float64 `json:"elapsed_seconds,omitempty"`
	AudioURL         string  `json:"audio_url,omitempty"`
	Path             string  `json:"-"`
}

type Job struct {
	ID                   string    `json:"id"`
	Title                string    `json:"title"`
	SourceText           string    `json:"-"`
	Text                 string    `json:"-"`
	ChunkSize            int       `json:"-"`
	TranslationChunkSize int       `json:"translation_chunk_size"`
	Voice                string    `json:"voice"`
	RefAudio             string    `json:"ref_audio"`
	RefText              string    `json:"ref_text,omitempty"`
	SpeakerOnly          bool      `json:"speaker_only"`
	AutoMerge            bool      `json:"auto_merge"`
	Status               string    `json:"status"`
	Progress             int       `json:"progress"`
	TranslationStatus    string    `json:"translation_status"`
	TranslationProgress  int       `json:"translation_progress"`
	TranslationChunk     int       `json:"translation_chunk"`
	TranslationChunks    int       `json:"translation_chunks"`
	TranslationAttempt   int       `json:"translation_attempt"`
	TranslatedCharacters int       `json:"translated_characters"`
	TranslationURL       string    `json:"translation_url,omitempty"`
	ErrorMessage         string    `json:"error_message,omitempty"`
	TranslationParts     []string  `json:"-"`
	CreatedAt            time.Time `json:"created_at"`
	Chunks               []*Chunk  `json:"chunks"`
	MergedURL            string    `json:"merged_url,omitempty"`
	MergedPath           string    `json:"-"`
	Deleted              bool      `json:"-"`
}

type createRequest struct {
	Text                 string `json:"text"`
	Voice                string `json:"voice"`
	RefAudio             string `json:"ref_audio"`
	RefText              string `json:"ref_text"`
	SpeakerOnly          bool   `json:"speaker_only"`
	ChunkSize            int    `json:"chunk_size"`
	TranslationChunkSize int    `json:"translation_chunk_size"`
	AutoMerge            bool   `json:"auto_merge"`
}
type Studio struct {
	mu         sync.RWMutex
	jobs       map[string]*Job
	order      []string
	queue      chan string
	queueMu    sync.Mutex
	queued     map[string]bool
	dataDir    string
	command    string
	root       string
	python     *PythonWorker
	translator *OllamaTranslator
	store      *SQLiteStore
	voices     []VoiceSample
	controlMu  sync.Mutex
	controls   map[string]context.CancelFunc
}

type OllamaTranslator struct {
	URL    string
	Model  string
	Client *http.Client
}

type ollamaGenerateRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	Stream    bool           `json:"stream"`
	Think     bool           `json:"think,omitempty"`
	KeepAlive any            `json:"keep_alive,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Thinking string `json:"thinking"`
	Done     bool   `json:"done"`
	Error    string `json:"error"`
}

var translationRetryBaseDelay = 2 * time.Second

type PythonWorker struct {
	mu        sync.Mutex
	processMu sync.Mutex
	root      string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
}

type pythonResponse struct {
	Ready      bool    `json:"ready"`
	OK         bool    `json:"ok"`
	Error      string  `json:"error"`
	Duration   float64 `json:"duration"`
	SampleRate int     `json:"sample_rate"`
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	data := env("TTS_DATA_DIR", "data")
	if err := os.MkdirAll(data, 0755); err != nil {
		log.Fatal(err)
	}
	s := &Studio{
		jobs: map[string]*Job{}, controls: map[string]context.CancelFunc{}, queued: map[string]bool{}, queue: make(chan string, 128), dataDir: data,
		command: os.Getenv("TTS_COMMAND"), root: root,
		translator: &OllamaTranslator{
			URL:    env("OLLAMA_URL", "http://127.0.0.1:11434/api/generate"),
			Model:  env("OLLAMA_MODEL", "gemma4:12b"),
			Client: &http.Client{Timeout: 30 * time.Minute},
		},
	}
	store, err := NewSQLiteStore(env("TTS_DB_PATH", filepath.Join(data, "tts-studio.db")))
	if err != nil {
		log.Fatal(err)
	}
	s.store = store
	s.voices, err = loadVoiceSamples(root)
	if err != nil {
		log.Fatal(err)
	}
	if fileExists(filepath.Join(root, "run.ps1")) && fileExists(filepath.Join(root, "tts.py")) {
		s.python = &PythonWorker{root: root}
	}
	loadedJobs, err := s.store.LoadJobs()
	if err != nil {
		log.Fatal(err)
	}
	var pending []string
	for _, job := range loadedJobs {
		s.recoverJob(job)
		s.jobs[job.ID] = job
		s.order = append(s.order, job.ID)
		if job.Status == "queued" {
			pending = append(pending, job.ID)
		}
		if err := s.persistJob(job); err != nil {
			log.Printf("persist recovered job %s: %v", job.ID, err)
		}
	}
	go s.worker()
	go func() {
		for i := len(pending) - 1; i >= 0; i-- {
			s.enqueue(pending[i])
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/shutdown", s.shutdown)
	mux.HandleFunc("/api/jobs", s.jobsHandler)
	mux.HandleFunc("/api/jobs/ready", s.readyHandler)
	mux.HandleFunc("/api/jobs/", s.jobHandler)
	mux.HandleFunc("/api/voices", s.voicesHandler)
	mux.HandleFunc("/api/settings", s.settingsHandler)
	mux.HandleFunc("/voice-samples/", s.voiceSampleHandler)
	mux.Handle("/audio/", http.StripPrefix("/audio/", http.FileServer(http.Dir(data))))
	dist, _ := fs.Sub(webFiles, "web/dist")
	mux.Handle("/", spa(dist))
	addr := env("TTS_ADDR", "127.0.0.1:8096")
	log.Printf("TTS Studio: http://%s (model installed: %v)", addr, s.python != nil || s.command != "")
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}

func (s *Studio) health(w http.ResponseWriter, _ *http.Request) {
	mode := "mock"
	if s.python != nil || s.command != "" {
		mode = "ready"
	}
	loaded := false
	if s.python != nil {
		loaded = s.python.Loaded()
	}
	writeJSON(w, map[string]any{"ok": true, "mode": mode, "model_loaded": loaded, "translation_model": s.translator.Model, "translation_context": 16384})
}

func (s *Studio) shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.python != nil {
		s.python.Shutdown()
	}
	_ = s.translator.Unload()
	_ = s.store.Close()
	writeJSON(w, map[string]any{"ok": true, "message": "model unloaded; server stopping"})
	go func() { time.Sleep(250 * time.Millisecond); os.Exit(0) }()
}
func (s *Studio) jobsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := make([]*Job, 0, len(s.order))
		for _, id := range s.order {
			out = append(out, s.jobs[id])
		}
		writeJSON(w, out)
	case http.MethodPost:
		var req createRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.Text) == "" {
			http.Error(w, "text is required", 400)
			return
		}
		if req.ChunkSize < 200 {
			req.ChunkSize = 1200
		}
		if req.TranslationChunkSize < 1000 || req.TranslationChunkSize > 9000 {
			req.TranslationChunkSize = 4000
		}
		if req.RefAudio == "" {
			req.RefAudio = filepath.Join(s.root, "reference_audio_2.wav")
		}
		if !filepath.IsAbs(req.RefAudio) {
			req.RefAudio = filepath.Join(s.root, req.RefAudio)
		}
		if !fileExists(req.RefAudio) {
			http.Error(w, "reference audio not found", 400)
			return
		}
		if !req.SpeakerOnly && strings.TrimSpace(req.RefText) == "" {
			http.Error(w, "reference transcript is required", 400)
			return
		}
		id := strconv.FormatInt(time.Now().UnixNano(), 36)
		title := titleFrom(req.Text)
		job := &Job{ID: id, Title: title, SourceText: req.Text, ChunkSize: req.ChunkSize, TranslationChunkSize: req.TranslationChunkSize, Voice: req.Voice, RefAudio: req.RefAudio, RefText: req.RefText, SpeakerOnly: req.SpeakerOnly, AutoMerge: req.AutoMerge, Status: "queued", TranslationStatus: "queued", CreatedAt: time.Now(), Chunks: []*Chunk{}}
		s.mu.Lock()
		s.jobs[id] = job
		s.order = append([]string{id}, s.order...)
		s.mu.Unlock()
		if err := s.persistJob(job); err != nil {
			s.mu.Lock()
			delete(s.jobs, id)
			s.order = s.order[1:]
			s.mu.Unlock()
			http.Error(w, "could not save job: "+err.Error(), 500)
			return
		}
		s.enqueue(id)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, job)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Studio) readyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}
	s.mu.Lock()
	kept := s.order[:0]
	var deleted []string
	for _, id := range s.order {
		if j := s.jobs[id]; j.Status == "ready" || j.Status == "failed" {
			j.Deleted = true
			delete(s.jobs, id)
			deleted = append(deleted, id)
		} else {
			kept = append(kept, id)
		}
	}
	s.order = kept
	s.mu.Unlock()
	for _, id := range deleted {
		if err := s.store.DeleteJob(id); err != nil {
			http.Error(w, "could not delete job: "+err.Error(), 500)
			return
		}
	}
	w.WriteHeader(204)
}
func (s *Studio) jobHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/"), "/")
	if len(parts) == 1 && r.Method == http.MethodDelete {
		s.deleteJob(w, parts[0])
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "pause":
			s.pauseJob(w, parts[0])
			return
		case "resume":
			s.resumeJob(w, parts[0])
			return
		case "retry":
			s.retryJob(w, parts[0])
			return
		}
	}
	if len(parts) != 2 || parts[1] != "merge" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	s.mu.RLock()
	job := s.jobs[parts[0]]
	s.mu.RUnlock()
	if job == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.merge(job); err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, job)
}

func (s *Studio) pauseJob(w http.ResponseWriter, id string) {
	s.mu.RLock()
	job := s.jobs[id]
	s.mu.RUnlock()
	if job == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	s.controlMu.Lock()
	cancel, active := s.controls[id]
	s.controlMu.Unlock()
	s.mu.Lock()
	if job.Status == "ready" || job.Status == "failed" || job.Status == "paused" || job.Status == "pausing" {
		s.mu.Unlock()
		http.Error(w, "job cannot be paused", http.StatusConflict)
		return
	}
	job.Status = "pausing"
	s.mu.Unlock()
	if active {
		cancel()
		if s.python != nil {
			s.python.Abort()
		}
	} else {
		s.markPaused(job)
	}
	if err := s.persistJob(job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, job)
}

func (s *Studio) resumeJob(w http.ResponseWriter, id string) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	if job.Status != "paused" {
		s.mu.Unlock()
		http.Error(w, "job is not paused", http.StatusConflict)
		return
	}
	job.Status = "queued"
	if job.TranslationStatus != "ready" {
		job.TranslationStatus = "queued"
	}
	s.mu.Unlock()
	if err := s.persistJob(job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.enqueue(id)
	writeJSON(w, job)
}

func (s *Studio) retryJob(w http.ResponseWriter, id string) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	if job.Status != "failed" {
		s.mu.Unlock()
		http.Error(w, "job has not failed", http.StatusConflict)
		return
	}
	job.Status = "queued"
	job.ErrorMessage = ""
	job.TranslationAttempt = 0
	if job.TranslationStatus != "ready" {
		job.TranslationStatus = "queued"
		job.TranslationChunk = len(job.TranslationParts)
		job.Text = strings.Join(job.TranslationParts, "\n\n")
		if job.TranslationChunks > 0 {
			job.TranslationProgress = job.TranslationChunk * 100 / job.TranslationChunks
		} else {
			job.TranslationProgress = 0
		}
		job.Progress = job.TranslationProgress * 40 / 100
	} else {
		job.Progress = 40
		for _, chunk := range job.Chunks {
			if chunk.Status == "failed" || chunk.Status == "running" {
				chunk.Status = "queued"
				chunk.Progress = 0
				chunk.ElapsedSeconds = 0
				chunk.SynthesisSeconds = 0
				chunk.AudioURL = ""
				chunk.Path = ""
			}
		}
	}
	s.mu.Unlock()
	if err := s.persistJob(job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.enqueue(id)
	writeJSON(w, job)
}

func (s *Studio) deleteJob(w http.ResponseWriter, id string) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	job.Deleted = true
	s.mu.Unlock()
	if err := s.store.DeleteJob(id); err != nil {
		s.mu.Lock()
		job.Deleted = false
		s.mu.Unlock()
		http.Error(w, "could not delete job: "+err.Error(), 500)
		return
	}
	s.mu.Lock()
	delete(s.jobs, id)
	kept := s.order[:0]
	for _, existing := range s.order {
		if existing != id {
			kept = append(kept, existing)
		}
	}
	s.order = kept
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Studio) worker() {
	for id := range s.queue {
		s.queueMu.Lock()
		delete(s.queued, id)
		s.queueMu.Unlock()
		s.mu.RLock()
		job := s.jobs[id]
		s.mu.RUnlock()
		if job == nil {
			continue
		}
		s.mu.RLock()
		paused := job.Status == "paused" || job.Status == "pausing"
		s.mu.RUnlock()
		if paused {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.controlMu.Lock()
		s.controls[job.ID] = cancel
		s.controlMu.Unlock()
		if s.pauseRequested(job) {
			if s.python != nil {
				s.python.Shutdown()
			}
			s.markPaused(job)
			s.finishJobControl(job.ID)
			continue
		}

		if job.TranslationStatus != "ready" || strings.TrimSpace(job.Text) == "" {
			// Only one large GPU model should be resident at a time. A TTS model
			// left over from the previous job is released before Gemma is loaded.
			if s.python != nil {
				s.python.Shutdown()
			}
			s.setJob(job, "translating")
			if s.pauseRequested(job) {
				s.markPaused(job)
				s.finishJobControl(job.ID)
				continue
			}
			translated, err := s.translateWithRetries(ctx, job)
			if s.isDeleted(job) {
				_ = s.translator.Unload()
				s.finishJobControl(job.ID)
				continue
			}
			if err != nil {
				_ = s.translator.Unload()
				if s.pauseRequested(job) {
					s.markPaused(job)
					s.finishJobControl(job.ID)
					continue
				}
				log.Printf("translation %s: %v", job.ID, err)
				s.failTranslation(job, err)
				s.finishJobControl(job.ID)
				continue
			}

			s.mu.Lock()
			job.Text = translated
			job.TranslatedCharacters = utf8.RuneCountInString(translated)
			job.TranslationStatus = "ready"
			job.TranslationAttempt = 0
			job.ErrorMessage = ""
			job.TranslationProgress = 100
			job.TranslationChunk = job.TranslationChunks
			job.Status = "unloading_translation"
			job.Progress = 42
			s.mu.Unlock()
			if err := s.saveTranslation(job); err != nil {
				_ = s.translator.Unload()
				log.Printf("save translation %s: %v", job.ID, err)
				s.setJob(job, "failed")
				s.finishJobControl(job.ID)
				continue
			}
		} else {
			s.setJob(job, "unloading_translation")
			if job.TranslationURL == "" {
				if err := s.saveTranslation(job); err != nil {
					log.Printf("restore translation file %s: %v", job.ID, err)
					s.setJob(job, "failed")
					s.finishJobControl(job.ID)
					continue
				}
			}
		}
		if err := s.translator.Unload(); err != nil {
			if s.pauseRequested(job) {
				s.markPaused(job)
				s.finishJobControl(job.ID)
				continue
			}
			log.Printf("unload translation model %s: %v", job.ID, err)
			s.setJob(job, "failed")
			s.finishJobControl(job.ID)
			continue
		}
		if s.isDeleted(job) {
			s.finishJobControl(job.ID)
			continue
		}
		if s.pauseRequested(job) {
			if s.python != nil {
				s.python.Shutdown()
			}
			s.markPaused(job)
			s.finishJobControl(job.ID)
			continue
		}

		s.setJob(job, "loading_tts")
		if s.python != nil {
			if err := s.python.Preload(); err != nil {
				if s.pauseRequested(job) {
					s.markPaused(job)
					s.finishJobControl(job.ID)
					continue
				}
				log.Printf("load TTS model %s: %v", job.ID, err)
				s.setJob(job, "failed")
				s.finishJobControl(job.ID)
				continue
			}
		}
		if s.isDeleted(job) {
			s.finishJobControl(job.ID)
			continue
		}
		if s.pauseRequested(job) {
			if s.python != nil {
				s.python.Shutdown()
			}
			s.markPaused(job)
			s.finishJobControl(job.ID)
			continue
		}
		s.mu.Lock()
		if len(job.Chunks) == 0 {
			job.Chunks = splitText(job.Text, job.ChunkSize)
		}
		job.Status = "running"
		job.Progress = 50
		s.mu.Unlock()
		if err := s.persistJob(job); err != nil {
			log.Printf("persist TTS chunks %s: %v", job.ID, err)
		}

		failed := false
		for _, c := range job.Chunks {
			if s.isDeleted(job) {
				failed = true
				break
			}
			if c.Status == "ready" && fileExists(c.Path) {
				continue
			}
			s.updateChunk(job, c, "running", 5)
			if err := s.runSynthesis(ctx, job, c); err != nil {
				if s.pauseRequested(job) {
					failed = false
					break
				}
				log.Printf("chunk %s/%d: %v", job.ID, c.ID, err)
				s.updateChunk(job, c, "failed", 0)
				failed = true
				break
			}
			s.updateChunk(job, c, "ready", 100)
		}
		if s.isDeleted(job) {
			s.finishJobControl(job.ID)
			continue
		}
		if s.pauseRequested(job) {
			s.markPaused(job)
			s.finishJobControl(job.ID)
			continue
		}
		if failed {
			s.setJob(job, "failed")
			s.finishJobControl(job.ID)
			continue
		}
		s.setJob(job, "ready")
		if job.AutoMerge {
			if err := s.merge(job); err != nil {
				log.Printf("merge %s: %v", job.ID, err)
			}
		}
		s.finishJobControl(job.ID)
	}
}

func (s *Studio) enqueue(id string) {
	s.queueMu.Lock()
	if s.queued[id] {
		s.queueMu.Unlock()
		return
	}
	s.queued[id] = true
	s.queueMu.Unlock()
	s.queue <- id
}

func (s *Studio) finishJobControl(id string) {
	s.controlMu.Lock()
	if cancel := s.controls[id]; cancel != nil {
		cancel()
	}
	delete(s.controls, id)
	s.controlMu.Unlock()
}

func (s *Studio) pauseRequested(job *Job) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return job.Status == "pausing" || job.Status == "paused"
}

func (s *Studio) markPaused(job *Job) {
	s.mu.Lock()
	job.Status = "paused"
	if job.TranslationStatus != "ready" {
		job.TranslationStatus = "queued"
		job.TranslationChunk = len(job.TranslationParts)
		if job.TranslationChunks > 0 {
			job.TranslationProgress = job.TranslationChunk * 100 / job.TranslationChunks
		} else {
			job.TranslationProgress = 0
		}
		job.Progress = job.TranslationProgress * 40 / 100
	}
	for _, chunk := range job.Chunks {
		if chunk.Status == "running" {
			chunk.Status = "queued"
			chunk.Progress = 0
			chunk.ElapsedSeconds = 0
			chunk.SynthesisSeconds = 0
			chunk.Duration = 0
			chunk.AudioURL = ""
			chunk.Path = ""
		}
	}
	s.mu.Unlock()
	if err := s.persistJob(job); err != nil {
		log.Printf("persist paused job %s: %v", job.ID, err)
	}
}

func (s *Studio) runSynthesis(ctx context.Context, job *Job, chunk *Chunk) error {
	started := time.Now()
	result := make(chan error, 1)
	go func() {
		result <- s.synthesize(ctx, job, chunk)
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if s.python != nil {
				s.python.Abort()
			}
			<-result
			return ctx.Err()
		case err := <-result:
			elapsed := time.Since(started).Seconds()
			s.mu.Lock()
			chunk.ElapsedSeconds = elapsed
			chunk.SynthesisSeconds = elapsed
			s.mu.Unlock()
			return err
		case <-ticker.C:
			s.updateSynthesisRuntime(job, chunk, time.Since(started).Seconds())
		}
	}
}

func (s *Studio) updateSynthesisRuntime(job *Job, chunk *Chunk, elapsed float64) {
	s.mu.Lock()
	if job.Deleted || chunk.Status != "running" {
		s.mu.Unlock()
		return
	}
	secondsPerCharacter := 0.08
	totalSeconds := 0.0
	totalCharacters := 0
	for _, ready := range job.Chunks {
		if ready.Status == "ready" && ready.SynthesisSeconds > 0 {
			totalSeconds += ready.SynthesisSeconds
			totalCharacters += ready.Characters
		}
	}
	if totalCharacters > 0 {
		secondsPerCharacter = totalSeconds / float64(totalCharacters)
	}
	estimated := math.Max(5, float64(chunk.Characters)*secondsPerCharacter)
	progress := int(elapsed / estimated * 100)
	if progress < 5 {
		progress = 5
	}
	if progress > 95 {
		progress = 95
	}
	if progress > chunk.Progress {
		chunk.Progress = progress
	}
	chunk.ElapsedSeconds = elapsed
	s.mu.Unlock()
}

func (s *Studio) saveTranslation(job *Job) error {
	dir := filepath.Join(s.dataDir, job.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "translation.txt"), []byte(job.Text), 0644); err != nil {
		return err
	}
	s.mu.Lock()
	job.TranslationURL = "/audio/" + filepath.ToSlash(filepath.Join(job.ID, "translation.txt"))
	s.mu.Unlock()
	return s.persistJob(job)
}

func (s *Studio) setTranslationProgress(job *Job, done, total int, current float64) {
	progress := 0
	if total > 0 {
		progress = int((float64(done) + current) / float64(total) * 100)
		if progress > 99 && done < total {
			progress = 99
		}
	}
	s.mu.Lock()
	if job.Deleted || (job.TranslationProgress == progress && job.TranslationChunk == done && job.TranslationChunks == total) {
		s.mu.Unlock()
		return
	}
	job.TranslationProgress = progress
	job.TranslationChunk = done
	job.TranslationChunks = total
	job.Progress = progress * 40 / 100
	s.mu.Unlock()
}

func (s *Studio) saveTranslationPart(job *Job, index, total int, translated string) error {
	s.mu.Lock()
	if job.Deleted {
		s.mu.Unlock()
		return nil
	}
	if index <= len(job.TranslationParts) {
		job.TranslationParts[index-1] = translated
	} else if index == len(job.TranslationParts)+1 {
		job.TranslationParts = append(job.TranslationParts, translated)
	} else {
		s.mu.Unlock()
		return fmt.Errorf("translation part %d arrived out of order", index)
	}
	job.Text = strings.Join(job.TranslationParts, "\n\n")
	job.TranslationChunk = len(job.TranslationParts)
	job.TranslationChunks = total
	job.TranslationProgress = job.TranslationChunk * 100 / total
	job.Progress = job.TranslationProgress * 40 / 100
	s.mu.Unlock()
	return s.persistJob(job)
}

func (s *Studio) translateWithRetries(ctx context.Context, job *Job) (string, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		s.mu.Lock()
		job.TranslationAttempt = attempt
		job.ErrorMessage = ""
		completedParts := append([]string(nil), job.TranslationParts...)
		chunkSize := job.TranslationChunkSize
		s.mu.Unlock()
		if chunkSize <= 0 {
			chunkSize = 9000 // Jobs created before this setting was introduced.
		}
		if err := s.persistJob(job); err != nil {
			log.Printf("persist translation attempt %s: %v", job.ID, err)
		}
		translated, err := s.translator.Translate(
			ctx, job.SourceText, chunkSize, completedParts,
			func(done, total int, current float64) { s.setTranslationProgress(job, done, total, current) },
			func(index, total int, translatedPart string) error {
				return s.saveTranslationPart(job, index, total, translatedPart)
			},
		)
		if err == nil {
			return translated, nil
		}
		lastErr = err
		s.mu.Lock()
		job.ErrorMessage = err.Error()
		s.mu.Unlock()
		_ = s.persistJob(job)
		if ctx.Err() != nil || s.pauseRequested(job) || s.isDeleted(job) {
			return "", err
		}
		if attempt < maxAttempts {
			log.Printf("translation %s attempt %d/%d: %v; retrying unfinished chunk", job.ID, attempt, maxAttempts, err)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * translationRetryBaseDelay):
			}
		}
	}
	return "", fmt.Errorf("translation failed after %d attempts: %w", maxAttempts, lastErr)
}

func (s *Studio) failTranslation(job *Job, cause error) {
	s.mu.Lock()
	job.TranslationStatus = "failed"
	job.Status = "failed"
	if cause != nil {
		job.ErrorMessage = cause.Error()
	}
	s.mu.Unlock()
	if err := s.persistJob(job); err != nil {
		log.Printf("persist translation failure %s: %v", job.ID, err)
	}
}

func (s *Studio) isDeleted(job *Job) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return job.Deleted
}

func (o *OllamaTranslator) Translate(ctx context.Context, text string, chunkSize int, completed []string, progress func(done, total int, current float64), savePart func(index, total int, translated string) error) (string, error) {
	// Translation chunks are larger than TTS chunks, but the conservative
	// default leaves most of the 16K context available for Gemma's thinking.
	parts := splitForTranslation(text, chunkSize)
	if len(completed) > len(parts) {
		completed = nil
	}
	translations := append([]string(nil), completed...)
	progress(len(translations), len(parts), 0)
	for i := len(translations); i < len(parts); i++ {
		part := parts[i]
		prompt := fmt.Sprintf(`Translate the following English text into natural, literary Russian.
Preserve paragraphs, meaning, names, numbers, punctuation, and tone. Do not summarize.
Return only the Russian translation, without comments, labels, or Markdown fences.
This is part %d of %d of one document.

<source>
%s
</source>`, i+1, len(parts), part)
		request := ollamaGenerateRequest{
			Model: o.Model, Prompt: prompt, Stream: true, Think: true, KeepAlive: "10m",
			Options: map[string]any{"num_ctx": 16384, "temperature": 0.1},
		}
		translated, err := o.stream(ctx, request, utf8.RuneCountInString(part), func(current float64) {
			progress(i, len(parts), current)
		})
		if err != nil {
			return "", fmt.Errorf("part %d of %d: %w", i+1, len(parts), err)
		}
		translated = strings.TrimSpace(translated)
		if translated == "" {
			return "", fmt.Errorf("part %d of %d: Ollama returned an empty translation", i+1, len(parts))
		}
		translations = append(translations, translated)
		if err := savePart(i+1, len(parts), translated); err != nil {
			return "", err
		}
		progress(i+1, len(parts), 0)
	}
	return strings.Join(translations, "\n\n"), nil
}

func (o *OllamaTranslator) stream(ctx context.Context, payload ollamaGenerateRequest, sourceRunes int, progress func(float64)) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := o.Client.Do(request)
	if err != nil {
		return "", fmt.Errorf("Ollama is unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("Ollama HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}

	var translated strings.Builder
	thinkingRunes := 0
	translatedRunes := 0
	lastEstimate := 0.0
	lastReport := time.Time{}
	done := false
	decoder := json.NewDecoder(response.Body)
	for {
		var chunk ollamaGenerateResponse
		if err := decoder.Decode(&chunk); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return "", fmt.Errorf("invalid Ollama stream: %w", err)
		}
		if chunk.Error != "" {
			return "", errors.New(chunk.Error)
		}
		translated.WriteString(chunk.Response)
		translatedRunes += utf8.RuneCountInString(chunk.Response)
		thinkingRunes += utf8.RuneCountInString(chunk.Thinking)

		estimate := 0.0
		if translatedRunes > 0 {
			target := math.Max(1, float64(sourceRunes)*1.05)
			estimate = math.Min(0.97, float64(translatedRunes)/target)
		} else if thinkingRunes > 0 {
			target := math.Max(1, float64(sourceRunes)*2)
			estimate = math.Min(0.15, float64(thinkingRunes)/target*0.15)
		}
		if estimate < lastEstimate {
			estimate = lastEstimate
		}
		if chunk.Done {
			estimate = 1
			done = true
		}
		if estimate == 1 || int(estimate*100) > int(lastEstimate*100) || time.Since(lastReport) >= 500*time.Millisecond {
			progress(estimate)
			lastReport = time.Now()
		}
		lastEstimate = estimate
		if chunk.Done {
			break
		}
	}
	if !done {
		return "", errors.New("Ollama stream ended before completion")
	}
	return translated.String(), nil
}

func (o *OllamaTranslator) Unload() error {
	request := ollamaGenerateRequest{Model: o.Model, Prompt: "", Stream: false, KeepAlive: 0}
	var response ollamaGenerateResponse
	return o.call(request, &response)
}

func (o *OllamaTranslator) call(payload ollamaGenerateRequest, result *ollamaGenerateResponse) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, o.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := o.Client.Do(request)
	if err != nil {
		return fmt.Errorf("Ollama is unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Ollama HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("invalid Ollama response: %w", err)
	}
	if result.Error != "" {
		return errors.New(result.Error)
	}
	return nil
}

func (s *Studio) synthesize(ctx context.Context, job *Job, c *Chunk) error {
	dir := filepath.Join(s.dataDir, job.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	out := filepath.Join(dir, fmt.Sprintf("chunk-%03d.wav", c.ID))
	input := filepath.Join(dir, fmt.Sprintf("chunk-%03d.txt", c.ID))
	if err := os.WriteFile(input, []byte(c.Text), 0644); err != nil {
		return err
	}
	if s.python != nil {
		response, err := s.python.Generate(map[string]any{
			"text": c.Text, "ref_audio": job.RefAudio, "ref_text": job.RefText,
			"output": out, "language": "Russian", "speaker_only": job.SpeakerOnly,
		})
		if err != nil {
			return err
		}
		s.mu.Lock()
		c.Duration = response.Duration
		s.mu.Unlock()
	} else if s.command == "" {
		select {
		case <-time.After(450 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		duration := math.Max(1, float64(c.Characters)/14)
		if err := writeSilenceWAV(out, duration); err != nil {
			return err
		}
		s.mu.Lock()
		c.Duration = duration
		s.mu.Unlock()
	} else {
		cmdline := strings.NewReplacer("{text_file}", shellQuote(input), "{output_file}", shellQuote(out), "{voice}", shellQuote(job.Voice)).Replace(s.command)
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", cmdline)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", cmdline)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("tts command: %w: %s", err, stderr.String())
		}
	}
	if _, err := os.Stat(out); err != nil {
		return fmt.Errorf("model did not create %s", out)
	}
	s.mu.Lock()
	c.Path = out
	c.AudioURL = "/audio/" + filepath.ToSlash(filepath.Join(job.ID, filepath.Base(out)))
	s.mu.Unlock()
	return nil
}

func (p *PythonWorker) Generate(request map[string]any) (pythonResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		if err := p.start(); err != nil {
			return pythonResponse{}, err
		}
	}
	payload, _ := json.Marshal(request)
	if _, err := p.stdin.Write(append(payload, '\n')); err != nil {
		p.stop()
		return pythonResponse{}, err
	}
	for p.stdout.Scan() {
		line := append([]byte(nil), p.stdout.Bytes()...)
		var response pythonResponse
		if json.Unmarshal(line, &response) != nil {
			log.Printf("TTS: %s", line)
			continue
		}
		if !response.OK {
			return response, errors.New(response.Error)
		}
		return response, nil
	}
	p.stop()
	return pythonResponse{}, errors.New("TTS worker stopped unexpectedly")
}

func (p *PythonWorker) Preload() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil {
		return nil
	}
	return p.start()
}

func (p *PythonWorker) start() error {
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(p.root, "run.ps1"), "--server")
	cmd.Dir = p.root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = log.Writer()
	if err := cmd.Start(); err != nil {
		return err
	}
	p.processMu.Lock()
	p.cmd, p.stdin, p.stdout = cmd, stdin, bufio.NewScanner(stdout)
	p.processMu.Unlock()
	p.stdout.Buffer(make([]byte, 64*1024), 1024*1024)
	for p.stdout.Scan() {
		var ready pythonResponse
		if json.Unmarshal(p.stdout.Bytes(), &ready) == nil && ready.Ready {
			return nil
		}
	}
	p.stop()
	return errors.New("TTS model failed to initialize")
}

func (p *PythonWorker) stop() {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		killProcessTree(p.cmd.Process)
	}
	p.cmd, p.stdin, p.stdout = nil, nil, nil
}

func (p *PythonWorker) Abort() {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		killProcessTree(p.cmd.Process)
	}
}

func killProcessTree(process *os.Process) {
	if process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
		return
	}
	_ = process.Kill()
}

func (p *PythonWorker) Loaded() bool {
	if !p.mu.TryLock() {
		return true
	}
	defer p.mu.Unlock()
	return p.cmd != nil && p.cmd.Process != nil
}

func (p *PythonWorker) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stop()
}

func (s *Studio) merge(job *Job) error {
	s.mu.Lock()
	for _, c := range job.Chunks {
		if c.Status != "ready" {
			s.mu.Unlock()
			return errors.New("all chunks must be ready")
		}
	}
	job.Status = "merging"
	s.mu.Unlock()
	if err := s.persistJob(job); err != nil {
		return err
	}
	dir := filepath.Join(s.dataDir, job.ID)
	out := filepath.Join(dir, "result.wav")
	if err := mergeWAV(job.Chunks, out); err != nil {
		s.setJob(job, "failed")
		return err
	}
	s.mu.Lock()
	job.MergedPath = out
	job.MergedURL = "/audio/" + filepath.ToSlash(filepath.Join(job.ID, "result.wav"))
	job.Status = "ready"
	job.Progress = 100
	s.mu.Unlock()
	return s.persistJob(job)
}
func (s *Studio) setJob(j *Job, status string) {
	s.mu.Lock()
	if j.Deleted || j.Status == "paused" || j.Status == "pausing" {
		s.mu.Unlock()
		return
	}
	j.Status = status
	switch status {
	case "unloading_translation":
		j.Progress = 42
	case "loading_tts":
		j.Progress = 46
	case "running":
		j.Progress = 50
	case "ready":
		j.Progress = 100
	}
	s.mu.Unlock()
	if err := s.persistJob(j); err != nil {
		log.Printf("persist job %s: %v", j.ID, err)
	}
}
func (s *Studio) updateChunk(j *Job, c *Chunk, status string, progress int) {
	s.mu.Lock()
	if j.Deleted || j.Status == "paused" || j.Status == "pausing" {
		s.mu.Unlock()
		return
	}
	c.Status = status
	c.Progress = progress
	done := 0
	for _, x := range j.Chunks {
		if x.Status == "ready" {
			done++
		}
	}
	if len(j.Chunks) > 0 {
		j.Progress = 50 + int(float64(done)/float64(len(j.Chunks))*50)
	}
	s.mu.Unlock()
	if err := s.persistJob(j); err != nil {
		log.Printf("persist chunk %s/%d: %v", j.ID, c.ID, err)
	}
}

func splitForTranslation(text string, max int) []string {
	sentences := splitWholeSentences(strings.TrimSpace(text))
	var chunks []string
	var current strings.Builder
	currentRunes := 0
	for _, sentence := range sentences {
		sentenceRunes := utf8.RuneCountInString(sentence)
		if currentRunes > 0 && currentRunes+sentenceRunes > max {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
			currentRunes = 0
		}
		current.WriteString(sentence)
		currentRunes += sentenceRunes
	}
	if strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}

func splitWholeSentences(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	abbreviations := map[string]bool{
		"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true,
		"sr": true, "jr": true, "st": true, "vs": true, "etc": true,
		"e.g": true, "i.e": true,
	}
	var sentences []string
	start := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' && i+1 < len(runes) && runes[i+1] == '\n' {
			end := i + 2
			for end < len(runes) && runes[end] == '\n' {
				end++
			}
			sentences = appendSentence(sentences, runes[start:end])
			start, i = end, end-1
			continue
		}
		if !strings.ContainsRune(".!?", runes[i]) {
			continue
		}
		if runes[i] == '.' && !isSentencePeriod(runes, i, abbreviations) {
			continue
		}
		end := i + 1
		for end < len(runes) && strings.ContainsRune(".!?", runes[end]) {
			end++
		}
		for end < len(runes) && strings.ContainsRune("\"'”’)]}", runes[end]) {
			end++
		}
		if end < len(runes) && !isSentenceSpace(runes[end]) {
			continue
		}
		for end < len(runes) && isSentenceSpace(runes[end]) {
			end++
		}
		sentences = appendSentence(sentences, runes[start:end])
		start, i = end, end-1
	}
	if start < len(runes) {
		sentences = appendSentence(sentences, runes[start:])
	}
	return sentences
}

func appendSentence(sentences []string, runes []rune) []string {
	if len(runes) > 0 && strings.TrimSpace(string(runes)) != "" {
		return append(sentences, string(runes))
	}
	return sentences
}

func isSentenceSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

func isSentencePeriod(runes []rune, at int, abbreviations map[string]bool) bool {
	if at > 0 && at+1 < len(runes) && runes[at-1] >= '0' && runes[at-1] <= '9' && runes[at+1] >= '0' && runes[at+1] <= '9' {
		return false
	}
	start := at - 1
	for start >= 0 && ((runes[start] >= 'A' && runes[start] <= 'Z') || (runes[start] >= 'a' && runes[start] <= 'z') || runes[start] == '.') {
		start--
	}
	word := strings.ToLower(strings.Trim(string(runes[start+1:at]), "."))
	if abbreviations[word] || (len([]rune(word)) == 1 && word != "a" && word != "i") {
		return false
	}
	return true
}

func splitText(text string, max int) []*Chunk {
	runes := []rune(strings.TrimSpace(text))
	var out []*Chunk
	start := 0
	for start < len(runes) {
		end := start + max
		if end > len(runes) {
			end = len(runes)
		} else {
			floor := start + max/2
			for i := end; i > floor; i-- {
				if strings.ContainsRune(".!?\n", runes[i-1]) {
					end = i
					break
				}
			}
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			out = append(out, &Chunk{ID: len(out) + 1, Start: start + 1, End: end, Characters: utf8.RuneCountInString(part), Text: part, Status: "queued"})
		}
		start = end
	}
	return out
}
func titleFrom(s string) string {
	line := strings.TrimSpace(strings.Split(s, "\n")[0])
	r := []rune(line)
	if len(r) > 38 {
		line = string(r[:38]) + "…"
	}
	return line
}

func writeSilenceWAV(path string, seconds float64) error {
	rate := 16000
	n := int(seconds * float64(rate))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data := uint32(n * 2)
	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(36)+data)
	f.Write([]byte("WAVEfmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint32(rate))
	binary.Write(f, binary.LittleEndian, uint32(rate*2))
	binary.Write(f, binary.LittleEndian, uint16(2))
	binary.Write(f, binary.LittleEndian, uint16(16))
	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, data)
	zero := make([]byte, 8192)
	for left := int(data); left > 0; {
		k := left
		if k > len(zero) {
			k = len(zero)
		}
		f.Write(zero[:k])
		left -= k
	}
	return nil
}
func mergeWAV(chunks []*Chunk, out string) error {
	var pcm []byte
	var header []byte
	for _, c := range chunks {
		b, err := os.ReadFile(c.Path)
		if err != nil {
			return err
		}
		if len(b) < 44 {
			return errors.New("invalid wav")
		}
		if header == nil {
			header = append([]byte(nil), b[:44]...)
		}
		pcm = append(pcm, b[44:]...)
	}
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(pcm)))
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(pcm)))
	return os.WriteFile(out, append(header, pcm...), 0644)
}
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
func spa(root fs.FS) http.Handler {
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(root, p); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "frontend missing", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
