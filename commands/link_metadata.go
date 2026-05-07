package commands

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/zeozeozeo/x3/db"
)

const (
	maxLinkMetadataPerMessage = 3
	maxLinkMetadataBytes      = 512 * 1024
	linkMetadataSuccessTTL    = 7 * 24 * time.Hour
	linkMetadataFailureTTL    = 6 * time.Hour
)

var (
	urlRegexp          = regexp.MustCompile(`https?://[^\s<>"']+`)
	linkMetadataClient = &http.Client{
		Timeout: 6 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return validatePublicHTTPURL(req.URL)
		},
	}
)

type linkMetadata struct {
	URL         string   `json:"url,omitempty"`
	FinalURL    string   `json:"final_url,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	SiteName    string   `json:"site_name,omitempty"`
	Type        string   `json:"type,omitempty"`
	Image       string   `json:"image,omitempty"`
	Author      string   `json:"author,omitempty"`
	PublishedAt string   `json:"published_at,omitempty"`
	Duration    string   `json:"duration,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type oEmbedResponse struct {
	Title           string `json:"title"`
	URL             string `json:"url"`
	AuthorName      string `json:"author_name"`
	AuthorURL       string `json:"author_url"`
	HTML            string `json:"html"`
	Type            string `json:"type"`
	ProviderName    string `json:"provider_name"`
	ProviderURL     string `json:"provider_url"`
	ThumbnailURL    string `json:"thumbnail_url"`
	ThumbnailWidth  int    `json:"thumbnail_width"`
	ThumbnailHeight int    `json:"thumbnail_height"`
}

func augmentContentWithLinkMetadata(content string) string {
	summaries := linkMetadataSummaries(content)
	for _, summary := range summaries {
		content = appendContextLine(content, summary)
	}
	return content
}

func linkMetadataSummaries(content string) []string {
	urls := linksFromContent(content)
	if len(urls) == 0 {
		return nil
	}
	if len(urls) > maxLinkMetadataPerMessage {
		urls = urls[:maxLinkMetadataPerMessage]
	}

	out := make([]string, 0, len(urls))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for _, raw := range urls {
		summary, err := getCachedLinkMetadataSummary(ctx, raw)
		if err != nil {
			slog.Debug("failed to get link metadata", "err", err, "url", raw)
			continue
		}
		if summary != "" {
			out = append(out, summary)
		}
	}
	return out
}

