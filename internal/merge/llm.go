package merge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LLMConfig configures the LLM merge resolver.
type LLMConfig struct {
	Enabled            bool    `toml:"enabled" json:"enabled"`
	Endpoint           string  `toml:"endpoint" json:"endpoint"`                         // e.g. "https://api.anthropic.com/v1/messages"
	APIKey             string  `toml:"api_key" json:"api_key"`
	Model              string  `toml:"model" json:"model"`                               // e.g. "claude-sonnet-4-5-20250514"
	AutoApplyThreshold float64 `toml:"auto_apply_threshold" json:"auto_apply_threshold"` // e.g. 0.9
	MaxFileSize        int64   `toml:"max_file_size" json:"max_file_size"`               // e.g. 100000
}

// LLMMerger resolves merge conflicts using an LLM.
type LLMMerger struct {
	config LLMConfig
	client *http.Client
}

// LLMResult is the structured response from the LLM.
type LLMResult struct {
	Content     string  `json:"content"`
	Explanation string  `json:"explanation"`
	Confidence  float64 `json:"confidence"`
}

// NewLLMMerger creates an LLMMerger with the given configuration.
func NewLLMMerger(config LLMConfig) *LLMMerger {
	return &LLMMerger{
		config: config,
		client: &http.Client{Timeout: 2 * time.Minute},
	}
}

// Resolve asks the LLM to produce a merged result for the given conflicts.
func (m *LLMMerger) Resolve(entityID string, base, ours, theirs []byte, conflicts []Conflict) (*LLMResult, error) {
	if !m.config.Enabled {
		return nil, fmt.Errorf("LLM merge disabled")
	}
	if int64(len(base)) > m.config.MaxFileSize {
		return nil, fmt.Errorf("file too large for LLM merge: %d > %d", len(base), m.config.MaxFileSize)
	}

	prompt := m.buildMergePrompt(entityID, base, ours, theirs, conflicts)
	response, err := m.callLLM(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM API call failed: %w", err)
	}

	result, err := parseResponse(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}
	return result, nil
}

// buildMergePrompt constructs the prompt for the LLM.
func (m *LLMMerger) buildMergePrompt(entityID string, base, ours, theirs []byte, conflicts []Conflict) string {
	var sb strings.Builder

	sb.WriteString("You are resolving a merge conflict in a versioning system.\n\n")
	sb.WriteString(fmt.Sprintf("File: %s\n\n", entityID))

	sb.WriteString("## Base version (common ancestor)\n")
	sb.WriteString("```\n")
	sb.WriteString(string(base))
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Version A (source changes)\n")
	sb.WriteString("```\n")
	sb.WriteString(string(ours))
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Version B (target changes)\n")
	sb.WriteString("```\n")
	sb.WriteString(string(theirs))
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Conflicts\n")
	for i, c := range conflicts {
		sb.WriteString(fmt.Sprintf("### Conflict %d (base line %d)\n", i+1, c.BaseStart))
		sb.WriteString(fmt.Sprintf("Base:   %s\n", strings.Join(c.BaseLines, "")))
		sb.WriteString(fmt.Sprintf("Ours:   %s\n", strings.Join(c.OurLines, "")))
		sb.WriteString(fmt.Sprintf("Theirs: %s\n\n", strings.Join(c.TheirLines, "")))
	}

	sb.WriteString("Produce the merged result that incorporates both sets of changes correctly.\n")
	sb.WriteString("Return as JSON:\n")
	sb.WriteString(`{"content": "the merged file content", "explanation": "what you did and why", "confidence": 0.95}`)
	sb.WriteString("\n")

	return sb.String()
}

// chatRequest is the OpenAI-compatible chat API request.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

// chatMessage is a single message in the chat API.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponse is the OpenAI-format API response.
type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// anthropicResponse is the Anthropic-format API response.
type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// callLLM sends the prompt to the configured LLM endpoint and returns the text response.
func (m *LLMMerger) callLLM(prompt string) (string, error) {
	reqBody := chatRequest{
		Model: m.config.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a merge conflict resolver. Return only valid JSON."},
			{Role: "user", Content: prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", m.config.Endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.config.APIKey)

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	// Try OpenAI format first.
	var oaiResp openAIResponse
	if err := json.Unmarshal(respBytes, &oaiResp); err == nil && len(oaiResp.Choices) > 0 {
		return oaiResp.Choices[0].Message.Content, nil
	}

	// Try Anthropic format.
	var antResp anthropicResponse
	if err := json.Unmarshal(respBytes, &antResp); err == nil && len(antResp.Content) > 0 {
		return antResp.Content[0].Text, nil
	}

	return "", fmt.Errorf("unrecognized API response format: %s", string(respBytes))
}

// parseResponse extracts an LLMResult from the LLM's text response.
// It looks for JSON between ```json ... ``` fences, or parses the whole response.
func parseResponse(response string) (*LLMResult, error) {
	jsonStr := response

	// Try to extract JSON from fenced code block.
	if idx := strings.Index(response, "```json"); idx != -1 {
		start := idx + len("```json")
		end := strings.Index(response[start:], "```")
		if end != -1 {
			jsonStr = strings.TrimSpace(response[start : start+end])
		}
	} else if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + len("```")
		end := strings.Index(response[start:], "```")
		if end != -1 {
			jsonStr = strings.TrimSpace(response[start : start+end])
		}
	}

	var result LLMResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON in response: %w", err)
	}

	if result.Confidence < 0 || result.Confidence > 1 {
		return nil, fmt.Errorf("confidence %f out of range [0, 1]", result.Confidence)
	}

	return &result, nil
}
