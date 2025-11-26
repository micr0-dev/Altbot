/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// PostmarkEmail represents the email payload for Postmark API
type PostmarkEmail struct {
	From          string `json:"From"`
	To            string `json:"To"`
	Subject       string `json:"Subject"`
	HtmlBody      string `json:"HtmlBody"`
	TextBody      string `json:"TextBody"`
	MessageStream string `json:"MessageStream"`
}

// SendAPIKeyEmail sends the API key to the user via Postmark
func SendAPIKeyEmail(toEmail string, apiKey *APIKey) error {
	if config.API.PostmarkToken == "" {
		log.Printf("Postmark token not configured, skipping email to %s", toEmail)
		return nil
	}

	email := PostmarkEmail{
		From:          config.API.PostmarkFromEmail,
		To:            toEmail,
		Subject:       "Your Altbot API Key",
		MessageStream: "outbound",
		HtmlBody:      generateAPIKeyEmailHTML(apiKey),
		TextBody:      generateAPIKeyEmailText(apiKey),
	}

	return sendPostmarkEmail(email)
}

// SendAPIKeyExtendedEmail notifies user their key was extended
func SendAPIKeyExtendedEmail(toEmail string, apiKey *APIKey, daysAdded int) error {
	if config.API.PostmarkToken == "" {
		log.Printf("Postmark token not configured, skipping email to %s", toEmail)
		return nil
	}

	email := PostmarkEmail{
		From:          config.API.PostmarkFromEmail,
		To:            toEmail,
		Subject:       "Your Altbot API Key Has Been Extended",
		MessageStream: "outbound",
		HtmlBody:      generateAPIKeyExtendedEmailHTML(apiKey, daysAdded),
		TextBody:      generateAPIKeyExtendedEmailText(apiKey, daysAdded),
	}

	return sendPostmarkEmail(email)
}

