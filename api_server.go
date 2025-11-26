/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// APIServer handles the REST API
type APIServer struct {
	port         int
	monthlyLimit int
	server       *http.Server
}

// APIRequest represents the request queue item
type APIRequest struct {
	ID        string
	ImageData []byte
	Format    string
	Language  string
	ResultCh  chan APIResult
}

// APIResult represents the result of processing
type APIResult struct {
	AltText string
	Error   error
}

// Request queue for batch processing
var (
	requestQueue = make(chan APIRequest, 100)
	queueOnce    sync.Once
)

// StartAPIServer starts the REST API server
func StartAPIServer(port int, monthlyLimit int) {
	apiServer := &APIServer{
		port:         port,
		monthlyLimit: monthlyLimit,
	}

	// Start the request processor
	queueOnce.Do(func() {
		go apiServer.processQueue()
	})

	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/v1/alt-text", apiServer.handleAltText)
	mux.HandleFunc("/api/v1/usage", apiServer.handleUsage)
	mux.HandleFunc("/api/v1/health", apiServer.handleHealth)

	// Webhook endpoint for Ko-fi (for future automation)
	mux.HandleFunc("/api/webhook/kofi", apiServer.handleKofiWebhook)

	apiServer.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // Longer for processing
		IdleTimeout:  60 * time.Second,
	}

	fmt.Printf("%s API Server: http://localhost:%d\n", getStatusSymbol(true), port)

	go func() {
		if err := apiServer.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("API Server error: %v", err)
		}
	}()
}

// extractAPIKey extracts the API key from the Authorization header
func extractAPIKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	// Support both "Bearer <key>" and just "<key>"
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	return auth
}

// handleAltText processes alt-text generation requests
func (s *APIServer) handleAltText(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
	if r.Method != http.MethodPost {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract and validate API key
	apiKey := extractAPIKey(r)
	if apiKey == "" {
		s.jsonError(w, "Missing API key. Use Authorization: Bearer <your-key>", http.StatusUnauthorized)
		return
	}

	keyData, err := ValidateAPIKey(apiKey)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Check usage limits
	if err := CheckAndIncrementUsage(apiKey, s.monthlyLimit); err != nil {
		s.jsonError(w, err.Error(), http.StatusTooManyRequests)
		return
	}

	// Parse multipart form (max 50MB)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		s.jsonError(w, "Failed to parse form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get the uploaded file
	file, header, err := r.FormFile("image")
	if err != nil {
		s.jsonError(w, "Missing 'image' field in form data", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read file data
	imageData, err := io.ReadAll(file)
	if err != nil {
		s.jsonError(w, "Failed to read image data", http.StatusBadRequest)
		return
	}

	// Determine format from filename or content-type
	format := getImageFormat(header.Filename, header.Header.Get("Content-Type"))
	if format == "" {
		s.jsonError(w, "Unsupported image format", http.StatusBadRequest)
		return
	}

	// Get optional language parameter (default to English)
	language := r.FormValue("language")
	if language == "" {
		language = "en"
	}

	// Create request and add to queue
	resultCh := make(chan APIResult, 1)
	request := APIRequest{
		ID:        fmt.Sprintf("%s-%d", keyData.Email, time.Now().UnixNano()),
		ImageData: imageData,
		Format:    format,
		Language:  language,
		ResultCh:  resultCh,
	}

	// Add to queue with timeout
	select {
	case requestQueue <- request:
		// Request queued
	case <-time.After(10 * time.Second):
		s.jsonError(w, "Server busy, please try again later", http.StatusServiceUnavailable)
		return
	}

	// Wait for result with timeout
	select {
	case result := <-resultCh:
		if result.Error != nil {
			s.jsonError(w, "Failed to generate alt-text: "+result.Error.Error(), http.StatusInternalServerError)
			return
		}

		// Success response
		s.jsonResponse(w, map[string]interface{}{
			"alt_text":   result.AltText,
			"media_type": "image",
			"language":   language,
		})

	case <-time.After(120 * time.Second):
		s.jsonError(w, "Request timeout", http.StatusGatewayTimeout)
	}
}

// processQueue processes requests from the queue
func (s *APIServer) processQueue() {
	for request := range requestQueue {
		// Downscale image
		downscaledImg, format, err := downscaleImage(request.ImageData, config.ImageProcessing.DownscaleWidth)
		if err != nil {
			request.ResultCh <- APIResult{Error: fmt.Errorf("failed to process image: %v", err)}
			continue
		}

		// Get prompt
		prompt := getLocalizedString(request.Language, "generateAltText", "prompt")

		// Generate alt-text using the LLM provider
		altText, err := llmProvider.GenerateAltText(prompt, downscaledImg, format, request.Language)
		if err != nil {
			request.ResultCh <- APIResult{Error: err}
			continue
		}

		// Post-process and send result
		altText = postProcessAltText(altText)
		request.ResultCh <- APIResult{AltText: altText}

		// Log for metrics
		LogEvent("api_alt_text_generated")
	}
}

// handleUsage returns usage information for an API key
func (s *APIServer) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := extractAPIKey(r)
	if apiKey == "" {
		s.jsonError(w, "Missing API key", http.StatusUnauthorized)
		return
	}

	usageMonth, daysRemaining, expiresAt, err := GetAPIKeyUsage(apiKey)
	if err != nil {
		s.jsonError(w, err.Error(), http.StatusUnauthorized)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"usage_this_month": usageMonth,
		"monthly_limit":    s.monthlyLimit,
		"remaining":        s.monthlyLimit - usageMonth,
		"days_remaining":   daysRemaining,
		"expires_at":       expiresAt.Format(time.RFC3339),
	})
}

// handleHealth returns API health status
func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, map[string]interface{}{
		"status":  "healthy",
		"version": Version,
	})
}

