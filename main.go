/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the Overworked License (OWL) v2.0
 */

package main

import (
	"Altbot/dashboard"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	genai "google.golang.org/genai"

	"github.com/mattn/go-mastodon"
	"github.com/nfnt/resize"
)

// Version of the bot
const Version = "2.1.3"

// AsciiArt is the ASCII art for the bot
const AsciiArt = `    _   _ _   _        _   
   /_\ | | |_| |__ ___| |_ 
  / _ \| |  _| '_ / _ \  _|
 /_/ \_\_|\__|_.__\___/\__|`
const Motto = "アクセシビリティロボット"

type Config struct {
	Server struct {
		MastodonServer string `toml:"mastodon_server"`
		ClientSecret   string `toml:"client_secret"`
		AccessToken    string `toml:"access_token"`
		Username       string `toml:"username"`
	} `toml:"server"`
	LLM struct {
		Provider            string `toml:"provider"`
		OllamaModel         string `toml:"ollama_model"`
		OllamaKeepAlive     string `toml:"ollama_keep_alive"`
		UseTranslationLayer bool   `toml:"use_translation_layer"`
		PromptAddition      string `toml:"prompt_additional_instructions"`
		PromptOverride      string `toml:"prompt_override"`
	} `toml:"llm"`
	TransformersServerArgs struct {
		Port       int     `toml:"port"`
		Model      string  `toml:"model"`
		Device     string  `toml:"device"`
		MaxMemory  float64 `toml:"max_memory"`
		TorchDtype string  `toml:"torch_dtype"`
	} `toml:"transformers"`
	Gemini struct {
		Model                     string  `toml:"model"`
		APIKey                    string  `toml:"api_key"`
		Temperature               float32 `toml:"temperature"`
		TopK                      int32   `toml:"top_k"`
		HarassmentThreshold       string  `toml:"harassment_threshold"`
		HateSpeechThreshold       string  `toml:"hate_speech_threshold"`
		SexuallyExplicitThreshold string  `toml:"sexually_explicit_threshold"`
		DangerousContentThreshold string  `toml:"dangerous_content_threshold"`
	} `toml:"gemini"`
	Localization struct {
		DefaultLanguage string `toml:"default_language"`
	} `toml:"localization"`
	DNI struct {
		Tags       []string `toml:"tags"`
		IgnoreBots bool     `toml:"ignore_bots"`
	} `toml:"dni"`
	ImageProcessing struct {
		DownscaleWidth uint `toml:"downscale_width"`
		MaxSizeMB      uint `toml:"max_size_mb"`
	} `toml:"image_processing"`
	VideoProcessing struct {
		MaxSizeMB          uint    `toml:"max_size_mb"`
		NumFramesPerSecond float64 `toml:"num_frames_per_second"`
		MaxFrames          int     `toml:"max_frames"`
	} `toml:"video_processing"`
	Behavior struct {
		ReplyVisibility string `toml:"reply_visibility"`
		FollowBack      bool   `toml:"follow_back"`
		AskForConsent   bool   `toml:"ask_for_consent"`
	} `toml:"behavior"`
	WeeklySummary struct {
		Enabled         bool     `toml:"enabled"`
		PostDay         string   `toml:"post_day"`
		PostTime        string   `toml:"post_time"`
		MessageTemplate string   `toml:"message_template"`
		Tips            []string `toml:"tips"`
	} `toml:"weekly_summary"`
	Metrics struct {
		Enabled          bool `toml:"enabled"`
		DashboardEnabled bool `toml:"dashboard_enabled"`
		DashboardPort    int  `toml:"dashboard_port"`
	} `toml:"metrics"`
	PowerMetrics struct {
		Enabled  bool    `toml:"enabled"`
		GPUWatts float64 `toml:"gpu_watts"`
	} `toml:"power_metrics"`
	RateLimit struct {
		Enabled                        bool   `toml:"enabled"`
		MaxRequestsPerMinute           int    `toml:"max_requests_per_user_per_minute"`
		MaxRequestsPerHour             int    `toml:"max_requests_per_user_per_hour"`
		NewAccountMaxRequestsPerMinute int    `toml:"new_account_max_requests_per_minute"`
		NewAccountMaxRequestsPerHour   int    `toml:"new_account_max_requests_per_hour"`
		NewAccountPeriodDays           int    `toml:"new_account_period_days"`
		ShadowBanThreshold             int    `toml:"shadow_ban_threshold"`
		AdminContactHandle             string `toml:"admin_contact_handle"`
	} `toml:"rate_limit"`
	AltTextReminders struct {
		Enabled      bool `toml:"enabled"`
		ReminderTime int  `toml:"reminder_time"`
	} `toml:"alt_text_reminders"`
	Profile struct {
		Enabled            bool     `toml:"enabled"`
		OverrideFeildCount bool     `toml:"override_field_count"`
		Fields             []string `toml:"fields"`
	} `toml:"profile"`
}

const (
	// Colors
	Blue   = "\033[34m"
	Pink   = "\033[38;5;219m"
	Green  = "\033[32m"
	Red    = "\033[31m"
	Yellow = "\033[33m"
	Reset  = "\033[0m"
	Cyan   = "\033[36m"
	White  = "\033[37m"
)

var defaultConfig Config
var config Config
var client *genai.Client
var geminiModelName string
var geminiGenerationConfig *genai.GenerateContentConfig
var ctx context.Context
var botAcct mastodon.Account

var consentRequests = make(map[mastodon.ID]ConsentRequest)

var videoProcessingCapability = false
var audioProcessingCapability = false

var rateLimiter *RateLimiter

var metricsManager *MetricsManager

var llmProvider LLMProvider

const (
	sourceURL = "https://github.com/micr0-dev/Altbot"
	donateURL = "https://ko-fi.com/micr0byte"
	creator   = "@micr0@wetdry.world"
)

