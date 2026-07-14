package requests

import (
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
	Timeout:   10 * time.Second,
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
	var body io.Reader = nil

	if payload != "" {
		body = strings.NewReader(payload)
	}

	req, err := http.NewRequest(method, url, body)

	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return doRequest(req, statusCode)
}
