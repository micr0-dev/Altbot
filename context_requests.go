/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/mattn/go-mastodon"
)

// ContextRequest tracks a pending two-step alt-text request
// This is used for the experimental feature where the bot asks questions first
type ContextRequest struct {
	RequestStatusID mastodon.ID `json:"request_status_id"` // The status ID of our question message
	OriginalStatusID mastodon.ID `json:"original_status_id"` // The original post with images
	UserID          string      `json:"user_id"`
	Username        string      `json:"username"`
	ImageURL        string      `json:"image_url"`
	ImageFormat     string      `json:"image_format"`
	Language        string      `json:"language"`
	Timestamp       time.Time   `json:"timestamp"`
	ReplyToID       mastodon.ID `json:"reply_to_id"` // The status we should reply to with alt-text
}

var contextRequests = make(map[mastodon.ID]ContextRequest) // key: RequestStatusID
var contextRequestsMutex sync.Mutex

const contextRequestsFile = "context_requests.json"
const contextRequestExpirationDays = 7

// InitializeContextRequests loads pending context requests from disk
func InitializeContextRequests() error {
	contextRequestsMutex.Lock()
	defer contextRequestsMutex.Unlock()

	data, err := os.ReadFile(contextRequestsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, that's okay
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &contextRequests); err != nil {
		return err
	}

	// Clean up expired requests on load
	now := time.Now()
	for id, req := range contextRequests {
		if now.Sub(req.Timestamp).Hours() > float64(contextRequestExpirationDays*24) {
			delete(contextRequests, id)
		}
	}

	if len(contextRequests) > 0 {
		fmt.Printf("Loaded %d pending context requests\n", len(contextRequests))
	}
	return nil
}

// saveContextRequests saves pending requests to disk
func saveContextRequests() error {
	contextRequestsMutex.Lock()
	defer contextRequestsMutex.Unlock()

	data, err := json.MarshalIndent(contextRequests, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(contextRequestsFile, data, 0644)
}

// AddContextRequest adds a pending context request
func AddContextRequest(requestStatusID mastodon.ID, req ContextRequest) {
	contextRequestsMutex.Lock()
	contextRequests[requestStatusID] = req
	contextRequestsMutex.Unlock()

	if err := saveContextRequests(); err != nil {
		log.Printf("Error saving context requests: %v", err)
	}
}

// GetContextRequestByParent returns a context request if the given status is a reply to our question
func GetContextRequestByParent(parentStatusID mastodon.ID) *ContextRequest {
	contextRequestsMutex.Lock()
	defer contextRequestsMutex.Unlock()

	req, exists := contextRequests[parentStatusID]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Since(req.Timestamp).Hours() > float64(contextRequestExpirationDays*24) {
		delete(contextRequests, parentStatusID)
		return nil
	}

	return &req
}

// RemoveContextRequest removes a pending context request
func RemoveContextRequest(requestStatusID mastodon.ID) {
	contextRequestsMutex.Lock()
	delete(contextRequests, requestStatusID)
	contextRequestsMutex.Unlock()

	if err := saveContextRequests(); err != nil {
		log.Printf("Error saving context requests: %v", err)
	}
}

// CleanupExpiredContextRequests removes requests older than the expiration period
func CleanupExpiredContextRequests() {
	contextRequestsMutex.Lock()
	defer contextRequestsMutex.Unlock()

	now := time.Now()
	removed := 0
	for id, req := range contextRequests {
		if now.Sub(req.Timestamp).Hours() > float64(contextRequestExpirationDays*24) {
			delete(contextRequests, id)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("Cleaned up %d expired context requests", removed)
		if err := saveContextRequests(); err != nil {
			log.Printf("Error saving context requests: %v", err)
		}
	}
}

// StartContextRequestCleanupRoutine starts a background routine to clean up expired requests
func StartContextRequestCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		for range ticker.C {
			CleanupExpiredContextRequests()
		}
	}()
}

// shouldUseExperimentalMode determines if we should use the two-step alt-text flow
// Returns true if:
// - The feature is enabled
// - The language matches one of the configured languages
// - The random roll succeeds based on the configured percentage
func shouldUseExperimentalMode(lang string) bool {
	if !config.Experimental.TwoStepEnabled {
		return false
	}

	// Check language
	langMatch := false
	for _, l := range config.Experimental.TwoStepLanguages {
		if l == lang {
			langMatch = true
			break
		}
	}
	if !langMatch {
		return false
	}

	// Roll percentage
	return rand.Intn(100) < config.Experimental.TwoStepPercentage
}

// shouldUseExperimentalModeForUser is like shouldUseExperimentalMode but always triggers for admin
func shouldUseExperimentalModeForUser(lang string, username string) bool {
	if !config.Experimental.TwoStepEnabled {
		return false
	}

	// Always trigger for admin account (for testing)
	if "@"+username == config.RateLimit.AdminContactHandle {
		// Still check language requirement
		for _, l := range config.Experimental.TwoStepLanguages {
			if l == lang {
				return true
			}
		}
		return false
	}

	return shouldUseExperimentalMode(lang)
}

// notifyAdminExperimentalUsed sends a DM to the admin when the experimental feature is triggered
func notifyAdminExperimentalUsed(c *mastodon.Client, username string, postID mastodon.ID) {
	message := fmt.Sprintf("%s %s",
		config.RateLimit.AdminContactHandle,
		fmt.Sprintf(getLocalizedString("en", "experimentalAdminNotification", "response"), username, postID))

	// Dev mode: print to terminal instead of posting
	if devMode {
		fmt.Printf("\n%s[DEV MODE - Would notify admin about experimental feature]%s\n", Yellow, Reset)
		fmt.Printf("  To: %s\n", config.RateLimit.AdminContactHandle)
		fmt.Printf("  Visibility: direct\n")
		fmt.Printf("  Content: %s\n", message)
		fmt.Println("---")
		return
	}

	_, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:     message,
		Visibility: "direct",
	})
	if err != nil {
		log.Printf("Error notifying admin about experimental feature: %v", err)
	}
}