func main() {
	setupFlag := flag.Bool("setup", false, "Run the setup wizard")
	flag.Parse()

	// Load default configuration from example.config.toml
	if _, err := toml.DecodeFile("example.config.toml", &defaultConfig); err != nil {
		log.Fatalf("Error loading default config from example.config.toml: %v", err)
	}

	// Check if config.toml exists, if not, create it by copying example.config.toml
	if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
		if err := copyConfig("example.config.toml", "config.toml", 5); err != nil {
			log.Fatalf("Error creating default config.toml: %v", err)
		}

		log.Println("config.toml not found. Running setup wizard...")
		*setupFlag = true
	}

	if *setupFlag {
		runSetupWizard("config.toml")
	}

	// Load configuration from config.toml
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalf("Error loading config.toml: %v", err)
	}

	// Compare config with defaultConfig and print warnings or custom settings
	customSettingsCount := compareConfigs(defaultConfig, config)

	if config.Server.MastodonServer == "https://mastodon.example.com" {
		log.Fatal("Please configure the Mastodon server in config.toml")
	}
	var err error
	llmProvider, err = NewLLMProvider(config)
	if err != nil {
		log.Fatalf("Error initializing LLM provider: %v", err)
	}
	defer llmProvider.Close()

	// Set video/audio processing capability based on provider
	switch config.LLM.Provider {
	case "transformers":
		// Transformers server management is now handled by the TransformersProvider
		// in setupTransformersProvider, so we don't need to manually check/start it here

		// Just set capability flag
		videoProcessingCapability = true

		// Log that we're using the Transformers provider
		fmt.Printf("%s Using Transformers provider with model %s\n",
			Yellow, config.TransformersServerArgs.Model)

	case "ollama":
		err := checkOllamaModel()
		if err != nil {
			log.Fatalf("Error checking Ollama model: %v", err)
		}

	case "gemini":
		// Gemini supports video/audio processing
		videoProcessingCapability = true
		audioProcessingCapability = true

	default:
		log.Fatalf("Unsupported LLM provider: %s", config.LLM.Provider)
	}

	err = loadLocalizations()
	if err != nil {
		log.Fatalf("Error loading localizations: %v", err)
	}

	// Print the version and art
	fmt.Printf("%s%s%s%s%s\n", Cyan, AsciiArt, Pink, Motto, Reset)
	fmt.Printf("%sAltbot%s v%s (%s)\n", Cyan, Reset, Version, config.LLM.Provider)
	checkForUpdates()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	c := mastodon.NewClient(&mastodon.Config{
		Server:       config.Server.MastodonServer,
		ClientSecret: config.Server.ClientSecret,
		AccessToken:  config.Server.AccessToken,
	})

	// Fetch and verify the bot account ID
	_, err = fetchAndVerifyBotAccountID(c)
	if err != nil {
		log.Fatalf("Error fetching bot account ID: %v", err)
	}

	fmt.Printf("%s %d Custom settings loaded\n\n", getStatusSymbol(customSettingsCount > 0), customSettingsCount)

	fmt.Printf("%s Mastodon Connection: %s\n", getStatusSymbol(true), config.Server.MastodonServer)

	if config.Profile.Enabled {
		if err := updateBotProfile(c, config); err != nil {
			fmt.Printf("%s Warning: Failed to update profile fields: %v\n", Yellow, err)
		}
	} else {
		fmt.Printf("%s Dynamic Profile Fields: %s\n", getStatusSymbol(false), "Disabled")
	}

	if videoProcessingCapability {
		fmt.Printf("%s Video Processing: %v\n", getStatusSymbol(true), videoProcessingCapability)
	} else {
		fmt.Printf("%s Video Processing: Unsupported by LLM\n", getStatusSymbol(false))
	}
	if audioProcessingCapability {
		fmt.Printf("%s Audio Processing: %v\n", getStatusSymbol(true), audioProcessingCapability)
	} else {
		fmt.Printf("%s Audio Processing: Unsupported by LLM\n", getStatusSymbol(false))
	}

	PromptAdditionState = config.LLM.PromptAddition != ""

	if PromptOverrideState {
		fmt.Printf("%s Prompt Override: Set to \"%.30s...\"\n", getStatusSymbol(true), config.LLM.PromptOverride)
	} else if PromptAdditionState {
		fmt.Printf("%s Prompt Additional Instructions: Set to \"%.30s...\"\n", getStatusSymbol(true), config.LLM.PromptAddition)
	} else {
		fmt.Printf("%s Default Prompts: %s\n", getStatusSymbol(true), "Loaded")
	}

	// Set up Gemini AI model
	err = Setup(config.Gemini.APIKey)
	if err != nil {
		log.Fatal(err)
	}

	// Connect to Mastodon streaming API
	ws := c.NewWSClient()

	events, err := ws.StreamingWSUser(ctx)
	if err != nil {
		log.Fatalf("Error connecting to streaming API: %v", err)
	}

	if config.WeeklySummary.Enabled {
		go startWeeklySummaryScheduler(c)
		fmt.Printf("%s Weekly Summary: %vs %v\n", getStatusSymbol(config.WeeklySummary.Enabled), config.WeeklySummary.PostDay, config.WeeklySummary.PostTime)
	} else {
		fmt.Printf("%s Weekly Summary: %v\n", getStatusSymbol(config.WeeklySummary.Enabled), config.WeeklySummary.Enabled)
	}

	if config.AltTextReminders.Enabled {
		go checkAltTextPeriodically(c, 1*time.Minute, time.Duration(config.AltTextReminders.ReminderTime)*time.Minute)
		fmt.Printf("%s Alt Text Reminders: %v mins\n", getStatusSymbol(config.AltTextReminders.Enabled), config.AltTextReminders.ReminderTime)

	} else {
		fmt.Printf("%s Alt Text Reminders: %v\n", getStatusSymbol(config.AltTextReminders.Enabled), config.AltTextReminders.Enabled)
	}

	// Initialize the rate limiter
	rateLimiter = NewRateLimiter()

	if config.RateLimit.Enabled {
		// Load rate limiter state from file
		if err := rateLimiter.LoadFromFile("ratelimiter.json"); err != nil {
			log.Fatalf("Error loading rate limiter state: %v", err)
		}

		// Reset minute counts every minute
		go func() {
			for {
				time.Sleep(1 * time.Minute)
				rateLimiter.ResetMinuteCounts()
			}
		}()

		// Reset hour counts every hour
		go func() {
			for {
				time.Sleep(1 * time.Hour)
				rateLimiter.ResetHourCounts()
			}
		}()
	}

	// Start a goroutine for periodic cleanup of old reply entries
	go cleanupOldEntries()

	if err := loadConsentRequestsFromFile("consent_requests.json"); err != nil {
		log.Fatalf("Error loading consent requests: %v", err)
	}

	go func() {
		for {
			time.Sleep(1 * time.Hour)
			cleanupOldConsentRequests()
		}
	}()

	fmt.Printf("%s GDPR Consent System: ", getStatusSymbol(true))

	// Initialize GDPR consent database
	if err := InitializeConsentDatabase(); err != nil {
		log.Fatalf("Error initializing GDPR consent database: %v", err)
	}

	fmt.Printf("%s Legacy Consent System: %v\n", getStatusSymbol(config.Behavior.AskForConsent), config.Behavior.AskForConsent)

	// Start metrics manager
	metricsManager = NewMetricsManager(config.Metrics.Enabled, "metrics.json", 10*time.Second)
	defer metricsManager.stop()

	fmt.Printf("%s Metrics Collection: %v\n", getStatusSymbol(config.Metrics.Enabled), config.Metrics.Enabled)

	if config.Metrics.DashboardEnabled {
		dashboard.StartDashboard("metrics.json", config.Metrics.DashboardPort)
		fmt.Printf("%s Metrics Dashboard: %s\n", getStatusSymbol(true), "http://localhost:"+strconv.Itoa(config.Metrics.DashboardPort))
	} else {
		fmt.Printf("%s Metrics Dashboard: %v\n", getStatusSymbol(false), config.Metrics.DashboardEnabled)
	}

	// Display power metrics status if using a local model
	if config.LLM.Provider != "gemini" {
		powerMetricsStatus := fmt.Sprintf("%v (%.1f watts)", config.PowerMetrics.Enabled, config.PowerMetrics.GPUWatts)
		fmt.Printf("%s Power Consumption Metrics: %s\n", getStatusSymbol(config.PowerMetrics.Enabled), powerMetricsStatus)
	}

	fmt.Println("\n-----------------------------------")

	fmt.Println("Connected to streaming API. All systems operational. Waiting for mentions and follows...")

	// Main event loop
	for event := range events {
		switch e := event.(type) {
		case *mastodon.NotificationEvent:
			switch e.Notification.Type {
			case "mention": // Get the ID of the status being replied to
				if "@"+e.Notification.Account.Acct == config.RateLimit.AdminContactHandle {
					handleAdminReply(c, e.Notification.Status, rateLimiter)
				}

				if parentStatusRef := e.Notification.Status.InReplyToID; parentStatusRef != nil {
					var parentStatusID mastodon.ID

					// Convert the parent status ID to the correct type
					switch typedID := parentStatusRef.(type) {
					case string:
						parentStatusID = mastodon.ID(typedID)
					case mastodon.ID:
						parentStatusID = typedID
					}

					// Fetch the parent status
					parentStatus, err := c.GetStatus(ctx, parentStatusID)

					if parentStatus == nil {
						log.Printf("Error fetching parent status: %v", err)
						break
					}

					if err != nil {
						handleMention(c, e.Notification)
					}

					// Get the grandparent status ID (the status that the parent was replying to)
					grandparentStatusRef := parentStatus.InReplyToID

					var grandparentStatusID mastodon.ID
					// Convert the grandparent status ID to the correct type
					switch typedID := grandparentStatusRef.(type) {
					case string:
						grandparentStatusID = mastodon.ID(typedID)
					case mastodon.ID:
						grandparentStatusID = typedID
					}

					// Check if this is a response to a consent request
					if _, isConsentRequest := consentRequests[grandparentStatusID]; isConsentRequest {
						handleConsentResponse(c, grandparentStatusID, e.Notification.Status)
					} else {
						// Check if this might be a GDPR consent response
						isGDPRConsent := HandleGDPRConsentResponse(c, e.Notification.Status)
						if !isGDPRConsent {
							handleMention(c, e.Notification)
						}
					}
				} else {
					handleMention(c, e.Notification)
				}
			case "follow":
				handleFollow(c, e.Notification)
			}
		case *mastodon.UpdateEvent:
			handleUpdate(c, e.Status)
		case *mastodon.ErrorEvent:
			log.Printf("Error event: %v", e.Error())
		case *mastodon.DeleteEvent:
			handleDeleteEvent(c, e.ID)
		}
	}
}

