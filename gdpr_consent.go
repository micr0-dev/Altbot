/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

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

// PendingGDPRRequest tracks a pending GDPR consent request for a user
// This is used to handle platforms like PixelFed that send DMs without InReplyToID
type PendingGDPRRequest struct {
	UserID          string        `json:"user_id"`
	RequestStatusID mastodon.ID   `json:"request_status_id"`
	Timestamp       time.Time     `json:"timestamp"`
}

var pendingGDPRRequests = make(map[string]PendingGDPRRequest) // key: userID
var pendingGDPRMutex sync.Mutex

const pendingGDPRRequestsFile = "pending_gdpr_requests.json"
const pendingGDPRExpirationDays = 30

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

// --- Pending GDPR Request Functions (for PixelFed and similar platforms) ---

// InitializePendingGDPRRequests loads pending requests from disk
func InitializePendingGDPRRequests() error {
	pendingGDPRMutex.Lock()
	defer pendingGDPRMutex.Unlock()

	data, err := os.ReadFile(pendingGDPRRequestsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, that's okay
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &pendingGDPRRequests); err != nil {
		return err
	}

	// Clean up expired requests on load
	now := time.Now()
	for userID, req := range pendingGDPRRequests {
		if now.Sub(req.Timestamp).Hours() > float64(pendingGDPRExpirationDays*24) {
			delete(pendingGDPRRequests, userID)
		}
	}

	if len(pendingGDPRRequests) > 0 {
		fmt.Printf("Loaded %d pending GDPR requests\n", len(pendingGDPRRequests))
	}
	return nil
}

// savePendingGDPRRequests saves pending requests to disk
func savePendingGDPRRequests() error {
	pendingGDPRMutex.Lock()
	defer pendingGDPRMutex.Unlock()

	data, err := json.MarshalIndent(pendingGDPRRequests, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(pendingGDPRRequestsFile, data, 0644)
}

// AddPendingGDPRRequest adds a pending GDPR consent request for a user
func AddPendingGDPRRequest(userID string, requestStatusID mastodon.ID) {
	pendingGDPRMutex.Lock()
	pendingGDPRRequests[userID] = PendingGDPRRequest{
		UserID:          userID,
		RequestStatusID: requestStatusID,
		Timestamp:       time.Now(),
	}
	pendingGDPRMutex.Unlock()

	if err := savePendingGDPRRequests(); err != nil {
		log.Printf("Error saving pending GDPR requests: %v", err)
	}
}

// GetPendingGDPRRequest returns a pending GDPR request for a user, or nil if none exists
func GetPendingGDPRRequest(userID string) *PendingGDPRRequest {
	pendingGDPRMutex.Lock()
	defer pendingGDPRMutex.Unlock()

	req, exists := pendingGDPRRequests[userID]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Since(req.Timestamp).Hours() > float64(pendingGDPRExpirationDays*24) {
		delete(pendingGDPRRequests, userID)
		return nil
	}

	return &req
}

// RemovePendingGDPRRequest removes a pending GDPR request for a user
func RemovePendingGDPRRequest(userID string) {
	pendingGDPRMutex.Lock()
	delete(pendingGDPRRequests, userID)
	pendingGDPRMutex.Unlock()

	if err := savePendingGDPRRequests(); err != nil {
		log.Printf("Error saving pending GDPR requests: %v", err)
	}
}

// CleanupExpiredGDPRRequests removes pending requests older than the expiration period
func CleanupExpiredGDPRRequests() {
	pendingGDPRMutex.Lock()
	defer pendingGDPRMutex.Unlock()

	now := time.Now()
	removed := 0
	for userID, req := range pendingGDPRRequests {
		if now.Sub(req.Timestamp).Hours() > float64(pendingGDPRExpirationDays*24) {
			delete(pendingGDPRRequests, userID)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("Cleaned up %d expired GDPR requests", removed)
		if err := savePendingGDPRRequests(); err != nil {
			log.Printf("Error saving pending GDPR requests: %v", err)
		}
	}
}

// StartGDPRCleanupRoutine starts a background routine to clean up expired requests
func StartGDPRCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			CleanupExpiredGDPRRequests()
		}
	}()
}

