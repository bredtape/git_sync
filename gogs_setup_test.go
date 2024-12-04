package git_sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CreateAccessTokenForUser generates an access token for an existing Gogs user
func CreateAccessTokenForUser(baseURL, username, password, tokenName string) (string, error) {
	// Prepare the token creation request
	tokenRequest := struct {
		Name string `json:"name"`
	}{
		Name: tokenName,
	}

	// Convert request to JSON
	reqBody, err := json.Marshal(tokenRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token request: %v", err)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Construct the API endpoint for token creation
	tokenURL := fmt.Sprintf("%s/api/v1/users/%s/tokens", baseURL, username)

	// Create the request
	req, err := http.NewRequestWithContext(
		context.Background(),
		"POST",
		tokenURL,
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	// Set authentication headers
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token creation request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("token creation failed with status: %d", resp.StatusCode)
	}

	// Parse the response
	var tokenResponse struct {
		Token string `json:"sha1"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return "", fmt.Errorf("failed to decode token response: %v", err)
	}

	return tokenResponse.Token, nil
}
