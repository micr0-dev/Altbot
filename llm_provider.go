package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	genai "google.golang.org/genai"
)

// LLMProvider interface defines the methods that all LLM providers must implement
type LLMProvider interface {
	GenerateAltText(prompt string, imageData []byte, format string, targetLanguage string) (string, error)
	GenerateVideoAltText(prompt string, videoData []byte, format string, targetLanguage string) (string, error)
	GenerateContextQuestions(imageData []byte, format string, lang string) (string, error)
	GenerateAltTextWithContext(prompt string, imageData []byte, format string, userContext string, lang string) (string, error)
	Close() error
}

// GeminiProvider implements LLMProvider for Google's Gemini
type GeminiProvider struct {
	client           *genai.Client
	modelName        string
	generationConfig *genai.GenerateContentConfig
}

// OllamaProvider implements LLMProvider for Ollama
type OllamaProvider struct {
	model                string
	keepAlive            string
	translationModel     string
	translationKeepAlive string
}

// TransformersProvider implements LLMProvider for Hugging Face Transformers
type TransformersProvider struct {
	ServerURL     string
	Model         string
	Config        *Config
	serverProcess *os.Process
	monitoring    bool
	stopMonitor   chan bool
}

// NewLLMProvider creates a new LLM provider based on the configuration
func NewLLMProvider(config Config) (LLMProvider, error) {
	switch config.LLM.Provider {
	case "gemini":
		return setupGeminiProvider(config)
	case "ollama":
		return setupOllamaProvider(config)
	case "transformers":
		return setupTransformersProvider(config)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}

// Setup functions for each provider
func setupGeminiProvider(config Config) (*GeminiProvider, error) {
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	geminiClient, err := genai.NewClient(baseCtx, &genai.ClientConfig{
		APIKey:  config.Gemini.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, err
	}

	provider := &GeminiProvider{
		client:    geminiClient,
		modelName: config.Gemini.Model,
		generationConfig: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(config.Gemini.Temperature),
			TopK:        genai.Ptr(float32(config.Gemini.TopK)),
		},
	}

	client = provider.client
	geminiModelName = provider.modelName
	geminiGenerationConfig = cloneGenerateContentConfig(provider.generationConfig)

	return provider, nil
}

func setupOllamaProvider(config Config) (*OllamaProvider, error) {
	// Check if Ollama is installed and the model is available
	cmd := exec.Command("ollama", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error checking Ollama installation: %v", err)
	}

	if !bytes.Contains(output, []byte(config.LLM.OllamaModel)) {
		return nil, fmt.Errorf("ollama model %s not found. Install it with: ollama pull %s",
			config.LLM.OllamaModel, config.LLM.OllamaModel)
	}

	// Set default keep-alive if not specified
	keepAlive := config.LLM.OllamaKeepAlive
	if keepAlive == "" {
		keepAlive = "5m" // Default to 5 minutes
	}

	// Set up translation model (defaults to main model if not specified)
	translationModel := config.LLM.OllamaTranslationModel
	translationKeepAlive := config.LLM.OllamaTranslationKeepAlive
	if translationKeepAlive == "" {
		translationKeepAlive = keepAlive // Use same keep-alive as main model
	}

	// Check if translation model is specified and available
	if translationModel != "" && translationModel != config.LLM.OllamaModel {
		if !bytes.Contains(output, []byte(translationModel)) {
			return nil, fmt.Errorf("ollama translation model %s not found. Install it with: ollama pull %s",
				translationModel, translationModel)
		}
		fmt.Printf("Using separate translation model: %s\n", translationModel)
	}

	provider := &OllamaProvider{
		model:                config.LLM.OllamaModel,
		keepAlive:            keepAlive,
		translationModel:     translationModel,
		translationKeepAlive: translationKeepAlive,
	}

	// If persistent serving is enabled, pre-load the model
	if keepAlive == "-1" {
		fmt.Println("Pre-loading Ollama model for persistent serving...")
		cmd := exec.Command("ollama", "run", provider.model, "--keepalive", keepAlive, "echo", "Model loaded")
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to pre-load model: %v\n", err)
		} else {
			fmt.Println("Ollama model loaded and will remain in RAM")
		}
	}

	// Pre-load translation model if different and persistent serving is enabled
	if translationModel != "" && translationModel != config.LLM.OllamaModel && translationKeepAlive == "-1" {
		fmt.Println("Pre-loading Ollama translation model for persistent serving...")
		cmd := exec.Command("ollama", "run", translationModel, "--keepalive", translationKeepAlive, "echo", "Model loaded")
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to pre-load translation model: %v\n", err)
		} else {
			fmt.Println("Ollama translation model loaded and will remain in RAM")
		}
	}

	return provider, nil
}

