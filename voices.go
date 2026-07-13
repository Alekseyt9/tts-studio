package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type VoiceSample struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	RefAudio    string  `json:"ref_audio"`
	Transcript  string  `json:"transcript,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
	AudioURL    string  `json:"audio_url"`
	Path        string  `json:"-"`
}

func loadVoiceSamples(root string) ([]VoiceSample, error) {
	data, err := os.ReadFile(filepath.Join(root, "voices.json"))
	if err != nil {
		return nil, err
	}
	var voices []VoiceSample
	if err := json.Unmarshal(data, &voices); err != nil {
		return nil, err
	}
	available := voices[:0]
	for i := range voices {
		voice := &voices[i]
		if voice.ID == "" || strings.ContainsAny(voice.ID, `/\`) || voice.RefAudio == "" {
			continue
		}
		voice.Path = voice.RefAudio
		if !filepath.IsAbs(voice.Path) {
			voice.Path = filepath.Join(root, voice.Path)
		}
		if !fileExists(voice.Path) {
			continue
		}
		voice.AudioURL = "/voice-samples/" + voice.ID
		available = append(available, *voice)
	}
	return available, nil
}

func (s *Studio) voicesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.voices)
}

func (s *Studio) settingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := s.store.LoadSettings()
		if err != nil {
			http.Error(w, "could not load settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		settings = s.normalizeSettings(settings)
		writeJSON(w, settings)
	case http.MethodPut:
		var settings AppSettings
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		if err := decoder.Decode(&settings); err != nil {
			http.Error(w, "invalid settings", http.StatusBadRequest)
			return
		}
		if settings.ChunkSize != 600 && settings.ChunkSize != 1200 && settings.ChunkSize != 2400 {
			http.Error(w, "unsupported chunk size", http.StatusBadRequest)
			return
		}
		if settings.TranslationChunkSize != 2500 && settings.TranslationChunkSize != 4000 && settings.TranslationChunkSize != 6000 {
			http.Error(w, "unsupported translation chunk size", http.StatusBadRequest)
			return
		}
		if len(settings.RefText) > 10000 {
			http.Error(w, "reference transcript is too long", http.StatusBadRequest)
			return
		}
		if !s.hasVoice(settings.RefAudio) {
			http.Error(w, "voice sample not found", http.StatusBadRequest)
			return
		}
		settings.RefText = strings.TrimSpace(settings.RefText)
		if err := s.store.SaveSettings(settings); err != nil {
			http.Error(w, "could not save settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, settings)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Studio) normalizeSettings(settings AppSettings) AppSettings {
	if settings.ChunkSize != 600 && settings.ChunkSize != 1200 && settings.ChunkSize != 2400 {
		settings.ChunkSize = 1200
	}
	if settings.TranslationChunkSize != 2500 && settings.TranslationChunkSize != 4000 && settings.TranslationChunkSize != 6000 {
		settings.TranslationChunkSize = 4000
	}
	if !s.hasVoice(settings.RefAudio) && len(s.voices) > 0 {
		settings.RefAudio = s.voices[0].RefAudio
	}
	return settings
}

func (s *Studio) hasVoice(refAudio string) bool {
	for _, voice := range s.voices {
		if voice.RefAudio == refAudio {
			return true
		}
	}
	return false
}

func (s *Studio) voiceSampleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/voice-samples/"), "/")
	for _, voice := range s.voices {
		if voice.ID == id {
			http.ServeFile(w, r, voice.Path)
			return
		}
	}
	http.NotFound(w, r)
}
