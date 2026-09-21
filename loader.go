package resolver

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

type LoaderFunc func(uri string) (io.ReadCloser, error)

func NewHTTPLoader(timeout time.Duration) LoaderFunc {
	client := &http.Client{Timeout: timeout}
	return func(uri string) (io.ReadCloser, error) {
		resp, err := client.Get(uri)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", uri, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("fetching %s: unexpected status %s", uri, resp.Status)
		}
		return resp.Body, nil
	}
}

func NewInMemoryLoader(docs map[string][]byte) LoaderFunc {
	return func(uri string) (io.ReadCloser, error) {
		data, ok := docs[uri]
		if !ok {
			return nil, &FetchError{URI: uri, Err: ErrNotFound}
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
}