// fetchAndVerifyBotAccountID fetches and prints the bot account details to verify the account ID
func fetchAndVerifyBotAccountID(c *mastodon.Client) (mastodon.ID, error) {
	acct, err := c.GetAccountCurrentUser(ctx)
	if err != nil {
		return "", err
	}
	fmt.Printf("Bot Account ID: %s, Username: %s\n\n", acct.ID, acct.Acct)
	botAcct = *acct
	return acct.ID, nil
}

// Setup initializes the Gemini AI model with the provided API key
func Setup(apiKey string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if config.LLM.Provider != "gemini" {
		return nil
	}

	if client == nil {
		var err error
		client, err = genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return err
		}
	}

	if geminiModelName == "" {
		geminiModelName = config.Gemini.Model
	}
	if geminiGenerationConfig == nil {
		geminiGenerationConfig = cloneGenerateContentConfig(&genai.GenerateContentConfig{
			Temperature: genai.Ptr(config.Gemini.Temperature),
			TopK:        genai.Ptr(float32(config.Gemini.TopK)),
		})
	}

	return nil
}

// handleMention processes incoming mentions and generates alt-text descriptions
func handleMention(c *mastodon.Client, notification *mastodon.Notification) {
	if isDNI(&notification.Account) {
		return
	}

	originalStatus := notification.Status.InReplyToID
	if originalStatus == nil {
		return
	}

	var originalStatusID mastodon.ID

	switch id := originalStatus.(type) {
	case string:
		originalStatusID = mastodon.ID(id)
	case mastodon.ID:
		originalStatusID = id
	default:
		log.Printf("Unexpected type for InReplyToID: %T", originalStatus)
	}

	status, err := c.GetStatus(ctx, originalStatusID)
	if err != nil {
		log.Printf("Error fetching original status: %v", err)
		return
	}

	//Check if the original status has any media attachments
	if len(status.MediaAttachments) == 0 {
		return
	}

	// Check if the person who mentioned the bot is the OP
	if status.Account.ID == notification.Account.ID {
		userID := string(notification.Account.ID)
		// If user hasn't provided GDPR consent, request it first
		if !HasUserConsent(userID) {
			log.Printf("User %s has not provided GDPR consent, requesting it", notification.Account.Acct)

			_, err := RequestGDPRConsent(c, userID, notification.Account.Acct, notification.Status.Language, notification.Status.ID, false)
			if err != nil {
				log.Printf("Error requesting GDPR consent: %v", err)
			}
			return
		}
		generateAndPostAltText(c, status, notification.Status.ID)
	} else if !config.Behavior.AskForConsent {
		generateAndPostAltText(c, status, notification.Status.ID)
	} else {
		requestConsent(c, status, notification)
	}
}

// requestConsent asks the original poster for consent to generate alt text
func requestConsent(c *mastodon.Client, status *mastodon.Status, notification *mastodon.Notification) {
	// Check if every image in the post already has a Alt text
	hasAltText := true

	for _, attachment := range status.MediaAttachments {
		if attachment.Description == "" && (attachment.Type == "image" || ((attachment.Type == "video" || attachment.Type == "gifv" && videoProcessingCapability) || (attachment.Type == "audio" && audioProcessingCapability))) {
			hasAltText = false
		}
	}

	if hasAltText {
		return
	}

	// Check if the original poster has already been asked for consent
	if _, ok := consentRequests[status.ID]; ok {
		return
	}

	consentRequests[status.ID] = ConsentRequest{
		RequestID: notification.Status.ID,
		Timestamp: time.Now(),
	}

	message := fmt.Sprintf("@%s "+getLocalizedString(notification.Status.Language, "consentRequest", "response"), status.Account.Acct, notification.Account.Acct)
	_, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:      message,
		InReplyToID: status.ID,
		Visibility:  status.Visibility,
		Language:    notification.Status.Language,
	})
	if err != nil {
		log.Printf("Error posting consent request: %v", err)
	}

	if err := saveConsentRequestsToFile("consent_requests.json"); err != nil {
		log.Printf("Error saving consent requests: %v", err)
	}
}

// handleConsentResponse processes the consent response from the original poster
func handleConsentResponse(c *mastodon.Client, ID mastodon.ID, consentStatus *mastodon.Status) {
	originalStatusID := ID
	status, err := c.GetStatus(ctx, originalStatusID)
	if err != nil {
		log.Printf("Error fetching original status for ID %s: %v", originalStatusID, err)
		return
	}

	if consentStatus.Account.Acct != status.Account.Acct {
		log.Printf("Unauthorized consent response from: %s, expected: %s", consentStatus.Account.Acct, status.Account.Acct)
		return
	}

	// Clean up HTML content to extract plain text
	plainTextContent := stripHTMLTags(consentStatus.Content)
	log.Printf("Cleaned consent content: %q from user: %s", plainTextContent, consentStatus.Account.Acct)

	if plainTextContent == "" {
		log.Printf("No content in consent response from: %s", consentStatus.Account.Acct)
		return
	}

	// Split content into words and check the last word
	consentResponse := strings.Fields(plainTextContent)
	if len(consentResponse) == 0 {
		log.Printf("Empty content after stripping HTML.")
		return
	}
	lastWord := strings.ToLower(consentResponse[len(consentResponse)-1])
	log.Printf("Extracted last word: %q from cleaned content", lastWord)

	if lastWord == "y" || lastWord == "yes" {
		log.Printf("Consent granted by the original poster: %s", consentStatus.Account.Acct)
		generateAndPostAltText(c, status, consentStatus.ID)
		metricsManager.logConsentRequest(string(status.Account.ID), true)
	} else {
		log.Printf("Consent denied based on last word: %q from user: %s", lastWord, consentStatus.Account.Acct)
		metricsManager.logConsentRequest(string(status.Account.ID), false)
	}

	delete(consentRequests, originalStatusID)
	log.Printf("Removed consent request for ID %s after processing", originalStatusID)

	if err := saveConsentRequestsToFile("consent_requests.json"); err != nil {
		log.Printf("Error saving consent requests: %v", err)
	}
}

// isDNI checks if an account meets the Do Not Interact (DNI) conditions
func isDNI(account *mastodon.Account) bool {
	dniList := config.DNI.Tags

	if account.Acct == config.Server.Username {
		return true
	} else if account.Bot && config.DNI.IgnoreBots {
		return true
	}

	for _, tag := range dniList {
		if strings.Contains(account.Note, tag) {
			return true
		}
	}

	return false
}

