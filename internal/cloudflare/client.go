package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

type Client struct {
	ZoneID  string
	Token   string
	BaseURL string
	HTTP    *http.Client
}

type Record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiMessage    `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type apiMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewClient(zoneID, token string) *Client {
	return &Client{
		ZoneID:  zoneID,
		Token:   token,
		BaseURL: defaultBaseURL,
		HTTP:    http.DefaultClient,
	}
}

func (c *Client) ListRecords(ctx context.Context, recordType, name string) ([]Record, error) {
	query := url.Values{}
	if recordType != "" {
		query.Set("type", recordType)
	}
	query.Set("name", name)
	path := fmt.Sprintf("/zones/%s/dns_records?%s", c.ZoneID, query.Encode())
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var records []Record
	if err := json.Unmarshal(body, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (c *Client) DeleteRecord(ctx context.Context, record Record) error {
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/dns_records/%s", c.ZoneID, record.ID), nil)
	return err
}

func (c *Client) CreateRecord(ctx context.Context, record Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", c.ZoneID), payload)
	return err
}

func (c *Client) UpdateRecord(ctx context.Context, record Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPatch, fmt.Sprintf("/zones/%s/dns_records/%s", c.ZoneID, record.ID), payload)
	return err
}

func (c *Client) do(ctx context.Context, method, path string, payload []byte) (json.RawMessage, error) {
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed apiResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("cloudflare returned non-json response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !parsed.Success {
		return nil, fmt.Errorf("cloudflare %s %s failed: status=%d errors=%v", method, path, resp.StatusCode, parsed.Errors)
	}
	return parsed.Result, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
