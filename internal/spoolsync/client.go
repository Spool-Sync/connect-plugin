// Package spoolsync is the part of the SpoolSync API this module uses: find a
// spool by its OpenPrintTag instance_uuid, and set its weight.
package spoolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound is a spool SpoolSync does not hold.
var ErrNotFound = errors.New("spoolsync: no spool with that instance uuid")

// Source is what SpoolSync's weight history records for a reading from here.
const Source = "prusa-connect"

// maxBody bounds what is read of an answer.
const maxBody = 1 << 20

// Spool is what this module reads of a SpoolSync spool.
type Spool struct {
	ID string `json:"spoolId"`
	// CurrentWeightG is the gross weight, as a scale reads it: the filament
	// and the empty spool.
	CurrentWeightG float64 `json:"currentWeight_g"`
	Status         string  `json:"status"`
	FilamentType   struct {
		// SpoolWeightG is the weight of the empty spool.
		SpoolWeightG float64 `json:"spoolWeight_g"`
	} `json:"filamentType"`
}

// Client calls one SpoolSync server with one API token.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client of the server at base, such as https://spools.lan.
// The API is under /api/v1 there.
func New(base, token string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("spoolsync: %q is not an http(s) address", base)
	}

	if token == "" {
		return nil, errors.New("spoolsync: no API token")
	}

	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{base: u.String() + "/api/v1", token: token, http: hc}, nil
}

// FindByInstanceUUID is the spool whose tag has this instance_uuid, or ErrNotFound.
func (c *Client) FindByInstanceUUID(ctx context.Context, uuid string) (Spool, error) {
	var sp Spool
	err := c.do(ctx, http.MethodGet, "/spools/by-opt/"+url.PathEscape(uuid), nil, &sp)

	return sp, err
}

// SetWeight sets the gross weight of a spool.
func (c *Client) SetWeight(ctx context.Context, spoolID string, grossG float64) error {
	body := map[string]any{"weight_g": grossG, "source": Source}

	return c.do(ctx, http.MethodPut, "/spools/"+url.PathEscape(spoolID)+"/update-weight", body, nil)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var reader io.Reader

	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("spoolsync: encode: %w", err)
		}

		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return fmt.Errorf("spoolsync: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("spoolsync: %s %s: %w", method, path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("spoolsync: read %s %s: %w", method, path, err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("spoolsync: %s %s answered %d: %s", method, path, resp.StatusCode, firstLine(b))
	case out == nil:
		return nil
	}

	if err = json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("spoolsync: decode %s %s: %w", method, path, err)
	}

	return nil
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if len(s) > 200 {
		s = s[:200]
	}

	return s
}