// handleFollow processes new follows and follows back
func handleFollow(c *mastodon.Client, notification *mastodon.Notification) {
	userID := string(notification.Account.ID)

	// Check if the user has already provided GDPR consent
	if !HasUserConsent(userID) {
		// Send a welcome message with GDPR consent request
		log.Printf("New follower %s, sending GDPR consent request", notification.Account.Acct)

		// Now send the GDPR consent request as a reply to our welcome message
		_, err := RequestGDPRConsent(c, userID, notification.Account.Acct, "en", mastodon.ID(""), true) // Hardcoded to English cuz we don't have the user's language
		if err != nil {
			log.Printf("Error requesting GDPR consent: %v", err)
		}

	}

	if config.Behavior.FollowBack {
		_, err := c.AccountFollow(ctx, notification.Account.ID)
		if err != nil {
			log.Printf("Error following back: %v", err)
			return
		}
		LogEvent("new_follower")
		metricsManager.logFollow(string(notification.Account.ID))
		fmt.Printf("Followed back: %s\n", notification.Account.Acct)
	}
}

// handleUpdate processes new posts and generates alt-text descriptions if missing
func handleUpdate(c *mastodon.Client, status *mastodon.Status) {
	if status.Account.Acct == config.Server.Username {
		return
	}

	userID := string(status.Account.ID)

	for _, attachment := range status.MediaAttachments {
		if attachment.Type == "image" || ((attachment.Type == "video" || attachment.Type == "gifv" && videoProcessingCapability) || (attachment.Type == "audio" && audioProcessingCapability)) {
			if attachment.Description == "" {

				if !HasUserConsent(userID) {
					// Send a GDPR consent request
					_, err := RequestGDPRConsent(c, userID, status.Account.Acct, status.Language, status.ID, false)
					if err != nil {
						log.Printf("Error requesting GDPR consent: %v", err)
					}
					return
				}
				generateAndPostAltText(c, status, status.ID)
				break
			} else {
				LogEventWithUsername("human_written_alt_text", status.Account.Acct)
			}
		}
	}
}

// generateAndPostAltText generates alt-text for images and posts it as a reply
func generateAndPostAltText(c *mastodon.Client, status *mastodon.Status, replyToID mastodon.ID) {
	replyPost, err := c.GetStatus(ctx, replyToID)
	if err != nil {
		log.Printf("Error fetching reply status: %v", err)
		return
	}

	metricsManager.logRequest(string(replyPost.Account.ID))

	var wg sync.WaitGroup
	var mu sync.Mutex
	var responses []string
	sucessCount := 0
	altTextGenerated := false
	altTextAlreadyExists := false

	// Track total processing time for power calculation
	var totalProcessingTimeMs int64
	var isLocalModel bool = config.LLM.Provider != "gemini"

	for _, attachment := range status.MediaAttachments {
		wg.Add(1)
		go func(attachment mastodon.Attachment) {
			defer wg.Done()
			var altText string
			var err error

			start := time.Now()

			// Check if the user has exceeded their rate limit
			if !rateLimiter.Increment(c, string(replyPost.Account.ID)) {
				log.Printf("User @%s has exceeded their rate limit", replyPost.Account.Acct)
				metricsManager.logRateLimitHit(string(replyPost.Account.ID))
				mu.Lock()
				responses = append(responses, getLocalizedString(replyPost.Language, "altTextError", "response"))
				mu.Unlock()
				return
			}

			if attachment.Type == "image" && attachment.Description == "" {
				altText, err = generateImageAltText(attachment.URL, replyPost.Language)
			} else if (attachment.Type == "video" || attachment.Type == "gifv") && videoProcessingCapability && attachment.Description == "" {
				altText, err = generateVideoAltText(attachment.URL, replyPost.Language)
			} else if attachment.Type == "audio" && audioProcessingCapability && attachment.Description == "" {
				altText, err = generateAudioAltText(attachment.URL, replyPost.Language)
			} else if attachment.Description != "" {
				if !altTextGenerated && !altTextAlreadyExists {
					mu.Lock()
					responses = append(responses, getLocalizedString(replyPost.Language, "imageAlreadyHasAltText", "response"))
					mu.Unlock()
					altTextAlreadyExists = true
				}
				return
			} else if videoProcessingCapability && audioProcessingCapability {
				mu.Lock()
				responses = append(responses, getLocalizedString(replyPost.Language, "unsupportedFile", "response"))
				mu.Unlock()
				return
			}

			if err != nil {
				log.Printf("Error generating alt-text: %v", err)
				sucessCount -= 1
				altText = getLocalizedString(replyPost.Language, "altTextError", "response")
			} else if altText == "" {
				log.Printf("Error generating alt-text: Empty response")
				sucessCount -= 1
				altText = getLocalizedString(replyPost.Language, "altTextError", "response")
			}

			elapsed := time.Since(start).Milliseconds()

			mu.Lock()
			responses = append(responses, altText)
			totalProcessingTimeMs += elapsed
			mu.Unlock()

			sucessCount += 1

			// Log metrics for successful generation
			metricsManager.logSuccessfulGeneration(string(replyPost.Account.ID), attachment.Type, elapsed, replyPost.Language)
		}(attachment)
	}

	wg.Wait()

	altTextGenerated = sucessCount > 0

	// Combine all responses with a separator
	combinedResponse := strings.Join(responses, "\n―\n")

	// Prepare the content warning for the reply
	contentWarning := status.SpoilerText
	if contentWarning != "" && !strings.HasPrefix(contentWarning, "re:") {
		contentWarning = "re: " + contentWarning
	}

	// Add mention to the original poster at the start
	combinedResponse = fmt.Sprintf("@%s %s", replyPost.Account.Acct, combinedResponse)

	// Add provider attribution
	if altTextGenerated {
		combinedResponse = fmt.Sprintf("%s\n\n%s", combinedResponse, getProviderAttribution(config, replyPost.Language))
	}

	// Add power consumption information at the end if enabled and using a local model
	if config.PowerMetrics.Enabled && isLocalModel && altTextGenerated {
		powerConsumption := calculatePowerConsumption(totalProcessingTimeMs, config.PowerMetrics.GPUWatts)
		powerInfo := fmt.Sprintf("\n\n"+getLocalizedString(replyPost.Language, "energyUsageMessage", "response"), powerConsumption)
		combinedResponse += powerInfo
	}

	// Post the combined response
	if combinedResponse != "" {
		visibility := replyPost.Visibility

		// Map the visibility of the reply based on the original post and the bot's settings
		switch strings.ToLower(config.Behavior.ReplyVisibility + "," + replyPost.Visibility) {
		case "public,public":
			visibility = "public"
		case "public,unlisted":
			visibility = "unlisted"
		case "public,private":
			visibility = "private"
		case "public,direct":
			visibility = "direct"
		case "unlisted,public":
			visibility = "unlisted"
		case "unlisted,unlisted":
			visibility = "unlisted"
		case "unlisted,private":
			visibility = "private"
		case "unlisted,direct":
			visibility = "direct"
		case "private,public":
			visibility = "private"
		case "private,unlisted":
			visibility = "private"
		case "private,private":
			visibility = "private"
		case "private,direct":
			visibility = "direct"
		case "direct,public":
			visibility = "direct"
		case "direct,unlisted":
			visibility = "direct"
		case "direct,private":
			visibility = "direct"
		case "direct,direct":
			visibility = "direct"
		}

		if replyPost.Visibility == "private" {
			visibility = "direct"
		}

		reply, err := c.PostStatus(ctx, &mastodon.Toot{
			Status:      combinedResponse,
			InReplyToID: replyToID,
			Visibility:  visibility,
			Language:    replyPost.Language,
			SpoilerText: contentWarning,
		})

		if err != nil {
			log.Printf("Error posting reply: %v", err)
			_, err = c.PostStatus(ctx, &mastodon.Toot{
				Status:      getLocalizedString(replyPost.Language, "replyError", "response"),
				InReplyToID: replyToID,
				Visibility:  visibility,
			})
			if err != nil {
				log.Printf("What the fuck happened here....")
			}
		}

		if config.AltTextReminders.Enabled && visibility != "direct" && HasUserConsent(string(replyPost.Account.ID)) {
			queuePostForAltTextCheck(status, string(replyPost.Account.ID))
		}

		if reply != nil {
			// Track the reply with a timestamp
			mapMutex.Lock()
			replyMap[status.ID] = ReplyInfo{ReplyID: reply.ID, Timestamp: time.Now()}
			mapMutex.Unlock()
		}
	}
}

