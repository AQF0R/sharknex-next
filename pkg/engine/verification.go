package engine

import (
	"autonomous-engine/models"
	"autonomous-engine/pkg/contracts"
)

// confirmVerification 仅保留本地演示确认；Pi 观察不参与同目标批量确认。
func confirmVerification(u Unit, r models.Run, t models.Task, observations []contracts.Observation) error {
	verified := false
	for _, o := range observations {
		if o.Kind == "fact" && o.Type == "verification_completed" {
			verified = true
		}
	}
	if !verified {
		return nil
	}
	spec, er := specOf(t)
	if er != nil {
		return er
	}
	if spec.Executor == "pi" {
		return nil
	}
	var facts []models.Fact
	if er := collect(u, &models.Fact{}, Filter{RunID: r.ID}, &facts); er != nil {
		return er
	}
	for _, f := range facts {
		c, er := Decode[map[string]any](f.Content)
		if er != nil {
			return er
		}
		if f.SourceTaskID != nil {
			var source models.Task
			if er := u.Get(&source, *f.SourceTaskID); er != nil {
				return er
			}
			sourceSpec, er := specOf(source)
			if er != nil {
				return er
			}
			if sourceSpec.Executor == "pi" {
				continue
			}
		}
		if c["target"] == spec.Target && f.Status != "rejected" && f.Status != "stale" && f.Status != "confirmed" {
			if er := u.Patch(&f, f.ID, map[string]any{"status": "corroborated"}); er != nil {
				return er
			}
			if er := u.Patch(&f, f.ID, map[string]any{"status": "confirmed"}); er != nil {
				return er
			}
		}
	}
	var findings []models.Finding
	if er := collect(u, &models.Finding{}, Filter{RunID: r.ID, Status: []string{"unverified"}}, &findings); er != nil {
		return er
	}
	for _, f := range findings {
		if f.SourceTaskID == nil {
			continue
		}
		var source models.Task
		if er := u.Get(&source, *f.SourceTaskID); er != nil {
			return er
		}
		sp, er := specOf(source)
		if er != nil {
			return er
		}
		if sp.Executor == "pi" || sp.Target != spec.Target || source.ID == t.ID {
			continue
		}
		if er := u.Patch(&f, f.ID, map[string]any{"status": "confirmed"}); er != nil {
			return er
		}
		if er := recordEvent(u, r.ID, "finding.confirmed", &t.ID, t.AssignedWorkerID, map[string]any{"finding_id": f.ID}); er != nil {
			return er
		}
	}
	return nil
}
