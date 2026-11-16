package engine

import (
	"autonomous-engine/pkg/contracts"
	"autonomous-engine/pkg/security"
	"context"
	"slices"
)

// RecordTool 在实际工具调用前校验允许清单并生成仅服务端追加的审计事件。
func (e *Engine) RecordTool(ctx context.Context, id, token, tool string, p contracts.LeaseProof) error {
	return e.Repo.Transaction(ctx, p.RunID, func(u Unit) error {
		r, t, _, _, er := leaseAuth(u, id, p, token, false)
		if er != nil {
			return er
		}
		sp, er := specOf(t)
		if er != nil {
			return er
		}
		scope, er := scopeOf(r)
		if er != nil {
			return er
		}
		if security.ValidateTask(scope, sp) != nil || !slices.Contains(sp.Tools, tool) || !slices.Contains(scope.Tools, tool) {
			return ErrForbidden
		}
		return recordEvent(u, r.ID, "tool.called", &id, &p.WorkerID, map[string]any{"tool": tool, "target": sp.Target})
	})
}
