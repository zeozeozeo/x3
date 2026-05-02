package commands

import (
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/llm"
)

func testUser(id snowflake.ID, name string, bot bool) discord.User {
	return discord.User{ID: id, Username: name, Bot: bot}
}

func testMessage(id snowflake.ID, author discord.User, content string) discord.Message {
	return discord.Message{
		ID:        id,
		Author:    author,
		Content:   content,
		CreatedAt: time.Unix(int64(id), 0),
	}
}

func TestReplayMessagesForArchiveFullLobotomy(t *testing.T) {
	botID := snowflake.ID(100)
	user := testUser(1, "user", false)
	bot := testUser(botID, "x3", true)

	messagesNewestFirst := []discord.Message{
		testMessage(4, bot, "after"),
		testMessage(3, bot, "Lobotomized for this channel"),
		testMessage(2, bot, "before"),
		testMessage(1, user, "hello"),
	}
	messagesNewestFirst[1].Interaction = &discord.MessageInteraction{Name: "lobotomy"}

	got := replayMessagesForArchive(messagesNewestFirst, botID)
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1: %#v", len(got), got)
	}
	if got[0].Role != llm.RoleAssistant || got[0].Content != "after" {
		t.Fatalf("unexpected message: %#v", got[0])
	}
}

func TestReplayMessagesForArchivePartialLobotomy(t *testing.T) {
	botID := snowflake.ID(100)
	user := testUser(1, "user", false)
	bot := testUser(botID, "x3", true)

	messagesNewestFirst := []discord.Message{
		testMessage(5, bot, "after"),
		testMessage(4, bot, "Removed last 2 messages from the context"),
		testMessage(3, bot, "remove me too"),
		testMessage(2, user, "remove me"),
		testMessage(1, user, "keep me"),
	}
	messagesNewestFirst[1].Interaction = &discord.MessageInteraction{Name: "lobotomy"}

	got := replayMessagesForArchive(messagesNewestFirst, botID)
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %#v", len(got), got)
	}
	if got[0].Role != llm.RoleUser || got[0].Content != "user: keep me" {
		t.Fatalf("unexpected first message: %#v", got[0])
	}
	if got[1].Role != llm.RoleAssistant || got[1].Content != "after" {
		t.Fatalf("unexpected second message: %#v", got[1])
	}
}

func TestReplayMessagesForArchiveSkipsChatlogMessages(t *testing.T) {
	botID := snowflake.ID(100)
	user := testUser(1, "user", false)
	bot := testUser(botID, "x3", true)

	messagesNewestFirst := []discord.Message{
		testMessage(3, bot, "Exported 1 message."),
		testMessage(2, bot, "real response"),
		testMessage(1, user, "hello"),
	}
	messagesNewestFirst[0].Interaction = &discord.MessageInteraction{Name: "chatlog"}

	got := replayMessagesForArchive(messagesNewestFirst, botID)
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %#v", len(got), got)
	}
	if got[1].Content != "real response" {
		t.Fatalf("unexpected assistant message: %#v", got[1])
	}
}

func TestReplayMessagesForArchiveIgnoresEmptyDeferredLobotomy(t *testing.T) {
	botID := snowflake.ID(100)
	user := testUser(1, "user", false)
	bot := testUser(botID, "x3", true)

	messagesNewestFirst := []discord.Message{
		testMessage(3, bot, ""),
		testMessage(2, bot, "real response"),
		testMessage(1, user, "hello"),
	}
	messagesNewestFirst[0].Interaction = &discord.MessageInteraction{Name: "lobotomy"}

	got := replayMessagesForArchive(messagesNewestFirst, botID)
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %#v", len(got), got)
	}
	if got[1].Content != "real response" {
		t.Fatalf("unexpected assistant message: %#v", got[1])
	}
}
