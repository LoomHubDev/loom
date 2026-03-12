package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to LoomHub's sync API.
type Client struct {
	baseURL    string
	owner      string
	loom       string
	token      string
	httpClient *http.Client
}

// NewClient creates a sync client. url should be like "https://hub.example.com/owner/loom"
// or "https://hub.example.com" with owner/loom separate.
func NewClient(remoteURL, token string) *Client {
	// Parse owner/loom from URL: https://host/owner/loom
	u := strings.TrimSuffix(remoteURL, "/")
	parts := strings.Split(u, "/")

	var baseURL, owner, loom string
	if len(parts) >= 5 {
		// https://host/owner/loom → base = https://host, owner = parts[3], loom = parts[4]
		baseURL = strings.Join(parts[:3], "/")
		owner = parts[3]
		loom = parts[4]
	} else {
		baseURL = u
	}

	return &Client{
		baseURL: baseURL,
		owner:   owner,
		loom:    loom,
		token:   token,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) syncURL(endpoint string) string {
	return fmt.Sprintf("%s/%s/%s/api/v1/%s", c.baseURL, c.owner, c.loom, endpoint)
}

func (c *Client) doRequest(method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	return c.httpClient.Do(req)
}

// --- Wire types (match LoomHub's sync protocol) ---

type StreamSyncState struct {
	StreamID string `json:"stream_id"`
	Name     string `json:"name"`
	HeadSeq  int64  `json:"head_seq"`
}

type NegotiateRequest struct {
	ProjectID string            `json:"project_id"`
	Streams   []StreamSyncState `json:"streams"`
}

type NegotiateResponse struct {
	CommonSeqs map[string]int64 `json:"common_seqs"`
	ServerSeqs map[string]int64 `json:"server_seqs"`
	NeedsPush  bool             `json:"needs_push"`
	NeedsPull  bool             `json:"needs_pull"`
}

type OperationWire struct {
	ID        string          `json:"id"`
	Seq       int64           `json:"seq"`
	StreamID  string          `json:"stream_id"`
	SpaceID   string          `json:"space_id"`
	EntityID  string          `json:"entity_id"`
	Type      string          `json:"type"`
	Path      string          `json:"path"`
	Delta     json.RawMessage `json:"delta,omitempty"`
	ObjectRef string          `json:"object_ref,omitempty"`
	ParentSeq int64           `json:"parent_seq"`
	Author    string          `json:"author"`
	Timestamp string          `json:"timestamp"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

type ObjectData struct {
	Hash    string `json:"hash"`
	Content []byte `json:"content"`
}

type PushRequest struct {
	ProjectID  string          `json:"project_id"`
	StreamID   string          `json:"stream_id"`
	FromSeq    int64           `json:"from_seq"`
	Operations []OperationWire `json:"operations"`
	Objects    []ObjectData    `json:"objects"`
}

type PushResponse struct {
	OK         bool   `json:"ok"`
	Applied    int    `json:"applied"`
	ServerHead int64  `json:"server_head"`
	Error      string `json:"error,omitempty"`
}

type PullRequest struct {
	ProjectID string `json:"project_id"`
	StreamID  string `json:"stream_id"`
	FromSeq   int64  `json:"from_seq"`
}

type PullResponse struct {
	Operations []OperationWire `json:"operations"`
	Objects    []ObjectData    `json:"objects"`
	ServerHead int64           `json:"server_head"`
}

// --- API methods ---

// Negotiate compares local and server stream states.
func (c *Client) Negotiate(req *NegotiateRequest) (*NegotiateResponse, error) {
	resp, err := c.doRequest("POST", c.syncURL("negotiate"), req)
	if err != nil {
		return nil, fmt.Errorf("negotiate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result NegotiateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode negotiate response: %w", err)
	}
	return &result, nil
}

// Push sends operations and objects to the hub.
func (c *Client) Push(req *PushRequest) (*PushResponse, error) {
	resp, err := c.doRequest("POST", c.syncURL("push"), req)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result PushResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode push response: %w", err)
	}
	return &result, nil
}

// Pull fetches operations and objects from the hub.
func (c *Client) Pull(req *PullRequest) (*PullResponse, error) {
	resp, err := c.doRequest("POST", c.syncURL("pull"), req)
	if err != nil {
		return nil, fmt.Errorf("pull: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result PullResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode pull response: %w", err)
	}
	return &result, nil
}

// Login authenticates with the hub and returns a token.
func (c *Client) Login(username, password string) (string, error) {
	body := map[string]string{"username": username, "password": password}
	resp, err := c.doRequest("POST", c.baseURL+"/api/v1/auth/login", body)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.readError(resp)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	return result.Token, nil
}

func (c *Client) readError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var apiErr struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		return fmt.Errorf("hub error %d: %s", resp.StatusCode, apiErr.Message)
	}
	if len(body) > 0 {
		return fmt.Errorf("hub error %d: %s", resp.StatusCode, string(body))
	}
	return fmt.Errorf("hub error %d", resp.StatusCode)
}
