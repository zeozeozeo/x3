package llm

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zeozeozeo/x3/websearch"
)

var thinkingTags = [][2]string{
	{"<think>", "</think>"},
	{"<thought>", "</thought>"},
	{"<thinking>", "</thinking>"},
	{"<reasoning>", "</reasoning>"},
}

// ExtractThinking returns the thinking content and the rest of the response (thinking, response)
func ExtractThinking(response string) (string, string) {
	response = strings.TrimSpace(response)
	lowerResponse := strings.ToLower(response)

	for _, tag := range thinkingTags {
		startTag := tag[0]
		if !strings.HasPrefix(lowerResponse, startTag) {
			continue
		}

		thinkingContent, remainingResponse, ok := extractLeadingThinkingBlock(response, lowerResponse, startTag)
		if ok {
			return thinkingContent, remainingResponse
		}

		// found a start tag but no end tag
		return response, ""
	}

	return "", response
}

func extractLeadingThinkingBlock(response, lowerResponse, startTag string) (string, string, bool) {
	depth := 1
	contentStart := len(startTag)
	searchFrom := contentStart

	for depth > 0 {
		tokenStart, tokenEnd, delta, ok := findNextThinkingToken(lowerResponse, searchFrom)
		if !ok {
			return "", "", false
		}
		if depth+delta == 0 {
			thinkingContent := strings.TrimSpace(stripThinkingSelfClosingTags(response[contentStart:tokenStart]))
			remainingResponse := strings.TrimSpace(stripLeadingThinkingArtifacts(response[tokenEnd:]))
			return thinkingContent, remainingResponse, true
		}
		depth += delta
		searchFrom = tokenEnd
	}

	return "", "", false
}

func findNextThinkingToken(lowerResponse string, from int) (start int, end int, delta int, ok bool) {
	bestStart := len(lowerResponse)
	bestEnd := 0
	bestDelta := 0
	found := false

	for _, tag := range thinkingTags {
		tagName := strings.TrimSuffix(strings.TrimPrefix(tag[0], "<"), ">")
		candidates := []struct {
			token string
			delta int
		}{
			{token: tag[0], delta: 1},
			{token: "<" + tagName + "/>", delta: 0},
			{token: "<" + tagName + " />", delta: 0},
			{token: tag[1], delta: -1},
		}

		for _, candidate := range candidates {
			idx := strings.Index(lowerResponse[from:], candidate.token)
			if idx == -1 {
				continue
			}
			absStart := from + idx
			if !found || absStart < bestStart {
				bestStart = absStart
				bestEnd = absStart + len(candidate.token)
				bestDelta = candidate.delta
				found = true
			}
		}
	}

	if !found {
		return 0, 0, 0, false
	}
	return bestStart, bestEnd, bestDelta, true
}

func stripThinkingSelfClosingTags(s string) string {
	for _, tag := range thinkingTags {
		tagName := strings.TrimSuffix(strings.TrimPrefix(tag[0], "<"), ">")
		s = strings.ReplaceAll(s, "<"+tagName+"/>", "")
		s = strings.ReplaceAll(s, "<"+tagName+" />", "")
	}
	return s
}

func stripLeadingThinkingArtifacts(s string) string {
	s = strings.TrimSpace(s)
	for {
		trimmed := false
		for _, tag := range thinkingTags {
			tagName := strings.TrimSuffix(strings.TrimPrefix(tag[0], "<"), ">")
			for _, prefix := range []string{"<" + tagName + "/>", "<" + tagName + " />", tag[1]} {
				if strings.HasPrefix(strings.ToLower(s), prefix) {
					s = strings.TrimSpace(s[len(prefix):])
					trimmed = true
				}
			}
		}
		if !trimmed {
			return s
		}
	}
}

