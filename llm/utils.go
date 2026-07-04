package llm

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/zeozeozeo/x3/ddg"
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

// extract stuff in <search></search>
func extractSearch(s string) string {
	return extractTaggedContent(s, "<search>", "</search>")
}

func extractDiscordSearch(s string) string {
	return extractTaggedContent(s, "<discord>", "</discord>")
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
	for id, url := range src {
		dst[id] = url
	}
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

func getSearchResults(search string) (string, map[int]string) {
	citemap := make(map[int]string)
	search = strings.TrimSpace(search)
	if search == "" {
		return "<search query was empty>", citemap
	}

	slog.Info("running search", "query", search)
	var err error
	var results []ddg.Result
	for range 3 {
		results, err = ddg.Query(search, 10)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Sprintf("<failed to search for '%s': %v>", search, err), citemap
	}
	slog.Info("search: got results", "query", search, "results", len(results))
	if len(results) == 0 {
		return fmt.Sprintf("<The search for '%s' completed, but returned no results. Try a shorter or different query if the answer requires search.>", search), citemap
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n<You ran a search for '%s', here are the 10 search results. If these are not useful, you may run a new search. Make sure to use citing in your response when using relevant sources, e.g. [1]>\n", search)
	for i, res := range results {
		fmt.Fprintf(&sb, "---\n## Source [%d]: '%s'\nURL: %s\nContent: %s\n---\n", i+1, res.Title, res.URL, res.Info)
		citemap[i+1] = res.URL
	}

	return sb.String(), citemap
}
