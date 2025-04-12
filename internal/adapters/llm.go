package adapters

import (
	"github.com/DoctorRyner/mistral-go"
)

type LlmAdapter struct {
	Client   *mistral.MistralClient
	apiKey   string
	AgentKey string
}

func NewLlmAdapter(apiKey string, agentKey string) *LlmAdapter {
	adapter := &LlmAdapter{apiKey: apiKey, AgentKey: agentKey}
	adapter.Client = mistral.NewMistralClientDefault(apiKey)
	return adapter
}
