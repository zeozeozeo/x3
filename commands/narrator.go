package commands

import (
	"log/slog"
	"sync"
	"time"

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
		p := persona.GetPersonaByMeta(meta, nil, "", false /* dm */, time.Time{})
		qn.llmer.SetPersona(p, nil) // TODO: custom personas
		res, _, err := qn.llmer.RequestCompletion(meta.GetModels(), nil, meta.Settings, qn.prepend)

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

		n.mu.Lock()
		n.queue = n.queue[1:]
		n.mu.Unlock()
	}
}
