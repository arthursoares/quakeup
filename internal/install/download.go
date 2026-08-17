package install

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const downloadAttempts = 3

// download fetches url into destPath (via a temp file + rename so a partial
// download is never mistaken for a finished one), reporting progress to the
// given step. Retries transient failures.
func (e *Engine) download(ctx context.Context, step StepID, url, destPath string) error {
	var lastErr error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		if attempt > 1 {
			e.log(step, fmt.Sprintf("retrying download (%d/%d)…", attempt, downloadAttempts))
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if lastErr = e.downloadOnce(ctx, step, url, destPath); lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("download failed after %d attempts: %w", downloadAttempts, lastErr)
}

func (e *Engine) downloadOnce(ctx context.Context, step StepID, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}

	tmp, err := os.CreateTemp(dirOf(destPath), ".quakeup-dl-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	var done int64
	total := resp.ContentLength
	buf := make([]byte, 256*1024)
	lastReport := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if time.Since(lastReport) > 150*time.Millisecond {
				e.progress(step, "downloading", done, total)
				lastReport = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	e.progress(step, "downloading", done, total)
	if total > 0 && done != total {
		return fmt.Errorf("truncated download: got %d of %d bytes", done, total)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), destPath)
}

func dirOf(p string) string {
	if i := lastSlash(p); i >= 0 {
		return p[:i]
	}
	return "."
}

func lastSlash(p string) int {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return i
		}
	}
	return -1
}
