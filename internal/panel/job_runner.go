package panel

import (
	"context"
	"errors"
	"log"
	"strings"
)

func RunJob(config Config, logger *log.Logger, action string, jobID string) error {
	action = strings.TrimSpace(action)
	jobID = strings.TrimSpace(jobID)
	if action == "" || jobID == "" {
		return errors.New("用法: mxinhy-panel -config <panel.env> run-job <action> <job-id>")
	}

	db, err := OpenDB(config)
	if err != nil {
		return err
	}
	defer db.Close()

	if migrateErr := newMigrationService(config).ensureUpToDate(db); migrateErr != nil {
		return migrateErr
	}

	sshKeys := newSSHKeyUploadService(config)
	remote := newRemoteExecutor()
	agents := newAgentService(db, &config)
	jobs := newJobManager(config)
	hysteria := newHysteriaService(db, &config, sshKeys, remote, agents)

	fail := func(runErr error) error {
		if runErr == nil {
			return nil
		}
		_ = jobs.markError(jobID, runErr.Error())
		return runErr
	}

	if markErr := jobs.markRunning(jobID); markErr != nil {
		return fail(markErr)
	}

	job, jobErr := jobs.get(jobID)
	if jobErr != nil {
		return fail(jobErr)
	}

	payload := mapValue(job["payload"])
	nodeID := int64Value(payload["node_id"])
	var result map[string]any
	switch action {
	case "install":
		result, err = hysteria.install(context.Background(), toString(payload["api_base_url"]), nodeID)
	case "uninstall":
		result, err = hysteria.uninstall(context.Background(), nodeID)
	default:
		err = errors.New("不支持的任务动作")
	}
	if err != nil {
		return fail(err)
	}
	if doneErr := jobs.markDone(jobID, result); doneErr != nil {
		return doneErr
	}
	if logger != nil {
		logger.Printf("job %s %s completed", jobID, action)
	}
	return nil
}