// GenerateAltText implementations for each provider
func (p *GeminiProvider) GenerateAltText(prompt string, imageData []byte, format string, targetLanguage string) (string, error) {
	mimeType, err := inferImageMIME(format)
	if err != nil {
		return "", err
	}
	parts := []*genai.Part{
		&genai.Part{Text: prompt},
		&genai.Part{InlineData: &genai.Blob{Data: imageData, MIMEType: mimeType}},
	}

	resp, err := p.generateContent(parts)
	if err != nil {
		return "", err
	}

	return getResponse(resp), nil
}

func (p *GeminiProvider) GenerateVideoAltText(prompt string, videoData []byte, format string, targetLanguage string) (string, error) {
	// Create a temporary file for the video
	tmpFile, err := os.CreateTemp("", "video-*."+format)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up temp file when done

	// Write the video data to the temporary file
	if _, err := tmpFile.Write(videoData); err != nil {
		return "", fmt.Errorf("failed to write video to temp file: %v", err)
	}

	// Close the file before passing it to the Gemini processor
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %v", err)
	}

	// Use the existing method to generate alt-text with Gemini
	return GenerateVideoAltWithGemini(prompt, tmpFile.Name())
}

func (p *GeminiProvider) generateContent(parts []*genai.Part) (*genai.GenerateContentResponse, error) {
	if p.client == nil {
		return nil, fmt.Errorf("gemini client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	contents := []*genai.Content{{Parts: parts}}
	return p.client.Models.GenerateContent(ctx, p.modelName, contents, cloneGenerateContentConfig(p.generationConfig))
}

func (p *GeminiProvider) GenerateContextQuestions(imageData []byte, format string, lang string) (string, error) {
	mimeType, err := inferImageMIME(format)
	if err != nil {
		return "", err
	}

	prompt := getLocalizedString(lang, "contextQuestionPrompt", "prompt")

	parts := []*genai.Part{
		{Text: prompt},
		{InlineData: &genai.Blob{Data: imageData, MIMEType: mimeType}},
	}

	resp, err := p.generateContent(parts)
	if err != nil {
		return "", err
	}

	return getResponse(resp), nil
}

func (p *GeminiProvider) GenerateAltTextWithContext(prompt string, imageData []byte, format string, userContext string, lang string) (string, error) {
	mimeType, err := inferImageMIME(format)
	if err != nil {
		return "", err
	}

	// Build the prompt with user context
	contextPrompt := fmt.Sprintf(getLocalizedString(lang, "contextAltTextPrompt", "prompt"), userContext)

	parts := []*genai.Part{
		{Text: contextPrompt},
		{InlineData: &genai.Blob{Data: imageData, MIMEType: mimeType}},
	}

	resp, err := p.generateContent(parts)
	if err != nil {
		return "", err
	}

	return getResponse(resp), nil
}

func (p *OllamaProvider) GenerateAltText(prompt string, imageData []byte, format string, targetLanguage string) (string, error) {
	if config.LLM.UseTranslationLayer && targetLanguage != "en" {
		// Use translation layer
		translationLayer := NewTranslationLayer(p)
		return translationLayer.GenerateAndTranslateAltText(prompt, imageData, format, targetLanguage)
	}

	// Create a temporary file for the image
	tmpFile, err := os.CreateTemp("", "image.*."+format)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(imageData); err != nil {
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	// Prepare the Ollama command
	cmd := exec.Command("ollama", "run", p.model, "--hidethinking", "--keepalive", p.keepAlive, fmt.Sprintf("%s %s", prompt, tmpFile.Name()))

	var out bytes.Buffer
	cmd.Stdout = &out

	err = cmd.Run()
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

func (p *OllamaProvider) GenerateVideoAltText(prompt string, videoData []byte, format string, targetLanguage string) (string, error) {
	// Ollama currently doesn't support video processing directly
	// You could extract frames and process as images, or return an error
	return "", fmt.Errorf("video processing not supported by Ollama provider")
}

func (p *OllamaProvider) GenerateContextQuestions(imageData []byte, format string, lang string) (string, error) {
	// Create a temporary file for the image
	tmpFile, err := os.CreateTemp("", "image.*."+format)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(imageData); err != nil {
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	prompt := getLocalizedString(lang, "contextQuestionPrompt", "prompt")

	cmd := exec.Command("ollama", "run", p.model, "--hidethinking", "--keepalive", p.keepAlive, fmt.Sprintf("%s %s", prompt, tmpFile.Name()))

	var out bytes.Buffer
	cmd.Stdout = &out

	err = cmd.Run()
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

func (p *OllamaProvider) GenerateAltTextWithContext(prompt string, imageData []byte, format string, userContext string, lang string) (string, error) {
	// Create a temporary file for the image
	tmpFile, err := os.CreateTemp("", "image.*."+format)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(imageData); err != nil {
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	// Build the prompt with user context
	contextPrompt := fmt.Sprintf(getLocalizedString(lang, "contextAltTextPrompt", "prompt"), userContext)

	cmd := exec.Command("ollama", "run", p.model, "--hidethinking", "--keepalive", p.keepAlive, fmt.Sprintf("%s %s", contextPrompt, tmpFile.Name()))

	var out bytes.Buffer
	cmd.Stdout = &out

	err = cmd.Run()
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

func (p *TransformersProvider) GenerateAltText(prompt string, imageData []byte, format string, targetLanguage string) (string, error) {
	if config.LLM.UseTranslationLayer && targetLanguage != "en" {
		// Use translation layer
		translationLayer := NewTranslationLayer(p)
		return translationLayer.GenerateAndTranslateAltText(prompt, imageData, format, targetLanguage)
	}

	// Convert image to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Prepare the request payload
	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": fmt.Sprintf("data:image/%s;base64,%s", format, base64Image),
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %v", err)
	}

	fullURL := fmt.Sprintf("%s/v1/chat/completions", p.ServerURL)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the HTTP request to the server
	resp, err := client.Post(
		fullURL,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("error making request to server: %v", err)
	}
	defer resp.Body.Close()

	// Read the entire response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	// Check if response is successful
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	// Try to parse as JSON
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		// Log the actual response for debugging
		return "", fmt.Errorf("error parsing JSON response (status %d): %s", resp.StatusCode, string(body))
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response: %s", string(body))
	}

	return result.Choices[0].Message.Content, nil
}

// GenerateVideoAltText generates alt text for a video using the Transformers model
func (p *TransformersProvider) GenerateVideoAltText(prompt string, videoData []byte, format string, targetLanguage string) (string, error) {
	if config.LLM.UseTranslationLayer && targetLanguage != "en" {
		// Use translation layer
		translationLayer := NewTranslationLayer(p)
		return translationLayer.GenerateAndTranslateVideoAltText(prompt, videoData, format, targetLanguage)
	}

	// Extract frames from video
	framesPerSecond := p.Config.VideoProcessing.NumFramesPerSecond
	maxFrames := p.Config.VideoProcessing.MaxFrames

	base64Frames, err := ExtractVideoFrames(videoData, framesPerSecond, maxFrames)
	if err != nil {
		return "", fmt.Errorf("error extracting video frames: %v", err)
	}

	if len(base64Frames) == 0 {
		return "", fmt.Errorf("no frames could be extracted from video")
	}

	// Prepare the request payload
	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type":   "video_frames",
						"frames": base64Frames,
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %v", err)
	}

	fullURL := fmt.Sprintf("%s/v1/chat/completions", p.ServerURL)

	// Create HTTP client with longer timeout for videos
	client := &http.Client{
		Timeout: 120 * time.Second, // Longer timeout for video processing
	}

	// Make the HTTP request to the server
	resp, err := client.Post(
		fullURL,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", fmt.Errorf("error making request to server: %v", err)
	}
	defer resp.Body.Close()

	// Read the entire response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	// Check if response is successful
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
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

func (p *TransformersProvider) GenerateContextQuestions(imageData []byte, format string, lang string) (string, error) {
	// Convert image to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	prompt := getLocalizedString(lang, "contextQuestionPrompt", "prompt")

	// Prepare the request payload
	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": fmt.Sprintf("data:image/%s;base64,%s", format, base64Image),
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %v", err)
	}

	fullURL := fmt.Sprintf("%s/v1/chat/completions", p.ServerURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

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

func (p *TransformersProvider) GenerateAltTextWithContext(prompt string, imageData []byte, format string, userContext string, lang string) (string, error) {
	// Convert image to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Build the prompt with user context
	contextPrompt := fmt.Sprintf(getLocalizedString(lang, "contextAltTextPrompt", "prompt"), userContext)

	// Prepare the request payload
	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": contextPrompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": fmt.Sprintf("data:image/%s;base64,%s", format, base64Image),
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error marshaling JSON: %v", err)
	}

	fullURL := fmt.Sprintf("%s/v1/chat/completions", p.ServerURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

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

// Close implementations for each provider
func (p *GeminiProvider) Close() error {
	return nil
}

func (p *OllamaProvider) Close() error {
	return nil // Nothing to close for Ollama
}

func (p *TransformersProvider) Close() error {
	if p.monitoring {
		p.stopMonitor <- true
		p.monitoring = false
	}

	if p.serverProcess != nil {
		p.serverProcess.Kill()
		p.serverProcess = nil
	}
	return nil
}

func inferImageMIME(format string) (string, error) {
	switch strings.ToLower(format) {
	case "jpg", "jpeg":
		return "image/jpeg", nil
	case "png":
		return "image/png", nil
	case "gif":
		return "image/gif", nil
	case "bmp":
		return "image/bmp", nil
	case "tif", "tiff":
		return "image/tiff", nil
	case "webp":
		return "image/webp", nil
	default:
		return "", fmt.Errorf("unsupported image format: %s", format)
	}
}

func inferMIMEFromExtension(format string, fallbackPrefix string) (string, error) {
	if format == "" {
		return "", fmt.Errorf("missing file format")
	}
	ext := "." + strings.TrimPrefix(strings.ToLower(format), ".")
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		return mimeType, nil
	}
	switch ext {
	case ".mp4":
		return "video/mp4", nil
	case ".mov":
		return "video/quicktime", nil
	case ".mkv":
		return "video/x-matroska", nil
	case ".avi":
		return "video/x-msvideo", nil
	case ".webm":
		return "video/webm", nil
	case ".m4v":
		return "video/x-m4v", nil
	case ".3gp":
		return "video/3gpp", nil
	case ".wav":
		return "audio/wav", nil
	case ".mp3":
		return "audio/mpeg", nil
	case ".flac":
		return "audio/flac", nil
	case ".ogg":
		return "audio/ogg", nil
	case ".aac":
		return "audio/aac", nil
	case ".m4a":
		return "audio/mp4", nil
	case ".opus":
		return "audio/ogg", nil
	}
	if fallbackPrefix == "" {
		return "", fmt.Errorf("unknown MIME type for extension %s", ext)
	}
	return fmt.Sprintf("%s/%s", fallbackPrefix, strings.TrimPrefix(ext, ".")), nil
}

func cloneGenerateContentConfig(cfg *genai.GenerateContentConfig) *genai.GenerateContentConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	return &clone
}

func setupTransformersProvider(config Config) (*TransformersProvider, error) {
	serverURL := fmt.Sprintf("http://localhost:%d", config.TransformersServerArgs.Port)
	provider := &TransformersProvider{
		Model:       config.TransformersServerArgs.Model,
		ServerURL:   serverURL,
		Config:      &config,
		stopMonitor: make(chan bool),
	}

	// Check if server is already running
	if !checkTransformersServer(serverURL) {
		if err := provider.startServer(); err != nil {
			return nil, err
		}
	}

	// Start monitoring
	provider.monitoring = true
	go provider.monitorServer()

	return provider, nil
}

func checkTransformersServer(serverURL string) bool {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(serverURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func (p *TransformersProvider) monitorServer() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	retryCount := 0
	maxRetries := 5

	for {
		select {
		case <-p.stopMonitor:
			return
		case <-ticker.C:
			if !checkTransformersServer(p.ServerURL) {
				fmt.Printf("Transformers server is not responding. Attempting restart (attempt %d/%d)...\n", retryCount+1, maxRetries)

				// Kill existing process if any
				if p.serverProcess != nil {
					p.serverProcess.Kill()
					p.serverProcess = nil
				}

				// Restart the server
				err := p.startServer()
				if err != nil {
					fmt.Printf("Failed to restart Transformers server: %v\n", err)
					retryCount++

					if retryCount >= maxRetries {
						fmt.Println("Maximum retry attempts reached. Will try again in 5 minutes.")
						retryCount = 0
						time.Sleep(5*time.Minute - 30*time.Second) // Adjust for ticker
					}
				} else {
					fmt.Println("Transformers server restarted successfully!")
					retryCount = 0
				}
			} else {
				// Server is healthy, reset retry count
				retryCount = 0
			}
		}
	}
}

func (p *TransformersProvider) startServer() error {
	args := []string{
		"transformers_server.py",
		"--port", strconv.Itoa(p.Config.TransformersServerArgs.Port),
		"--model", p.Config.TransformersServerArgs.Model,
		"--device", p.Config.TransformersServerArgs.Device,
		"--max-memory", fmt.Sprintf("%.2f", p.Config.TransformersServerArgs.MaxMemory),
		"--torch-dtype", p.Config.TransformersServerArgs.TorchDtype,
	}

	cmd := exec.Command("python3", args...)

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Transformers server: %v", err)
	}

	// Store the process
	p.serverProcess = cmd.Process

	// Create channels for server ready signal and error
	ready := make(chan bool)
	errorChan := make(chan error)

	// Start goroutine to read stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Printf("Transformers stdout: %s\n", line)
		}
	}()

	// Start goroutine to read stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Printf("Transformers stderr: %s\n", line)
			if strings.Contains(line, "Running on all addresses") {
				// Give the server a moment to fully initialize
				time.Sleep(1 * time.Second)
				ready <- true
				return
			}
			if strings.Contains(line, "Error") || strings.Contains(line, "error") {
				errorChan <- fmt.Errorf("server error: %s", line)
			}
		}
	}()

	fmt.Println("Waiting for Transformers server to start...")

	// Wait for either ready signal or error with a timeout
	select {
	case <-ready:
		fmt.Println("Transformers server is ready!")
		return nil
	case err := <-errorChan:
		return fmt.Errorf("server failed to start: %v", err)
	case <-time.After(5 * time.Minute): // Timeout for model loading
		return fmt.Errorf("timeout waiting for server to start")
	}
}
