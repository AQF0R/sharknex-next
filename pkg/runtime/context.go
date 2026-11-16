package runtime

import (
	"autonomous-engine/pkg/contracts"
	"encoding/json"
)

// decodeSpec 将独立任务快照还原为受验证的执行契约。
func decodeSpec(b []byte) (contracts.TaskSpec, error) {
	var s contracts.TaskSpec
	er := json.Unmarshal(b, &s)
	return s, er
}
