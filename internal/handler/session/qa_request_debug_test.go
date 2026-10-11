package session

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyMessageRequestSnapshotPreservesOriginalRoutingOptions(t *testing.T) {
	request := CreateKnowledgeQARequest{
		ExecutionScope:      "global_videos",
		AgentEnabled:        false,
		AutoRoute:           true,
		AgentID:             "",
		AgentSourceTenantID: 10000,
		SummaryModelID:      "summary-model",
		DisableTitle:        true,
	}
	context := types.MessageExecutionContext{}

	applyMessageRequestSnapshot(&context, request)

	if context.ExecutionScope != request.ExecutionScope ||
		context.AgentEnabled != request.AgentEnabled ||
		context.AutoRoute != request.AutoRoute ||
		context.RequestedAgentID != request.AgentID ||
		context.AgentSourceTenantID != request.AgentSourceTenantID ||
		context.RequestedSummaryModel != request.SummaryModelID ||
		context.DisableTitle != request.DisableTitle {
		t.Fatalf("snapshot = %#v, request = %#v", context, request)
	}
}
