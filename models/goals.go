package models

import (
	"gorm.io/datatypes"
	"time"
)

type Goal struct {
	ID              string         `json:"id" gorm:"column:id;primaryKey;type:uuid"`                   // id 字段
	RunID           string         `json:"run_id" gorm:"column:run_id;type:uuid"`                      // run_id 字段
	ParentID        *string        `json:"parent_id" gorm:"column:parent_id;type:uuid"`                // parent_id 字段
	Name            string         `json:"name" gorm:"column:name"`                                    // name 字段
	Description     string         `json:"description" gorm:"column:description"`                      // description 字段
	Status          string         `json:"status" gorm:"column:status"`                                // status 字段
	Priority        int            `json:"priority" gorm:"column:priority"`                            // priority 字段
	SuccessCriteria datatypes.JSON `json:"success_criteria" gorm:"column:success_criteria;type:jsonb"` // success_criteria 字段
	CreatedAt       time.Time      `json:"created_at" gorm:"column:created_at"`                        // created_at 字段
	UpdatedAt       time.Time      `json:"updated_at" gorm:"column:updated_at"`                        // updated_at 字段
}

// TableName 固定对应数据库表名。
func (Goal) TableName() string { return "goals" }
