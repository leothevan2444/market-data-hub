package r2

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestStoreUsesCloudflareRESTAPI(t *testing.T) {
	var saved []byte
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		if r.URL.EscapedPath() != "/accounts/acct/r2/buckets/market-data/objects/daily%2Fus%2F2024%2Ftest.json" {
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
		switch r.Method {
		case http.MethodPut:
			var err error
			saved, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			return response(http.StatusOK, ""), nil
		case http.MethodGet:
			if saved == nil {
				return response(http.StatusNotFound, ""), nil
			}
			return response(http.StatusOK, string(saved)), nil
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
		return response(http.StatusInternalServerError, ""), nil
	})}

	store := Store{AccountID: "acct", APIToken: "token", Bucket: "market-data", BaseURL: "https://api.test", Client: client}
	ctx := context.Background()
	if exists, err := store.Exists(ctx, "daily/us/2024/test.json"); err != nil || exists {
		t.Fatalf("expected missing object before put, exists=%v err=%v", exists, err)
	}
	if err := store.Put(ctx, "daily/us/2024/test.json", []byte("ok"), "application/json"); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx, "daily/us/2024/test.json"); err != nil || !exists {
		t.Fatalf("expected object after put, exists=%v err=%v", exists, err)
	}
	got, err := store.Get(ctx, "daily/us/2024/test.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("unexpected payload: %q", string(got))
	}
}

func TestStoreRetriesPutOnTransientFailures(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		attempts++
		switch attempts {
		case 1:
			return response(http.StatusInternalServerError, "try again"), nil
		case 2:
			return response(http.StatusTooManyRequests, "slow down"), nil
		default:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "ok" {
				t.Fatalf("unexpected payload: %q", string(body))
			}
			return response(http.StatusOK, ""), nil
		}
	})}

	store := Store{
		AccountID:       "acct",
		APIToken:        "token",
		Bucket:          "market-data",
		BaseURL:         "https://api.test",
		Client:          client,
		PutRetryBackoff: -1,
	}
	if err := store.Put(context.Background(), "daily/us/2024/test.json", []byte("ok"), "application/json"); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 put attempts, got %d", attempts)
	}
}

func TestStoreDoesNotRetryPutOnClientError(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts++
		return response(http.StatusForbidden, "forbidden"), nil
	})}

	store := Store{
		AccountID:       "acct",
		APIToken:        "token",
		Bucket:          "market-data",
		BaseURL:         "https://api.test",
		Client:          client,
		PutRetryBackoff: -1,
	}
	if err := store.Put(context.Background(), "daily/us/2024/test.json", []byte("ok"), "application/json"); err == nil {
		t.Fatal("expected put to fail")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 put attempt, got %d", attempts)
	}
}

func TestPutRetryDelayForRateLimit(t *testing.T) {
	store := Store{}
	res := response(http.StatusTooManyRequests, "")
	res.Header.Set("Retry-After", "7")
	if delay := store.putRetryDelay(res, 0); delay != 7*time.Second {
		t.Fatalf("expected retry-after delay, got %s", delay)
	}

	res = response(http.StatusTooManyRequests, "")
	if delay := store.putRetryDelay(res, 0); delay != defaultRateLimitDelay {
		t.Fatalf("expected default rate-limit delay, got %s", delay)
	}
}

func TestRetryAfterDelayParsesHTTPDate(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	value := now.Add(3 * time.Second).Format(http.TimeFormat)
	delay, ok := retryAfterDelay(value, now)
	if !ok || delay != 3*time.Second {
		t.Fatalf("expected 3s retry-after date delay, got %s ok=%v", delay, ok)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{},
	}
}
