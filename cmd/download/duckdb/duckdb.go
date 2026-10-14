package main

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

// Bootstrap HTTPS using Go's TLS client: DuckDB needs httpfs before it can
// fetch other extensions over HTTPS. INSTALL still checks the extension signature.
func bootstrapHTTPFS(ctx context.Context, sqlDB *sql.DB) error {
	var version, platform string
	if err := sqlDB.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		return err
	}
	if err := sqlDB.QueryRowContext(ctx, "PRAGMA platform").Scan(&platform); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "duckdb-httpfs-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "httpfs.duckdb_extension")
	url := fmt.Sprintf("https://extensions.duckdb.org/%s/%s/httpfs.duckdb_extension.gz", version, platform)
	if err := retryWithBackoff(ctx, duckdbExtensionInstallAttempts, duckdbExtensionRetryDelay, func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return downloadGzipExtension(attemptCtx, url, path)
	}); err != nil {
		return fmt.Errorf("bootstrap httpfs: %w", err)
	}
	_, err = sqlDB.ExecContext(ctx, fmt.Sprintf("INSTALL '%s'; LOAD httpfs;", path))
	return err
}

func downloadGzipExtension(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("extension download HTTP %d", resp.StatusCode)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer zr.Close()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, zr)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// duckdbExtensions is the list of DuckDB extensions required by WeKnora's
// data analysis tool. `spatial` is used for layer metadata (st_read_meta)
// so we can enumerate sheet names from Excel files, while `excel` provides
// the dedicated read_xlsx reader with proper type inference.
var duckdbExtensions = []string{"spatial", "excel"}

const (
	duckdbExtensionInstallAttempts = 4
	duckdbExtensionRetryDelay      = 2 * time.Second
	duckdbExtensionAttemptTimeout  = 30 * time.Second
)

// retryWithBackoff retries an operation that may fail because an external
// service is temporarily unavailable. The context keeps a cancelled build
// from waiting for the next backoff interval.
func retryWithBackoff(ctx context.Context, attempts int, initialDelay time.Duration, operation func() error) error {
	if attempts < 1 {
		attempts = 1
	}

	var err error
	delay := initialDelay
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = operation(); err == nil {
			return nil
		}
		if attempt == attempts {
			break
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
		delay *= 2
	}

	return err
}

func installDuckDBExtension(ctx context.Context, sqlDB *sql.DB, extension string) error {
	if err := retryWithBackoff(ctx, duckdbExtensionInstallAttempts, duckdbExtensionRetryDelay, func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, duckdbExtensionAttemptTimeout)
		defer cancel()
		_, err := sqlDB.ExecContext(attemptCtx, fmt.Sprintf("INSTALL %s;", extension))
		return err
	}); err != nil {
		return fmt.Errorf("failed to install %s extension after %d attempts: %w", extension, duckdbExtensionInstallAttempts, err)
	}

	if err := retryWithBackoff(ctx, duckdbExtensionInstallAttempts, duckdbExtensionRetryDelay, func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, duckdbExtensionAttemptTimeout)
		defer cancel()
		_, err := sqlDB.ExecContext(attemptCtx, fmt.Sprintf("LOAD %s;", extension))
		return err
	}); err != nil {
		return fmt.Errorf("failed to load %s extension after %d attempts: %w", extension, duckdbExtensionInstallAttempts, err)
	}

	return nil
}

func downloadExtensions() {
	ctx := context.Background()

	sqlDB, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		panic(err)
	}
	defer sqlDB.Close()
	if err := bootstrapHTTPFS(ctx, sqlDB); err != nil {
		panic(err)
	}
	// The default HTTP endpoint is rejected by some release build networks.
	if _, err := sqlDB.ExecContext(ctx, "SET custom_extension_repository = 'https://extensions.duckdb.org'"); err != nil {
		panic(err)
	}

	for _, ext := range duckdbExtensions {
		if err := installDuckDBExtension(ctx, sqlDB, ext); err != nil {
			panic(err)
		}
	}
}

func main() {
	downloadExtensions()
}
