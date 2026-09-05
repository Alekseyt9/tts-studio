package main

import (
	"fmt"
	"net/http"
)

const defaultTTSModel = "faster"
const defaultTranslationModel = "gemma4_direct"

// IDs are persisted per job: changing form settings must never change queued audio.
type ModelOption struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Seconds float64 `json:"seconds"`
	Speedup float64 `json:"speedup"`
	LoadSeconds float64 `json:"load_seconds,omitempty"`
	Note string `json:"note,omitempty"`
	Model string `json:"-"`
	Think any `json:"-"`
}

var ttsModels = []ModelOption{
	{ID:"faster", Name:"Faster Qwen3-TTS", Seconds:23.5583, Speedup:8.519, Note:"Качество подтверждено прослушиванием"},
	{ID:"omni32", Name:"OmniVoice · 32 шага", Seconds:16.1103, Speedup:12.458, LoadSeconds:7.996, Note:"Более медленный темп речи; транскрипт образца распознаётся автоматически"},
	{ID:"omni16", Name:"OmniVoice · 16 шагов", Seconds:8.4254, Speedup:23.82, LoadSeconds:7.022, Note:"Самый быстрый в тесте; транскрипт образца распознаётся автоматически"},
	{ID:"qwen", Name:"Qwen3-TTS", Seconds:200.696, Speedup:1, LoadSeconds:9.244},
}

var translationModels = []ModelOption{
	{ID:"gemma4_direct",Name:"Gemma 4 12B · без рассуждений",Model:"gemma4:12b",Think:false,Seconds:17.6228,Speedup:8.775},
	{ID:"gemma4_think",Name:"Gemma 4 12B · с рассуждениями",Model:"gemma4:12b",Think:true,Seconds:154.644,Speedup:1,Note:"В тесте потребовался повтор после пустого ответа"},
	{ID:"hy_mt2_1_8b",Name:"Hy-MT2 1.8B Q8",Model:"hy-mt2:1.8b-q8_0",Seconds:7.1192,Speedup:21.723,Note:"Быстро, но заметные смысловые ошибки в художественном тексте"},
}

func findModel(options []ModelOption,id string) (ModelOption,bool) {
	for _, option := range options { if option.ID == id { return option,true } }
	return ModelOption{},false
}

func normalizeModelIDs(tts,translation *string) error {
	if *tts == "" { *tts = defaultTTSModel }
	if *translation == "" { *translation = defaultTranslationModel }
	if _,ok := findModel(ttsModels,*tts); !ok { return fmt.Errorf("unknown TTS model: %s",*tts) }
	if _,ok := findModel(translationModels,*translation); !ok { return fmt.Errorf("unknown translation model: %s",*translation) }
	return nil
}

func (s *Studio) modelsHandler(w http.ResponseWriter,r *http.Request) {
	if r.Method != http.MethodGet { http.Error(w,"method not allowed",405); return }
	writeJSON(w,map[string]any{"tts":ttsModels,"translation":translationModels,"tts_characters":1095,"translation_characters":3904,"context":16384})
}

func (s *Studio) translatorForJob(job *Job) *OllamaTranslator {
	translator := *s.translator
	// Empty IDs belong to legacy in-memory jobs and retain the configured translator.
	if option,ok := findModel(translationModels,job.TranslationModel); ok {
		translator.Model = option.Model
		translator.Profile = option.ID
	}
	return &translator
}

func (o *OllamaTranslator) translationPrompt(source,legacy string) (string,any) {
	if option,ok := findModel(translationModels,o.Profile); ok {
		switch option.ID {
		case "hy_mt2_1_8b", "hy_mt2_7b":
			return "Please translate the following text into Russian. Note that the translation style must strictly conform to [natural literary prose]. Preserve meaning, paragraphs, names, numbers and tone. Do not summarize. Output only the translation:\n"+source,nil
		case "translategemma_4b", "translategemma_12b":
			return "You are a professional English (en) to Russian (ru) translator. Your goal is to accurately convey the meaning and nuances of the original English text while adhering to Russian grammar, vocabulary, and cultural sensitivities.\nProduce only the Russian translation, without any additional explanations or commentary. Please translate the following English text into Russian:\n\n\n"+source,nil
		default:
			return legacy,option.Think
		}
	}
	return legacy,true
}
