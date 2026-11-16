package task

import (
	"autonomous-engine/pkg/contracts"
	"fmt"
)

const (
	Pending   = "pending"
	Blocked   = "blocked"
	Ready     = "ready"
	Claimed   = "claimed"
	Running   = "running"
	Completed = "completed"
	Failed    = "failed"
	Cancelled = "cancelled"
	RetryWait = "retry_wait"
	Timeout   = "timeout"
)

// Terminal 判断任务是否已进入终态。
func Terminal(s string) bool { return s == Completed || s == Failed || s == Cancelled || s == Timeout }

// Transition 验证有限状态机的合法迁移。
func Transition(from, to string) error {
	allowed := map[string][]string{Pending: {Blocked, Ready, Cancelled}, Blocked: {Ready, Cancelled, Failed}, Ready: {Claimed, Cancelled, Timeout}, Claimed: {Running, Cancelled, RetryWait, Failed, Timeout}, Running: {Completed, Failed, Cancelled, RetryWait, Timeout}, RetryWait: {Ready, Cancelled, Timeout}}
	for _, v := range allowed[from] {
		if v == to {
			return nil
		}
	}
	return fmt.Errorf("任务状态不允许从 %s 转到 %s", from, to)
}

// ValidateDAG 检测重复键、悬空引用及依赖环路。
func ValidateDAG(tasks []contracts.TaskSpec) error {
	graph := map[string][]string{}
	for _, t := range tasks {
		if _, ok := graph[t.Key]; ok {
			return fmt.Errorf("任务键重复")
		}
		graph[t.Key] = []string{}
	}
	for _, t := range tasks {
		for _, d := range t.Dependencies {
			if _, ok := graph[d.Key]; !ok {
				return fmt.Errorf("依赖任务不存在")
			}
			if d.Type != "success" && d.Type != "completed" {
				return fmt.Errorf("依赖类型无效")
			}
			graph[t.Key] = append(graph[t.Key], d.Key)
		}
	}
	colors := map[string]int{}
	var visit func(string) error
	visit = func(k string) error {
		if colors[k] == 1 {
			return fmt.Errorf("任务依赖存在环路")
		}
		if colors[k] == 2 {
			return nil
		}
		colors[k] = 1
		for _, d := range graph[k] {
			if e := visit(d); e != nil {
				return e
			}
		}
		colors[k] = 2
		return nil
	}
	for k := range graph {
		if e := visit(k); e != nil {
			return e
		}
	}
	return nil
}
