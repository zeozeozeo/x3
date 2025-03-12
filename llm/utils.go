package llm

import "strings"

var thinkingTags = [][2]string{
	{"<think>", "</think>"},
	{"<reasoning>", "</reasoning>"},
}

func ExtractThinking(response string) (string, string) {
	for _, tag := range thinkingTags {
		startTag := tag[0]
		endTag := tag[1]

		startIdx := strings.Index(response, startTag)
		endIdx := strings.Index(response, endTag)

		if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
			continue
		}

		thinkingContent := strings.TrimSpace(response[startIdx+len(startTag) : endIdx])
		return thinkingContent, strings.TrimSpace(response[endIdx+len(endTag):])
	}

	return "", response
}
