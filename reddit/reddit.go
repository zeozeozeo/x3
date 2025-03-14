package reddit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path"
	"slices"
	"strings"
)

var (
	errNoPosts             = errors.New("no posts in subreddit")
	allowedImageExtensions = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".tiff"}
)

const (
	UserAgent = "insomnia/10.1.1"
)

func isImage(url string) bool {
	filename := path.Ext(url)
	return slices.Contains(allowedImageExtensions, filename)
}

type MetadataProp struct {
	URL string `json:"u"`
}

type MetadataItem struct {
	Status string         `json:"status"`
	E      string         `json:"e"`
	Mime   string         `json:"m"`
	Props  []MetadataProp `json:"p"`
}

type PostData struct {
	Selftext      string                  `json:"selftext"`
	Title         string                  `json:"title"`
	Downs         int                     `json:"downs"`
	Ups           int                     `json:"ups"`
	URL           string                  `json:"url"`
	Author        string                  `json:"author"`
	IsVideo       bool                    `json:"is_video"`
	Over18        bool                    `json:"over_18"`
	Permalink     string                  `json:"permalink"`
	MediaMetadata map[string]MetadataItem `json:"media_metadata"`
}

func trimStringFromStr(s string, str string) string {
	if idx := strings.Index(s, str); idx != -1 {
		return s[:idx]
	}
	return s
}

func (p PostData) GetRandomImage() string {
	if isImage(p.URL) {
		return p.URL
	}

	var choices []string
	for k, v := range p.MediaMetadata {
		if v.E == "Image" && len(v.Props) != 0 {
			// super hacky way to convert a preview link like
			// https://preview.redd.it/qauf6koaluic1.png?width=108&amp;crop=smart&amp;auto=webp&amp;s=1e95f3c7cbd6c7ac12e6d1bb2a0cc1f930757be2
			// to an i.redd.it link
			ext := trimStringFromStr(path.Ext(v.Props[len(v.Props)-1].URL), "?")
			choices = append(choices, fmt.Sprintf("https://i.redd.it/%s%s", k, ext))
		}
	}

	if len(choices) > 0 {
		return choices[rand.Intn(len(choices))]
	}

	return ""
}

func (p PostData) GetPostLink() string {
	return fmt.Sprintf("https://www.reddit.com%s", p.Permalink)
}

type Post struct {
	Kind string   `json:"kind"`
	Data PostData `json:"data"`
}

type SubredditData struct {
	Children []Post `json:"children"`
}

type Subreddit struct {
	Kind string        `json:"kind"`
	Data SubredditData `json:"data"`
}

func getClient() *http.Client {
	return &http.Client{}
}

func getSubreddit(name string) (*Subreddit, error) {
	url := fmt.Sprintf("https://www.reddit.com/r/%s.json", name)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := getClient().Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to get subreddit: %s", string(body))
	}

	var subreddit Subreddit
	err = json.Unmarshal(body, &subreddit)
	if err != nil {
		return nil, err
	}

	return &subreddit, nil
}

func GetRandomImageFromSubreddits(subreddits ...string) (Post, error) {
	name := subreddits[rand.Intn(len(subreddits))]
	subreddit, err := getSubreddit(name)
	if err != nil {
		return Post{}, err
	}

	if len(subreddit.Data.Children) == 0 {
		return Post{}, errNoPosts
	}

	var post Post
	attempts := 0
	for {
		attempts++
		if attempts > 1000 {
			return post, errNoPosts
		}
		post = subreddit.Data.Children[rand.Intn(len(subreddit.Data.Children))]
		if post.Data.Over18 {
			continue
		}
		if len(post.Data.GetRandomImage()) != 0 {
			break
		}
	}

	return post, nil
}
