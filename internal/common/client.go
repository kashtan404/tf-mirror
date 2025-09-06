package common

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// HTTPClient represents an HTTP client with proxy support
type HTTPClient struct {
	client     *http.Client
	userAgent  string
	maxRetries int
}

// NewHTTPClient creates a new HTTP client with optional proxy support
func NewHTTPClient(config *RegistryConfig) (*HTTPClient, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}

	// Configure proxy if provided
	if config.ProxyURL != "" {
		proxyURL, err := url.Parse(config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}

		switch proxyURL.Scheme {
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
		case "socks5":
			dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
			}
			transport.Dial = dialer.Dial
		default:
			return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}

	return &HTTPClient{
		client:     client,
		userAgent:  config.UserAgent,
		maxRetries: config.MaxRetries,
	}, nil
}

// Get performs a GET request with retry logic
func (c *HTTPClient) Get(url string) (*http.Response, error) {
	return c.GetWithContext(context.Background(), url)
}

// GetWithContext performs a GET request with retry logic and context support
func (c *HTTPClient) GetWithContext(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", c.userAgent)

	var resp *http.Response
	var lastErr error

	for i := 0; i <= c.maxRetries; i++ {
		resp, lastErr = c.client.Do(req)
		if lastErr == nil && resp.StatusCode < 500 {
			return resp, nil
		}

		if resp != nil {
			resp.Body.Close()
		}

		if i < c.maxRetries {
			// Wait before retry with exponential backoff
			waitTime := time.Duration(1<<uint(i)) * time.Second
			time.Sleep(waitTime)
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", c.maxRetries, lastErr)
	}

	return resp, nil
}

// Close closes the HTTP client
func (c *HTTPClient) Close() error {
	if transport, ok := c.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}
