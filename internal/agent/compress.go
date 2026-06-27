package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
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
