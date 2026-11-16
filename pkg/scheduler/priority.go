package scheduler

import (
	"math"
	"slices"
	"time"
)

// Match 要求 Worker 覆盖全部能力，评分不能绕过硬约束。
func Match(required, available []string) bool {
	for _, v := range required {
		if !slices.Contains(available, v) {
			return false
		}
	}
	return true
}

// Score 按文档权重计算归一化确定性调度分数。
func Score(priority, blocking, retries int, created time.Time) float64 {
	p := math.Max(0, math.Min(100, float64(priority)))
	d := math.Min(100, float64(blocking)*20)
	a := math.Min(100, math.Max(0, time.Since(created).Seconds()/6))
	f := math.Max(0, 100-float64(retries)*25)
	return .35*p + .25*d + .20*100 + .10*a + .10*f
}