func linksFromContent(content string) []string {
	matches := urlRegexp.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, raw := range matches {
		raw = normalizeLinkURL(raw)
		if raw == "" || isLikelyImageURL(raw) {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func normalizeLinkURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, ".,!?;:)]}")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

func getCachedLinkMetadataSummary(ctx context.Context, raw string) (string, error) {
	now := time.Now().UTC()
	entry, err := db.GetLinkMetadata(raw)
	if err == nil {
		ttl := linkMetadataSuccessTTL
		if entry.Error != "" {
			ttl = linkMetadataFailureTTL
		}
		staleKnownSiteFailure := entry.Error != "" && entry.Metadata == "" && shouldBypassCachedFailure(raw)
		if !staleKnownSiteFailure && now.Sub(entry.FetchedAt) < ttl {
			return entry.Metadata, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("failed to read link metadata cache", "err", err, "url", raw)
	}

	summary, fetchErr := fetchLinkMetadataSummary(ctx, raw)
	entry = db.LinkMetadataCacheEntry{
		URL:       raw,
		Metadata:  summary,
		FetchedAt: now,
	}
	if fetchErr != nil {
		entry.Error = fetchErr.Error()
	}
	if err := db.WriteLinkMetadata(entry); err != nil {
		slog.Warn("failed to cache link metadata", "err", err, "url", raw)
	}
	return summary, fetchErr
}

func fetchLinkMetadataSummary(ctx context.Context, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if err := validatePublicHTTPURL(parsed); err != nil {
		return "", err
	}

	if isYouTubeURL(raw) {
		if summary, err := fetchYouTubeOEmbedSummary(ctx, raw); err == nil && summary != "" {
			return summary, nil
		} else if err != nil {
			slog.Debug("youtube oembed metadata failed, falling back to html", "err", err, "url", raw)
		}
	}
	if isTwitterURL(raw) {
		if summary, err := fetchTwitterOEmbedSummary(ctx, raw); err == nil && summary != "" {
			return summary, nil
		} else if err != nil {
			slog.Debug("twitter oembed metadata failed, falling back to html", "err", err, "url", raw)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; x3bot/1.0)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.5,*/*;q=0.1")

	resp, err := linkMetadataClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return "", fmt.Errorf("unsupported content type %s", mediaType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLinkMetadataBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxLinkMetadataBytes {
		return "", fmt.Errorf("metadata response too large")
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("metadata response is not valid utf8")
	}

	meta, err := parseLinkMetadata(bytes.NewReader(body), raw, resp.Request.URL.String())
	if err != nil {
		return "", err
	}
	return formatLinkMetadata(meta), nil
}

func fetchYouTubeOEmbedSummary(ctx context.Context, raw string) (string, error) {
	endpoint, err := url.Parse("https://www.youtube.com/oembed")
	if err != nil {
		return "", err
	}
	q := endpoint.Query()
	q.Set("url", raw)
	q.Set("format", "json")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; x3bot/1.0)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := linkMetadataClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oembed HTTP %d", resp.StatusCode)
	}
	var payload oEmbedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		return "", err
	}
	meta := linkMetadata{
		URL:      raw,
		FinalURL: raw,
		Title:    payload.Title,
		SiteName: firstNonEmpty(payload.ProviderName, "YouTube"),
		Type:     payload.Type,
		Image:    payload.ThumbnailURL,
		Author:   payload.AuthorName,
	}
	return formatLinkMetadata(meta), nil
}

func isYouTubeURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "youtube.com" ||
		host == "www.youtube.com" ||
		host == "m.youtube.com" ||
		host == "youtu.be" ||
		strings.HasSuffix(host, ".youtube.com")
}

func fetchTwitterOEmbedSummary(ctx context.Context, raw string) (string, error) {
	endpoint, err := url.Parse("https://publish.twitter.com/oembed")
	if err != nil {
		return "", err
	}
	q := endpoint.Query()
	q.Set("url", raw)
	q.Set("omit_script", "true")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; x3bot/1.0)")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := linkMetadataClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oembed HTTP %d", resp.StatusCode)
	}
	var payload oEmbedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 128*1024)).Decode(&payload); err != nil {
		return "", err
	}
	text, published := parseTwitterOEmbedHTML(payload.HTML)
	meta := linkMetadata{
		URL:         raw,
		FinalURL:    payload.URL,
		Title:       twitterTitle(payload),
		Description: text,
		SiteName:    firstNonEmpty(payload.ProviderName, "Twitter/X"),
		Type:        payload.Type,
		Author:      payload.AuthorName,
		PublishedAt: published,
	}
	return formatLinkMetadata(meta), nil
}

func parseTwitterOEmbedHTML(raw string) (text, published string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return "", ""
	}
	text = cleanMetadataValue(doc.Find("blockquote.twitter-tweet p").First().Text())
	doc.Find("blockquote.twitter-tweet > a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		published = cleanMetadataValue(s.Text())
		return published == ""
	})
	return text, published
}

func twitterTitle(payload oEmbedResponse) string {
	if payload.AuthorName == "" {
		return firstNonEmpty(payload.ProviderName, "X post")
	}
	return payload.AuthorName + " on X"
}

func isTwitterURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "x.com" ||
		host == "www.x.com" ||
		host == "twitter.com" ||
		host == "www.twitter.com" ||
		host == "mobile.twitter.com" ||
		strings.HasSuffix(host, ".twitter.com")
}

func shouldBypassCachedFailure(raw string) bool {
	return isYouTubeURL(raw) || isTwitterURL(raw)
}

func parseLinkMetadata(r io.Reader, sourceURL, finalURL string) (linkMetadata, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return linkMetadata{}, err
	}
	m := linkMetadata{URL: sourceURL, FinalURL: finalURL}

	props := map[string][]string{}
	addProp := func(key, value string) {
		key = strings.ToLower(strings.TrimSpace(key))
		value = cleanMetadataValue(value)
		if key == "" || value == "" {
			return
		}
		props[key] = append(props[key], value)
	}

	doc.Find("meta").Each(func(_ int, s *goquery.Selection) {
		content, ok := s.Attr("content")
		if !ok {
			return
		}
		for _, attr := range []string{"property", "name", "itemprop"} {
			if key, ok := s.Attr(attr); ok {
				addProp(key, content)
			}
		}
	})

	first := func(keys ...string) string {
		for _, key := range keys {
			values := props[strings.ToLower(key)]
			for _, value := range values {
				if value != "" {
					return value
				}
			}
		}
		return ""
	}

	m.Title = first("og:title", "twitter:title", "title")
	if m.Title == "" {
		m.Title = cleanMetadataValue(doc.Find("title").First().Text())
	}
	m.Description = first("og:description", "twitter:description", "description")
	m.SiteName = first("og:site_name", "application-name")
	m.Type = first("og:type", "twitter:card")
	m.Image = first("og:image", "twitter:image", "thumbnailurl", "thumbnail")
	m.Author = first("author", "article:author")
	m.PublishedAt = first("article:published_time", "datepublished", "uploadDate")
	m.Duration = first("duration", "video:duration")

	m.Keywords = splitMetadataList(first("keywords", "news_keywords"))
	m.Tags = valuesForKeys(props, "article:tag", "video:tag")
	m.Tags = append(m.Tags, jsonLDTags(doc)...)
	m.Tags = compactStrings(m.Tags, 24)
	m.Keywords = compactStrings(m.Keywords, 24)

	return m, nil
}

func jsonLDTags(doc *goquery.Document) []string {
	var tags []string
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return true
		}
		var data any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return true
		}
		tags = append(tags, jsonLDKeywords(data)...)
		return len(tags) < 24
	})
	return tags
}

func jsonLDKeywords(v any) []string {
	switch t := v.(type) {
	case map[string]any:
		var out []string
		if kw, ok := t["keywords"]; ok {
			out = append(out, jsonLDKeywords(kw)...)
		}
		if graph, ok := t["@graph"]; ok {
			out = append(out, jsonLDKeywords(graph)...)
		}
		return out
	case []any:
		var out []string
		for _, item := range t {
			out = append(out, jsonLDKeywords(item)...)
		}
		return out
	case string:
		return splitMetadataList(t)
	default:
		return nil
	}
}

func valuesForKeys(values map[string][]string, keys ...string) []string {
	var out []string
	for _, key := range keys {
		for _, value := range values[strings.ToLower(key)] {
			out = append(out, splitMetadataList(value)...)
		}
	}
	return out
}

func splitMetadataList(value string) []string {
	value = strings.ReplaceAll(value, "|", ",")
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanMetadataValue(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func compactStrings(values []string, maxCount int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, min(len(values), maxCount))
	for _, value := range values {
		value = ellipsisTrim(cleanMetadataValue(value), 80)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= maxCount {
			break
		}
	}
	return out
}

func cleanMetadataValue(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func formatLinkMetadata(m linkMetadata) string {
	parts := []string{fmt.Sprintf("url=%q", m.URL)}
	if m.FinalURL != "" && m.FinalURL != m.URL {
		parts = append(parts, fmt.Sprintf("final_url=%q", m.FinalURL))
	}
	if m.Title != "" {
		parts = append(parts, fmt.Sprintf("title=%q", ellipsisTrim(m.Title, 180)))
	}
	if m.SiteName != "" {
		parts = append(parts, fmt.Sprintf("site=%q", ellipsisTrim(m.SiteName, 80)))
	}
	if m.Type != "" {
		parts = append(parts, fmt.Sprintf("type=%q", ellipsisTrim(m.Type, 80)))
	}
	if m.Description != "" {
		parts = append(parts, fmt.Sprintf("description=%q", ellipsisTrim(m.Description, 300)))
	}
	if m.Author != "" {
		parts = append(parts, fmt.Sprintf("author=%q", ellipsisTrim(m.Author, 80)))
	}
	if m.PublishedAt != "" {
		parts = append(parts, fmt.Sprintf("published=%q", ellipsisTrim(m.PublishedAt, 80)))
	}
	if m.Duration != "" {
		parts = append(parts, fmt.Sprintf("duration=%q", ellipsisTrim(m.Duration, 80)))
	}
	if len(m.Keywords) > 0 {
		parts = append(parts, fmt.Sprintf("keywords=%q", strings.Join(m.Keywords, ", ")))
	}
	if len(m.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("tags=%q", strings.Join(m.Tags, ", ")))
	}
	if m.Image != "" {
		parts = append(parts, fmt.Sprintf("image=%q", ellipsisTrim(m.Image, 180)))
	}
	if len(parts) == 1 {
		return ""
	}
	return "<link " + strings.Join(parts, " ") + ">"
}

func validatePublicHTTPURL(u *url.URL) error {
	if u == nil || u.Hostname() == "" {
		return fmt.Errorf("missing host")
	}
	if u.User != nil {
		return fmt.Errorf("url credentials are not allowed")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme")
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("private host is not allowed")
		}
		return nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host has no addresses")
	}
	for _, ip := range addrs {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok || !isPublicIP(addr) {
			return fmt.Errorf("private host is not allowed")
		}
	}
	return nil
}

func isPublicIP(ip netip.Addr) bool {
	if ip.Is4In6() {
		ip = netip.AddrFrom4(ip.As4())
	}
	return ip.IsValid() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified()
}