func StripThinkingEdgeTags(s string) string {
	s = strings.TrimSpace(s)
	for {
		trimmed := false
		lower := strings.ToLower(s)
		for _, tag := range thinkingTags {
			tagName := strings.TrimSuffix(strings.TrimPrefix(tag[0], "<"), ">")
			for _, token := range []string{tag[0], tag[1], "<" + tagName + "/>", "<" + tagName + " />"} {
				if strings.HasPrefix(lower, token) {
					s = strings.TrimSpace(s[len(token):])
					lower = strings.ToLower(s)
					trimmed = true
				}
				if strings.HasSuffix(lower, token) {
					s = strings.TrimSpace(s[:len(s)-len(token)])
					lower = strings.ToLower(s)
					trimmed = true
				}
			}
		}
		if !trimmed {
			return s
		}
	}
}

// extractSearchAll returns every distinct <search> query, up to limit.
func extractSearchAll(s string, limit int) []string {
	return extractAllTaggedContent(s, "<search>", "</search>", limit)
}

func extractDiscordSearch(s string) string {
	return extractTaggedContent(s, "<discord>", "</discord>")
}

func extractAllTaggedContent(s, startTag, endTag string, limit int) []string {
	var queries []string
	seen := make(map[string]struct{})
	for {
		if limit > 0 && len(queries) >= limit {
			return queries
		}
		startIdx := strings.Index(s, startTag)
		if startIdx == -1 {
			return queries
		}
		endIdx := strings.Index(s[startIdx+len(startTag):], endTag)
		if endIdx == -1 {
			return queries
		}
		contentStart := startIdx + len(startTag)
		contentEnd := contentStart + endIdx
		query := strings.TrimSpace(s[contentStart:contentEnd])
		if query != "" {
			key := strings.ToLower(query)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				queries = append(queries, query)
			}
		}
		s = s[contentEnd+len(endTag):]
	}
}

func extractTaggedContent(s, startTag, endTag string) string {

	startIdx := strings.Index(s, startTag)
	if startIdx == -1 {
		return ""
	}

	endIdx := strings.Index(s[startIdx+len(startTag):], endTag)
	if endIdx == -1 {
		return ""
	}

	contentStart := startIdx + len(startTag)
	contentEnd := contentStart + endIdx

	return strings.TrimSpace(s[contentStart:contentEnd])
}

func formatCites(response string, citemap map[int]string) string {
	re := regexp.MustCompile(`\[(\d+)\]`)
	return re.ReplaceAllStringFunc(response, func(match string) string {
		var id int
		_, err := fmt.Sscanf(match, "[%d]", &id)
		if err != nil {
			return match
		}
		url, exists := citemap[id]
		if !exists {
			return match
		}
		return fmt.Sprintf("[[%d]](<%s>)", id, url)
	})
}

func remapSearchCites(text string, citemap map[int]string, offset int) (string, map[int]string) {
	if offset <= 0 || len(citemap) == 0 {
		return text, citemap
	}

	remapped := make(map[int]string, len(citemap))
	for id, url := range citemap {
		remapped[id+offset] = url
	}

	re := regexp.MustCompile(`\[(\d+)\]`)
	text = re.ReplaceAllStringFunc(text, func(match string) string {
		var id int
		if _, err := fmt.Sscanf(match, "[%d]", &id); err != nil {
			return match
		}
		if _, exists := citemap[id]; !exists {
			return match
		}
		return fmt.Sprintf("[%d]", id+offset)
	})

	return text, remapped
}

func mergeCitemaps(dst, src map[int]string) map[int]string {
	if dst == nil {
		dst = make(map[int]string, len(src))
	}
	maps.Copy(dst, src)
	return dst
}

