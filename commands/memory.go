package commands

import (
	"regexp"
	"strings"

	"github.com/zeozeozeo/x3/llm"
)

var memoryTagRegexp = regexp.MustCompile(`(?is)<memory\b[^>]*>(.*?)</memory>`)

func extractMemoryTags(response string) (string, []string) {
	matches := memoryTagRegexp.FindAllStringSubmatch(response, -1)
	memories := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		memory := strings.TrimSpace(match[1])
		if memory != "" {
			memories = append(memories, memory)
		}
	}

	response = memoryTagRegexp.ReplaceAllString(response, "")
	response = strings.TrimSpace(response)
	return response, memories
}

func setLatestAssistantMessageContent(llmer *llm.Llmer, content string) {
	if llmer == nil {
		return
	}
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleAssistant {
			llmer.Messages[i].Content = content
			return
		}
	}
}
