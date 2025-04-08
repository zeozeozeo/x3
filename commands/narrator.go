package commands

import (
	"log/slog"
	"strings" // Added for buffering
	"sync"
	"time"

	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

const ratelimit = 10 * time.Second

var narrator *Narrator = NewNarrator()

func GetNarrator() *Narrator {
	return narrator
}

type narrationCallback func(llmer *llm.Llmer, response string)

type queuedNarration struct {
	llmer    llm.Llmer
	prepend  string
	callback narrationCallback
}

type Narrator struct {
	queue           []queuedNarration
	lastInteraction time.Time
	ticker          *time.Ticker
	mu              sync.Mutex
}

func NewNarrator() *Narrator {
	return &Narrator{ticker: time.NewTicker(ratelimit)}
}

func (n *Narrator) LastInteractionTime() time.Time { return n.lastInteraction }

func (n *Narrator) QueueNarration(llmer llm.Llmer, prepend string, cb narrationCallback) {
	slog.Info("narrator: queueing narration", slog.String("prepend", prepend), slog.Int("queue_len", len(n.queue)), slog.Int("num_messages", llmer.NumMessages()))
	n.mu.Lock()
	defer n.mu.Unlock()
	n.queue = append(n.queue, queuedNarration{llmer: llmer, prepend: prepend, callback: cb})
}

func (n *Narrator) Run() {
	slog.Info("narrator: starting mainloop", slog.String("ratelimit", ratelimit.String()))
	defer n.ticker.Stop()

	for range n.ticker.C {
		// check queue
		if len(n.queue) == 0 {
			continue
		}

		n.mu.Lock()
		qn := n.queue[0]
		n.mu.Unlock()

		// request completion
		slog.Info("narrator: requesting completion")
		meta := persona.PersonaStableNarrator
		p := persona.GetPersonaByMeta(meta, nil, "")
		qn.llmer.SetPersona(p) // TODO: custom personas
		// RequestCompletion now returns (chan llm.StreamChunk, error)
		llmChan, err := qn.llmer.RequestCompletion(model.GetModelByName(meta.Model), nil, meta.Settings, qn.prepend)

		// update last interaction time
		n.mu.Lock()
		n.lastInteraction = time.Now()
		n.mu.Unlock()

		if err != nil { // Handle immediate error from RequestCompletion
			slog.Error("narrator: failed to initiate stream request", slog.Any("err", err))
			// Remove the failed item from the queue before continuing
			n.mu.Lock()
			if len(n.queue) > 0 && &n.queue[0] == &qn { // Ensure we remove the correct item
				n.queue = n.queue[1:]
			}
			n.mu.Unlock()
			continue
		}

		// Consume stream and buffer response
		var fullResponse strings.Builder
		var streamErr error
		slog.Debug("narrator: consuming stream")
		for chunk := range llmChan {
			if chunk.Err != nil {
				streamErr = chunk.Err
				slog.Error("narrator: stream error received", slog.Any("err", streamErr))
				break
			}
			if chunk.Content != "" {
				fullResponse.WriteString(chunk.Content)
			}
			if chunk.Done {
				// We don't need usage info here, just wait for Done
				break
			}
		}
		slog.Debug("narrator: finished consuming stream")

		// Handle stream error if occurred
		if streamErr != nil {
			slog.Error("narrator: stream failed during consumption", slog.Any("err", streamErr))
			// Remove the failed item from the queue before continuing
			n.mu.Lock()
			if len(n.queue) > 0 && &n.queue[0] == &qn { // Ensure we remove the correct item
				n.queue = n.queue[1:]
			}
			n.mu.Unlock()
			continue
		}

		// Use the buffered response
		res := fullResponse.String()

		// callback
		if qn.callback != nil {
			qn.callback(&qn.llmer, res)
		}

		// Remove the processed item from the queue
		n.mu.Lock()
		if len(n.queue) > 0 && &n.queue[0] == &qn { // Ensure we remove the correct item
			n.queue = n.queue[1:]
		} else {
			slog.Warn("narrator: queue state changed unexpectedly during processing")
		}
		n.mu.Unlock()
	}
}