func maxCiteID(citemap map[int]string) int {
	maxID := 0
	for id := range citemap {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

const (
	// maxSearchesPerTurn caps how many search tool calls one assistant turn can
	// execute, so a model cannot flood the prompt with results.
	maxSearchesPerTurn = 4
	// webSearchConcurrency bounds simultaneous provider requests.
	webSearchConcurrency = 3
)

// webQueryResult is one web search in a batch, before it is formatted.
type webQueryResult struct {
	Query   string
	Results []websearch.Result
	Notices []string
	Err     error
}

// resultsPerSearch keeps a multi-search turn close to the token cost of a
// single search by trading breadth per query for breadth across queries.
func resultsPerSearch(count int) int {
	switch {
	case count <= 1:
		return 10
	case count == 2:
		return 6
	default:
		return 4
	}
}

var (
	// webSearch is a variable so tests can inject a fake provider.
	webSearch = websearch.Query
)

// runWebSearches runs the queries concurrently and returns their results in
// the same order as the input, so citation numbering stays deterministic.
func runWebSearches(ctx context.Context, queries []string) []webQueryResult {
	out := make([]webQueryResult, len(queries))
	perQuery := resultsPerSearch(len(queries))
	sem := make(chan struct{}, webSearchConcurrency)
	var wg sync.WaitGroup
	for i, query := range queries {
		out[i].Query = query
		wg.Add(1)
		go func(i int, query string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			slog.Info("running search", "query", query)
			resp, err := webSearch(ctx, query, perQuery)
			if err != nil {
				out[i].Err = err
				return
			}
			out[i].Results = resp.Results
			out[i].Notices = resp.Notices
			slog.Info("search: got results", "query", query, "provider", resp.Provider,
				"results", len(resp.Results), "duration", time.Since(start))
		}(i, query)
	}
	wg.Wait()
	return out
}

// formatWebQuery renders one query's results as a tool result. Citation ids
// start at startID, and URLs already present in seen are not repeated.
func formatWebQuery(res webQueryResult, startID int, seen map[string]int) (string, map[int]string) {
	citemap := make(map[int]string)
	var sb strings.Builder
	if res.Err != nil {
		fmt.Fprintf(&sb, "<failed to search for '%s': %v>", res.Query, res.Err)
		return sb.String(), citemap
	}
	if len(res.Results) == 0 {
		fmt.Fprintf(&sb, "<The search for '%s' completed, but returned no results. Try a shorter or different query if the answer requires search.>", res.Query)
		return sb.String(), citemap
	}

	next := startID
	fmt.Fprintf(&sb, "\n<You ran a search for '%s', here are the %d search results. If these are not useful, you may run a new search. Make sure to use citing in your response when using relevant sources, e.g. [%d]>\n", res.Query, len(res.Results), next)
	if len(res.Notices) > 0 {
		fmt.Fprintf(&sb, "<Search notes: %s>\n", strings.Join(res.Notices, " "))
	}
	for _, r := range res.Results {
		if _, duplicate := seen[r.URL]; duplicate {
			fmt.Fprintf(&sb, "---\n## Source: '%s'\nURL: %s (already listed above, not repeated)\n---\n", r.Title, r.URL)
			continue
		}
		fmt.Fprintf(&sb, "---\n## Source [%d]: '%s'\nURL: %s\n", next, r.Title, r.URL)
		if r.Published != "" {
			fmt.Fprintf(&sb, "Published: %s\n", r.Published)
		}
		if r.Author != "" {
			fmt.Fprintf(&sb, "Author: %s\n", r.Author)
		}
		fmt.Fprintf(&sb, "Content: %s\n---\n", r.Info)
		seen[r.URL] = next
		citemap[next] = r.URL
		next++
	}
	if len(citemap) == 0 {
		sb.WriteString("<All results for this query were already listed above.>\n")
	}
	return sb.String(), citemap
}

// searchWeb runs the queries and renders them into one block with continuous
// citation numbering.
func searchWeb(ctx context.Context, queries []string) (string, map[int]string) {
	results := runWebSearches(ctx, queries)
	citemap := make(map[int]string)
	seen := make(map[string]int)
	var sb strings.Builder
	next := 1
	for _, res := range results {
		text, cm := formatWebQuery(res, next, seen)
		sb.WriteString(text)
		for id, url := range cm {
			citemap[id] = url
		}
		next += len(cm)
	}
	return sb.String(), citemap
}
