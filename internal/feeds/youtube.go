package feeds

// YouTube: the address of a channel, handle, playlist or video becomes the
// feed YouTube publishes for it, and the media:group of each entry (the
// feed has no content nor summary) becomes the article.

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/mmcdole/gofeed"

	"github.com/trapassati/ebi-furai/internal/netsafe"
)

const ytFeedBase = "https://www.youtube.com/feeds/videos.xml"

// ytConsentCookie is the "reject all" answer to the cookie banner: from the
// EU, YouTube sends anyone without one to consent.youtube.com instead of
// the page. It accepts nothing.
const ytConsentCookie = "SOCS=CAI"

// ErrYouTubeConsent: YouTube showed its cookie page instead of the channel.
var ErrYouTubeConsent = errors.New("YouTube asked to accept cookies instead of showing the channel: paste the channel address in the form youtube.com/channel/UC…")

var (
	ytChannelID  = regexp.MustCompile(`^UC[0-9A-Za-z_-]{22}$`)
	ytPlaylistID = regexp.MustCompile(`^[0-9A-Za-z_-]{12,64}$`)
	ytVideoID    = regexp.MustCompile(`^[0-9A-Za-z_-]{11}$`)
	ytHandle     = regexp.MustCompile(`^@[\p{L}\p{N}._-]{1,100}$`)
	ytName       = regexp.MustCompile(`^[\p{L}\p{N}._-]{1,100}$`)

	// The channel of a page, in order of trust: the canonical address of
	// a channel page, its RSS link, the id in its data.
	ytPageChannel = []*regexp.Regexp{
		regexp.MustCompile(`<link rel="canonical" href="https://www\.youtube\.com/channel/(UC[0-9A-Za-z_-]{22})"`),
		regexp.MustCompile(`feeds/videos\.xml\?channel_id=(UC[0-9A-Za-z_-]{22})`),
		regexp.MustCompile(`"externalId":"(UC[0-9A-Za-z_-]{22})"`),
		regexp.MustCompile(`itemprop="channelId" content="(UC[0-9A-Za-z_-]{22})"`),
	}
	ytVideoChannel = regexp.MustCompile(`"channelId":"(UC[0-9A-Za-z_-]{22})"`)
)

func isYouTubeHost(host string) bool {
	switch strings.ToLower(host) {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com", "youtu.be", "www.youtube-nocookie.com", "youtube-nocookie.com":
		return true
	}
	return false
}

// youTubeTarget tells what to do with a YouTube address: feed is the feed
// address when the URL already says which one, page the YouTube page to
// read to find the channel (a handle, a custom name, a video). Both empty:
// not a YouTube address this knows.
func youTubeTarget(u *url.URL) (feed, page string) {
	if u == nil || !isYouTubeHost(u.Hostname()) {
		return "", ""
	}
	q := u.Query()
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	first := ""
	if len(parts) > 0 {
		first, _ = url.PathUnescape(parts[0])
	}
	second := ""
	if len(parts) > 1 {
		second, _ = url.PathUnescape(parts[1])
	}
	if strings.EqualFold(u.Hostname(), "youtu.be") {
		if ytVideoID.MatchString(first) {
			return "", "https://www.youtube.com/watch?v=" + first
		}
		return "", ""
	}
	switch {
	case u.Path == "/feeds/videos.xml":
		if id := q.Get("channel_id"); ytChannelID.MatchString(id) {
			return ytFeedBase + "?channel_id=" + id, ""
		}
		if id := q.Get("playlist_id"); ytPlaylistID.MatchString(id) {
			return ytFeedBase + "?playlist_id=" + id, ""
		}
		if name := q.Get("user"); ytName.MatchString(name) {
			return ytFeedBase + "?user=" + url.QueryEscape(name), ""
		}
		return "", ""
	case playlistWithFeed(q.Get("list")):
		// A playlist, or a video opened from one: the playlist is what
		// was chosen.
		return ytFeedBase + "?playlist_id=" + q.Get("list"), ""
	case first == "channel" && ytChannelID.MatchString(second):
		return ytFeedBase + "?channel_id=" + second, ""
	case ytHandle.MatchString(first):
		return "", "https://www.youtube.com/" + url.PathEscape(first)
	case (first == "c" || first == "user") && ytName.MatchString(second):
		return "", "https://www.youtube.com/" + first + "/" + url.PathEscape(second)
	case first == "watch" && ytVideoID.MatchString(q.Get("v")):
		return "", "https://www.youtube.com/watch?v=" + q.Get("v")
	case (first == "shorts" || first == "live" || first == "embed" || first == "v") && ytVideoID.MatchString(second):
		return "", "https://www.youtube.com/watch?v=" + second
	}
	return "", ""
}

