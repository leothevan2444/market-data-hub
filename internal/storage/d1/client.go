package d1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Client struct {
	AccountID  string
	DatabaseID string
	APIToken   string
	HTTPClient *http.Client
}

type Statement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
}

func FromEnv() Client {
	return Client{
		AccountID:  os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
		DatabaseID: os.Getenv("D1_DATABASE_ID"),
		APIToken:   os.Getenv("CLOUDFLARE_API_TOKEN"),
		HTTPClient: http.DefaultClient,
	}
}

func (c Client) Enabled() bool {
	return c.AccountID != "" && c.DatabaseID != "" && c.APIToken != ""
}

func (c Client) Exec(ctx context.Context, sql string, params ...any) error {
	if !c.Enabled() {
		return nil
	}
	return c.post(ctx, map[string]any{"sql": sql, "params": params})
}

func (c Client) ExecBatch(ctx context.Context, statements []Statement) error {
	if !c.Enabled() || len(statements) == 0 {
		return nil
	}
	return c.post(ctx, map[string]any{"batch": statements})
}

func (c Client) post(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/d1/database/%s/query", c.AccountID, c.DatabaseID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("d1 query failed: %s: %s", res.Status, string(raw))
	}
	return nil
}

func (c Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}