func sendPostmarkEmail(email PostmarkEmail) error {
	jsonData, err := json.Marshal(email)
	if err != nil {
		return fmt.Errorf("failed to marshal email: %v", err)
	}

	req, err := http.NewRequest("POST", "https://api.postmarkapp.com/email", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", config.API.PostmarkToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("postmark returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Email sent successfully to %s", email.To)
	return nil
}

func generateAPIKeyEmailHTML(apiKey *APIKey) string {
	return fmt.Sprintf(`
 <!DOCTYPE html>
 <html>
 <head>
	 <style>
		 body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
		 .header { background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%); color: white; padding: 30px; border-radius: 10px 10px 0 0; text-align: center; }
		 .content { background: #f9f9f9; padding: 30px; border-radius: 0 0 10px 10px; }
		 .key-box { background: #1a1a2e; color: #00ff88; padding: 15px; border-radius: 5px; font-family: monospace; font-size: 14px; word-break: break-all; margin: 20px 0; }
		 .info { background: white; padding: 15px; border-radius: 5px; margin: 15px 0; }
		 .info-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #eee; }
		 .info-row:last-child { border-bottom: none; }
		 .label { color: #666; }
		 .value { font-weight: 600; }
		 code { background: #e8e8e8; padding: 2px 6px; border-radius: 3px; font-size: 13px; }
		 .button { display: inline-block; background: #667eea; color: white; padding: 12px 24px; text-decoration: none; border-radius: 5px; margin-top: 15px; }
		 .footer { text-align: center; margin-top: 30px; color: #888; font-size: 13px; }
	 </style>
 </head>
 <body>
	 <div class="header">
		 <h1 style="margin: 0;">🤖 Altbot API</h1>
		 <p style="margin: 10px 0 0 0; opacity: 0.9;">Your API Key is Ready!</p>
	 </div>
	 <div class="content">
		 <p>Thank you for supporting Altbot! Here's your API key:</p>
		 
		 <div class="key-box">%s</div>
		 
		 <div class="info">
			 <div class="info-row">
				 <span class="label">Valid Until</span>
				 <span class="value">%s</span>
			 </div>
			 <div class="info-row">
				 <span class="label">Monthly Limit</span>
				 <span class="value">5,000 images</span>
			 </div>
		 </div>
		 
		 <h3>Quick Start</h3>
		 <p>Generate alt-text with a simple API call:</p>
		 <pre style="background: #1a1a2e; color: #ccc; padding: 15px; border-radius: 5px; overflow-x: auto; font-size: 12px;">curl -X POST https://altbot.micr0.dev/api/v1/alt-text \
   -H "Authorization: Bearer %s" \
   -F "image=@your-image.jpg"</pre>
		 
		 <a href="https://github.com/micr0-dev/Altbot/blob/main/API.md" class="button">View Full Documentation</a>
		 
		 <div class="footer">
			 <p>Questions? Reply to this email!</p>
			 <p>Made with 💜 by <a href="https://micr0.dev">micr0</a></p>
		 </div>
	 </div>
 </body>
 </html>
 `, apiKey.Key, apiKey.ExpiresAt.Format("January 2, 2006"), apiKey.Key)
}

func generateAPIKeyEmailText(apiKey *APIKey) string {
	return fmt.Sprintf(`Altbot API - Your API Key is Ready!
 
 Thank you for supporting Altbot! Here's your API key:
 
 %s
 
 Valid Until: %s
 Monthly Limit: 5,000 images
 
 Quick Start
 -----------
 curl -X POST https://altbot.micr0.dev/api/v1/alt-text \
   -H "Authorization: Bearer %s" \
   -F "image=@your-image.jpg"
 
 Documentation: https://github.com/micr0-dev/Altbot/blob/main/API.md
 
 Questions? Reply to this email!
 
 Made with love by micr0
 `, apiKey.Key, apiKey.ExpiresAt.Format("January 2, 2006"), apiKey.Key)
}

func generateAPIKeyExtendedEmailHTML(apiKey *APIKey, daysAdded int) string {
	return fmt.Sprintf(`
 <!DOCTYPE html>
 <html>
 <head>
	 <style>
		 body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
		 .header { background: linear-gradient(135deg, #11998e 0%%, #38ef7d 100%%); color: white; padding: 30px; border-radius: 10px 10px 0 0; text-align: center; }
		 .content { background: #f9f9f9; padding: 30px; border-radius: 0 0 10px 10px; }
		 .key-box { background: #1a1a2e; color: #00ff88; padding: 15px; border-radius: 5px; font-family: monospace; font-size: 14px; word-break: break-all; margin: 20px 0; }
		 .info { background: white; padding: 15px; border-radius: 5px; margin: 15px 0; }
		 .info-row { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #eee; }
		 .info-row:last-child { border-bottom: none; }
		 .label { color: #666; }
		 .value { font-weight: 600; }
		 .footer { text-align: center; margin-top: 30px; color: #888; font-size: 13px; }
	 </style>
 </head>
 <body>
	 <div class="header">
		 <h1 style="margin: 0;">🎉 Subscription Renewed!</h1>
		 <p style="margin: 10px 0 0 0; opacity: 0.9;">Your Altbot API key has been extended</p>
	 </div>
	 <div class="content">
		 <p>Thank you for your continued support! Your API key has been extended by <strong>%d days</strong>.</p>
		 
		 <div class="info">
			 <div class="info-row">
				 <span class="label">Your API Key</span>
				 <span class="value" style="font-family: monospace; font-size: 12px;">%s...%s</span>
			 </div>
			 <div class="info-row">
				 <span class="label">New Expiration</span>
				 <span class="value">%s</span>
			 </div>
			 <div class="info-row">
				 <span class="label">Usage This Month</span>
				 <span class="value">%d / 5,000 images</span>
			 </div>
		 </div>
		 
		 <p>Your existing API key continues to work - no changes needed on your end!</p>
		 
		 <div class="footer">
			 <p>Questions? Reply to this email!</p>
			 <p>Made with 💜 by <a href="https://micr0.dev">micr0</a></p>
		 </div>
	 </div>
 </body>
 </html>
 `, daysAdded, apiKey.Key[:12], apiKey.Key[len(apiKey.Key)-6:], apiKey.ExpiresAt.Format("January 2, 2006"), apiKey.UsageMonth)
}

func generateAPIKeyExtendedEmailText(apiKey *APIKey, daysAdded int) string {
	return fmt.Sprintf(`Altbot API - Subscription Renewed!
 
 Thank you for your continued support! Your API key has been extended by %d days.
 
 Your API Key: %s...%s
 New Expiration: %s
 Usage This Month: %d / 5,000 images
 
 Your existing API key continues to work - no changes needed on your end!
 
 Questions? Reply to this email!
 
 Made with love by micr0
 `, daysAdded, apiKey.Key[:12], apiKey.Key[len(apiKey.Key)-6:], apiKey.ExpiresAt.Format("January 2, 2006"), apiKey.UsageMonth)
}
