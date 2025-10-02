/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the Overworked License (OWL) v2.0
 */

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// RunSetupWizard guides the user through setup and writes config to a file
// RunSetupWizard guides the user through setup and writes config to a file
func runSetupWizard(filePath string) {
	fmt.Println(Cyan + "Welcome to the Altbot Setup Wizard!" + Reset)

	// Load the default config
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalf("Error loading config.toml: %v", err)
	}

	config.Server.MastodonServer = promptString(Blue+"Mastodon Server URL:"+Reset, config.Server.MastodonServer)
	config.Server.ClientSecret = promptString(Pink+"Mastodon Client Secret:"+Reset, config.Server.ClientSecret)
	config.Server.AccessToken = promptString(Green+"Mastodon Access Token:"+Reset, config.Server.AccessToken)
	config.Server.Username = promptString(Yellow+"Bot Username:"+Reset, config.Server.Username)

	config.RateLimit.AdminContactHandle = promptString(Red+"Admin Contact Handle:"+Reset, config.RateLimit.AdminContactHandle)

	// LLM provider selection
	providerOptions := []string{"gemini", "ollama", "transformers"}
	fmt.Println(Blue + "Select LLM Provider:" + Reset)
	for i, option := range providerOptions {
		fmt.Printf("%d. %s\n", i+1, option)
	}

	var providerChoice int
	for {
		fmt.Print(Blue + "Enter choice (1-3): " + Reset)
		fmt.Scanln(&providerChoice)
		if providerChoice >= 1 && providerChoice <= len(providerOptions) {
			break
		}
		fmt.Println(Red + "Invalid choice. Please try again." + Reset)
	}
	config.LLM.Provider = providerOptions[providerChoice-1]

	// Add translation layer option for local LLMs
	if config.LLM.Provider == "ollama" || config.LLM.Provider == "transformers" {
		fmt.Println(Yellow + "\nLocal LLMs often perform better at generating alt-text in English." + Reset)
		fmt.Println("The translation layer will:")
		fmt.Println("1. Generate alt-text in English first")
		fmt.Println("2. Then translate to the target language")
		config.LLM.UseTranslationLayer = promptBool(Cyan+"Enable translation layer (true/false)?"+Reset, "true")

		if config.LLM.Provider == "ollama" {
			config.LLM.OllamaModel = promptString(Green+"Ollama Model Name:"+Reset, config.LLM.OllamaModel)
			
			fmt.Println(Yellow + "\nOllama Model Keep-Alive Settings:" + Reset)
			fmt.Println("This controls how long the model stays loaded in RAM after a request.")
			fmt.Println("Options:")
			fmt.Println("  -1  = Keep model loaded persistently (best for active instances)")
			fmt.Println("  0   = Unload immediately after each request")
			fmt.Println("  5m  = Keep loaded for 5 minutes (default)")
			fmt.Println("  30m = Keep loaded for 30 minutes")
			config.LLM.OllamaKeepAlive = promptString(Cyan+"Keep-Alive Duration:"+Reset, "-1")
		}
	} else if config.LLM.Provider == "gemini" {
		config.Gemini.APIKey = promptString(Green+"Gemini API Key:"+Reset, config.Gemini.APIKey)
		config.Gemini.Model = promptString(Yellow+"Gemini Model (gemini-1.5-flash/gemini-1.5-pro):"+Reset, config.Gemini.Model)
	}

	config.RateLimit.Enabled = promptBool(Cyan+"Enable Rate Limiting (true/false)?"+Reset, fmt.Sprintf("%t", config.RateLimit.Enabled))
	config.WeeklySummary.Enabled = promptBool(Blue+"Enable Weekly Summary (true/false)?"+Reset, fmt.Sprintf("%t", config.WeeklySummary.Enabled))
	config.Metrics.Enabled = promptBool(Cyan+"Enable Metrics (true/false)?"+Reset, fmt.Sprintf("%t", config.Metrics.Enabled))
	config.Metrics.DashboardEnabled = promptBool(Blue+"Enable Metrics Dashboard (true/false)?"+Reset, fmt.Sprintf("%t", config.Metrics.DashboardEnabled))
	config.AltTextReminders.Enabled = promptBool(Cyan+"Enable Alt-Text Reminders (true/false)?"+Reset, fmt.Sprintf("%t", config.AltTextReminders.Enabled))

	// Power metrics section (only relevant for local models)
	if config.LLM.Provider == "ollama" || config.LLM.Provider == "transformers" {
		fmt.Println(Green + "\nPower Metrics Settings:" + Reset)
		fmt.Println("This feature shows the estimated electricity used for each alt-text generation.")

		config.PowerMetrics.Enabled = promptBool(Cyan+"Enable Power Consumption Metrics (true/false)?"+Reset, fmt.Sprintf("%t", config.PowerMetrics.Enabled))

		if config.PowerMetrics.Enabled {
			// Convert the float to a string for the prompt
			gpuWattsStr := fmt.Sprintf("%.1f", config.PowerMetrics.GPUWatts)
			gpuWattsInput := promptString(Yellow+"GPU Power Consumption (watts):"+Reset, gpuWattsStr)
			config.PowerMetrics.GPUWatts = parseFloat(gpuWattsInput, config.PowerMetrics.GPUWatts)
		}
	}

	saveConfig(filePath)

	fmt.Println(Green + "Configuration complete! Your settings have been saved to " + filePath + Reset)
}

// getStringInput prompts for a string input and returns the entered value or a default
func promptString(prompt, defaultValue string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [%s]: ", prompt, defaultValue)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultValue
	}
	return input
}

// getBoolInput prompts for a boolean input and returns the boolean value
func promptBool(prompt, defaultValue string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [%s]: ", prompt, defaultValue)
		input, _ := reader.ReadString('\n')
		input = strings.ToLower(strings.TrimSpace(input))

		if input == "" {
			input = defaultValue
		}

		switch input {
		case "true", "t", "yes", "y":
			return true
		case "false", "f", "no", "n":
			return false
		default:
			fmt.Println(Red + "Please enter 'true' or 'false'." + Reset)
		}
	}
}

// parseFloat parses a string to a float64, returning defaultValue if parsing fails
func parseFloat(input string, defaultValue float64) float64 {
	// Try to parse the input string as a float64
	var value float64
	_, err := fmt.Sscanf(input, "%f", &value)
	if err != nil {
		fmt.Printf(Red+"Error parsing float value, using default: %v"+Reset+"\n", defaultValue)
		return defaultValue
	}
	return value
}

// saveConfig writes the config struct to a file named config.toml
func saveConfig(filePath string) {
	file, err := os.Create(filePath)
	if err != nil {
		log.Fatalf("Error creating config file: %v", err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		log.Fatalf("Error encoding config to file: %v", err)
	}
}
