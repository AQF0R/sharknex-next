package engine

// collect 按固定批次读取完整聚合数据，避免目标评估或归档静默截断。
func collect[T any](u Unit, record Record, filter Filter, out *[]T) error {
	filter.Limit = 2000
	*out = []T{}
	for {
		batch := []T{}
		if er := u.List(record, filter, &batch); er != nil {
			return er
		}
		*out = append(*out, batch...)
		if len(batch) < filter.Limit {
			return nil
		}
		filter.Offset += len(batch)
	}
}
