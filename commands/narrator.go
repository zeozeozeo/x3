package commands

import (
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
)

const ratelimit = 10 * time.Second

var narrator *Narrator = NewNarrator()

func GetNarrator() *Narrator {
	return narrator
}

type narrationCallback func(llmer *llm.Llmer, response string)

type queuedNarration struct {
	llmer     llm.Llmer
	prepend   string
	callback  narrationCallback
	isSummary bool
	channelID snowflake.ID
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

func (n *Narrator) QueueSummaryGeneration(channelID snowflake.ID, llmer llm.Llmer) {
	slog.Info("narrator: queueing summary generation", slog.String("channel_id", channelID.String()))
	n.mu.Lock()
	defer n.mu.Unlock()
	n.queue = append(n.queue, queuedNarration{llmer: llmer, isSummary: true, channelID: channelID})
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
		slog.Info("narrator: requesting completion", slog.Bool("is_summary", qn.isSummary))
		var meta persona.PersonaMeta
		if qn.isSummary {
			meta = persona.PersonaSummaryGenerator
		} else {
			meta = persona.PersonaStableNarrator
		}
		p := persona.GetPersonaByMeta(meta, persona.Summary{}, "", false /* dm */, time.Time{}, nil)
		qn.llmer.SetPersona(p, nil) // TODO: custom personas
		res, _, err := qn.llmer.RequestCompletion(meta.GetModels(), meta.Settings, qn.prepend)

		// update last interaction time
		n.mu.Lock()
		n.lastInteraction = time.Now()
		n.mu.Unlock()

		if err != nil {
			slog.Error("narrator: failed to request completion", "err", err)
			continue
		}

		// callback
		if qn.isSummary {
			cache := db.GetChannelCache(qn.channelID)
			cache.UpdateSummary(persona.Summary{Str: res})
			if err := cache.Write(qn.channelID); err != nil {
				slog.Error("narrator: failed to write summary to cache", "err", err)
			} else {
				slog.Info("narrator: summary updated", slog.String("channel_id", qn.channelID.String()))
			}
		} else if qn.callback != nil {
			qn.callback(&qn.llmer, res)
		}

		n.mu.Lock()
		n.queue = n.queue[1:]
		n.mu.Unlock()
	}
}