// playlistWithFeed: a playlist id that has a feed. Mixes (RD…), "Watch
// later" and "Liked" are made for one person and have none.
func playlistWithFeed(id string) bool {
	if !ytPlaylistID.MatchString(id) || strings.HasPrefix(id, "RD") {
		return false
	}
	return id != "WL" && id != "LL"
}

// youTubeFeed returns the feed of a YouTube address, or "" when raw is not
// one.
func (f *Fetcher) youTubeFeed(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil
	}
	feed, page := youTubeTarget(u)
	if feed != "" || page == "" {
		return feed, nil
	}
	resp, err := netsafe.Get(ctx, f.client, page, map[string]string{
		"Accept":          "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en",
		"Cookie":          ytConsentCookie,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if host := resp.Request.URL.Hostname(); strings.HasPrefix(host, "consent.") {
		return "", ErrYouTubeConsent
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNoFeed
	}
	if resp.StatusCode != http.StatusOK {
		return "", &httpError{status: resp.StatusCode}
	}
	body, err := netsafe.ReadLimited(resp.Body, maxFeedSize)
	if err != nil {
		return "", err
	}
	id := channelOfPage(string(body))
	if id == "" {
		if strings.Contains(string(body), "consent.youtube.com") && !strings.Contains(string(body), `"channelId"`) {
			return "", ErrYouTubeConsent
		}
		return "", ErrNoFeed
	}
	return ytFeedBase + "?channel_id=" + id, nil
}

// channelOfPage finds the channel id in a YouTube page. On a video page
// the video's channel comes first, from its details: the page also names
// the channels of other videos.
func channelOfPage(page string) string {
	if i := strings.Index(page, `"videoDetails":{`); i >= 0 {
		if m := ytVideoChannel.FindStringSubmatch(page[i:]); m != nil {
			return m[1]
		}
	}
	for _, re := range ytPageChannel {
		if m := re.FindStringSubmatch(page); m != nil {
			return m[1]
		}
	}
	return ""
}

// IsYouTubeShort tells whether an article link is a YouTube Short: the
// feed links them as youtube.com/shorts/ID, the other videos as watch?v=.
func IsYouTubeShort(link string) bool {
	u, err := url.Parse(link)
	if err != nil || !isYouTubeHost(u.Hostname()) {
		return false
	}
	return strings.HasPrefix(u.Path, "/shorts/")
}

// mediaHTML builds the content of an entry that only has a media:group
// (YouTube): the thumbnail, linked to the video, and the description,
// whose text keeps its line breaks and gets its links. Empty when there is
// no description and the entry is not a video.
func mediaHTML(it *gofeed.Item, link, thumb string) string {
	desc := ""
	if media, ok := it.Extensions["media"]; ok {
		for _, g := range media["group"] {
			for _, d := range g.Children["description"] {
				desc = d.Value
			}
		}
		if desc == "" {
			for _, d := range media["description"] {
				desc = d.Value
			}
		}
	}
	desc = strings.TrimSpace(desc)
	video := false
	if yt, ok := it.Extensions["yt"]; ok && len(yt["videoId"]) > 0 {
		video = true
	}
	if desc == "" && !video {
		return ""
	}
	var b strings.Builder
	if thumb != "" {
		b.WriteString("<p>")
		if link != "" {
			b.WriteString(`<a href="` + html.EscapeString(link) + `">`)
		}
		b.WriteString(`<img src="` + html.EscapeString(thumb) + `" alt="">`)
		if link != "" {
			b.WriteString("</a>")
		}
		b.WriteString("</p>")
	}
	for para := range strings.SplitSeq(strings.ReplaceAll(desc, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>")
		for i, line := range strings.Split(para, "\n") {
			if i > 0 {
				// The newline keeps the lines apart in the plain text too
				// (the preview and the search).
				b.WriteString("<br>\n")
			}
			b.WriteString(linkify(line))
		}
		b.WriteString("</p>")
	}
	return b.String()
}

var plainLink = regexp.MustCompile(`https?://[^\s<>"]+`)

// linkify escapes a line of plain text and turns its web addresses into
// links (without the punctuation that usually closes a sentence).
func linkify(line string) string {
	var b strings.Builder
	last := 0
	for _, m := range plainLink.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		for end > start && strings.ContainsRune(".,;:!?)]}'", rune(line[end-1])) {
			if line[end-1] == ')' && strings.Count(line[start:end], "(") >= strings.Count(line[start:end], ")") {
				break
			}
			end--
		}
		b.WriteString(html.EscapeString(line[last:start]))
		u := line[start:end]
		b.WriteString(`<a href="` + html.EscapeString(u) + `">` + html.EscapeString(u) + `</a>`)
		last = end
	}
	b.WriteString(html.EscapeString(line[last:]))
	return b.String()
}
