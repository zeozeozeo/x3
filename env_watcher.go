package main

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
)

const dotEnvReloadDebounce = 200 * time.Millisecond

type dotEnvState struct {
	mu     sync.Mutex
	values map[string]string
}

func newDotEnvState(path string) *dotEnvState {
	values, err := godotenv.Read(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to read initial .env state", "path", path, "err", err)
	}
	return &dotEnvState{values: values}
}

func (s *dotEnvState) reload(path string) error {
	values, err := godotenv.Read(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			values = map[string]string{}
		} else {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, oldValue := range s.values {
		if _, ok := values[key]; ok {
			continue
		}
		if os.Getenv(key) == oldValue {
			_ = os.Unsetenv(key)
		}
	}

	for key, value := range values {
		_ = os.Setenv(key, value)
	}
	s.values = values

	slog.SetLogLoggerLevel(levelFromString(os.Getenv("X3_LOG_LEVEL")))
	slog.Info("reloaded .env", "path", path, "keys", len(values))
	return nil
}

func startDotEnvWatcher(path string) (func(), error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(absPath)
	filename := filepath.Base(absPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		return nil, err
	}

	state := newDotEnvState(absPath)
	done := make(chan struct{})

	go func() {
		defer watcher.Close()

		var timer *time.Timer
		var timerC <-chan time.Time
		scheduleReload := func() {
			if timer == nil {
				timer = time.NewTimer(dotEnvReloadDebounce)
				timerC = timer.C
				return
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(dotEnvReloadDebounce)
		}

		for {
			select {
			case <-done:
				if timer != nil {
					timer.Stop()
				}
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) != filename {
					continue
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
					scheduleReload()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn(".env watcher error", "err", err)
			case <-timerC:
				timerC = nil
				if timer != nil {
					timer.Stop()
				}
				if err := state.reload(absPath); err != nil {
					slog.Warn("failed to reload .env", "path", absPath, "err", err)
				}
			}
		}
	}()

	return func() {
		close(done)
	}, nil
}
