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

/*
"kind": "t3",

	"data": {
		"approved_at_utc": null,
		"subreddit": "boykisser",
		"selftext": "You like thick thighs and thigh highs and fluffy tails don't you?~",
		"author_fullname": "t2_vdf7te33e",
		"saved": false,
		"mod_reason_title": null,
		"gilded": 0,
		"clicked": false,
		"title": "You like kissing Boys in cute hoodies and tight shorts don't you~",
		"link_flair_richtext": [],
		"subreddit_name_prefixed": "r/boykisser",
		"hidden": false,
		"pwls": null,
		"link_flair_css_class": "",
		"downs": 0,
		"thumbnail_height": 140,
		"top_awarded_type": null,
		"hide_score": false,
		"name": "t3_1gutsw5",
		"quarantine": false,
		"link_flair_text_color": "dark",
		"upvote_ratio": 1.0,
		"author_flair_background_color": null,
		"ups": 472,
		"total_awards_received": 0,
		"media_embed": {},
		"thumbnail_width": 140,
		"author_flair_template_id": null,
		"is_original_content": false,
		"user_reports": [],
		"secure_media": null,
		"is_reddit_media_domain": true,
		"is_meta": false,
		"category": null,
		"secure_media_embed": {},
		"link_flair_text": "boykisser",
		"can_mod_post": false,
		"score": 472,
		"approved_by": null,
		"is_created_from_ads_ui": false,
		"author_premium": false,
		"thumbnail": "https://b.thumbs.redditmedia.com/xFioJd19Q7rCclepzIDEGrgho6-7NAKejbj5K66oJqo.jpg",
		"edited": false,
		"author_flair_css_class": null,
		"author_flair_richtext": [],
		"gildings": {},
		"post_hint": "image",
		"content_categories": null,
		"is_self": false,
		"subreddit_type": "public",
		"created": 1732010620.0,
		"link_flair_type": "text",
		"wls": null,
		"removed_by_category": null,
		"banned_by": null,
		"author_flair_type": "text",
		"domain": "i.redd.it",
		"allow_live_comments": false,
		"selftext_html": "&lt;!-- SC_OFF --&gt;&lt;div class=\"md\"&gt;&lt;p&gt;You like thick thighs and thigh highs and fluffy tails don&amp;#39;t you?~&lt;/p&gt;\n&lt;/div&gt;&lt;!-- SC_ON --&gt;",
		"likes": null,
		"suggested_sort": null,
		"banned_at_utc": null,
		"url_overridden_by_dest": "https://i.redd.it/bfyrt3dt0u1e1.png",
		"view_count": null,
		"archived": false,
		"no_follow": false,
		"is_crosspostable": false,
		"pinned": false,
		"over_18": false,
		"preview": {
			"images": [
				{
					"source": {
						"url": "https://preview.redd.it/bfyrt3dt0u1e1.png?auto=webp&amp;s=5a361162b0d173940d10d53a6a81da767e771863",
						"width": 736,
						"height": 877
					},
					"resolutions": [
						{
							"url": "https://preview.redd.it/bfyrt3dt0u1e1.png?width=108&amp;crop=smart&amp;auto=webp&amp;s=0e56d0e4a384745aebce3e9d523ad8675d0f93af",
							"width": 108,
							"height": 128
						},
						{
							"url": "https://preview.redd.it/bfyrt3dt0u1e1.png?width=216&amp;crop=smart&amp;auto=webp&amp;s=9632044225d253a9d2a3e22aabdfea04a6ce62e6",
							"width": 216,
							"height": 257
						},
						{
							"url": "https://preview.redd.it/bfyrt3dt0u1e1.png?width=320&amp;crop=smart&amp;auto=webp&amp;s=b4129dd0b7e7b4238d16ff6dc481aa4b65b295f0",
							"width": 320,
							"height": 381
						},
						{
							"url": "https://preview.redd.it/bfyrt3dt0u1e1.png?width=640&amp;crop=smart&amp;auto=webp&amp;s=ab5ad779fa6d86197af4ea20c9d6ac815407fb7e",
							"width": 640,
							"height": 762
						}
					],
					"variants": {},
					"id": "oVAUexEKikkV0cI8r9H0m0ZgAFv9Uzd2rXRaizvjVtc"
				}
			],
			"enabled": true
		},
		"all_awardings": [],
		"awarders": [],
		"media_only": false,
		"link_flair_template_id": "5e62f62a-4acf-11ee-ad64-ce5f1439ce1c",
		"can_gild": false,
		"spoiler": false,
		"locked": false,
		"author_flair_text": null,
		"treatment_tags": [],
		"visited": false,
		"removed_by": null,
		"mod_note": null,
		"distinguished": null,
		"subreddit_id": "t5_4bbk2i",
		"author_is_blocked": false,
		"mod_reason_by": null,
		"num_reports": null,
		"removal_reason": null,
		"link_flair_background_color": "#dadada",
		"id": "1gutsw5",
		"is_robot_indexable": true,
		"report_reasons": null,
		"author": "Lexi_Bean21",
		"discussion_type": null,
		"num_comments": 52,
		"send_replies": true,
		"contest_mode": false,
		"mod_reports": [],
		"author_patreon_flair": false,
		"author_flair_text_color": null,
		"permalink": "/r/boykisser/comments/1gutsw5/you_like_kissing_boys_in_cute_hoodies_and_tight/",
		"stickied": false,
		"url": "https://i.redd.it/bfyrt3dt0u1e1.png",
		"subreddit_subscribers": 79868,
		"created_utc": 1732010620.0,
		"num_crossposts": 0,
		"media": null,
		"is_video": false
	}
*/

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

/*
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

func getSubreddit(name string) {

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
*/
