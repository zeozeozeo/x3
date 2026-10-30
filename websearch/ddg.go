package websearch

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// defaultUserAgent defines a default value for the user-agent header.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"

const ddgRequestTimeout = 5 * time.Second

// queryDDG requests the DuckDuckGo HTML endpoint and parses the results.
func queryDDG(query string, maxResult int) ([]Result, error) {
	return queryDDGWithProxy(query, maxResult, "")
}

func queryDDGWithProxy(query string, maxResult int, proxyURL string) ([]Result, error) {
	results := []Result{}
	queryURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: ddgRequestTimeout,
		}).DialContext,
	}
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return results, fmt.Errorf("parse proxy url error: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}

	client := &http.Client{Transport: transport, Timeout: ddgRequestTimeout}
	req, err := http.NewRequest("GET", queryURL, nil)
	if err != nil {
		return results, fmt.Errorf("new request error: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	response, err := client.Do(req)
	if err != nil {
		return results, fmt.Errorf("get %v error: %w", queryURL, err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return results, fmt.Errorf("status code error: %d %s", response.StatusCode, response.Status)
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return results, fmt.Errorf("goquery.NewDocument error: %w", err)
	}

	sel := doc.Find(".web-result")

	for i := range sel.Nodes {
		if maxResult == len(results) {
			break
		}
		node := sel.Eq(i)
		titleNode := node.Find(".result__a")

		info := node.Find(".result__snippet").Text()
		title := titleNode.Text()
		ref := ""

		if len(titleNode.Nodes) > 0 && len(titleNode.Nodes[0].Attr) > 2 {
			ref = getDDGURL(titleNode.Nodes[0].Attr[2].Val)
		}

		results = append(results[:], Result{Title: title, Info: info, URL: ref})
	}

	return results, nil
}

func getDDGURL(urlStr string) string {
	trimmed := strings.TrimPrefix(urlStr, "//duckduckgo.com/l/?uddg=")
	if before, _, ok := strings.Cut(trimmed, "&rut="); ok {
		decodedStr, err := url.PathUnescape(before)
		if err != nil {
			return ""
		}

		return decodedStr
	}

	return ""
}
