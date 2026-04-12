package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	logger, closeLogger, err := initializeLogger(os.Getenv("LINKO_LOG_FILE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		return 1
	}
	defer func() {
		if err := closeLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to properly close the logger: %v\n", err)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to create store: %v", err))
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	logger.Debug("Linko is shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error(fmt.Sprintf("failed to shutdown server: %v", err))
		return 1
	}
	if serverErr != nil {
		logger.Error(fmt.Sprintf("server error: %v", serverErr))
		return 1
	}
	return 0
}

type closeFunc func() error

func initializeLogger(logFileName string) (*slog.Logger, closeFunc, error) {
	handlers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	}

	if logFileName == "" {
		closer := func() error {
			return nil
		}
		return slog.New(slog.NewMultiHandler(handlers...)), closer, nil
	}
	logFile, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create file: %v", err)
	}
	logBuffer := bufio.NewWriterSize(logFile, 8192)
	closer := func() error {
		if err := logBuffer.Flush(); err != nil {
			return fmt.Errorf("failed to flush log file: %v", err)
		}
		if err := logFile.Close(); err != nil {
			return fmt.Errorf("failed to close log file: %v", err)
		}
		return nil
	}
	handlers = append(handlers, slog.NewJSONHandler(logFile, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	return slog.New(slog.NewMultiHandler(handlers...)), closer, nil
}
