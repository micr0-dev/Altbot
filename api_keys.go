/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// APIKey represents a single API key and its metadata
type APIKey struct {
	Key        string    `json:"key"`
	Email      string    `json:"email"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	UsageMonth int       `json:"usage_month"`
	LastReset  time.Time `json:"last_reset"`
	Active     bool      `json:"active"`
	Note       string    `json:"note,omitempty"`
}

// APIKeyStore manages all API keys
type APIKeyStore struct {
	Keys     map[string]*APIKey `json:"keys"`
	mu       sync.RWMutex
	filePath string
}

// Global API key store
var apiKeyStore *APIKeyStore

// InitAPIKeyStore initializes the API key store
func InitAPIKeyStore(filePath string) error {
	apiKeyStore = &APIKeyStore{
		Keys:     make(map[string]*APIKey),
		filePath: filePath,
	}

	if err := apiKeyStore.LoadFromFile(); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No API keys file found. Starting fresh.")
			return apiKeyStore.SaveToFile()
		}
		return err
	}

	fmt.Printf("Loaded %d API keys\n", len(apiKeyStore.Keys))
	return nil
}

// LoadFromFile loads API keys from the JSON file
func (store *APIKeyStore) LoadFromFile() error {
	store.mu.Lock()
	defer store.mu.Unlock()

	data, err := os.ReadFile(store.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &store.Keys)
}

// SaveToFile saves API keys to the JSON file (acquires lock)
func (store *APIKeyStore) SaveToFile() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saveToFileUnlocked()
}

// saveToFileUnlocked saves without acquiring lock (caller must hold lock)
func (store *APIKeyStore) saveToFileUnlocked() error {
	data, err := json.MarshalIndent(store.Keys, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(store.filePath, data, 0644)
}

// GenerateAPIKey creates a new API key for a user
func GenerateAPIKey(email string, durationDays int, note string) (*APIKey, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %v", err)
	}

	keyString := "altbot_" + hex.EncodeToString(keyBytes)

	now := time.Now()
	apiKey := &APIKey{
		Key:        keyString,
		Email:      email,
		CreatedAt:  now,
		ExpiresAt:  now.AddDate(0, 0, durationDays),
		UsageMonth: 0,
		LastReset:  now,
		Active:     true,
		Note:       note,
	}

	apiKeyStore.mu.Lock()
	apiKeyStore.Keys[keyString] = apiKey
	err := apiKeyStore.saveToFileUnlocked()
	apiKeyStore.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("failed to save API key: %v", err)
	}

	return apiKey, nil
}

// ValidateAPIKey checks if an API key is valid and not expired
func ValidateAPIKey(key string) (*APIKey, error) {
	apiKeyStore.mu.RLock()
	apiKey, exists := apiKeyStore.Keys[key]
	apiKeyStore.mu.RUnlock()

	// If key not found in memory, try reloading from file
	if !exists {
		if err := apiKeyStore.LoadFromFile(); err == nil {
			apiKeyStore.mu.RLock()
			apiKey, exists = apiKeyStore.Keys[key]
			apiKeyStore.mu.RUnlock()
		}
	}

	if !exists {
		return nil, fmt.Errorf("invalid API key")
	}

	if !apiKey.Active {
		return nil, fmt.Errorf("API key is deactivated")
	}

	if time.Now().After(apiKey.ExpiresAt) {
		return nil, fmt.Errorf("API key has expired")
	}

	return apiKey, nil
}

// CheckAndIncrementUsage checks if user is within limits and increments usage
func CheckAndIncrementUsage(key string, monthlyLimit int) error {
	apiKeyStore.mu.Lock()
	defer apiKeyStore.mu.Unlock()

	apiKey, exists := apiKeyStore.Keys[key]
	if !exists {
		return fmt.Errorf("invalid API key")
	}

	// Reset monthly counter if we're in a new month
	now := time.Now()
	if now.Month() != apiKey.LastReset.Month() || now.Year() != apiKey.LastReset.Year() {
		apiKey.UsageMonth = 0
		apiKey.LastReset = now
	}

	if apiKey.UsageMonth >= monthlyLimit {
		return fmt.Errorf("monthly usage limit exceeded (%d/%d)", apiKey.UsageMonth, monthlyLimit)
	}

	apiKey.UsageMonth++

	// Save periodically (every 10 requests)
	if apiKey.UsageMonth%10 == 0 {
		go func() {
			apiKeyStore.mu.Lock()
			apiKeyStore.saveToFileUnlocked()
			apiKeyStore.mu.Unlock()
		}()
	}

	return nil
}

// GetAPIKeyUsage returns usage info for an API key
func GetAPIKeyUsage(key string) (int, int, time.Time, error) {
	apiKeyStore.mu.RLock()
	defer apiKeyStore.mu.RUnlock()

	apiKey, exists := apiKeyStore.Keys[key]
	if !exists {
		return 0, 0, time.Time{}, fmt.Errorf("invalid API key")
	}

	daysRemaining := int(time.Until(apiKey.ExpiresAt).Hours() / 24)
	if daysRemaining < 0 {
		daysRemaining = 0
	}

	return apiKey.UsageMonth, daysRemaining, apiKey.ExpiresAt, nil
}

// RevokeAPIKey deactivates an API key
func RevokeAPIKey(key string) error {
	apiKeyStore.mu.Lock()
	defer apiKeyStore.mu.Unlock()

	apiKey, exists := apiKeyStore.Keys[key]
	if !exists {
		return fmt.Errorf("API key not found")
	}

	apiKey.Active = false

	return apiKeyStore.saveToFileUnlocked()
}

// ExtendAPIKey extends the expiration of an existing key
func ExtendAPIKey(key string, additionalDays int) error {
	apiKeyStore.mu.Lock()
	defer apiKeyStore.mu.Unlock()

	apiKey, exists := apiKeyStore.Keys[key]
	if !exists {
		return fmt.Errorf("API key not found")
	}

	// If expired, extend from now; otherwise extend from current expiry
	if time.Now().After(apiKey.ExpiresAt) {
		apiKey.ExpiresAt = time.Now().AddDate(0, 0, additionalDays)
	} else {
		apiKey.ExpiresAt = apiKey.ExpiresAt.AddDate(0, 0, additionalDays)
	}

	apiKey.Active = true

	return apiKeyStore.saveToFileUnlocked() // Use unlocked version!
}

// ListAPIKeys returns all API keys (for admin purposes)
func ListAPIKeys() []*APIKey {
	apiKeyStore.mu.RLock()
	defer apiKeyStore.mu.RUnlock()

	keys := make([]*APIKey, 0, len(apiKeyStore.Keys))
	for _, key := range apiKeyStore.Keys {
		keys = append(keys, key)
	}

	return keys
}

// FindAPIKeyByEmail finds an API key by email
func FindAPIKeyByEmail(email string) *APIKey {
	apiKeyStore.mu.RLock()
	defer apiKeyStore.mu.RUnlock()

	for _, key := range apiKeyStore.Keys {
		if key.Email == email {
			return key
		}
	}

	return nil
}

// CleanupExpiredKeys removes keys that have been expired for more than 30 days
func CleanupExpiredKeys() int {
	apiKeyStore.mu.Lock()
	defer apiKeyStore.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -30)
	removed := 0

	for key, apiKey := range apiKeyStore.Keys {
		if apiKey.ExpiresAt.Before(cutoff) {
			delete(apiKeyStore.Keys, key)
			removed++
		}
	}

	if removed > 0 {
		apiKeyStore.saveToFileUnlocked()
	}

	return removed
}