// RequestGDPRConsent sends a consent request message to a user
func RequestGDPRConsent(c *mastodon.Client, userID string, username string, language string, replyToID mastodon.ID, isStandaloneMsg bool) (mastodon.ID, error) {
	// Always use English for GDPR messages for now, regardless of user language
	// We'll use "en" as the language code for consistency
	consentLanguage := "en"

	// Prepare the consent message with localization support
	var message string
	if isStandaloneMsg {
		message = fmt.Sprintf("@%s %s\n\n%s", username, getLocalizedString(consentLanguage, "gdprWelcomeMessage", "response"), getLocalizedString(consentLanguage, "gdprConsentRequest", "response"))
	} else {
		message = fmt.Sprintf("@%s %s", username, getLocalizedString(consentLanguage, "gdprConsentRequest", "response"))
	}

	// Dev mode: print to terminal instead of posting
	if devMode {
		fmt.Printf("\n%s[DEV MODE - Would post GDPR consent request]%s\n", Yellow, Reset)
		fmt.Printf("  To: @%s\n", username)
		fmt.Printf("  Visibility: direct\n")
		fmt.Printf("  Content: %s\n", message)
		fmt.Println("---")
		return "", nil
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

	// Track this pending request (for PixelFed and other platforms that don't use reply threading)
	AddPendingGDPRRequest(userID, status.ID)

	log.Printf("Sent GDPR consent request to %s", username)
	return status.ID, nil
}

// HandleGDPRConsentResponse processes a user's response to a consent request
func HandleGDPRConsentResponse(c *mastodon.Client, status *mastodon.Status) bool {
	userID := string(status.Account.ID)

	// Case 1: Reply-based response (standard Mastodon flow)
	if status.InReplyToID != nil {
		return handleReplyBasedConsent(c, status, userID)
	}

	// Case 2: Non-reply DM (PixelFed and similar platforms)
	// These platforms send DMs as new messages without InReplyToID
	if status.Visibility == "direct" {
		return handleNonReplyConsent(c, status, userID)
	}

	return false
}

// handleReplyBasedConsent handles consent responses that are replies to the original request
func handleReplyBasedConsent(c *mastodon.Client, status *mastodon.Status, userID string) bool {
	var originalStatusID mastodon.ID

	switch id := status.InReplyToID.(type) {
	case string:
		originalStatusID = mastodon.ID(id)
	case mastodon.ID:
		originalStatusID = id
	default:
		log.Printf("Unexpected type for InReplyToID: %T", status.InReplyToID)
		return false
	}

	parentStatus, err := c.GetStatus(ctx, originalStatusID)
	if err != nil {
		log.Printf("Error fetching original status: %v", err)
		return false
	}

	// Check if the parent status is a consent request (contains privacy policy link)
	if !containsWord(stripHTMLTags(parentStatus.Content), "https://github.com/micr0-dev/Altbot/blob/main/PRIVACY.md") {
		return false
	}

	// Check for affirmative response
	if checkAndRecordConsent(c, status, userID) {
		// Remove from pending requests if present
		RemovePendingGDPRRequest(userID)
		return true
	}

	return false
}

// handleNonReplyConsent handles consent responses from platforms like PixelFed
// that send DMs as new messages without InReplyToID
func handleNonReplyConsent(c *mastodon.Client, status *mastodon.Status, userID string) bool {
	// Check if this user has a pending GDPR consent request
	pendingRequest := GetPendingGDPRRequest(userID)
	if pendingRequest == nil {
		return false
	}

	// User has a pending request - check for affirmative response
	if checkAndRecordConsent(c, status, userID) {
		// Remove the pending request
		RemovePendingGDPRRequest(userID)
		log.Printf("Accepted GDPR consent from %s via non-reply DM (PixelFed flow)", status.Account.Acct)
		return true
	}

	return false
}

// checkAndRecordConsent checks for affirmative response and records consent if found
func checkAndRecordConsent(c *mastodon.Client, status *mastodon.Status, userID string) bool {
	// Clean up HTML content to extract plain text
	plainTextContent := stripHTMLTags(status.Content)
	if plainTextContent == "" {
		return false
	}

	// Convert to lowercase and check for affirmative responses
	responseText := strings.ToLower(plainTextContent)

	// Check for various affirmative responses (must be whole words, not substrings)
	affirmativeResponses := []string{"yes", "agree", "i agree", "consent", "i consent", "ok", "okay", "ja", "oui", "si"}
	consent := false
	for _, response := range affirmativeResponses {
		if containsWholeWord(responseText, response) {
			consent = true
			break
		}
	}

	if !consent {
		return false
	}

	// Record the user's consent
	err := RecordUserConsent(userID, "explicit")
	if err != nil {
		log.Printf("Error recording consent for user %s: %v", status.Account.Acct, err)
		return false
	}

	log.Printf("User %s provided explicit consent", status.Account.Acct)

	// Send confirmation message
	sendConsentConfirmation(c, status)

	return true
}

// sendConsentConfirmation sends a confirmation message to the user
func sendConsentConfirmation(c *mastodon.Client, status *mastodon.Status) {
	// Always use English for GDPR messages
	consentLanguage := "en"
	confirmationMsg := fmt.Sprintf("@%s %s", status.Account.Acct, getLocalizedString(consentLanguage, "gdprConsentConfirmation", "response"))

	// Dev mode: print to terminal instead of posting
	if devMode {
		fmt.Printf("\n%s[DEV MODE - Would post GDPR consent confirmation]%s\n", Yellow, Reset)
		fmt.Printf("  To: @%s\n", status.Account.Acct)
		fmt.Printf("  Visibility: direct\n")
		fmt.Printf("  Content: %s\n", confirmationMsg)
		fmt.Println("---")
		return
	}

	_, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:      confirmationMsg,
		InReplyToID: status.ID,
		Visibility:  "direct",
		Language:    status.Language,
	})

	if err != nil {
		log.Printf("Error sending consent confirmation: %v", err)
	}
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

// containsWord checks if a string contains a specific substring
func containsWord(text, word string) bool {
	return strings.Contains(text, word)
}

// containsWholeWord checks if a string contains a specific word as a whole word (not as a substring)
// e.g., "yes" matches "yes" or "yes!" but not "eyes" or "yesterday"
func containsWholeWord(text, word string) bool {
	// Handle exact match
	if text == word {
		return true
	}

	// Check for word at various positions with word boundaries
	wordLen := len(word)
	textLen := len(text)

	for i := 0; i <= textLen-wordLen; i++ {
		// Check if substring matches
		if text[i:i+wordLen] != word {
			continue
		}

		// Check left boundary (start of string or non-letter)
		leftOk := i == 0 || !isLetter(text[i-1])

		// Check right boundary (end of string or non-letter)
		rightOk := i+wordLen == textLen || !isLetter(text[i+wordLen])

		if leftOk && rightOk {
			return true
		}
	}
	return false
}

// isLetter checks if a byte is a letter (a-z, A-Z)
func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
