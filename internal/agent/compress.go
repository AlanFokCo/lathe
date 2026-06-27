package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

// compressConfig controls when/how lathe compresses its conversation. Defaults
// mirror agentscope-go's agent.ContextConfig (whose withDefaults is unexported).
type compressConfig struct {
	TriggerRatio      float64
	ReserveRatio      float64
	ContextSize       int
	CompressionPrompt string
	SummaryTemplate   string
	SummarySchema     json.RawMessage
	ToolResultLimit   int
}

func defaultCompressConfig() compressConfig {
	return compressConfig{
		TriggerRatio:      0.8,
		ReserveRatio:      0.1,
		CompressionPrompt: defaultCompressionPrompt,
		SummaryTemplate:   defaultSummaryTemplate,
		SummarySchema:     defaultSummarySchema,
		ToolResultLimit:   50000,
	}
}

const defaultCompressionPrompt = "<system-hint>You have been working on the task described above " +
	"but have not yet completed it. Now write a continuation summary that will allow you to resume " +
	"work efficiently in a future context window where the conversation history will be replaced " +
	"with this summary. Your summary should be structured, concise, and actionable.</system-hint>"

const defaultSummaryTemplate = "<system-info>Here is a summary of your previous work\n" +
	"# Task Overview\n{task_overview}\n\n" +
	"# Current State\n{current_state}\n\n" +
	"# Important Discoveries\n{important_discoveries}\n\n" +
	"# Next Steps\n{next_steps}\n\n" +
	"# Context to Preserve\n{context_to_preserve}</system-info>"

var defaultSummarySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"task_overview": {"type": "string"},
		"current_state": {"type": "string"},
		"important_discoveries": {"type": "string"},
		"next_steps": {"type": "string"},
		"context_to_preserve": {"type": "string"}
	},
	"required": ["task_overview", "current_state", "important_discoveries", "next_steps", "context_to_preserve"]
}`)

// formatSummary fills the template's {field} placeholders from the structured output.
func formatSummary(template string, result json.RawMessage) (string, error) {
	var fields map[string]string
	if err := json.Unmarshal(result, &fields); err != nil {
		return "", fmt.Errorf("unmarshal structured output: %w", err)
	}
	out := template
	for k, v := range fields {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out, nil
}

// buildCompressionMessages assembles the messages sent to the model to produce
// a summary: system prompt, (optional prior summary), the messages to compress,
// and the compression prompt.
func buildCompressionMessages(systemPrompt, summary string, toCompress []*message.Msg, compressionPrompt string) []*message.Msg {
	msgs := make([]*message.Msg, 0, len(toCompress)+3)
	msgs = append(msgs, message.SystemMsg("system", systemPrompt))
	if summary != "" {
		msgs = append(msgs, message.UserMsg("user", summary))
	}
	msgs = append(msgs, toCompress...)
	msgs = append(msgs, message.UserMsg("user", compressionPrompt))
	return msgs
}

func copyMsgs(msgs []*message.Msg) []*message.Msg {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*message.Msg, len(msgs))
	copy(out, msgs)
	return out
}

// TokenCounter is the subset of model.ChatModel needed for token counting.
type TokenCounter interface {
	CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int
}

// splitContextForCompression splits conv (system at [0]) into (toCompress, toReserve)
// over conv[1:]. It walks backward, accumulating the reserve until the token budget
// is reached, keeping tool_call/result pairs together (via adjustSplitForToolPairs).
func splitContextForCompression(conv []*message.Msg, reserveBudget int, counter TokenCounter, tools []model.ToolSchema) (toCompress, toReserve []*message.Msg) {
	if len(conv) <= 1 {
		return nil, nil
	}
	ctxMsgs := conv[1:]
	systemMsg := conv[0]
	baseMsgs := []*message.Msg{systemMsg}

	if reserveBudget <= 0 {
		return copyMsgs(ctxMsgs), nil
	}
	splitIdx := len(ctxMsgs)
	for i := len(ctxMsgs) - 1; i >= 0; i-- {
		candidate := make([]*message.Msg, len(baseMsgs))
		copy(candidate, baseMsgs)
		candidate = append(candidate, ctxMsgs[i:]...)
		tokens := counter.CountTokens(candidate, tools)
		if tokens >= reserveBudget {
			splitIdx = i + 1
			break
		}
		if i == 0 {
			return nil, copyMsgs(ctxMsgs) // everything fits in reserve
		}
	}
	if splitIdx >= len(ctxMsgs) {
		splitIdx = adjustSplitForToolPairs(ctxMsgs, len(ctxMsgs)-1)
	} else {
		splitIdx = adjustSplitForToolPairs(ctxMsgs, splitIdx)
	}
	if splitIdx < 0 {
		splitIdx = 0
	}
	if splitIdx > len(ctxMsgs) {
		splitIdx = len(ctxMsgs)
	}
	return copyMsgs(ctxMsgs[:splitIdx]), copyMsgs(ctxMsgs[splitIdx:])
}

// adjustSplitForToolPairs pushes the split forward so a tool_result whose
// tool_call is in the compressed portion also moves to the compressed portion.
func adjustSplitForToolPairs(msgs []*message.Msg, splitIdx int) int {
	callIDs := make(map[string]bool)
	resultPositions := make(map[string]int)
	for i := splitIdx; i < len(msgs); i++ {
		for _, b := range msgs[i].GetContentBlocks(message.ContentBlockToolCall) {
			if tc, ok := b.(message.ToolCallBlock); ok {
				callIDs[tc.ID] = true
			}
		}
		for _, b := range msgs[i].GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok {
				resultPositions[tr.ID] = i
			}
		}
	}
	maxOrphanIdx := -1
	for id, pos := range resultPositions {
		if !callIDs[id] && pos > maxOrphanIdx {
			maxOrphanIdx = pos
		}
	}
	if maxOrphanIdx < 0 {
		return splitIdx
	}
	return maxOrphanIdx + 1
}

// compressContext checks if e.conv exceeds the context threshold and, if so,
// generates a structured summary of the older messages and replaces them.
// If force is true it compresses regardless of threshold (for /compact).
// Returns compacted=true if compression happened, plus before/after token counts.
func (e *Engine) compressContext(ctx context.Context, force bool) (compacted bool, before, after int, err error) {
	cfg := e.compressCfg
	ctxSize := cfg.ContextSize
	if ctxSize == 0 {
		ctxSize = model.ResolveContextSize(e.chatModel, 128000)
	}
	tools := e.toolkit.GetToolSchemas()
	before = e.chatModel.CountTokens(e.conv, tools)
	threshold := int(float64(ctxSize) * cfg.TriggerRatio)
	if !force && before < threshold {
		return false, before, 0, nil
	}
	if len(e.conv) <= 1 {
		return false, before, before, nil // only system, nothing to compress
	}
	reserveBudget := int(float64(ctxSize) * cfg.ReserveRatio)
	toCompress, toReserve := splitContextForCompression(e.conv, reserveBudget, e.chatModel, tools)
	if len(toCompress) == 0 {
		return false, before, before, nil
	}
	systemPrompt := ""
	if t := e.conv[0].GetTextContent("\n"); t != nil {
		systemPrompt = *t
	}
	compMsgs := buildCompressionMessages(systemPrompt, "", toCompress, cfg.CompressionPrompt)
	result, rerr := model.GenerateStructuredOutput(ctx, e.chatModel, compMsgs, cfg.SummarySchema)
	if rerr != nil {
		result, rerr = e.retryCompressWithFewer(ctx, systemPrompt, toCompress, cfg, ctxSize, tools)
		if rerr != nil {
			return false, before, 0, fmt.Errorf("compress: %w", rerr)
		}
	}
	summary, ferr := formatSummary(cfg.SummaryTemplate, result)
	if ferr != nil {
		return false, before, 0, fmt.Errorf("format summary: %w", ferr)
	}
	summaryMsg := message.UserMsg("user", "[Previous context summary]\n"+summary)
	newConv := make([]*message.Msg, 0, 2+len(toReserve))
	newConv = append(newConv, e.conv[0], summaryMsg)
	newConv = append(newConv, toReserve...)
	e.conv = newConv
	after = e.chatModel.CountTokens(e.conv, tools)
	return true, before, after, nil
}

// retryCompressWithFewer drops the oldest to-compress messages until the
// compression messages fit under the trigger threshold, then retries.
func (e *Engine) retryCompressWithFewer(ctx context.Context, systemPrompt string, toCompress []*message.Msg, cfg compressConfig, ctxSize int, tools []model.ToolSchema) (json.RawMessage, error) {
	threshold := int(float64(ctxSize) * cfg.TriggerRatio)
	for i := 1; i <= len(toCompress); i++ {
		msgs := buildCompressionMessages(systemPrompt, "", toCompress[i:], cfg.CompressionPrompt)
		tokens := e.chatModel.CountTokens(msgs, tools)
		if tokens < threshold {
			return model.GenerateStructuredOutput(ctx, e.chatModel, msgs, cfg.SummarySchema)
		}
	}
	return nil, fmt.Errorf("cannot reduce context below threshold")
}

// CompressNow forces a context compression (for /compact), regardless of
// threshold. Returns a human-readable message.
func (e *Engine) CompressNow(ctx context.Context) (string, error) {
	compacted, before, after, err := e.compressContext(ctx, true)
	if err != nil {
		return "", err
	}
	if !compacted {
		return "no compaction needed", nil
	}
	return fmt.Sprintf("compressed: %d→%d tokens", before, after), nil
}