// downloadToTempFile downloads a file from a given URL and saves it to a temporary file.
// It returns the path to the temporary file.
func downloadToTempFile(fileURL, prefix, extension string) (string, error) {
	// Download the file from the remote URL
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check the Content-Length header
	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "" {
		size, err := strconv.ParseInt(contentLength, 10, 64)
		if err == nil && size > int64(config.ImageProcessing.MaxSizeMB*1024*1024) {
			return "", fmt.Errorf("file size exceeds maximum limit of %d MB", config.ImageProcessing.MaxSizeMB)
		}
	}

	// Read the file content
	fileData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Create a temporary file to save the content
	tmpFile, err := os.CreateTemp("", prefix+"-*."+extension)
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Write the file data to the temporary file
	if _, err := tmpFile.Write(fileData); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

// generateImageAltText generates alt-text for an image using Gemini AI or Ollama
func generateImageAltText(imageURL string, lang string) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "" {
		size, err := strconv.ParseInt(contentLength, 10, 64)
		if err == nil && size > int64(config.ImageProcessing.MaxSizeMB*1024*1024) {
			return "", fmt.Errorf("file size exceeds maximum limit of %d MB", config.ImageProcessing.MaxSizeMB)
		}
	}

	img, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Downscale the image to a smaller width using config settings
	downscaledImg, format, err := downscaleImage(img, config.ImageProcessing.DownscaleWidth)
	if err != nil {
		return "", err
	}

	LogEvent("alt_text_generated")

	prompt := getLocalizedString(lang, "generateAltText", "prompt")

	fmt.Println("Processing image: " + imageURL)

	altText, err := llmProvider.GenerateAltText(prompt, downscaledImg, format, lang)
	if err != nil {
		return "", err
	}

	return postProcessAltText(altText), nil
}

// generateVideoAltText generates alt-text for a video using the configured LLM provider
func generateVideoAltText(videoURL string, lang string) (string, error) {
	resp, err := http.Get(videoURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "" {
		size, err := strconv.ParseInt(contentLength, 10, 64)
		if err == nil && size > int64(config.VideoProcessing.MaxSizeMB*1024*1024) {
			return "", fmt.Errorf("video file size exceeds maximum limit of %d MB", config.VideoProcessing.MaxSizeMB)
		}
	}

	videoData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	LogEvent("video_alt_text_generated")

	prompt := getLocalizedString(lang, "generateVideoAltText", "prompt")

	fmt.Println("Processing video: " + videoURL)

	// Determine the video format from URL or content type
	format := "mp4" // Default
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "video/") {
		format = strings.TrimPrefix(contentType, "video/")
	} else if strings.Contains(videoURL, ".") {
		parts := strings.Split(videoURL, ".")
		possibleFormat := parts[len(parts)-1]
		if isVideoFormat(possibleFormat) {
			format = possibleFormat
		}
	}

	altText, err := llmProvider.GenerateVideoAltText(prompt, videoData, format, lang)
	if err != nil {
		return "", err
	}

	return postProcessAltText(altText), nil
}

// isVideoFormat checks if the given string is a known video format extension
func isVideoFormat(format string) bool {
	videoFormats := []string{"mp4", "webm", "mov", "avi", "mkv", "m4v", "3gp"}
	format = strings.ToLower(format)
	for _, f := range videoFormats {
		if format == f {
			return true
		}
	}
	return false
}

// generateAudioAltText generates alt-text for an audio file using Gemini AI
func generateAudioAltText(audioURL string, lang string) (string, error) {
	prompt := getLocalizedString(lang, "generateAudioAltText", "prompt")

	fmt.Println("Processing audio: " + audioURL)

	// Use the helper function to download the audio
	audioFilePath, err := downloadToTempFile(audioURL, "audio", "mp3")
	if err != nil {
		return "", err
	}
	defer os.Remove(audioFilePath) // Clean up the file afterwards

	LogEvent("audio_alt_text_generated")

	// Pass the local temporary file path to GenerateAudioAltWithGemini
	return GenerateAudioAltWithGemini(prompt, audioFilePath)
}

// Generate creates a response using the Gemini AI model
func GenerateImageAltWithGemini(strPrompt string, image []byte, fileExtension string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return "", fmt.Errorf("gemini client not initialized")
	}
	if geminiModelName == "" {
		geminiModelName = config.Gemini.Model
	}
	mimeType, err := inferImageMIME(fileExtension)
	if err != nil {
		return "", err
	}
	parts := []*genai.Part{
		{Text: strPrompt},
		{InlineData: &genai.Blob{Data: image, MIMEType: mimeType}},
	}
	contents := []*genai.Content{{Parts: parts}}

	fmt.Println("Generating content...")

	resp, err := client.Models.GenerateContent(ctx, geminiModelName, contents, cloneGenerateContentConfig(geminiGenerationConfig))
	if err != nil {
		return "", err
	}
	return postProcessAltText(getResponse(resp)), nil
}

// GenerateVideoAltWithGemini generates alt-text for a video using the Gemini AI model
func GenerateVideoAltWithGemini(strPrompt string, videoFilePath string) (string, error) {
	// Open the temporary video file
	videoFile, err := os.Open(videoFilePath)
	if err != nil {
		return "", err
	}
	defer videoFile.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return "", fmt.Errorf("gemini client not initialized")
	}
	if geminiModelName == "" {
		geminiModelName = config.Gemini.Model
	}
	mimeType, err := inferMIMEFromExtension(filepath.Ext(videoFilePath), "video")
	if err != nil {
		return "", err
	}

	uploadedFile, err := client.Files.Upload(ctx, videoFile, &genai.UploadFileConfig{
		DisplayName: "Video for Alt-Text",
		MIMEType:    mimeType,
	})
	if err != nil {
		return "", err
	}

	// Poll until the file is in the ACTIVE state
	response := uploadedFile
	for response.State == genai.FileStateProcessing {
		time.Sleep(1 * time.Second)
		response, err = client.Files.Get(ctx, response.Name, nil)
		if err != nil {
			return "", err
		}
	}

	// Create a prompt using the text and the URI reference for the uploaded file
	parts := []*genai.Part{
		{FileData: &genai.FileData{FileURI: response.URI, MIMEType: response.MIMEType}},
		{Text: strPrompt},
	}
	contents := []*genai.Content{{Parts: parts}}

	resp, err := client.Models.GenerateContent(ctx, geminiModelName, contents, cloneGenerateContentConfig(geminiGenerationConfig))
	if err != nil {
		return "", err
	}

	// Handle the response of generated text
	return postProcessAltText(getResponse(resp)), nil
}

