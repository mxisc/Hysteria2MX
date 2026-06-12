package panel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type jobManager struct {
	rootDir string
}

func newJobManager(cfg Config) *jobManager {
	return &jobManager{rootDir: projectRootFromConfig(cfg)}
}

func (m *jobManager) create(action string, payload map[string]any) (map[string]any, error) {
	id, err := randomHex(8)
	if err != nil {
		return nil, errors.New("任务创建失败")
	}
	job := map[string]any{
		"id":         id,
		"action":     action,
		"status":     "pending",
		"message":    "",
		"result":     nil,
		"payload":    payload,
		"created_at": time.Now().Format(time.RFC3339),
		"updated_at": time.Now().Format(time.RFC3339),
	}
	return job, m.write(job)
}

func (m *jobManager) runNow(action string, payload map[string]any, runner func() (map[string]any, error)) (map[string]any, error) {
	job, err := m.create(action, payload)
	if err != nil {
		return nil, err
	}
	id := toString(job["id"])
	_ = m.markRunning(id)
	result, runErr := runner()
	if runErr != nil {
		_ = m.markError(id, runErr.Error())
	} else {
		_ = m.markDone(id, result)
	}
	return m.get(id)
}

func (m *jobManager) get(id string) (map[string]any, error) {
	path := m.jobPath(id)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("任务不存在")
		}
		return nil, errors.New("任务读取失败")
	}
	var job map[string]any
	if err := json.Unmarshal(content, &job); err != nil {
		return nil, errors.New("任务数据损坏")
	}
	return job, nil
}

func (m *jobManager) markRunning(id string) error {
	job, err := m.get(id)
	if err != nil {
		return err
	}
	job["status"] = "running"
	job["updated_at"] = time.Now().Format(time.RFC3339)
	return m.write(job)
}

func (m *jobManager) markDone(id string, result map[string]any) error {
	job, err := m.get(id)
	if err != nil {
		return err
	}
	job["status"] = "done"
	job["result"] = result
	job["updated_at"] = time.Now().Format(time.RFC3339)
	return m.write(job)
}

func (m *jobManager) markError(id string, message string) error {
	job, err := m.get(id)
	if err != nil {
		return err
	}
	job["status"] = "error"
	job["message"] = message
	job["updated_at"] = time.Now().Format(time.RFC3339)
	return m.write(job)
}

func (m *jobManager) write(job map[string]any) error {
	if err := os.MkdirAll(m.jobDir(), 0o775); err != nil {
		return errors.New("无法创建任务目录")
	}
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return errors.New("任务写入失败")
	}
	if err := os.WriteFile(m.jobPath(toString(job["id"])), data, 0o664); err != nil {
		return errors.New("任务写入失败")
	}
	return nil
}

func (m *jobManager) jobDir() string {
	return filepath.Join(m.rootDir, "storage", "jobs")
}

func (m *jobManager) jobPath(id string) string {
	return filepath.Join(m.jobDir(), id+".json")
}
