package podman

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const apiVersion = "v4.0.0"

// client wraps Podman's HTTP API.
type client struct {
	address string
	baseURL *url.URL
	http    *http.Client
}

func newClient(address string) (*client, error) {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return nil, errors.New("podman address is required")
	}
	baseURL, transport, err := parseAddress(addr)
	if err != nil {
		return nil, err
	}
	return &client{
		address: addr,
		baseURL: baseURL,
		http: &http.Client{
			Transport: transport,
			Timeout:   0,
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	res, err := c.do(ctx, http.MethodGet, "/libpod/info", nil, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return readAPIError(res)
	}
	return nil
}

func parseAddress(addr string) (*url.URL, *http.Transport, error) {
	if strings.HasPrefix(addr, "unix://") {
		socket := strings.TrimPrefix(addr, "unix://")
		if socket == "" {
			return nil, nil, errors.New("podman unix socket path is required")
		}
		transport := &http.Transport{
			DisableCompression: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		}
		baseURL, _ := url.Parse("http://unix")
		return baseURL, transport, nil
	}
	if strings.HasPrefix(addr, "tcp://") {
		addr = "http://" + strings.TrimPrefix(addr, "tcp://")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	baseURL, err := url.Parse(addr)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return baseURL, transport, nil
}

func (c *client) do(ctx context.Context, method, endpoint string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	if c == nil || c.http == nil || c.baseURL == nil {
		return nil, errors.New("podman client not initialized")
	}
	if query == nil {
		query = url.Values{}
	}
	reqURL := *c.baseURL
	joined := path.Join("/", apiVersion, strings.TrimPrefix(endpoint, "/"))
	reqURL.Path = joined
	reqURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.http.Do(req)
}

func readAPIError(res *http.Response) error {
	if res == nil {
		return errors.New("podman API error")
	}
	body, _ := io.ReadAll(res.Body)
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = res.Status
	}
	return fmt.Errorf("podman API error: %s", msg)
}

func candidateAddresses(primary string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	add(primary)

	runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeDir != "" {
		add(fmt.Sprintf("unix://%s", path.Join(runtimeDir, "podman", "podman.sock")))
	}
	userRunDir := path.Join("/run", "user", fmt.Sprintf("%d", os.Getuid()))
	if userRunDir != runtimeDir {
		add(fmt.Sprintf("unix://%s", path.Join(userRunDir, "podman", "podman.sock")))
	}
	add("unix:///run/podman/podman.sock")
	return out
}

func escapeImagePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	escaped := url.PathEscape(value)
	return strings.ReplaceAll(escaped, "%2F", "/")
}
