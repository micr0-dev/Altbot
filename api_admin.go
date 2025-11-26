/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// RunAdminCommand handles admin CLI commands
func RunAdminCommand(args []string) {
	if len(args) < 1 {
		printAdminHelp()
		return
	}

	// Initialize API key store (needed for all commands)
	if err := InitAPIKeyStore("api_keys.json"); err != nil {
		fmt.Printf("Error initializing API key store: %v\n", err)
		os.Exit(1)
	}

	command := args[0]
	switch command {
	case "create-key":
		handleCreateKey(args[1:])
	case "list-keys":
		handleListKeys()
	case "revoke-key":
		handleRevokeKey(args[1:])
	case "extend-key":
		handleExtendKey(args[1:])
	case "lookup":
		handleLookup(args[1:])
	case "cleanup":
		handleCleanup()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printAdminHelp()
	}
}

func printAdminHelp() {
	fmt.Println(`Altbot Admin Commands:
 
   create-key --email <email> [--days <days>] [--note <note>]
	   Create a new API key for a user
	   Default: 30 days
 
   list-keys
	   List all API keys
 
   revoke-key <key>
	   Revoke/deactivate an API key
 
   extend-key <key> --days <days>
	   Extend an API key's expiration
 
   lookup --email <email>
	   Find API key by email
 
   cleanup
	   Remove keys expired more than 30 days ago
 
 Examples:
   ./altbot admin create-key --email lily@example.com --days 30 --note "Ko-fi purchase"
   ./altbot admin list-keys
   ./altbot admin revoke-key altbot_abc123...
   ./altbot admin extend-key altbot_abc123... --days 30
   ./altbot admin lookup --email lily@example.com`)
}

func handleCreateKey(args []string) {
	var email string
	days := 30
	note := "Manual creation"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--email", "-e":
			if i+1 < len(args) {
				email = args[i+1]
				i++
			}
		case "--days", "-d":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &days)
				i++
			}
		case "--note", "-n":
			if i+1 < len(args) {
				note = args[i+1]
				i++
			}
		}
	}

	if email == "" {
		fmt.Println("Error: --email is required")
		return
	}

	// Check if email already has a key
	existing := FindAPIKeyByEmail(email)
	if existing != nil && existing.Active && time.Now().Before(existing.ExpiresAt) {
		fmt.Printf("Warning: User %s already has an active key (expires %s)\n",
			email, existing.ExpiresAt.Format("2006-01-02"))
		fmt.Println("Use 'extend-key' to extend it, or 'revoke-key' first to create a new one.")
		return
	}

	apiKey, err := GenerateAPIKey(email, days, note)
	if err != nil {
		fmt.Printf("Error creating key: %v\n", err)
		return
	}

	fmt.Printf("\n%s=== API Key Created ===%s\n", Green, Reset)
	fmt.Printf("Email:   %s\n", apiKey.Email)
	fmt.Printf("Key:     %s\n", apiKey.Key)
	fmt.Printf("Expires: %s (%d days)\n", apiKey.ExpiresAt.Format("2006-01-02"), days)
	fmt.Printf("Note:    %s\n", note)
	fmt.Printf("%s========================%s\n\n", Green, Reset)

	fmt.Println("Send this key to the user!")
}

func handleListKeys() {
	keys := ListAPIKeys()

	if len(keys) == 0 {
		fmt.Println("No API keys found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMAIL\tSTATUS\tUSAGE\tEXPIRES\tKEY (prefix)")
	fmt.Fprintln(w, "-----\t------\t-----\t-------\t-----------")

	for _, key := range keys {
		status := "active"
		if !key.Active {
			status = "revoked"
		} else if time.Now().After(key.ExpiresAt) {
			status = "expired"
		}

		keyPrefix := key.Key
		if len(keyPrefix) > 20 {
			keyPrefix = keyPrefix[:20] + "..."
		}

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
			key.Email,
			status,
			key.UsageMonth,
			key.ExpiresAt.Format("2006-01-02"),
			keyPrefix,
		)
	}

	w.Flush()
	fmt.Printf("\nTotal: %d keys\n", len(keys))
}

