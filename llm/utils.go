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
