package repository

import (
	"fmt"
	"log/slog"
	"team_exe/internal/adapters"

	"github.com/DoctorRyner/mistral-go"
)

type LlmRepo struct {
	adapter *adapters.LlmAdapter
}

func NewLlmRepository(adapter *adapters.LlmAdapter) *LlmRepo {
	return &LlmRepo{adapter: adapter}
}

func (l *LlmRepo) SendRequestToLlm(request string) (response string, err error) {
	agentReqParam := mistral.DefaultChatRequestParams
	agentReqParam.AgentId = l.adapter.AgentKey
	agentRes, err := l.adapter.Client.Chat("mistral-large-latest", []mistral.ChatMessage{{Content: request, Role: mistral.RoleUser}}, &agentReqParam)
	if err != nil {
		slog.Error("send request to llm error" + err.Error())
		return "", nil
	}
	return fmt.Sprintf("%v", agentRes.Choices[0].Message.Content), nil
}
