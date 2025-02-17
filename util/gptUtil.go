package util

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// GPTRequest represents the request payload for the GPT-4o API
type GPTRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_completion_tokens"`
}

// Message represents a single message in the chat history
type Message struct {
	Role    string `json:"role"` // "system", "user", or "assistant"
	Content string `json:"content"`
}

// GPTResponse represents the response payload from GPT-4o API
type GPTResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// GPTClient handles communication with OpenAI's API
type GPTClient struct {
	APIKey  string
	APIURL  string
	Timeout time.Duration
}

// NewGPTClient initializes a new GPTClient
func NewGPTClient(apiKey string) *GPTClient {
	return &GPTClient{
		APIKey:  apiKey,
		APIURL:  "https://api.openai.com/v1/chat/completions",
		Timeout: 120 * time.Second,
	}
}

// SendMessage sends a message to GPT-4o and retrieves a response
func (c *GPTClient) SendMessage(ctx context.Context, prompt string) (string, error) {
	reqPayload := GPTRequest{
		Model: "o1-mini", // Using GPT-4o Mini
		Messages: []Message{
			//{Role: "system", Content: "You are a helpful assistant that provides Go code execution."},
			{Role: "user", Content: prompt},
		},
		MaxTokens: 10240,
	}

	// Convert request to JSON
	reqBody, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.APIURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	// HTTP Client with timeout
	client := &http.Client{Timeout: c.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Decode response
	var gptResp GPTResponse
	if err := json.NewDecoder(resp.Body).Decode(&gptResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	// Ensure we have a valid response
	if len(gptResp.Choices) == 0 {
		return "", errors.New("empty response from GPT")
	}

	// Return the AI-generated content
	return gptResp.Choices[0].Message.Content, nil
}
