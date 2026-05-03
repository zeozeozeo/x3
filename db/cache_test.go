package db

import (
	"fmt"
	"testing"
)

func TestAddMemoryDedupeAndNormalize(t *testing.T) {
	cache := NewChannelCache()
	if !cache.AddMemory(" user   likes   rust ") {
		t.Fatal("expected memory to be added")
	}
	if cache.AddMemory("USER LIKES RUST") {
		t.Fatal("expected duplicate memory to be ignored")
	}
	if len(cache.Memories) != 1 || cache.Memories[0] != "USER LIKES RUST" {
		t.Fatalf("memories = %#v", cache.Memories)
	}
}

func TestAddMemoryLimit(t *testing.T) {
	cache := NewChannelCache()
	for i := 0; i < maxChatMemories+1; i++ {
		cache.AddMemory(fmt.Sprintf("memory %d", i))
	}
	if len(cache.Memories) != maxChatMemories {
		t.Fatalf("len = %d", len(cache.Memories))
	}
	if cache.Memories[0] == "memory 0" {
		t.Fatal("expected oldest memory to be dropped")
	}
}
