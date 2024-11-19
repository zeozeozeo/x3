package imager

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/vartanbeno/go-reddit/v2/reddit"
)

var (
	redditUsername         = os.Getenv("X3ZEO_REDDIT_USERNAME")
	redditID               = os.Getenv("X3ZEO_REDDIT_ID")
	redditSecret           = os.Getenv("X3ZEO_REDDIT_SECRET")
	allowedImageExtensions = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".tiff"}
)

func newClient() *reddit.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	// The reddit API does not like HTTP/2
	// Per https://pkg.go.dev/net/http?utm_source=gopls#pkg-overview ,
	// I'm copying http.DefaultTransport and replacing the HTTP/2 stuff
	transport := &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: dialer.DialContext,
		// change from default
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		// use an empty map instead of nil per the link above
		TLSNextProto:          make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	client, _ := reddit.NewReadonlyClient(
		reddit.WithHTTPClient(httpClient),
		reddit.WithUserAgent("insomnia/10.1.1"),
	)
	return client
}

func isImage(url string) bool {
	filename := path.Ext(url)
	for _, ext := range allowedImageExtensions {
		if ext == filename {
			return true
		}
	}
	return false
}

func GetRandomImageFromSubreddits(subreddits ...string) (string, error) {
	client := newClient()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*10))
	defer cancel()

	var url string
	attempts := 0
	for !isImage(url) {
		attempts++
		post, _, err := client.Post.RandomFromSubreddits(ctx, subreddits...)
		if err != nil {
			slog.Error("failed to get random image from subreddit", slog.Any("err", err), slog.Int("attempts", attempts))
			if attempts > 5 {
				return "", err
			}
			time.Sleep(time.Second)
			continue
		}
		if post == nil || post.Post == nil || post.Post.NSFW {
			slog.Debug("got nsfw/nonexistent image from subreddit, retrying", slog.String("url", post.Post.URL))
			continue
		}
		url = post.Post.URL
	}

	return url, nil
}