// GenerateAudioAltWithGemini generates alt-text for an audio file using the Gemini AI model
func GenerateAudioAltWithGemini(strPrompt string, audioFilePath string) (string, error) {
	// Open the temporary audio file
	audioFile, err := os.Open(audioFilePath)
	if err != nil {
		return "", err
	}
	defer audioFile.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return "", fmt.Errorf("gemini client not initialized")
	}
	if geminiModelName == "" {
		geminiModelName = config.Gemini.Model
	}
	mimeType, err := inferMIMEFromExtension(filepath.Ext(audioFilePath), "audio")
	if err != nil {
		return "", err
	}

	uploadedFile, err := client.Files.Upload(ctx, audioFile, &genai.UploadFileConfig{
		DisplayName: "Audio for Alt-Text",
		MIMEType:    mimeType,
	})
	if err != nil {
		return "", err
	}

	// Poll until the file is in the ACTIVE state
	response := uploadedFile
	for response.State == genai.FileStateProcessing {
		time.Sleep(10 * time.Second)
		response, err = client.Files.Get(ctx, response.Name, nil)
		if err != nil {
			return "", err
		}
	}

	// Create a prompt using the text and the URI reference for the uploaded file
	parts := []*genai.Part{
		{FileData: &genai.FileData{FileURI: response.URI, MIMEType: response.MIMEType}},
		{Text: strPrompt},
	}
	contents := []*genai.Content{{Parts: parts}}

	resp, err := client.Models.GenerateContent(ctx, geminiModelName, contents, cloneGenerateContentConfig(geminiGenerationConfig))
	if err != nil {
		return "", err
	}

	// Handle the response of generated text
	return postProcessAltText(getResponse(resp)), nil
}

// downscaleImage resizes the image to the specified width while maintaining the aspect ratio
// and converts it to PNG or JPEG if it is in a different format.
func downscaleImage(imgData []byte, width uint) ([]byte, string, error) {
	img, format, err := decodeImage(imgData)
	if err != nil {
		return nil, "", err
	}

	// Resize the image to the specified width while maintaining the aspect ratio
	resizedImg := resize.Resize(width, 0, img, resize.Lanczos3)

	// Convert the image to PNG or JPEG if it is in a different format
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, resizedImg, nil)
		format = "jpeg"
	case "png":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "gif":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "bmp":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "tiff":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "webp":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	default:
		return nil, "", fmt.Errorf("unsupported image format: %s", format)
	}

	if err != nil {
		return nil, "", err
	}

	return buf.Bytes(), format, nil
}

// decodeImage decodes an image from bytes and returns the image and its format
func decodeImage(imgData []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, format, nil
	}

	// Try decoding as WebP if the standard decoding fails
	img, err = webp.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "webp", nil
	}

	// Try decoding as BMP if the previous decodings fail
	img, err = bmp.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "bmp", nil
	}

	// Try decoding as TIFF if the previous decodings fail
	img, err = tiff.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "tiff", nil
	}

	// Try decoding as GIF if the previous decodings fail
	img, err = gif.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "gif", nil
	}

	return nil, "", fmt.Errorf("unsupported image format: %v", err)
}

// getResponse extracts the text response from the AI model's output
func getResponse(resp *genai.GenerateContentResponse) string {
	var response string
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				str := fmt.Sprintf("%v", part)
				response += str
			}
		}
	}
	return response
}

// postProcessAltText cleans up the alt-text by removing unwanted introductory phrases.
func postProcessAltText(altText string) string {
	// Define a regex pattern to match introductory phrases
	// This pattern matches phrases like "Here's alt text describing the image:" or "Here's alt text for the image:"
	pattern := `(?i)here's alt text (describing|for) the (image|video|audio):?\s*`

	// Compile the regex
	re := regexp.MustCompile(pattern)

	// Use the regex to replace matches with an empty string
	altText = re.ReplaceAllString(altText, "")

	// Remove any mentions
	altText = strings.ReplaceAll(altText, "@", "[@]")

	// Remove any leading or trailing whitespace
	altText = strings.TrimSpace(altText)

	return altText
}

// checkOllamaModel checks if the Ollama model is available and working
func checkOllamaModel() error {
	cmd := exec.Command("ollama", "list")

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return err
	}

	if !strings.Contains(out.String(), config.LLM.OllamaModel) {
		return fmt.Errorf("ollama model not found: %s\nInstall it via:\nollama run %s", config.LLM.OllamaModel, config.LLM.OllamaModel)
	}

	return nil
}

// Struct to store reply information with a timestamp
type ReplyInfo struct {
	ReplyID   mastodon.ID
	Timestamp time.Time
}

var replyMap = make(map[mastodon.ID]ReplyInfo)
var mapMutex sync.Mutex

func handleDeleteEvent(c *mastodon.Client, originalID mastodon.ID) {
	mapMutex.Lock()
	defer mapMutex.Unlock()

	if replyInfo, exists := replyMap[originalID]; exists {
		// Delete Altbot's reply
		err := c.DeleteStatus(ctx, replyInfo.ReplyID)
		if err != nil {
			log.Printf("Error deleting reply: %v", err)
		} else {
			log.Printf("Deleted reply for original post ID: %v", originalID)
			delete(replyMap, originalID)
		}
	}
}

func cleanupOldEntries() {
	for {
		time.Sleep(10 * time.Minute) // Run cleanup every 10 minutes

		mapMutex.Lock()
		for originalID, replyInfo := range replyMap {
			if time.Since(replyInfo.Timestamp) > time.Hour {
				delete(replyMap, originalID)
			}
		}
		mapMutex.Unlock()
	}
}

type RateLimiter struct {
	MinuteCounts   map[string]int       `json:"minute_counts"`
	HourCounts     map[string]int       `json:"hour_counts"`
	AccountAges    map[string]time.Time `json:"account_ages"`
	mu             sync.Mutex
	ExceededCounts map[string]int  `json:"exceeded_counts"`
	ShadowBanned   map[string]bool `json:"shadow_banned"`
	Whitelist      map[string]bool `json:"whitelist"`
}

// NewRateLimiter creates a new RateLimiter
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		MinuteCounts:   make(map[string]int),
		HourCounts:     make(map[string]int),
		AccountAges:    make(map[string]time.Time),
		ExceededCounts: make(map[string]int),
		ShadowBanned:   make(map[string]bool),
		Whitelist:      make(map[string]bool),
	}
}

// IsNewAccount checks if the user account age is within the new account period
func (rl *RateLimiter) IsNewAccount(c *mastodon.Client, userID string) bool {
	creationDate, exists := rl.AccountAges[userID]
	if !exists {
		// Fetch the account creation date if it doesn't exist
		account, err := c.GetAccount(ctx, mastodon.ID(userID))
		if err != nil {
			log.Printf("Error fetching account: %v", err)
			return false
		}

		creationDate = account.CreatedAt
		rl.AccountAges[userID] = creationDate
	}
	log.Printf("Account creation date: %v", creationDate)
	return time.Since(creationDate).Hours() < 24*float64(config.RateLimit.NewAccountPeriodDays)
}

// Increment increments the request count for a user and checks limits
func (rl *RateLimiter) Increment(c *mastodon.Client, userID string) bool {
	if !config.RateLimit.Enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	isBanned := rl.IsShadowBanned(userID)
	if isBanned {
		log.Printf("User %s is shadow banned: %v", userID, isBanned)
		return false
	}

	defer func() {
		if err := rateLimiter.SaveToFile("ratelimiter.json"); err != nil {
			log.Printf("Error saving rate limiter state: %v", err)
		}
	}()

	isNew := rl.IsNewAccount(c, userID)

	if isNew {
		log.Printf("Sussy baka New account!!1!1!! feds get his ass: %s", userID)
		metricsManager.logNewAccountActivity(string(userID))
	}

	// Determine limits based on account age
	maxPerMinute := config.RateLimit.MaxRequestsPerMinute
	maxPerHour := config.RateLimit.MaxRequestsPerHour
	if isNew {
		maxPerMinute = config.RateLimit.NewAccountMaxRequestsPerMinute
		maxPerHour = config.RateLimit.NewAccountMaxRequestsPerHour
	}

	// Check per-minute limit
	if rl.MinuteCounts[userID] >= maxPerMinute {
		rl.ExceededCounts[userID]++
		if rl.ExceededCounts[userID] >= config.RateLimit.ShadowBanThreshold {
			rl.ShadowBanUser(c, userID)
		}
		return false
	}

	// Check per-hour limit
	if rl.HourCounts[userID] >= maxPerHour {
		rl.ExceededCounts[userID]++
		if rl.ExceededCounts[userID] >= config.RateLimit.ShadowBanThreshold {
			rl.ShadowBanUser(c, userID)
		}
		return false
	}

	rl.MinuteCounts[userID]++
	rl.HourCounts[userID]++
	return true
}

