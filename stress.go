package main

import (
	"regexp"
	"strings"
)

const russianStressInstruction = `For Russian pronunciation, place a combining acute accent U+0301 immediately AFTER the stressed vowel in each Russian word with two or more syllables, except words containing ё. Select stress according to the meaning and context. Examples: castle = за́мок; door lock = замо́к; cotton = хло́пок; clap = хлопо́к. Preserve the letter ё. Never mark one-syllable words or words with ё. Use only U+0301, never ˊ, +, apostrophes or uppercase vowels. Return only the translation with these marks, without explanations.

`

func usesAutomaticTranscript(engine string) bool {
	return strings.HasPrefix(engine, "omni") || engine == "faster_stress"
}

func (o *OllamaTranslator) stressPrompt(prompt string) string {
	if o.AddStress {
		return russianStressInstruction + prompt
	}
	return prompt
}

var russianStressWord = regexp.MustCompile(`[А-Яа-яЁё\x{0301}]+`)

// Remove mechanically invalid marks without changing spelling or guessing stress.
func cleanRussianStress(text string) string {
	return russianStressWord.ReplaceAllStringFunc(text, func(word string) string {
		vowels, marks := 0, 0
		var prev rune
		invalid := false
		for _, r := range word {
			if strings.ContainsRune("аеёиоуыэюяАЕЁИОУЫЭЮЯ", r) {
				vowels++
			}
			if r == '\u0301' {
				marks++
				invalid = invalid || !strings.ContainsRune("аеёиоуыэюяАЕЁИОУЫЭЮЯ", prev)
			}
			prev = r
		}
		if vowels < 2 || strings.ContainsAny(word, "ёЁ") || invalid || marks > 1 {
			return strings.ReplaceAll(word, "\u0301", "")
		}
		return word
	})
}
