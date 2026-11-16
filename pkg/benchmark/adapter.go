package benchmark

import (
	"autonomous-engine/pkg/contracts"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"log/slog"
	"os"
	"path/filepath"
)

type Adapter interface {
	Name() string
	ParseTask(context.Context, []byte) (*contracts.RunSpec, error)
	SubmitResult(context.Context, *contracts.RunResult) error
	Cancel(context.Context, string) error
}
type Local struct {
	Directory string // 本地结果归档根目录
}

// Name 返回本地适配器名称。
func (Local) Name() string { return "local" }

// ParseTask 只接收内部 RunSpec，不冒充第三方评测协议。
func (Local) ParseTask(ctx context.Context, b []byte) (*contracts.RunSpec, error) {
	if er := ctx.Err(); er != nil {
		return nil, er
	}
	var s contracts.RunSpec
	er := json.Unmarshal(b, &s)
	return &s, er
}

// SubmitResult 用不可覆盖文件归档结果，重复提交已存在的相同运行结果不产生新文件。
func (l Local) SubmitResult(ctx context.Context, r *contracts.RunResult) error {
	if er := ctx.Err(); er != nil {
		return er
	}
	if r == nil {
		return errors.New("结果不能为空")
	}
	if _, er := uuid.Parse(r.Run.ID); er != nil {
		return errors.New("运行标识无效")
	}
	if r.Run.Status == "running" || r.Run.Status == "created" {
		return errors.New("运行尚未结束")
	}
	if er := os.MkdirAll(l.Directory, 0700); er != nil {
		return er
	}
	b, er := json.MarshalIndent(r, "", "  ")
	if er != nil {
		return er
	}
	destination := filepath.Join(l.Directory, r.Run.ID+".json")
	f, er := os.CreateTemp(l.Directory, ".result-*")
	if er != nil {
		return er
	}
	temporary := f.Name()
	defer func() {
		if er := os.Remove(temporary); er != nil && !os.IsNotExist(er) {
			slog.Warn("清理本地归档临时文件失败", "error", er)
		}
	}()
	if _, er := f.Write(b); er != nil {
		return errors.Join(er, f.Close())
	}
	if er := f.Sync(); er != nil {
		return errors.Join(er, f.Close())
	}
	if er := f.Close(); er != nil {
		return er
	}
	if er := ctx.Err(); er != nil {
		return er
	}
	// 同文件系统硬链接原子发布，不覆盖已成功归档的结果。
	er = os.Link(temporary, destination)
	if os.IsExist(er) {
		old, readEr := os.ReadFile(destination)
		if readEr != nil {
			return readEr
		}
		if !bytes.Equal(old, b) {
			return errors.New("已归档结果与本次提交不同")
		}
		return nil
	}
	return er
}

// Cancel 本地适配器没有外部资源，取消由 Run Manager 统一执行。
func (Local) Cancel(ctx context.Context, _ string) error { return ctx.Err() }