// handleKofiWebhook handles Ko-fi webhook for automatic key generation
func (s *APIServer) handleKofiWebhook(w http.ResponseWriter, r *http.Request) {
	log.Printf("Ko-fi webhook received: %s %s", r.Method, r.URL.Path)

	if r.Method != http.MethodPost {
		log.Printf("Ko-fi webhook: wrong method %s", r.Method)
		s.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify this is from Ko-fi (they send a verification_token)
	if config.API.KofiVerificationToken == "" {
		log.Printf("Ko-fi webhook: no verification token configured")
		s.jsonError(w, "Webhook not configured", http.StatusNotImplemented)
		return
	}

	if err := r.ParseForm(); err != nil {
		log.Printf("Ko-fi webhook: failed to parse form: %v", err)
		s.jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Ko-fi sends data as form-encoded with a "data" field containing JSON
	dataStr := r.FormValue("data")
	if dataStr == "" {
		log.Printf("Ko-fi webhook: missing 'data' field")
		s.jsonError(w, "Missing data", http.StatusBadRequest)
		return
	}

	log.Printf("Ko-fi webhook data: %s", dataStr)

	var kofiData struct {
		VerificationToken string `json:"verification_token"`
		MessageID         string `json:"message_id"`
		Email             string `json:"email"`
		Type              string `json:"type"` // "Donation", "Subscription", "Commission", or "Shop Order"
		IsSubscription    bool   `json:"is_subscription_payment"`
		IsFirstSub        bool   `json:"is_first_subscription_payment"`
		FromName          string `json:"from_name"`
		Message           string `json:"message"`
		Amount            string `json:"amount"`
		Currency          string `json:"currency"`
		ShopItems         []struct {
			DirectLinkCode string `json:"direct_link_code"`
			VariationName  string `json:"variation_name"`
			Quantity       int    `json:"quantity"`
		} `json:"shop_items"`
		TierName string `json:"tier_name"` // NOTE: TierName not TeirName!
	}

	if err := json.Unmarshal([]byte(dataStr), &kofiData); err != nil {
		log.Printf("Ko-fi webhook: failed to parse JSON: %v", err)
		s.jsonError(w, "Invalid JSON data", http.StatusBadRequest)
		return
	}

	log.Printf("Ko-fi webhook parsed: type=%s, from=%s, email=%s, amount=%s %s, tier=%s, shop_items=%d",
		kofiData.Type, kofiData.FromName, kofiData.Email, kofiData.Amount, kofiData.Currency,
		kofiData.TierName, len(kofiData.ShopItems))

	// Verify token
	if kofiData.VerificationToken != config.API.KofiVerificationToken {
		log.Printf("Ko-fi webhook: invalid verification token")
		s.jsonError(w, "Invalid verification token", http.StatusUnauthorized)
		return
	}

	// Check if this is an API key related purchase
	isAPIKeyPurchase := false

	// Check for Shop Order with the API key product
	if kofiData.Type == "Shop Order" && len(kofiData.ShopItems) > 0 {
		for _, item := range kofiData.ShopItems {
			log.Printf("Ko-fi webhook: checking shop item code '%s' against config '%s'",
				item.DirectLinkCode, config.API.KofiShopItemCode)
			if item.DirectLinkCode == config.API.KofiShopItemCode {
				isAPIKeyPurchase = true
				log.Printf("Ko-fi webhook: matched shop item!")
				break
			}
		}
	}

	// Check for Subscription with the API key tier
	if kofiData.Type == "Subscription" && kofiData.TierName != "" {
		log.Printf("Ko-fi webhook: checking tier name '%s' against config '%s'",
			kofiData.TierName, config.API.KofiTierName)
		if kofiData.TierName == config.API.KofiTierName {
			isAPIKeyPurchase = true
			log.Printf("Ko-fi webhook: matched subscription tier!")
		}
	}

	if !isAPIKeyPurchase {
		log.Printf("Ko-fi webhook: not an API key purchase - ignoring")
		// Still return 200 OK - Ko-fi doesn't need to retry for non-API purchases
		s.jsonResponse(w, map[string]string{"status": "ok", "action": "ignored"})
		return
	}

	// Generate API key for the purchaser
	duration := 30 // days
	if kofiData.IsSubscription {
		duration = 31 // Slightly longer for subscriptions to handle billing timing
	}

	// Check if user already has a key
	existingKey := FindAPIKeyByEmail(kofiData.Email)
	if existingKey != nil && existingKey.Active {
		// Extend existing key instead of creating new one
		if err := ExtendAPIKey(existingKey.Key, duration); err != nil {
			log.Printf("Ko-fi webhook: error extending API key for %s: %v", kofiData.Email, err)
			s.jsonError(w, "Failed to extend key", http.StatusInternalServerError)
			return
		}
		log.Printf("Ko-fi webhook: extended API key for %s by %d days", kofiData.Email, duration)

		fmt.Printf("\n%s=== API KEY EXTENDED ===%s\n", Cyan, Reset)
		fmt.Printf("Email: %s\n", kofiData.Email)
		fmt.Printf("Key: %s\n", existingKey.Key)
		fmt.Printf("Extended by: %d days\n", duration)
		fmt.Printf("%s=========================%s\n\n", Cyan, Reset)

		go func() {
			SendAPIKeyExtendedEmail(kofiData.Email, existingKey, duration)
		}()
	} else {
		// Create new key
		note := fmt.Sprintf("Ko-fi %s from %s (%s %s)", kofiData.Type, kofiData.FromName, kofiData.Amount, kofiData.Currency)
		apiKey, err := GenerateAPIKey(kofiData.Email, duration, note)
		if err != nil {
			log.Printf("Ko-fi webhook: error generating API key for %s: %v", kofiData.Email, err)
			s.jsonError(w, "Failed to generate key", http.StatusInternalServerError)
			return
		}
		log.Printf("Ko-fi webhook: generated new API key for %s", kofiData.Email)

		fmt.Printf("\n%s=== NEW API KEY PURCHASE ===%s\n", Green, Reset)
		fmt.Printf("Email: %s\n", kofiData.Email)
		fmt.Printf("From: %s\n", kofiData.FromName)
		fmt.Printf("Amount: %s %s\n", kofiData.Amount, kofiData.Currency)
		fmt.Printf("Key: %s\n", apiKey.Key)
		fmt.Printf("Expires: %s\n", apiKey.ExpiresAt.Format("2006-01-02"))
		fmt.Printf("%s=============================%s\n\n", Green, Reset)

		go func() {
			SendAPIKeyEmail(kofiData.Email, apiKey)
		}()
	}

	s.jsonResponse(w, map[string]string{"status": "ok", "action": "key_generated"})
}

// Helper functions

func (s *APIServer) jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *APIServer) jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  message,
		"status": status,
	})
}

func getImageFormat(filename, contentType string) string {
	// Try to get format from filename extension
	if filename != "" {
		parts := strings.Split(strings.ToLower(filename), ".")
		if len(parts) > 1 {
			ext := parts[len(parts)-1]
			switch ext {
			case "jpg", "jpeg":
				return "jpeg"
			case "png":
				return "png"
			case "gif":
				return "gif"
			case "webp":
				return "webp"
			case "bmp":
				return "bmp"
			case "tiff", "tif":
				return "tiff"
			}
		}
	}

	// Try content type
	switch contentType {
	case "image/jpeg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/bmp":
		return "bmp"
	case "image/tiff":
		return "tiff"
	}

	return ""
}
