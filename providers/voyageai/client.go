package voyageai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xraph/nexus/provider"
)

type client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func newClient(apiKey, baseURL string) *client {
	return &client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("voyageai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("voyageai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("voyageai: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, fmt.Errorf("voyageai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var resp struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("voyageai: decode response: %w", err)
	}

	embeddings := make([][]float64, len(resp.Data))
	for i, d := range resp.Data {
		embeddings[i] = d.Embedding
	}

	return &provider.EmbeddingResponse{
		Provider:   "voyageai",
		Model:      req.Model,
		Embeddings: embeddings,
		Usage: provider.Usage{
			TotalTokens: resp.Usage.TotalTokens,
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	// Voyage AI does not have a dedicated health endpoint; send a minimal
	// embeddings request to verify connectivity.
	payload := map[string]any{
		"model": "voyage-3",
		"input": []string{"ping"},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body) //nolint:errcheck // drain body before close

	if httpResp.StatusCode >= 500 {
		return fmt.Errorf("voyageai: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

func (c *client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}