func handleRevokeKey(args []string) {
	if len(args) < 1 {
		fmt.Println("Error: API key required")
		fmt.Println("Usage: revoke-key <key>")
		return
	}

	key := args[0]
	if err := RevokeAPIKey(key); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("API key revoked successfully.\n")
}

func handleExtendKey(args []string) {
	if len(args) < 1 {
		fmt.Println("Error: API key required")
		fmt.Println("Usage: extend-key <key> --days <days>")
		return
	}

	key := args[0]
	days := 30

	for i := 1; i < len(args); i++ {
		if args[i] == "--days" || args[i] == "-d" {
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &days)
			}
		}
	}

	if err := ExtendAPIKey(key, days); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Get updated info
	_, daysRemaining, expiresAt, _ := GetAPIKeyUsage(key)
	fmt.Printf("API key extended by %d days.\n", days)
	fmt.Printf("New expiration: %s (%d days remaining)\n", expiresAt.Format("2006-01-02"), daysRemaining)
}

func handleLookup(args []string) {
	var email string

	for i := 0; i < len(args); i++ {
		if args[i] == "--email" || args[i] == "-e" {
			if i+1 < len(args) {
				email = args[i+1]
			}
		}
	}

	if email == "" {
		fmt.Println("Error: --email is required")
		return
	}

	key := FindAPIKeyByEmail(email)
	if key == nil {
		fmt.Printf("No API key found for %s\n", email)
		return
	}

	status := "active"
	if !key.Active {
		status = "revoked"
	} else if time.Now().After(key.ExpiresAt) {
		status = "expired"
	}

	fmt.Printf("\n%s=== API Key Details ===%s\n", Cyan, Reset)
	fmt.Printf("Email:      %s\n", key.Email)
	fmt.Printf("Key:        %s\n", key.Key)
	fmt.Printf("Status:     %s\n", status)
	fmt.Printf("Created:    %s\n", key.CreatedAt.Format("2006-01-02 15:04"))
	fmt.Printf("Expires:    %s\n", key.ExpiresAt.Format("2006-01-02 15:04"))
	fmt.Printf("Usage:      %d this month\n", key.UsageMonth)
	if key.Note != "" {
		fmt.Printf("Note:       %s\n", key.Note)
	}
	fmt.Printf("%s========================%s\n", Cyan, Reset)
}

func handleCleanup() {
	removed := CleanupExpiredKeys()
	fmt.Printf("Cleaned up %d expired keys.\n", removed)
}

// FormatKeyForEmail formats an API key message for copy-pasting into an email
func FormatKeyForEmail(apiKey *APIKey) string {
	var sb strings.Builder

	sb.WriteString("Hi!\n\n")
	sb.WriteString("Thank you for supporting Altbot! Here's your API key:\n\n")
	sb.WriteString(fmt.Sprintf("API Key: %s\n\n", apiKey.Key))
	sb.WriteString(fmt.Sprintf("This key is valid until: %s\n\n", apiKey.ExpiresAt.Format("January 2, 2006")))
	sb.WriteString("Quick start:\n")
	sb.WriteString("curl -X POST https://your-server/api/v1/alt-text \\\n")
	sb.WriteString(fmt.Sprintf("  -H \"Authorization: Bearer %s\" \\\n", apiKey.Key))
	sb.WriteString("  -F \"image=@your-image.jpg\"\n\n")
	sb.WriteString("Documentation: https://github.com/micr0-dev/Altbot/blob/main/API.md\n\n")
	sb.WriteString("If you have any questions, feel free to reach out!\n\n")
	sb.WriteString("- Altbot\n")

	return sb.String()
}
