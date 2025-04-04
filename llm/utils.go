package llm

import "strings"

var thinkingTags = [][2]string{
	{"<think>", "</think>"},
	{"<thought>", "</thought>"},
	{"<thinking>", "</thinking>"},
	{"<reasoning>", "</reasoning>"},
}

// ExtractThinking returns the thinking content and the rest of the response (thinking, response)
func ExtractThinking(response string) (string, string) {
	foundAnyStartTag := false
	foundAnyEndTag := false
	for _, tag := range thinkingTags {
		startTag := tag[0]
		endTag := tag[1]

		startIdx := strings.Index(response, startTag)
		endIdx := strings.Index(response, endTag)

		if startIdx != -1 && !foundAnyEndTag {
			foundAnyStartTag = true
		}
		if endIdx != -1 {
			foundAnyEndTag = true
		}

		if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
			continue
		}

		thinkingContent := strings.TrimSpace(response[startIdx+len(startTag) : endIdx])
		return thinkingContent, strings.TrimSpace(response[endIdx+len(endTag):])
	}

	if foundAnyStartTag && !foundAnyEndTag {
		return response, ""
	}
	return "", response
}
