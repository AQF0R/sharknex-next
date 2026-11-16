package engine

import (
	"autonomous-engine/pkg/contracts"
	"context"
)

// RecordAgent 只接受服务端执行器产生的受限审计，不接收模型原始文本或思维链。
func (e *Engine) RecordAgent(ctx context.Context, id, token, kind string, p contracts.LeaseProof, payload map[string]any) error {
	switch kind {
	case "agent.discovery", "agent.started", "agent.turn", "agent.usage", "agent.completed", "agent.failed", "agent.tool_result":
	default:
		return ErrInvalid
	}
	if len(JSON(payload)) > 4096 {
		return ErrInvalid
	}
	return e.Repo.Transaction(ctx, p.RunID, func(u Unit) error {
		r, _, _, _, er := leaseAuth(u, id, p, token, false)
		if er != nil {
			return er
		}
		payload["generation"] = p.Generation
		return recordEvent(u, r.ID, kind, &id, &p.WorkerID, payload)
	})
}
