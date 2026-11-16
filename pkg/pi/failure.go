package pi

import (
	"context"
	"errors"
)

// FailureMessage 只公开固定本地诊断，不把模型响应、密钥或底层请求错误写入结果。
func FailureMessage(err error) string {
	for _, known := range []error{errNetworkPolicy, errTargetConnect, errTargetDNS} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Pi 执行或目标请求超时"
	}
	if errors.Is(err, context.Canceled) {
		return "Pi 执行已取消"
	}
	return "Pi 执行失败，请检查模型配置、工具参数和预算；提供方原始错误未公开"
}
