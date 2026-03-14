package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/pkg/errors"
)

type Client struct {
	Server  string
	network string
	c       *http.Client
	JSONOut bool
}

// Connect establishes a connection to the GoCable server and retrieves network metadata.
func Connect(host, port string) (*Client, error) {
	c := &Client{
		Server: fmt.Sprintf("%s:%s/api/", host, port),
		c:      &http.Client{Timeout: 10 * time.Second},
	}

	resp, err := c.c.Get(c.Server + "networks")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("non-200: %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var networks []domain.Network
	err = json.Unmarshal(body, &networks)
	if err != nil {
		return nil, err
	}

	if len(networks) == 0 {
		return nil, errors.New("no networks")
	}

	c.network = networks[0].CallSign

	return c, nil
}

// readBody extracts the response body as a trimmed string for error reporting.
func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b))
}

// CurrentChannel retrieves the metadata for the currently tuned live channel.
func (c *Client) CurrentChannel() (domain.Channel, error) {
	var channel domain.Channel

	resp, err := c.c.Get(c.Server + "networks/" + url.PathEscape(c.network) + "/live")
	if err != nil {
		return channel, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return channel, errors.Errorf("server error %d: %s", resp.StatusCode, readBody(resp))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return channel, err
	}

	err = json.Unmarshal(body, &channel)
	if err != nil {
		return channel, err
	}

	return channel, nil
}

// Channels retrieves a list of all active channels on the network.
func (c *Client) Channels() ([]domain.Channel, error) {
	var channels []domain.Channel

	resp, err := c.c.Get(c.Server + "networks/" + url.PathEscape(c.network) + "/channels")
	if err != nil {
		return channels, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return channels, errors.Errorf("server error %d: %s", resp.StatusCode, readBody(resp))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return channels, err
	}

	err = json.Unmarshal(body, &channels)
	if err != nil {
		return channels, err
	}

	return channels, nil
}

// Tune switches the network's live tuner to the specified channel ID.
func (c *Client) Tune(channelID string) (domain.Channel, error) {
	var channel domain.Channel
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%snetworks/%s/channels/%s/set_live", c.Server, url.PathEscape(c.network), url.PathEscape(channelID)), nil)
	if err != nil {
		return channel, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.c.Do(req)
	if err != nil {
		return channel, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return channel, errors.Errorf("server error %d: %s", resp.StatusCode, readBody(resp))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return channel, err
	}

	err = json.Unmarshal(body, &channel)
	if err != nil {
		return channel, err
	}

	return channel, nil
}

// LiveNext advances the currently tuned live channel to the next media item.
func (c *Client) LiveNext() (domain.Channel, error) {
	var channel domain.Channel
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%snetworks/%s/live/next", c.Server, url.PathEscape(c.network)), nil)
	if err != nil {
		return channel, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.c.Do(req)
	if err != nil {
		return channel, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return channel, errors.Errorf("server error %d: %s", resp.StatusCode, readBody(resp))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return channel, err
	}

	err = json.Unmarshal(body, &channel)
	if err != nil {
		return channel, err
	}

	return channel, nil
}
