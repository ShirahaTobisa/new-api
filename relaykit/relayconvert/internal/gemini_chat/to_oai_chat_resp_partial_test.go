package geminichat

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func partialChunk(name string, willContinue bool, partials ...dto.GeminiPartialArg) *dto.GeminiChatResponse {
	return &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
		Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{
			FunctionName: name, PartialArgs: partials, WillContinue: &willContinue,
		}}}},
	}}}
}

func stringPartial(path, text string) dto.GeminiPartialArg {
	return dto.GeminiPartialArg{JSONPath: path, StringValue: &text}
}

// collectToolDeltas runs the chunks through one stream state and gathers the
// tool-call deltas it emits, in order.
func collectToolDeltas(t *testing.T, chunks ...*dto.GeminiChatResponse) []dto.ToolCallResponse {
	t.Helper()
	state := NewGeminiToChatStreamState("chatcmpl-test", 1)
	var deltas []dto.ToolCallResponse
	for _, chunk := range chunks {
		responses, err := state.ConvertChunk(chunk, "gemini-3.8-flash", nil)
		require.NoError(t, err)
		for _, response := range responses {
			for _, choice := range response.Choices {
				deltas = append(deltas, choice.Delta.ToolCalls...)
			}
		}
	}
	return deltas
}

func TestStreamedFunctionArgumentsBecomeToolCallDeltas(t *testing.T) {
	deltas := collectToolDeltas(t,
		partialChunk("output_reply", true),
		partialChunk("", true, stringPartial("$.content", "风雪")),
		partialChunk("", true, stringPartial("$.content", "压断了"), stringPartial("$.content", "枯枝。")),
		partialChunk("", false),
	)
	require.Len(t, deltas, 4)
	assert.Equal(t, "output_reply", deltas[0].Function.Name)
	assert.NotEmpty(t, deltas[0].ID)
	var arguments strings.Builder
	for i, delta := range deltas {
		assert.Equal(t, deltas[0].ID, delta.ID, "delta %d keeps the call id", i)
		require.NotNil(t, delta.Index)
		assert.Equal(t, 0, *delta.Index)
		if i > 0 {
			assert.Empty(t, delta.Function.Name, "only the first delta names the function")
		}
		arguments.WriteString(delta.Function.Arguments)
	}
	assert.Equal(t, `{`, deltas[0].Function.Arguments)
	assert.Equal(t, `"content":"风雪`, deltas[1].Function.Arguments)
	assert.Equal(t, `{"content":"风雪压断了枯枝。"}`, arguments.String())
}

func TestStreamedFunctionArgumentsWriteOtherMembersWhole(t *testing.T) {
	count := 3.0
	lang := "ja"
	deltas := collectToolDeltas(t,
		partialChunk("plan", true, stringPartial("$.content", "a"), dto.GeminiPartialArg{JSONPath: "$.meta.lang", StringValue: &lang}),
		partialChunk("", true, stringPartial("$.content", "b"), dto.GeminiPartialArg{JSONPath: "$.count", NumberValue: &count}),
		partialChunk("", false),
	)
	var arguments strings.Builder
	for _, delta := range deltas {
		arguments.WriteString(delta.Function.Arguments)
	}
	var decoded map[string]any
	require.NoError(t, kitutil.UnmarshalJsonStr(arguments.String(), &decoded))
	assert.Equal(t, map[string]any{"content": "ab", "count": 3.0, "meta": map[string]any{"lang": "ja"}}, decoded)
}

func TestCompleteFunctionCallWithExplicitWillContinueFalseIsEmittedWhole(t *testing.T) {
	willContinue := false
	chunk := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
		Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{
			FunctionName: "output_reply", Arguments: map[string]any{"content": "done"}, WillContinue: &willContinue,
		}}}},
	}}}
	deltas := collectToolDeltas(t, chunk)
	require.Len(t, deltas, 1)
	assert.Equal(t, "output_reply", deltas[0].Function.Name)
	assert.JSONEq(t, `{"content":"done"}`, deltas[0].Function.Arguments)
}
