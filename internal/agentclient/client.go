package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
)

const DefaultVsockPort = 1024

type Client struct {
	http *http.Client
}

func NewWithDialer(dialContext func(context.Context, string, string) (net.Conn, error)) *Client {
	transport := &http.Transport{
		DialContext:           dialContext,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &Client{http: &http.Client{Transport: transport}}
}

func NewFirecrackerVsock(socketPath string, port int) *Client {
	if port == 0 {
		port = DefaultVsockPort
	}
	return NewWithDialer(func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "unix", socketPath)
		if err != nil {
			return nil, err
		}
		if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
			_ = conn.Close()
			return nil, err
		}
		line, err := readLine(conn, 1024)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if _, err := parseConnectOK(line); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	})
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://vm/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Jobs(ctx context.Context, request agentapi.BatchRequest) (*agentapi.BatchResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://vm/jobs", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var decoded agentapi.BatchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&decoded); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &decoded, fmt.Errorf("jobs status %d", resp.StatusCode)
	}
	return &decoded, nil
}

func readLine(conn net.Conn, limit int) (string, error) {
	buf := make([]byte, 0, 64)
	one := make([]byte, 1)
	for len(buf) < limit {
		n, err := conn.Read(one)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		buf = append(buf, one[0])
		if one[0] == '\n' {
			return string(buf), nil
		}
	}
	return "", fmt.Errorf("line exceeded %d bytes", limit)
}

func parseConnectOK(line string) (int, error) {
	fields := bytes.Fields([]byte(line))
	if len(fields) != 2 || string(fields[0]) != "OK" {
		return 0, fmt.Errorf("unexpected vsock response %q", line)
	}
	port, err := strconv.Atoi(string(fields[1]))
	if err != nil || port <= 0 {
		return 0, fmt.Errorf("invalid assigned port %q", fields[1])
	}
	return port, nil
}