func (rl *RateLimiter) ShadowBanUser(c *mastodon.Client, userID string) {
	if rl.Whitelist[userID] {
		return
	}

	log.Printf("Get shadow banned noob %s", userID)
	rl.ShadowBanned[userID] = true
	metricsManager.logShadowBan(string(userID))
	rl.notifyAdmin(c, userID)
}

func (rl *RateLimiter) IsShadowBanned(userID string) bool {
	return rl.ShadowBanned[userID]
}

func (rl *RateLimiter) notifyAdmin(c *mastodon.Client, userID string) {
	account, err := c.GetAccount(ctx, mastodon.ID(userID))
	if err != nil {
		log.Printf("Error fetching account: %v", err)
		return
	}
	name := account.Acct

	message := fmt.Sprintf("%s User %s has been shadow banned for exceeding rate limits.\nTo unban, reply with 'unban %s'.", config.RateLimit.AdminContactHandle, name, userID)
	_, err = c.PostStatus(ctx, &mastodon.Toot{
		Status:     message,
		Visibility: "direct",
	})
	if err != nil {
		log.Printf("Error posting shadow ban notification: %v", err)
	}
}

func (rl *RateLimiter) UnbanAndWhitelistUser(userID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.ShadowBanned, userID)
	rl.Whitelist[userID] = true

	log.Printf("User %s has been unbanned and added to the whitelist.", userID)

	if err := rateLimiter.SaveToFile("ratelimiter.json"); err != nil {
		log.Printf("Error saving rate limiter state: %v", err)
	}
}

func handleAdminReply(c *mastodon.Client, reply *mastodon.Status, rl *RateLimiter) {
	content := stripHTMLTags(reply.Content)
	content = strings.ToLower(content)

	parts := strings.Fields(content)
	if len(parts) == 3 && parts[1] == "unban" {
		userID := parts[2]
		rl.UnbanAndWhitelistUser(userID)
		log.Printf("Admin unbanned user %s based on reply.", userID)
		metricsManager.logUnBan(string(userID))
		_, err := c.PostStatus(ctx, &mastodon.Toot{
			Status:      fmt.Sprintf("%s User %s has been unbanned and added to the whitelist.", config.RateLimit.AdminContactHandle, userID),
			Visibility:  "direct",
			InReplyToID: reply.ID,
		})
		if err != nil {
			log.Printf("Error sending confirmation of unban: %v", err)
		}
	}
}

// ResetMinuteCounts resets the per-minute request counts for all users
func (rl *RateLimiter) ResetMinuteCounts() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	for userID := range rl.MinuteCounts {
		rl.MinuteCounts[userID] = 0
	}
}

// ResetHourCounts resets the per-hour request counts for all users
func (rl *RateLimiter) ResetHourCounts() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	for userID := range rl.HourCounts {
		rl.HourCounts[userID] = 0
	}

	for userID := range rl.ExceededCounts {
		rl.ExceededCounts[userID] = 0
	}
}

func (rl *RateLimiter) LoadFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File does not exist. Start fresh.
		}
		return err
	}
	return json.Unmarshal(data, rl)
}

func (rl *RateLimiter) SaveToFile(filePath string) error {
	data, err := json.Marshal(rl)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// ConsentRequest struct to store consent requests
type ConsentRequest struct {
	RequestID mastodon.ID
	Timestamp time.Time
}

func saveConsentRequestsToFile(filePath string) error {
	data, err := json.Marshal(consentRequests)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func loadConsentRequestsFromFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, so initialize an empty map
			consentRequests = make(map[mastodon.ID]ConsentRequest)
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &consentRequests); err != nil {
		return err
	}

	return nil
}

func cleanupOldConsentRequests() {
	for id, request := range consentRequests {
		if time.Since(request.Timestamp) > 30*24*time.Hour { // 30 days
			delete(consentRequests, id)
		}
	}
}

// stripHTMLTags extracts and returns plain text from HTML content
func stripHTMLTags(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		log.Printf("Error parsing HTML: %v", err)
		return htmlContent // Return unchanged if parsing fails
	}
	return extractText(doc)
}

// extractText recursively extracts text from an HTML node
func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += extractText(c)
	}
	return text
}

func getStatusSymbol(enabled bool) string {
	if enabled {
		return Green + "✓" + Reset
	}
	return Red + "✗" + Reset
}

func checkForUpdates() {
	latestVersion := fetchLatestVersion()
	if latestVersion == "" {
		return
	}

	// Remove 'v' prefix if present
	currentVer := strings.TrimPrefix(Version, "v")
	latestVer := strings.TrimPrefix(latestVersion, "v")

	// Split versions into parts
	currentParts := strings.Split(currentVer, ".")
	latestParts := strings.Split(latestVer, ".")

	// Convert to integers for comparison
	current := make([]int, len(currentParts))
	latest := make([]int, len(latestParts))

	for i, v := range currentParts {
		current[i], _ = strconv.Atoi(v)
	}
	for i, v := range latestParts {
		latest[i], _ = strconv.Atoi(v)
	}

	// Compare versions
	var comparison int
	for i := 0; i < len(current) && i < len(latest); i++ {
		if current[i] < latest[i] {
			comparison = -1
			break
		} else if current[i] > latest[i] {
			comparison = 1
			break
		}
	}

	// If all parts are equal but one version has more parts, the longer one is newer
	if comparison == 0 && len(current) != len(latest) {
		if len(current) < len(latest) {
			comparison = -1
		} else {
			comparison = 1
		}
	}

	// Print appropriate message based on comparison
	if comparison < 0 {
		fmt.Printf("New version %s available! Visit: https://github.com/micr0-dev/Altbot/releases\n", latestVersion)
	} else if comparison == 0 {
		fmt.Println("Altbot is up-to-date.")
	} else {
		fmt.Println("Wowie~ ur using a newer version than the latest release! UwU u must be a developer or something!~")
	}
}

func fetchLatestVersion() string {
	resp, err := http.Get("https://api.github.com/repos/micr0-dev/Altbot/releases/latest")
	if err != nil {
		log.Printf("Error fetching latest version: %v", err)
		return ""
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("Error decoding JSON: %v", err)
		return ""
	}

	return release.TagName
}

// Check up on requests for alt text requests, to make sure people are adding them to their posts instead of just leaving them as a comment.

type AltTextCheck struct {
	PostID    mastodon.ID
	UserID    string
	Timestamp time.Time
}

var altTextChecks = make(map[mastodon.ID]AltTextCheck)

type AltTextReminderTracker struct {
	LastReminded map[string]time.Time
	mu           sync.Mutex
}

var altTextReminderTracker = AltTextReminderTracker{
	LastReminded: make(map[string]time.Time),
}

func shouldSendReminder(userID string) bool {
	altTextReminderTracker.mu.Lock()
	defer altTextReminderTracker.mu.Unlock()

	lastReminded, exists := altTextReminderTracker.LastReminded[userID]

	if !exists || time.Since(lastReminded) >= 24*time.Hour {
		altTextReminderTracker.LastReminded[userID] = time.Now()
		return true
	}

	return false
}

func queuePostForAltTextCheck(post *mastodon.Status, userID string) {
	altTextChecks[post.ID] = AltTextCheck{
		PostID:    post.ID,
		UserID:    userID,
		Timestamp: time.Now(),
	}
}

