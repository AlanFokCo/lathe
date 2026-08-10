package agent

import (
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/protocol"
	"github.com/sirupsen/logrus"
)

// persistHook is a loop.Hook that flushes new agent state.Context messages to
// the lathe session JSONL when a reply loop ends, mirroring the old engine's
// appendConv-to-session behavior. It holds a back-reference to the Engine so
// it reads e.session/e.state live (tests assign e.session after construction;
// the hook picks it up at OnLoopEnd time).
//
// OnLoopEnd fires once per ReplyStream (deferred in agentscope's replyLoop),
// after state.Context is fully populated for that turn. `saved` persists
// across calls so only newly-appended messages are flushed.
type persistHook struct {
	baseHook
	e     *Engine
	saved int
}

func (h *persistHook) OnLoopEnd(_ error) {
	s := h.e.session
	st := h.e.state
	if s == nil || st == nil {
		return
	}
	for i := h.saved; i < len(st.Context); i++ {
		if err := s.Save(st.Context[i]); err != nil {
			logrus.WithError(err).Warn("session: failed to persist message")
		}
	}
	h.saved = len(st.Context)
}

// baseHook provides empty defaults for every loop.Hook method except
// OnLoopEnd (which concrete hooks override).
type baseHook struct{}

func (baseHook) BeforeModelCall(protocol.LoopState, int)              {}
func (baseHook) AfterModelCall(protocol.LoopState, int, error)        {}
func (baseHook) BeforeToolExec(protocol.LoopState, int, string)       {}
func (baseHook) AfterToolExec(protocol.LoopState, int, string, error) {}
func (baseHook) OnStateTransition(protocol.LoopState, protocol.LoopState, int) {
}
func (baseHook) OnLoopStart() {}
