package main

import (
	"encoding/json"
	"os"
)

// Localization holds the localized strings for different languages
type Localization struct {
	Prompts   map[string]string `json:"prompts"`
	Responses map[string]string `json:"responses"`
}

var localizations map[string]Localization

var PromptOverrideState bool
var PromptAdditionState bool

func loadLocalizations() error {
	data, err := os.ReadFile("localizations.json")
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, &localizations)
	if err != nil {
		return err
	}

	return nil
}

func getLocalizedString(lang, key string, category string) string {
	localization := localizations[config.Localization.DefaultLanguage]

	if value, ok := localizations[lang]; ok {
		localization = value
	}

	switch category {
	case "prompt":
		var prompt string
		if PromptOverrideState {
			prompt = config.LLM.PromptOverride
		}
		if value, ok := localization.Prompts[key]; ok {
			prompt = value
		}

		if PromptAdditionState {
			prompt += " " + config.LLM.PromptAddition
		}

		return prompt
	case "response":
		if value, ok := localization.Responses[key]; ok {
			return value
		}
	}
	return ""
}
