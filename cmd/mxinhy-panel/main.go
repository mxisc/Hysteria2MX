package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mxinhy/internal/panel"
)

func main() {
	configPath := flag.String("config", panel.DefaultConfigPath, "Path to panel env file")
	port := flag.Int("port", 0, "Override listen port, keeping the configured host")
	flag.Parse()

	logger, closeLog := setupPanelLogger()
	defer closeLog()

	config, err := panel.LoadConfig(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}
	logger.SetLevel(config.LogLevel)
	if *port > 0 {
		config.BindAddr = overrideBindPort(config.BindAddr, *port)
	}

	args := flag.Args()
	if len(args) > 0 {
		if args[0] != "run-job" || len(args) != 3 {
			logger.Fatalf("usage: mxinhy-panel [-config %s] [-port 18080] run-job <install|uninstall> <job-id>", panel.DefaultConfigPath)
		}
		if err := panel.RunJob(config, logger, args[1], args[2]); err != nil {
			logger.Fatalf("run job: %v", err)
		}
		return
	}

	server, err := panel.NewServer(config, logger)
	if err != nil {
		logger.Fatalf("create server: %v", err)
	}

	go func() {
		logger.Infof("listening on %s", config.BindAddr)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Fatalf("listen: %v", serveErr)
		}
	}()

	stopSignal := make(chan os.Signal, 1)
	signal.Notify(stopSignal, os.Interrupt, syscall.SIGTERM)
	<-stopSignal

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Fatalf("shutdown: %v", err)
	}
}

func setupPanelLogger() (*panel.PanelLogger, func()) {
	logPath := panel.PanelLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		logger := panel.NewPanelLogger(os.Stdout, "[mxinhy-panel] ", panel.LogFlags(), panel.DefaultLogLevel())
		logger.Errorf("create log directory failed: %v", err)
		return logger, func() {}
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logger := panel.NewPanelLogger(os.Stdout, "[mxinhy-panel] ", panel.LogFlags(), panel.DefaultLogLevel())
		logger.Errorf("open log file failed: %v", err)
		return logger, func() {}
	}
	os.Stdout = file
	os.Stderr = file
	return panel.NewPanelLogger(file, "[mxinhy-panel] ", panel.LogFlags(), panel.DefaultLogLevel()), func() {
		_ = file.Close()
	}
}

func overrideBindPort(bindAddr string, port int) string {
	if port <= 0 {
		return bindAddr
	}
	bindAddr = strings.TrimSpace(bindAddr)
	if bindAddr == "" {
		bindAddr = panel.DefaultBindAddr
	}
	host, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
