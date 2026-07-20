package requests

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var tr = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}
var testHTTPClient = &http.Client{
	Transport: tr,
}

func doRequest(req *http.Request, statusCode int) (err error) {
	r, err := testHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, r.Body)
		if closeErr := r.Body.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	if r.StatusCode != statusCode {
		return fmt.Errorf("expected HTTP status %d, but got: %d", statusCode, r.StatusCode)
	}

	return nil
}

func DoHTTPRequest(url string, method string, headers map[string]string, payload string, statusCode int) error {
	return doHTTPRequest(context.Background(), url, method, headers, payload, statusCode)
}

// DoHTTPRequestWithTimeout drives an application request with the supplied
// timeout. A non-positive timeout preserves the no-deadline behavior of
// DoHTTPRequest.
func DoHTTPRequestWithTimeout(url string, method string, headers map[string]string, payload string, statusCode int, timeout time.Duration) error {
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return doHTTPRequest(ctx, url, method, headers, payload, statusCode)
}

func doHTTPRequest(ctx context.Context, url string, method string, headers map[string]string, payload string, statusCode int) error {
	var body io.Reader = nil

	if payload != "" {
		body = strings.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)

	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return doRequest(req, statusCode)
}
