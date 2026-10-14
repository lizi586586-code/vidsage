package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDownloadGzipExtension(t *testing.T) {
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	w.Write([]byte("extension bytes"))
	w.Close()
	for _, tc := range []struct {
		name      string
		status    int
		body      []byte
		wantError bool
	}{
		{"complete", 200, compressed.Bytes(), false},
		{"upstream error", 502, compressed.Bytes(), true},
		{"truncated", 200, compressed.Bytes()[:compressed.Len()-4], true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write(tc.body)
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "extension")
			err := downloadGzipExtension(context.Background(), server.URL, path)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError = %v", err, tc.wantError)
			}
			if !tc.wantError {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != "extension bytes" {
					t.Fatalf("downloaded bytes = %q, error = %v", got, err)
				}
			}
		})
	}
}

func TestRetryWithBackoffRetriesTransientFailure(t *testing.T) {
	attempts := 0
	wantErr := errors.New("temporary upstream failure")

	err := retryWithBackoff(context.Background(), 3, 0, func() error {
		attempts++
		if attempts < 3 {
			return wantErr
		}
		return nil
	})

	if err != nil {
		t.Fatalf("retryWithBackoff() error = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("operation attempts = %d, want 3", attempts)
	}
}

func TestRetryWithBackoffReturnsLastError(t *testing.T) {
	attempts := 0
	wantErr := errors.New("persistent upstream failure")

	err := retryWithBackoff(context.Background(), 2, 0, func() error {
		attempts++
		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("retryWithBackoff() error = %v, want %v", err, wantErr)
	}
	if attempts != 2 {
		t.Fatalf("operation attempts = %d, want 2", attempts)
	}
}

func TestRetryWithBackoffHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retryWithBackoff(ctx, 2, time.Hour, func() error {
		return errors.New("temporary upstream failure")
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retryWithBackoff() error = %v, want context.Canceled", err)
	}
}
