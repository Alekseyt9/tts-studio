package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex
}

type AppSettings struct {
	RefAudio             string `json:"ref_audio"`
	RefText              string `json:"ref_text"`
	SpeakerOnly          bool   `json:"speaker_only"`
	ChunkSize            int    `json:"chunk_size"`
	TranslationChunkSize int    `json:"translation_chunk_size"`
	AutoMerge            bool   `json:"auto_merge"`
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) init() error {
	_, err := s.db.Exec(`
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    source_text TEXT NOT NULL,
    translated_text TEXT NOT NULL DEFAULT '',
    chunk_size INTEGER NOT NULL,
    voice TEXT NOT NULL,
    ref_audio TEXT NOT NULL,
    ref_text TEXT NOT NULL DEFAULT '',
    speaker_only INTEGER NOT NULL,
    auto_merge INTEGER NOT NULL,
    status TEXT NOT NULL,
    progress INTEGER NOT NULL,
    translation_status TEXT NOT NULL,
    translation_progress INTEGER NOT NULL,
    translation_chunk INTEGER NOT NULL,
    translation_chunks INTEGER NOT NULL,
    translated_characters INTEGER NOT NULL,
    translation_url TEXT NOT NULL DEFAULT '',
	translation_attempt INTEGER NOT NULL DEFAULT 0,
	error_message TEXT NOT NULL DEFAULT '',
	translation_chunk_size INTEGER NOT NULL DEFAULT 9000,
    created_at TEXT NOT NULL,
    merged_url TEXT NOT NULL DEFAULT '',
    merged_path TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS chunks (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    id INTEGER NOT NULL,
    start_pos INTEGER NOT NULL,
    end_pos INTEGER NOT NULL,
    characters INTEGER NOT NULL,
    text TEXT NOT NULL,
    status TEXT NOT NULL,
    progress INTEGER NOT NULL,
    duration REAL NOT NULL,
    audio_url TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (job_id, id)
);
CREATE TABLE IF NOT EXISTS translation_parts (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    id INTEGER NOT NULL,
    text TEXT NOT NULL,
    PRIMARY KEY (job_id, id)
);
CREATE TABLE IF NOT EXISTS app_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    ref_audio TEXT NOT NULL,
    ref_text TEXT NOT NULL DEFAULT '',
    speaker_only INTEGER NOT NULL,
    chunk_size INTEGER NOT NULL,
	translation_chunk_size INTEGER NOT NULL DEFAULT 4000,
    auto_merge INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_created_at_idx ON jobs(created_at DESC);
`)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE chunks ADD COLUMN synthesis_seconds REAL NOT NULL DEFAULT 0`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	for _, migration := range []string{
		`ALTER TABLE jobs ADD COLUMN translation_attempt INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE jobs ADD COLUMN error_message TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE jobs ADD COLUMN translation_chunk_size INTEGER NOT NULL DEFAULT 9000`,
		`ALTER TABLE app_settings ADD COLUMN translation_chunk_size INTEGER NOT NULL DEFAULT 4000`,
	} {
		if _, err := s.db.Exec(migration); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) SaveJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
INSERT INTO jobs (
    id, title, source_text, translated_text, chunk_size, voice, ref_audio,
    ref_text, speaker_only, auto_merge, status, progress, translation_status,
    translation_progress, translation_chunk, translation_chunks,
    translated_characters, translation_url, translation_attempt, error_message,
	translation_chunk_size, created_at, merged_url, merged_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title=excluded.title, source_text=excluded.source_text,
    translated_text=excluded.translated_text, chunk_size=excluded.chunk_size,
    voice=excluded.voice, ref_audio=excluded.ref_audio, ref_text=excluded.ref_text,
    speaker_only=excluded.speaker_only, auto_merge=excluded.auto_merge,
    status=excluded.status, progress=excluded.progress,
    translation_status=excluded.translation_status,
    translation_progress=excluded.translation_progress,
    translation_chunk=excluded.translation_chunk,
    translation_chunks=excluded.translation_chunks,
    translated_characters=excluded.translated_characters,
    translation_url=excluded.translation_url,
	translation_attempt=excluded.translation_attempt,
	error_message=excluded.error_message,
	translation_chunk_size=excluded.translation_chunk_size,
	created_at=excluded.created_at,
    merged_url=excluded.merged_url, merged_path=excluded.merged_path
`, job.ID, job.Title, job.SourceText, job.Text, job.ChunkSize, job.Voice,
		job.RefAudio, job.RefText, job.SpeakerOnly, job.AutoMerge, job.Status,
		job.Progress, job.TranslationStatus, job.TranslationProgress,
		job.TranslationChunk, job.TranslationChunks, job.TranslatedCharacters,
		job.TranslationURL, job.TranslationAttempt, job.ErrorMessage,
		job.TranslationChunkSize, job.CreatedAt.UTC().Format(time.RFC3339Nano),
		job.MergedURL, job.MergedPath)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE job_id = ?`, job.ID); err != nil {
		return err
	}
	for _, chunk := range job.Chunks {
		_, err := tx.Exec(`
