// Package bytebin uploads content to a bytebin server, where spark's viewer
// reads it. spark: BytebinClient
package bytebin

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
)

// The content types spark's viewer recognises. spark: MediaTypes
const (
	SamplerMediaType = "application/x-spark-sampler"
	HeapMediaType    = "application/x-spark-heap"
	HealthMediaType  = "application/x-spark-health"
)

// Client posts to one bytebin server.
type Client struct {
	url       string
	userAgent string
	http      *http.Client
}

// New answers a client for the bytebin at url, identifying as userAgent.
func New(url, userAgent string) *Client {
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return &Client{url: url, userAgent: userAgent, http: &http.Client{Timeout: 20 * time.Second}}
}

// PostProto uploads msg gzip-compressed as contentType and answers its key;
// userAgentExtra, when set, is appended to the user agent after a slash.
func (c *Client) PostProto(ctx context.Context, msg proto.Message, contentType, userAgentExtra string) (string, error) {
	data, err := proto.Marshal(msg)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	if _, err := zw.Write(data); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"post", &body)
	if err != nil {
		return "", err
	}
	ua := c.userAgent
	if userAgentExtra != "" {
		ua += "/" + userAgentExtra
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("User-Agent", ua)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("bytebin: %s", resp.Status)
	}
	key := resp.Header.Get("Location")
	if key == "" {
		return "", errors.New("bytebin: key not returned")
	}
	return key, nil
}
