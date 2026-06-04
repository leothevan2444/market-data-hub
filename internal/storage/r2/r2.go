package r2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Store struct {
	AccountID       string
	APIToken        string
	Bucket          string
	BaseURL         string
	Client          *http.Client
	PutMaxRetries   int
	PutRetryBackoff time.Duration
}

const (
	defaultPutMaxRetries   = 3
	defaultPutRetryBackoff = 250 * time.Millisecond
	defaultRateLimitDelay  = 2 * time.Second
	maxPutRetryBackoff     = 5 * time.Second
)

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
	var lastErr error
	for attempt := 0; attempt <= s.putMaxRetries(); attempt++ {
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
			lastErr = err
			if attempt < s.putMaxRetries() {
				if sleepErr := sleepContext(ctx, s.putNetworkRetryDelay(attempt)); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return err
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		_ = res.Body.Close()
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("r2 put %s: %s: %s", key, res.Status, string(body))
		if !retryablePutStatus(res.StatusCode) || attempt >= s.putMaxRetries() {
			return lastErr
		}
		if err := sleepContext(ctx, s.putRetryDelay(res, attempt)); err != nil {
			return err
		}
	}
	if lastErr != nil {
		return lastErr
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

func (s Store) putMaxRetries() int {
	if s.PutMaxRetries > 0 {
		return s.PutMaxRetries
	}
	return defaultPutMaxRetries
}

func (s Store) putRetryDelay(res *http.Response, attempt int) time.Duration {
	if s.PutRetryBackoff < 0 {
		return 0
	}
	if res != nil && res.StatusCode == http.StatusTooManyRequests {
		if delay, ok := retryAfterDelay(res.Header.Get("Retry-After"), time.Now()); ok {
			return delay
		}
		return defaultRateLimitDelay
	}
	backoff := s.PutRetryBackoff
	if backoff == 0 {
		backoff = defaultPutRetryBackoff
	}
	delay := backoff * time.Duration(1<<attempt)
	if delay > maxPutRetryBackoff {
		return maxPutRetryBackoff
	}
	return delay
}

func (s Store) putNetworkRetryDelay(attempt int) time.Duration {
	return s.putRetryDelay(nil, attempt)
}

func retryAfterDelay(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	t, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if t.Before(now) {
		return 0, true
	}
	return t.Sub(now), true
}

func retryablePutStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
