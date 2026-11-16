package storage

import (
	"autonomous-engine/pkg/security"
	"context"
	"errors"
	"github.com/google/uuid"
	"os"
	"path/filepath"
)

type Objects struct {
	Directory string // 私有对象根目录
}

// Put 以内容哈希和 Run UUID 寻址，证据不可覆盖且不接受客户端文件路径。
func (o Objects) Put(ctx context.Context, runID string, b []byte) (string, error) {
	if er := ctx.Err(); er != nil {
		return "", er
	}
	if _, er := uuid.Parse(runID); er != nil {
		return "", er
	}
	if len(b) > 16384 {
		return "", errors.New("证据内容过大")
	}
	hash := security.Hash(string(b))
	dir := filepath.Join(o.Directory, runID)
	if er := os.MkdirAll(dir, 0700); er != nil {
		return "", er
	}
	f, er := os.OpenFile(filepath.Join(dir, hash), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(er) {
		return "object://" + runID + "/" + hash, nil
	}
	if er != nil {
		return "", er
	}
	_, writeEr := f.Write(b)
	closeEr := f.Close()
	if er := errors.Join(writeEr, closeEr); er != nil {
		return "", er
	}
	return "object://" + runID + "/" + hash, nil
}
