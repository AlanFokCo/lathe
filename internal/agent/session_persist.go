package agent

import (
	asagent "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/protocol"
	"github.com/alanfokco/lathe/internal/session"
)

// persistHook is a loop.Hook that flushes new agent state.Context messages to
// the lathe session JSONL when a reply loop ends, mirroring the old engine's
// appendConv-to-session behavior. It embeds baseHook for no-op lifecycle
// methods and overrides OnLoopEnd.
type persistHook struct {
	baseHook
	state   *asagent.AgentState
	session *session.Session
	saved   int
}

func (h *persistHook) OnLoopEnd(_ error) {
	if h.session == nil || h.state == nil {
		return
	}
	for i := h.saved; i < len(h.state.Context); i++ {
		_ = h.session.Save(h.state.Context[i])
	}
	h.saved = len(h.state.Context)
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
