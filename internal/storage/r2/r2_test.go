package r2

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
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
