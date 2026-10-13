package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToGeminiInstructionsAndInput(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model:        "gemini-test",
		Instructions: mustGeminiRawMessage(t, "system rules"),
		Input:        mustGeminiRawMessage(t, "hello"),
	})

	require.NotNil(t, got.SystemInstructions)
	require.Len(t, got.SystemInstructions.Parts, 1)
	assert.Equal(t, "system rules", got.SystemInstructions.Parts[0].Text)
	require.Len(t, got.Contents, 1)
	assert.Equal(t, "user", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 1)
	assert.Equal(t, "hello", got.Contents[0].Parts[0].Text)
}

func TestConvertOpenAIResponsesRequestToGeminiFunctionToolAndChoice(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, "lookup weather"),
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
			{"type": "custom", "name": "freeform"},
		}),
		ToolChoice: mustGeminiRawMessage(t, map[string]any{
			"type": "function",
			"name": "lookup",
		}),
	})

	tools := got.GetTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "lookup", gjson.GetBytes(got.Tools, "0.functionDeclarations.0.name").String())
	assert.Equal(t, "Lookup data", gjson.GetBytes(got.Tools, "0.functionDeclarations.0.description").String())
	require.NotNil(t, got.ToolConfig)
	require.NotNil(t, got.ToolConfig.FunctionCallingConfig)
	assert.Equal(t, dto.FunctionCallingConfigMode("ANY"), got.ToolConfig.FunctionCallingConfig.Mode)
	assert.Equal(t, []string{"lookup"}, got.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
}

func TestConvertOpenAIResponsesRequestToGeminiFunctionCallConversation(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	})

	require.Len(t, got.Contents, 2)
	assert.Equal(t, "model", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 2)
	require.NotNil(t, got.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "lookup", got.Contents[0].Parts[0].FunctionCall.FunctionName)
	assert.Equal(t, map[string]any{"q": "x"}, got.Contents[0].Parts[0].FunctionCall.Arguments)
	assert.Equal(t, "I will call.", got.Contents[0].Parts[1].Text)

	assert.Equal(t, "user", got.Contents[1].Role)
	require.Len(t, got.Contents[1].Parts, 1)
	require.NotNil(t, got.Contents[1].Parts[0].FunctionResponse)
	assert.Equal(t, "lookup", got.Contents[1].Parts[0].FunctionResponse.Name)
	assert.Equal(t, map[string]any{"ok": true}, got.Contents[1].Parts[0].FunctionResponse.Response)
}

func TestConvertOpenAIResponsesRequestToGeminiKeepsCustomToolCalls(t *testing.T) {
	got := mustConvertResponsesToGemini(t, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "before custom"},
				},
			},
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom",
				"name":    "apply_patch",
				"input":   "patch body",
			},
			{
				"type":    "custom_tool_call_output",
				"call_id": "call_custom",
				"output":  "ok",
			},
			{
				"type":    "function_call_output",
				"call_id": "call_custom",
				"output":  "legacy custom output",
			},
			{
				"role":    "user",
				"content": "next turn",
			},
		}),
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
			{"type": "unknown", "name": "unknown"},
		}),
	})

	// The cleaned schema has no additionalProperties, and the unknown tool is dropped.
	assert.JSONEq(t, `[{"functionDeclarations":[{
		"name":"apply_patch",
		"description":"This tool takes freeform text. Put the complete raw text in the \"input\" argument.",
		"parameters":{"type":"OBJECT","properties":{"input":{"type":"STRING","description":"Raw input for the tool."}},"required":["input"]}
	}]}]`, string(got.Tools))

	require.Len(t, got.Contents, 3)
	assert.Equal(t, "model", got.Contents[0].Role)
	require.Len(t, got.Contents[0].Parts, 2)
	require.NotNil(t, got.Contents[0].Parts[0].FunctionCall)
	assert.Equal(t, "apply_patch", got.Contents[0].Parts[0].FunctionCall.FunctionName)
	assert.Equal(t, map[string]any{"input": "patch body"}, got.Contents[0].Parts[0].FunctionCall.Arguments)
	assert.Equal(t, "before custom", got.Contents[0].Parts[1].Text)

	assert.Equal(t, "user", got.Contents[1].Role)
	require.Len(t, got.Contents[1].Parts, 2)
	for i, output := range []string{"ok", "legacy custom output"} {
		response := got.Contents[1].Parts[i].FunctionResponse
		require.NotNil(t, response)
		assert.Equal(t, "apply_patch", response.Name)
		assert.Equal(t, map[string]any{"content": output}, response.Response)
		assert.JSONEq(t, `"call_custom"`, string(response.ID))
	}

	assert.Equal(t, "user", got.Contents[2].Role)
	require.Len(t, got.Contents[2].Parts, 1)
	assert.Equal(t, "next turn", got.Contents[2].Parts[0].Text)
}

