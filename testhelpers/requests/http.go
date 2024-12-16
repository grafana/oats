package requests

import (
	"crypto/tls"
	"fmt"
	"net/http"
)

var tr = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
}
var testHTTPClient = &http.Client{Transport: tr}

func doRequest(req *http.Request, statusCode int) error {
	req.Header.Set("Content-Type", "application/json")

	r, err := testHTTPClient.Do(req)

	if err != nil {
		return err
	}

	if r.StatusCode != statusCode {
		return fmt.Errorf("expected HTTP status %d, but got: %d", statusCode, r.StatusCode)
	}

	return nil
}

func DoHTTPGet(url string, statusCode int) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		return err
	}

	return doRequest(req, statusCode)
}
