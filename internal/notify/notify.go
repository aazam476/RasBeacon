// Package notify sends a short message to a web address the owner chose:
// an ntfy topic (https://ntfy.sh/your-topic, or a self-hosted ntfy), which
// becomes a push notification on their phone, or a webhook (Discord, Home
// Assistant, and others that take a message).
//
// It is how RasBeacon announces a new version (see agent/update.go).
package notify

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
)

// CheckURL reports whether address can receive messages: an http:// or
// https:// address with a host, at most 2048 characters.
func CheckURL(address string) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(address) > 2048 {
		return errors.New("Enter a web address like https://ntfy.sh/your-topic.")
	}
	return nil
}

// Send posts a message. Discord webhooks get the JSON they expect; every
// other address gets the message as plain text, with ntfy's headers for a
// title, a link to open and an icon (other services ignore them).
func Send(ctx context.Context, client *http.Client, address, title, message, link string) error {
	if err := CheckURL(address); err != nil {
		return err
	}
	var req *http.Request
	var err error
	if isDiscord(address) {
		body, _ := json.Marshal(map[string]string{"content": "**" + title + "**\n" + message + "\n" + link})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, address, strings.NewReader(message))
		if err == nil {
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
			req.Header.Set("Title", title)
			req.Header.Set("Tags", "arrow_up")
			if link != "" {
				req.Header.Set("Click", link)
			}
		}
	}
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return nil
}

// isDiscord reports whether address is a Discord webhook.
func isDiscord(address string) bool {
	u, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "discord.com" || host == "discordapp.com" || strings.HasSuffix(host, ".discord.com")) &&
		strings.HasPrefix(u.Path, "/api/webhooks/")
}
