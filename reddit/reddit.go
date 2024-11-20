package reddit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path"
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
	for _, ext := range allowedImageExtensions {
		if ext == filename {
			return true
		}
	}
	return false
}

type PostData struct {
	Selftext  string `json:"selftext"`
	Title     string `json:"title"`
	Downs     int    `json:"downs"`
	Ups       int    `json:"ups"`
	URL       string `json:"url"`
	Author    string `json:"author"`
	IsVideo   bool   `json:"is_video"`
	Over18    bool   `json:"over_18"`
	Permalink string `json:"permalink"`
}

func (p *PostData) GetPostLink() string {
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
		if attempts > 1000 {
			return post, errNoPosts
		}
		post = subreddit.Data.Children[rand.Intn(len(subreddit.Data.Children))]
		if !post.Data.Over18 && isImage(post.Data.URL) {
			break
		}
		attempts++
	}

	return post, nil
}
