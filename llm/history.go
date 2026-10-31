package llm

import "strings"

func NormalizeMessageForDedupe(content string) string {
	content = strings.ReplaceAll(content, "\u200B", "")
	content = strings.ReplaceAll(content, "<new_message>", " ")
	content = strings.ReplaceAll(content, "<new_message]", " ")
	return strings.Join(strings.Fields(content), " ")
}

func CollapseDuplicateAssistantMessages(messages []Message) []Message {
	if len(messages) < 2 {
		return messages
	}

	out := make([]Message, 0, len(messages))
	var prevAssistant string
	prevWasAssistant := false
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			norm := NormalizeMessageForDedupe(msg.Content)
			if prevWasAssistant && norm != "" && norm == prevAssistant {
				continue
			}
			prevAssistant = norm
			prevWasAssistant = true
		} else {
			prevAssistant = ""
			prevWasAssistant = false
		}
		out = append(out, msg)
	}
	return out
}

func MessagesForStorage(messages []Message, maxNonSystem int) []Message {
	out := TrimPersistedMessages(messages)
	out = CollapseDuplicateAssistantMessages(out)
	if maxNonSystem > 0 {
		trimmed := Llmer{Messages: out}
		if trimmed.TrimNonSystemMessages(maxNonSystem) {
			out = trimmed.Messages
		}
	}
	return out
}