func TestConvertOpenAIResponsesRequestToGeminiCodexToolCompatibility(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustGeminiRawMessage(t, []map[string]any{
			{"role": "user", "content": "look up the weather"},
			{
				"type":      "function_call",
				"call_id":   "call_lookup",
				"name":      "lookup",
				"arguments": map[string]any{"city": "Paris"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_lookup",
				"output":  map[string]any{"temperature": 18},
			},
			{
				"type":    "custom_tool_call",
				"call_id": "call_patch",
				"name":    "apply_patch",
				"input":   "patch body",
			},
			{
				"type":    "custom_tool_call_output",
				"call_id": "call_patch",
				"output":  "ok",
			},
			{
				"type":    "function_call_output",
				"call_id": "call_patch",
				"output":  "legacy custom output",
			},
		}),
		Tools: mustGeminiRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Look up weather",
				"strict":      true,
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			},
			{"type": "custom", "name": "apply_patch"},
			{"type": "namespace", "name": "workspace"},
			{"type": "tool_search", "name": "find"},
			{
				"type":                "web_search_preview",
				"external_web_access": false,
				"filters": map[string]any{
					"allowed_domains": []string{"example.com"},
				},
				"user_location": map[string]any{
					"type":    "approximate",
					"country": "FR",
				},
			},
		}),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: req.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: req.Model,
		},
	}

	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	geminiReq, ok := got.(*dto.GeminiChatRequest)
	require.True(t, ok)
	assert.Equal(t, types.ConversionLossPolicyAllow, info.ConvOptions().EffectiveToolLossPolicy())

	tools := geminiReq.GetTools()
	require.Len(t, tools, 2)
	assert.True(t, gjson.GetBytes(geminiReq.Tools, "0.googleSearch").Exists())
	assert.Equal(t, "lookup", gjson.GetBytes(geminiReq.Tools, "1.functionDeclarations.0.name").String())
	assert.Equal(t, "apply_patch", gjson.GetBytes(geminiReq.Tools, "1.functionDeclarations.1.name").String())
	assert.NotContains(t, string(geminiReq.Tools), "namespace")
	assert.NotContains(t, string(geminiReq.Tools), "tool_search")
	assert.NotContains(t, string(geminiReq.Tools), "allowed_domains")
	assert.NotContains(t, string(geminiReq.Tools), "external_web_access")

	require.Len(t, geminiReq.Contents, 5)
	assert.Equal(t, "user", geminiReq.Contents[0].Role)
	assert.Equal(t, "look up the weather", geminiReq.Contents[0].Parts[0].Text)
	assert.Equal(t, "model", geminiReq.Contents[1].Role)
	require.NotNil(t, geminiReq.Contents[1].Parts[0].FunctionCall)
	assert.Equal(t, "lookup", geminiReq.Contents[1].Parts[0].FunctionCall.FunctionName)
	assert.Equal(t, "user", geminiReq.Contents[2].Role)
	require.NotNil(t, geminiReq.Contents[2].Parts[0].FunctionResponse)
	assert.Equal(t, "lookup", geminiReq.Contents[2].Parts[0].FunctionResponse.Name)
	assert.Equal(t, "model", geminiReq.Contents[3].Role)
	require.Len(t, geminiReq.Contents[3].Parts, 1)
	customCall := geminiReq.Contents[3].Parts[0].FunctionCall
	require.NotNil(t, customCall)
	assert.Equal(t, "call_patch", customCall.ID)
	assert.Equal(t, "apply_patch", customCall.FunctionName)
	assert.Equal(t, map[string]any{"input": "patch body"}, customCall.Arguments)
	assert.Equal(t, "user", geminiReq.Contents[4].Role)
	require.Len(t, geminiReq.Contents[4].Parts, 2)
	for i, output := range []string{"ok", "legacy custom output"} {
		response := geminiReq.Contents[4].Parts[i].FunctionResponse
		require.NotNil(t, response)
		assert.Equal(t, "apply_patch", response.Name)
		assert.Equal(t, map[string]any{"content": output}, response.Response)
		assert.JSONEq(t, `"call_patch"`, string(response.ID))
	}
	contentsJSON, err := common.Marshal(geminiReq.Contents)
	require.NoError(t, err)
	assert.Contains(t, string(contentsJSON), "call_patch")

	diagnosticCodes := make(map[string]struct{})
	for _, diagnostic := range info.ConversionDiagnostics() {
		diagnosticCodes[diagnostic.Code] = struct{}{}
		assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), diagnostic.From)
		assert.Equal(t, types.RelayFormat(types.RelayFormatGemini), diagnostic.To)
	}
	for _, code := range []string{
		"unsupported_function_strict",
		"custom_tool_as_function",
		"unsupported_opaque_tool",
		"unsupported_search_constraints",
		"unsupported_search_tuning",
	} {
		assert.Contains(t, diagnosticCodes, code)
	}
}

func mustConvertResponsesToGemini(t *testing.T, req dto.OpenAIResponsesRequest) *dto.GeminiChatRequest {
	t.Helper()
	info := &relaycommon.RelayInfo{
		OriginModelName: req.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: req.Model,
		},
	}
	got, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	geminiReq, ok := got.(*dto.GeminiChatRequest)
	require.True(t, ok)
	return geminiReq
}

func mustGeminiRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}
