// Package webdav is a small WebDAV client: enough to keep copies of the
// backups in a folder of a WebDAV server (Nextcloud, ownCloud, a NAS,
// Koofr, …): upload, list, delete.
//
// The address is set by an administrator, so unlike the feeds it may be on
// the local network (a NAS at home). Redirects are not followed: the
// password must not be sent to another address.
package webdav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// Client talks to one folder of a WebDAV server.
type Client struct {
	base   *url.URL // the folder, ending with "/"
	user   string
	pass   string
	client *http.Client
}

// ErrAuth is a wrong username or password.
var ErrAuth = errors.New("the server refused the username or password")

// ParseURL checks a folder address: http or https, a host, no credentials
// or query in it.
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("the address must start with https:// (or http://) and name a server")
	}
	if u.User != nil {
		return nil, errors.New("put the username and the password in their own fields, not in the address")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("the address of the folder cannot have a query (?) or a fragment (#)")
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawPath = ""
	return u, nil
}

// New returns a client for the folder at rawURL.
func New(rawURL, user, pass string) (*Client, error) {
	u, err := ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		base: u, user: user, pass: pass,
		client: &http.Client{
			Timeout: 30 * time.Minute, // a large backup over a slow upload
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("the server redirects to another address: use that address instead")
			},
		},
	}, nil
}

// URL is the address of the folder.
func (c *Client) URL() string { return c.base.String() }

func (c *Client) fileURL(name string) string {
	u := *c.base
	u.Path = c.base.Path + name
	return u.String()
}

func (c *Client) do(ctx context.Context, method, target string, body io.Reader, size int64, header map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	req.Header.Set("User-Agent", "Ebi-Furai")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) && ue.Err != nil {
			err = ue.Err
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("could not reach the server: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, ErrAuth
	}
	return resp, nil
}

func statusError(resp *http.Response) error {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusConflict:
		return fmt.Errorf("the folder does not exist on the server (HTTP %d)", resp.StatusCode)
	case http.StatusInsufficientStorage:
		return errors.New("the server is full (HTTP 507)")
	case http.StatusMethodNotAllowed:
		return errors.New("this address does not accept WebDAV (HTTP 405)")
	}
	return fmt.Errorf("the server answered HTTP %d", resp.StatusCode)
}

// Check makes sure the folder can be reached with these credentials,
// creating it if it does not exist yet (only the last folder of the path).
func (c *Client) Check(ctx context.Context) error {
	if _, err := c.List(ctx); err == nil {
		return nil
	} else if errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "does not exist") {
		return err
	}
	resp, err := c.do(ctx, "MKCOL", c.base.String(), nil, 0, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed {
		return statusError(resp)
	}
	resp.Body.Close()
	_, err = c.List(ctx)
	return err
}

// Upload sends a file to the folder with the given name. It is written as
// a ".part" first and moved in place once complete when the server allows
// it, so a broken upload never looks like a backup.
func (c *Client) Upload(ctx context.Context, name, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	tmp := name + ".part"
	resp, err := c.do(ctx, http.MethodPut, c.fileURL(tmp), f, st.Size(), map[string]string{"Content-Type": "application/gzip"})
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}
	resp.Body.Close()
	resp, err = c.do(ctx, "MOVE", c.fileURL(tmp), nil, 0, map[string]string{"Destination": c.fileURL(name), "Overwrite": "T"})
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}
	resp.Body.Close()
	return nil
}

type multistatus struct {
	Responses []struct {
		Href string `xml:"href"`
		Prop []struct {
			ResourceType struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"prop>resourcetype"`
		} `xml:"propstat"`
	} `xml:"response"`
}

// List returns the names of the files in the folder.
func (c *Client) List(ctx context.Context) ([]string, error) {
	body := `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`
	resp, err := c.do(ctx, "PROPFIND", c.base.String(), strings.NewReader(body), int64(len(body)),
		map[string]string{"Depth": "1", "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, statusError(resp)
	}
	defer resp.Body.Close()
	var ms multistatus
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("listing the folder: %w", err)
	}
	names := []string{}
	for _, r := range ms.Responses {
		collection := false
		for _, p := range r.Prop {
			if p.ResourceType.Collection != nil {
				collection = true
			}
		}
		href := r.Href
		if u, err := url.Parse(href); err == nil {
			href = u.Path
		}
		if collection || strings.HasSuffix(href, "/") {
			continue
		}
		name := path.Base(href)
		if name != "" && name != "." && name != "/" {
			names = append(names, name)
		}
	}
	return names, nil
}

// Delete removes a file of the folder (a missing one is not an error).
func (c *Client) Delete(ctx context.Context, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, c.fileURL(name), nil, 0, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return statusError(resp)
	}
	resp.Body.Close()
	return nil
}
