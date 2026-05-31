package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Store struct {
	AccountID string
	APIToken  string
	Bucket    string
	BaseURL   string
	Client    *http.Client
}

func FromEnv() Store {
	return Store{
		AccountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
		APIToken:  os.Getenv("CLOUDFLARE_API_TOKEN"),
		Bucket:    os.Getenv("R2_BUCKET"),
		Client:    http.DefaultClient,
	}
}

func (s Store) Get(ctx context.Context, key string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint(key), nil)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(req); err != nil {
		return nil, err
	}
	res, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("r2 get %s: %s: %s", key, res.Status, string(body))
	}
	return io.ReadAll(res.Body)
}

func (s Store) Put(ctx context.Context, key string, data []byte, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.endpoint(key), bytes.NewReader(data))
	if err != nil {
		return err
	}
	if err := s.authorize(req); err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("r2 put %s: %s: %s", key, res.Status, string(body))
	}
	return nil
}

func (s Store) Exists(ctx context.Context, key string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint(key), nil)
	if err != nil {
		return false, err
	}
	if err := s.authorize(req); err != nil {
		return false, err
	}
	res, err := s.client().Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return false, fmt.Errorf("r2 exists %s: %s: %s", key, res.Status, string(body))
	}
	return true, nil
}

func (s Store) endpoint(key string) string {
	escapedKey := strings.ReplaceAll(url.PathEscape(strings.TrimPrefix(key, "/")), "%2F", "%2F")
	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = "https://api.cloudflare.com/client/v4"
	}
	return fmt.Sprintf("%s/accounts/%s/r2/buckets/%s/objects/%s", strings.TrimRight(baseURL, "/"), s.AccountID, url.PathEscape(s.Bucket), escapedKey)
}

func (s Store) authorize(req *http.Request) error {
	if s.AccountID == "" || s.APIToken == "" || s.Bucket == "" {
		return fmt.Errorf("missing CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_API_TOKEN, or R2_BUCKET")
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)
	return nil
}

func (s Store) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}