func checkAltTextPeriodically(c *mastodon.Client, interval time.Duration, checkTime time.Duration) {
	for {
		time.Sleep(interval)
		now := time.Now()

		for postID, check := range altTextChecks {
			// Check if time has passed
			if now.Sub(check.Timestamp) >= checkTime {
				// Fetch post details
				post, err := c.GetStatus(ctx, check.PostID)
				if err != nil {
					log.Printf("Error fetching post %s during alt-text check. Deleting from queue: %v", check.PostID, err)
					delete(altTextChecks, postID)
					continue
				}

				// Check if the post still lacks alt-text
				missingAltText := false
				for _, media := range post.MediaAttachments {
					if media.Description == "" {
						missingAltText = true
						break
					}
				}

				if missingAltText {
					log.Printf("Notifying user %s about missing alt-text in post %s...", check.UserID, check.PostID)
					metricsManager.logMissingAltText(string(check.UserID))
					if shouldSendReminder(check.UserID) {
						username := post.Account.Acct
						notifyUserOfMissingAltText(c, post, username)
						metricsManager.logAltTextReminderSent(string(check.UserID))
					}
				}

				// Remove check entry after processing
				delete(altTextChecks, postID)
			}
		}
	}
}

func notifyUserOfMissingAltText(c *mastodon.Client, post *mastodon.Status, userID string) {
	message := fmt.Sprintf(getLocalizedString(post.Language, "altTextReminder", "response"), userID)

	_, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:      message,
		InReplyToID: post.ID,
		Visibility:  "direct",
	})
	if err != nil {
		log.Printf("Error notifying user %s about missing alt-text: %v", userID, err)
	}
}

// copyConfig copies a configuration file from src to dest, removing the first `skipLines` lines from src.
func copyConfig(src, dest string, skipLines int) error {
	// Open the source file for reading
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error opening source file for reading: %w", err)
	}
	defer sourceFile.Close()

	// Create the destination file for writing
	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("error creating destination file: %w", err)
	}
	defer destFile.Close()

	scanner := bufio.NewScanner(sourceFile)
	writer := bufio.NewWriter(destFile)

	// Skip the specified number of lines
	for i := 0; i < skipLines; i++ {
		if !scanner.Scan() {
			// If EOF is reached before skipping all lines, no copying is needed
			return nil
		}
	}

	// Write the rest of the file to the destination file
	for scanner.Scan() {
		_, err := writer.WriteString(scanner.Text() + "\n")
		if err != nil {
			return fmt.Errorf("error writing to destination file: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading from source file: %w", err)
	}

	// Flush the writer to ensure all content is written
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("error flushing writer: %w", err)
	}

	return nil
}

func compareConfigs(defaultConfig, userConfig Config) int {
	customCount := 0
	warnings := []string{}

	checkDifferences(reflect.ValueOf(defaultConfig), reflect.ValueOf(userConfig), "", &customCount, &warnings)

	if len(warnings) > 0 {
		fmt.Printf("Warnings:\n%s\n", warnings)
	}

	return customCount
}

func checkDifferences(d, u reflect.Value, prefix string, customCount *int, warnings *[]string) {
	dKind, uKind := d.Kind(), u.Kind()

	if dKind != uKind {
		*warnings = append(*warnings, fmt.Sprintf("Type mismatch at %s: default is %s, user is %s", prefix, dKind, uKind))
		return
	}

	switch dKind {
	case reflect.Struct:
		for i := 0; i < d.NumField(); i++ {
			fieldName := d.Type().Field(i).Name
			checkDifferences(d.Field(i), u.Field(i), prefix+"."+fieldName, customCount, warnings)
		}
	case reflect.Map:
		for _, key := range d.MapKeys() {
			du := d.MapIndex(key)
			uu := u.MapIndex(key)
			checkDifferences(du, uu, prefix+"."+fmt.Sprint(key), customCount, warnings)
		}
	case reflect.Slice:
		if d.Len() != u.Len() {
			*customCount++
		} else {
			for i := 0; i < d.Len(); i++ {
				// Compare elements of the slice
				checkDifferences(d.Index(i), u.Index(i), fmt.Sprintf("%s[%d]", prefix, i), customCount, warnings)
			}
		}
	default:
		if !reflect.DeepEqual(d.Interface(), u.Interface()) {
			*customCount++
		}
	}
}

func getProviderAttribution(config Config, lang string) string {
	var modelInfo string
	var messageKey string

	switch config.LLM.Provider {
	case "transformers", "ollama":
		// These are local providers
		messageKey = "providedByMessageLocal"

		if config.LLM.Provider == "transformers" {
			modelName := config.TransformersServerArgs.Model
			modelInfo = strings.Split(modelName, "/")[1] // Just use the model name without path
		} else {
			modelInfo = cases.Title(language.Und).String(strings.Split(config.LLM.OllamaModel, ":")[0]) + strings.Split(strings.Split(config.LLM.OllamaModel, ":")[1], "-")[0]
		}

	case "gemini":
		// Cloud provider uses standard message
		messageKey = "providedByMessage"
		modelInfo = "Gemini"

	default:
		messageKey = "providedByMessage"
		modelInfo = ""
	}

	providerMessage := getLocalizedString(lang, messageKey, "response")
	return fmt.Sprintf(providerMessage, config.Server.Username, modelInfo)
}

type ProfileField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func updateBotProfile(client *mastodon.Client, config Config) error {
	if !config.Profile.Enabled {
		return nil
	}

	// Prepare new fields based on config order
	var fields []mastodon.Field

	// Add a new config option to check if this is the official instance
	var isOfficialInstance bool = botAcct.Acct == "altbot" && config.Server.MastodonServer == "https://fuzzies.wtf"

	// Process fields in the order specified in config
	for _, fieldName := range config.Profile.Fields {
		switch fieldName {
		case "version":
			if isOfficialInstance {
				fields = append(fields, mastodon.Field{
					Name:  "Version",
					Value: fmt.Sprintf("v%s", Version),
				})
			} else {
				fields = append(fields, mastodon.Field{
					Name:  "Altbot Version",
					Value: fmt.Sprintf("v%s", Version),
				})
			}

		case "model":
			if config.LLM.Provider == "transformers" {
				modelName := strings.Split(config.TransformersServerArgs.Model, "/")[1]
				fields = append(fields, mastodon.Field{
					Name:  "Model",
					Value: modelName,
				})
			} else if config.LLM.Provider == "ollama" {
				modelName := strings.Split(config.LLM.OllamaModel, ":")[0]
				fields = append(fields, mastodon.Field{
					Name:  "Model",
					Value: modelName,
				})
			} else if config.LLM.Provider == "gemini" {
				fields = append(fields, mastodon.Field{
					Name:  "Model",
					Value: config.Gemini.Model,
				})
			}

		case "source":
			if isOfficialInstance {
				fields = append(fields, mastodon.Field{
					Name:  "Source Code",
					Value: sourceURL,
				})
			} else {
				fields = append(fields, mastodon.Field{
					Name:  "Powered by Altbot",
					Value: sourceURL,
				})
			}

		case "donate":
			fields = append(fields, mastodon.Field{
				Name:  "Support Development",
				Value: donateURL,
			})

		case "made-by":
			fields = append(fields, mastodon.Field{
				Name:  "Made by",
				Value: creator,
			})
		}
	}

	// Ensure we don't exceed the maximum number of fields (typically 4)
	if len(fields) > 4 && !config.Profile.OverrideFeildCount {
		fields = fields[:4]
		fmt.Printf("%s Warning: Some profile fields were omitted due to the 4-field limit\n", Yellow)
	}

	// Update profile
	_, err := client.AccountUpdate(context.Background(), &mastodon.Profile{
		Fields: &fields,
	})
	if err != nil {
		return fmt.Errorf("error updating profile: %v", err)
	}

	fmt.Printf("%s Profile fields updated successfully\n", getStatusSymbol(true))
	return nil
}
