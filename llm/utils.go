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

	for _, tag := range thinkingTags {
		startTag := tag[0]
		endTag := tag[1]

		if strings.HasPrefix(response, startTag) {
			relativeEndIdx := strings.Index(response[len(startTag):], endTag)

			if relativeEndIdx != -1 {
				endIdx := relativeEndIdx + len(startTag)
				thinkingContent := strings.TrimSpace(response[len(startTag):endIdx])
				remainingResponse := strings.TrimSpace(response[endIdx+len(endTag):])
				return thinkingContent, remainingResponse
			} else {
				// found a start tag but no end tag
				return response, ""
			}
		}
	}

	return "", response
}

// extract stuff in <search></search>
func extractSearch(s string) string {
	startTag := "<search>"
	endTag := "</search>"

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

func getSearchResults(search string) (string, map[int]string) {
	citemap := make(map[int]string)
	slog.Info("running search")
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
	slog.Info("search: got results", "results", len(results))

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n<You ran a search for '%s', here are the 10 search results. If these are not useful, you may run a new search. Make sure to use citing in your response when using relevant sources, e.g. [1]>\n", search)
	for i, res := range results {
		fmt.Fprintf(&sb, "---\n## Source [%d]: '%s'\nURL: %s\nContent: %s\n---\n", i+1, res.Title, res.URL, res.Info)
		citemap[i+1] = res.URL
	}

	return sb.String(), citemap
}

func replaceEnd(s string, pairs ...string) string {
	if len(pairs)%2 != 0 {
		panic("ReplaceEnd: odd argument count")
	}
	for i := 0; i < len(pairs); i += 2 {
		old, new := pairs[i], pairs[i+1]
		if before, found := strings.CutSuffix(s, old); found {
			return before + new
		}
	}
	return s
}
