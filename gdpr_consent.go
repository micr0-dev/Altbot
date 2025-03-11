package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-mastodon"
)

// ConsentDatabase stores user IDs who have provided informed consent
type ConsentDatabase struct {
	Users map[string]ConsentRecord `json:"users"`
	mu    sync.Mutex
}

// ConsentRecord stores information about a user's consent
type ConsentRecord struct {
	UserID        string    `json:"user_id"`
	Timestamp     time.Time `json:"timestamp"`
	ConsentMethod string    `json:"consent_method"`
}

var consentDB ConsentDatabase

// InitializeConsentDatabase initializes the consent database
func InitializeConsentDatabase() error {
	consentDB.Users = make(map[string]ConsentRecord)
	err := loadConsentDatabase("consent_database.json")
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, that's okay - we'll create it when we save
			fmt.Println("No consent database found. Creating a new one.")
			return saveConsentDatabase("consent_database.json")
		}
		return err
	}
	fmt.Printf("Database loaded with %d users\n", len(consentDB.Users))
	return nil
}

// loadConsentDatabase loads the consent database from a file
func loadConsentDatabase(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &consentDB.Users)
}

// saveConsentDatabase saves the consent database to a file
func saveConsentDatabase(filePath string) error {
	consentDB.mu.Lock()
	defer consentDB.mu.Unlock()

	data, err := json.MarshalIndent(consentDB.Users, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// HasUserConsent checks if a user has provided consent
func HasUserConsent(userID string) bool {
	consentDB.mu.Lock()
	defer consentDB.mu.Unlock()

	_, exists := consentDB.Users[userID]
	return exists
}

// RecordUserConsent adds a user to the consent database
func RecordUserConsent(userID string, method string) error {
	consentDB.mu.Lock()

	consentDB.Users[userID] = ConsentRecord{
		UserID:        userID,
		Timestamp:     time.Now(),
		ConsentMethod: method,
	}

	consentDB.mu.Unlock()

	return saveConsentDatabase("consent_database.json")
}

// RemoveUserConsent removes a user from the consent database
func RemoveUserConsent(userID string) error {
	consentDB.mu.Lock()
	defer consentDB.mu.Unlock()

	delete(consentDB.Users, userID)
	return saveConsentDatabase("consent_database.json")
}

// RequestGDPRConsent sends a consent request message to a user
func RequestGDPRConsent(c *mastodon.Client, userID string, username string, language string, replyToID mastodon.ID, isStandaloneMsg bool) (mastodon.ID, error) {
	// Always use English for GDPR messages for now, regardless of user language
	// We'll use "en" as the language code for consistency
	consentLanguage := "en"

	// Prepare the consent message with localization support
	var message string
	if isStandaloneMsg {
		message = fmt.Sprintf("@%s %s\n\n%s", username, getLocalizedString("en", "gdprWelcomeMessage", "response"), getLocalizedString(consentLanguage, "gdprConsentRequest", "response"))
	} else {
		message = fmt.Sprintf("@%s \n%s", username, getLocalizedString(consentLanguage, "gdprConsentRequest", "response"))
	}
	// Post the consent request
	status, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:      message,
		InReplyToID: replyToID,
		Visibility:  "direct", // Always send consent requests as direct messages
		Language:    language, // Keep original language for message metadata
	})

	if err != nil {
		log.Printf("Error sending GDPR consent request: %v", err)
		return "", err
	}

	log.Printf("Sent GDPR consent request to %s", username)
	return status.ID, nil
}

// HandleGDPRConsentResponse processes a user's response to a consent request
func HandleGDPRConsentResponse(c *mastodon.Client, status *mastodon.Status) bool {
	// Check if inreplyto is a consent request
	originalStatus := status.InReplyToID
	if originalStatus == nil {
		return false
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

	parentStatus, err := c.GetStatus(ctx, originalStatusID)
	if err != nil {
		log.Printf("Error fetching original status: %v", err)
		return false
	}

	// Check if the parent status is a consent request
	if !containsWord(stripHTMLTags(parentStatus.Content), "https://github.com/micr0-dev/Altbot/blob/main/PRIVACY.md") {
		return false
	}

	// Clean up HTML content to extract plain text
	plainTextContent := stripHTMLTags(status.Content)
	if plainTextContent == "" {
		return false
	}

	// Convert to lowercase and check for affirmative responses
	responseText := strings.ToLower(plainTextContent)
	consent := false

	// Check for various affirmative responses
	affirmativeResponses := []string{"yes", "y", "agree", "i agree", "consent", "i consent", "ok", "okay"}
	for _, response := range affirmativeResponses {
		if containsWord(responseText, response) {
			consent = true
			break
		}
	}

	if consent {
		// Record the user's consent
		err := RecordUserConsent(string(status.Account.ID), "explicit")
		if err != nil {
			log.Printf("Error recording consent for user %s: %v", status.Account.Acct, err)
			return false
		}

		log.Printf("User %s provided explicit consent", status.Account.Acct)

		// Always use English for GDPR messages
		consentLanguage := "en"

		// Send confirmation message
		confirmationMsg := fmt.Sprintf("@%s %s", status.Account.Acct, getLocalizedString(consentLanguage, "gdprConsentConfirmation", "response"))

		_, err = c.PostStatus(ctx, &mastodon.Toot{
			Status:      confirmationMsg,
			InReplyToID: status.ID,
			Visibility:  "direct",
			Language:    status.Language, // Keep original language for message metadata
		})

		if err != nil {
			log.Printf("Error sending consent confirmation: %v", err)
		}

		return true
	}

	return false
}

// Handle user blocking events (consent revocation)
func HandleBlockEvent(userID string) {
	if HasUserConsent(userID) {
		err := RemoveUserConsent(userID)
		if err != nil {
			log.Printf("Error removing consent for user %s: %v", userID, err)
		} else {
			log.Printf("User %s revoked consent by blocking", userID)
		}
	}
}

// containsWord checks if a string contains a specific word
func containsWord(text, word string) bool {
	for i := 0; i <= len(text)-len(word); i++ {
		match := true
		for j := 0; j < len(word); j++ {
			if i+j >= len(text) || text[i+j] != word[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
