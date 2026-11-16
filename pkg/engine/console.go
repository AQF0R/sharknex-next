package engine

import (
	"autonomous-engine/models"
	"context"
)

// ListRuns 返回控制端可见的最近运行，使用固定排序和有界分页。
func (e *Engine) ListRuns(ctx context.Context, limit, offset int) ([]models.Run, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 100000 {
		return nil, ErrInvalid
	}
	out := []models.Run{}
	er := e.Repo.Read(ctx, func(u Unit) error {
		return u.List(&models.Run{}, Filter{Limit: limit, Offset: offset, Newest: true}, &out)
	})
	return out, er
}
