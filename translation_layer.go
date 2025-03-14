/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the Overworked License (OWL) v2.0
 */

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// TranslationLayer handles the two-step process of generating alt-text in English
// and then translating it to the target language
type TranslationLayer struct {
	provider LLMProvider
}

// NewTranslationLayer creates a new translation layer for the given provider
func NewTranslationLayer(provider LLMProvider) *TranslationLayer {
	return &TranslationLayer{
		provider: provider,
	}
}

// GenerateAndTranslateAltText first generates alt-text in English, then translates to target language
func (t *TranslationLayer) GenerateAndTranslateAltText(prompt string, imageData []byte, format string, targetLanguageCode string) (string, error) {
	englishPrompt := getLocalizedString("en", "generateAltText", "prompt")

	englishAltText, err := t.provider.GenerateAltText(englishPrompt, imageData, format, "en")
	if err != nil {
		return "", fmt.Errorf("error generating English alt-text: %v", err)
	}

	// If target language is English, return the result directly
	if strings.HasPrefix(strings.ToLower(targetLanguageCode), "en") {
		return englishAltText, nil
	}

	targetLanguageName := getLanguageName(targetLanguageCode)

	translationPrompt := fmt.Sprintf(
		"Translate the following image description to %s, maintaining all details. Your response should only be the translated text:\n\n%s",
		targetLanguageName,
		englishAltText,
	)

	// Call the same LLM but without the image for translation
	translatedText, err := t.translateText(translationPrompt)
	if err != nil {
		return "", fmt.Errorf("error translating alt-text: %v", err)
	}

	return translatedText, nil
}

// translateText uses the LLM to translate text without an image
func (t *TranslationLayer) translateText(prompt string) (string, error) {
	// Implementation depends on the provider type
	switch provider := t.provider.(type) {
	case *OllamaProvider:
		return t.translateWithOllama(provider, prompt)
	case *TransformersProvider:
		return t.translateWithTransformers(provider, prompt)
	default:
		return "", fmt.Errorf("unsupported provider type for translation")
	}
}

// translateWithOllama translates text using Ollama
func (t *TranslationLayer) translateWithOllama(provider *OllamaProvider, prompt string) (string, error) {
	cmd := exec.Command("ollama", "run", provider.model, prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// translateWithTransformers translates text using Transformers
func (t *TranslationLayer) translateWithTransformers(provider *TransformersProvider, prompt string) (string, error) {
	// Prepare the request payload for text-only input
	payload := map[string]interface{}{
		"model": provider.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %v", err)
	}

	fullURL := fmt.Sprintf("%s/v1/chat/completions", provider.ServerURL)
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Post(fullURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("error making request to server: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("error parsing JSON response: %s", string(body))
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response: %s", string(body))
	}

	return result.Choices[0].Message.Content, nil
}

// getLanguageName returns the full language name for a given language code
func getLanguageName(langCode string) string {
	switch langCode {
	case "en":
		return "English"
	case "es":
		return "Spanish"
	case "fr":
		return "French"
	case "de":
		return "German"
	case "it":
		return "Italian"
	case "pt":
		return "Portuguese"
	case "ru":
		return "Russian"
	case "zh":
		return "Chinese"
	case "ja":
		return "Japanese"
	case "ko":
		return "Korean"
	case "ar":
		return "Arabic"
	case "bg":
		return "Bulgarian"
	case "ca":
		return "Catalan"
	case "cs":
		return "Czech"
	case "da":
		return "Danish"
	case "nl":
		return "Dutch"
	case "fi":
		return "Finnish"
	case "el":
		return "Greek"
	case "he":
		return "Hebrew"
	case "hi":
		return "Hindi"
	case "hu":
		return "Hungarian"
	case "id":
		return "Indonesian"
	case "lv":
		return "Latvian"
	case "lt":
		return "Lithuanian"
	case "no":
		return "Norwegian"
	case "pl":
		return "Polish"
	case "ro":
		return "Romanian"
	case "sk":
		return "Slovak"
	case "sl":
		return "Slovenian"
	case "sv":
		return "Swedish"
	case "th":
		return "Thai"
	case "tr":
		return "Turkish"
	case "uk":
		return "Ukrainian"
	case "vi":
		return "Vietnamese"
	case "fa":
		return "Persian"
	case "ms":
		return "Malay"
	case "bn":
		return "Bengali"
	case "ta":
		return "Tamil"
	case "te":
		return "Telugu"
	case "mr":
		return "Marathi"
	case "ur":
		return "Urdu"
	case "hr":
		return "Croatian"
	case "sr":
		return "Serbian"
	case "bs":
		return "Bosnian"
	case "mk":
		return "Macedonian"
	case "sq":
		return "Albanian"
	case "et":
		return "Estonian"
	case "is":
		return "Icelandic"
	case "ga":
		return "Irish"
	case "cy":
		return "Welsh"
	case "gl":
		return "Galician"
	case "eu":
		return "Basque"
	case "af":
		return "Afrikaans"
	case "sw":
		return "Swahili"
	case "zu":
		return "Zulu"
	case "xh":
		return "Xhosa"
	case "st":
		return "Sesotho"
	case "hy":
		return "Armenian"
	case "ka":
		return "Georgian"
	case "az":
		return "Azerbaijani"
	case "be":
		return "Belarusian"
	case "kk":
		return "Kazakh"
	case "ky":
		return "Kyrgyz"
	case "tg":
		return "Tajik"
	case "tk":
		return "Turkmen"
	case "uz":
		return "Uzbek"
	case "mn":
		return "Mongolian"
	case "my":
		return "Burmese"
	case "km":
		return "Khmer"
	case "lo":
		return "Lao"
	case "ne":
		return "Nepali"
	case "si":
		return "Sinhala"
	case "ml":
		return "Malayalam"
	case "kn":
		return "Kannada"
	case "pa":
		return "Punjabi"
	case "gu":
		return "Gujarati"
	case "or":
		return "Odia"
	case "as":
		return "Assamese"
	case "mt":
		return "Maltese"
	case "eo":
		return "Esperanto"
	case "la":
		return "Latin"
	case "gd":
		return "Scottish Gaelic"
	case "yi":
		return "Yiddish"
	case "fo":
		return "Faroese"
	case "haw":
		return "Hawaiian"
	case "mi":
		return "Maori"
	case "sm":
		return "Samoan"
	case "fil":
		return "Filipino"
	case "jv":
		return "Javanese"
	case "su":
		return "Sundanese"
	case "ha":
		return "Hausa"
	case "yo":
		return "Yoruba"
	case "ig":
		return "Igbo"
	case "am":
		return "Amharic"
	case "so":
		return "Somali"
	case "ps":
		return "Pashto"
	case "dv":
		return "Dhivehi"
	case "tt":
		return "Tatar"
	case "ug":
		return "Uyghur"
	case "bo":
		return "Tibetan"
	default:
		return "Unknown"
	}
}