INSERT INTO chunks (job_id, id, start_pos, end_pos, characters, text, status,
					progress, duration, audio_url, path, synthesis_seconds)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, job.ID, chunk.ID, chunk.Start, chunk.End, chunk.Characters, chunk.Text,
			chunk.Status, chunk.Progress, chunk.Duration, chunk.AudioURL, chunk.Path,
			chunk.SynthesisSeconds)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM translation_parts WHERE job_id = ?`, job.ID); err != nil {
		return err
	}
	for i, translated := range job.TranslationParts {
		if _, err := tx.Exec(`INSERT INTO translation_parts (job_id, id, text) VALUES (?, ?, ?)`, job.ID, i+1, translated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) LoadSettings() (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings := AppSettings{
		RefAudio: "reference_audio_2.wav", SpeakerOnly: true,
		ChunkSize: 1200, TranslationChunkSize: 4000, AutoMerge: true,
	}
	err := s.db.QueryRow(`
SELECT ref_audio, ref_text, speaker_only, chunk_size, translation_chunk_size, auto_merge
FROM app_settings WHERE id = 1
`).Scan(&settings.RefAudio, &settings.RefText, &settings.SpeakerOnly, &settings.ChunkSize, &settings.TranslationChunkSize, &settings.AutoMerge)
	if err == sql.ErrNoRows {
		return settings, nil
	}
	return settings, err
}

func (s *SQLiteStore) SaveSettings(settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
INSERT INTO app_settings (id, ref_audio, ref_text, speaker_only, chunk_size, translation_chunk_size, auto_merge, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    ref_audio=excluded.ref_audio, ref_text=excluded.ref_text,
    speaker_only=excluded.speaker_only, chunk_size=excluded.chunk_size,
	translation_chunk_size=excluded.translation_chunk_size,
    auto_merge=excluded.auto_merge, updated_at=excluded.updated_at
`, settings.RefAudio, settings.RefText, settings.SpeakerOnly, settings.ChunkSize, settings.TranslationChunkSize,
		settings.AutoMerge, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) LoadJobs() ([]*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
SELECT id, title, source_text, translated_text, chunk_size, voice, ref_audio,
       ref_text, speaker_only, auto_merge, status, progress, translation_status,
       translation_progress, translation_chunk, translation_chunks,
	   translated_characters, translation_url, translation_attempt, error_message,
	   translation_chunk_size, created_at, merged_url, merged_path
FROM jobs ORDER BY created_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		var created string
		if err := rows.Scan(&job.ID, &job.Title, &job.SourceText, &job.Text,
			&job.ChunkSize, &job.Voice, &job.RefAudio, &job.RefText,
			&job.SpeakerOnly, &job.AutoMerge, &job.Status, &job.Progress,
			&job.TranslationStatus, &job.TranslationProgress,
			&job.TranslationChunk, &job.TranslationChunks,
			&job.TranslatedCharacters, &job.TranslationURL,
			&job.TranslationAttempt, &job.ErrorMessage, &job.TranslationChunkSize, &created,
			&job.MergedURL, &job.MergedPath); err != nil {
			return nil, err
		}
		job.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("job %s created_at: %w", job.ID, err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, job := range jobs {
		chunkRows, err := s.db.Query(`
SELECT id, start_pos, end_pos, characters, text, status, progress, duration,
			   audio_url, path, synthesis_seconds
FROM chunks WHERE job_id = ? ORDER BY id
`, job.ID)
		if err != nil {
			return nil, err
		}
		for chunkRows.Next() {
			chunk := &Chunk{}
			if err := chunkRows.Scan(&chunk.ID, &chunk.Start, &chunk.End,
				&chunk.Characters, &chunk.Text, &chunk.Status, &chunk.Progress,
				&chunk.Duration, &chunk.AudioURL, &chunk.Path,
				&chunk.SynthesisSeconds); err != nil {
				chunkRows.Close()
				return nil, err
			}
			job.Chunks = append(job.Chunks, chunk)
		}
		if err := chunkRows.Close(); err != nil {
			return nil, err
		}
		partRows, err := s.db.Query(`SELECT text FROM translation_parts WHERE job_id = ? ORDER BY id`, job.ID)
		if err != nil {
			return nil, err
		}
		for partRows.Next() {
			var translated string
			if err := partRows.Scan(&translated); err != nil {
				partRows.Close()
				return nil, err
			}
			job.TranslationParts = append(job.TranslationParts, translated)
		}
		if err := partRows.Close(); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

func (s *Studio) persistJob(job *Job) error {
	s.mu.RLock()
	if job.Deleted {
		s.mu.RUnlock()
		return nil
	}
	snapshot := *job
	snapshot.TranslationParts = append([]string(nil), job.TranslationParts...)
	snapshot.Chunks = make([]*Chunk, 0, len(job.Chunks))
	for _, chunk := range job.Chunks {
		copy := *chunk
		snapshot.Chunks = append(snapshot.Chunks, &copy)
	}
	s.mu.RUnlock()
	return s.store.SaveJob(&snapshot)
}

func (s *Studio) recoverJob(job *Job) {
	if job.ChunkSize < 200 {
		job.ChunkSize = 1200
	}
	if job.Chunks == nil {
		job.Chunks = []*Chunk{}
	}
	if job.TranslationStatus == "ready" && job.Text == "" {
		job.TranslationStatus = "queued"
	}
	if job.TranslationStatus == "ready" && job.TranslationURL != "" {
		translationPath := filepath.Join(s.dataDir, job.ID, "translation.txt")
		if !fileExists(translationPath) {
			job.TranslationURL = ""
		}
	}

	wasPaused := job.Status == "paused" || job.Status == "pausing"
	recoverable := map[string]bool{
		"queued": true, "translating": true, "unloading_translation": true,
		"loading_tts": true, "running": true, "merging": true,
	}
	needsResume := recoverable[job.Status]
	for _, chunk := range job.Chunks {
		if chunk.Status == "ready" && fileExists(chunk.Path) {
			continue
		}
		if job.Status != "failed" {
			chunk.Status = "queued"
			chunk.Progress = 0
			chunk.ElapsedSeconds = 0
			if !fileExists(chunk.Path) {
				chunk.Path = ""
				chunk.AudioURL = ""
				chunk.Duration = 0
			}
			if !wasPaused {
				needsResume = true
			}
		}
	}
	if job.MergedPath != "" && !fileExists(job.MergedPath) {
		job.MergedPath = ""
		job.MergedURL = ""
	}
	if wasPaused {
		job.Status = "paused"
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
		}
		return
	}
	if needsResume {
		job.Status = "queued"
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
		}
	}
}
