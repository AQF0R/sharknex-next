package contracts

import "autonomous-engine/models"

// StageReviewInput 扩展内部任务输入，外部TaskSpec不得创建此类任务。
type StageReviewInput struct {
	TaskSpec        // 兼容普通认领与任务关联
	Digest   string `json:"review_digest"` // 创建复核时的完整阶段摘要
}

// StageEvidence 携带可核对的证据摘要，不读取对象正文或产生网络探测。
type StageEvidence struct {
	Evidence    models.Evidence `json:"evidence"`     // 数据库关联记录
	ContentHash string          `json:"content_hash"` // 原始观察的服务端摘要
}

// StageReviewSnapshot 为普通Worker提供完整且有界的阶段只读数据。
type StageReviewSnapshot struct {
	Goal     GoalSpec        `json:"goal"`     // 阶段目标与事实要求
	Scope    Scope           `json:"scope"`    // 不可扩大的精确授权
	Tasks    []models.Task   `json:"tasks"`    // 本阶段所有工作任务，不含复核自身
	Facts    []models.Fact   `json:"facts"`    // 本阶段工作来源的完整事实
	Evidence []StageEvidence `json:"evidence"` // 关联证据与可信摘要
	Digest   string          `json:"digest"`   // 当前输入revision的稳定摘要
	Complete bool            `json:"complete"` // 超限或缺页时必须失败而非误报通过
}

// StageReviewResult 仅表达只读核验结果，服务端复算后才推进阶段。
type StageReviewResult struct {
	Digest string `json:"digest"`           // 核验的输入摘要
	Passed bool   `json:"passed"`           // 工作、证据与覆盖是否全部满足
	Reason string `json:"reason,omitempty"` // 可解释的缺口或限制
}
