package commands

import (
	"log/slog"
	"sync"
	"time"

	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

const ratelimit = 30 * time.Second

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
	if time.Since(n.lastInteraction) > ratelimit {
		n.ticker.Reset(5 * time.Second)
	}
}

func (n *Narrator) Run() {
	slog.Info("narrator: starting mainloop", slog.String("ratelimit", ratelimit.String()))
	n.mu.Lock()
	n.ticker.Reset(1)
	n.mu.Unlock()
	defer n.ticker.Stop()

	for range n.ticker.C {
		// check queue
		n.mu.Lock()
		if len(n.queue) == 0 {
			n.mu.Unlock()
			continue
		}

		qn := n.queue[0]
		n.queue = n.queue[1:]
		n.mu.Unlock()

		// request completion
		slog.Info("narrator: requesting completion")
		meta := persona.PersonaStableNarrator
		p := persona.GetPersonaByMeta(meta, nil, "")
		qn.llmer.SetPersona(p) // TODO: custom personas
		res, _, err := qn.llmer.RequestCompletion(model.GetModelByName(meta.Model), nil, meta.Settings, qn.prepend)

		// update last interaction time
		n.mu.Lock()
		n.lastInteraction = time.Now()
		n.mu.Unlock()

		if err != nil {
			slog.Error("narrator: failed to request completion", slog.Any("err", err))
			continue
		}

		// callback
		if qn.callback != nil {
			qn.callback(&qn.llmer, res)
		}
	}
}
