package fs

import (
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	debounceDur = 50 * time.Millisecond
)

type Watcher struct {
	w      *fsnotify.Watcher
	cb     func(path, op string)
	timers map[string]*time.Timer
	mu     sync.Mutex
	done   chan struct{}
}

func newWatcher(cb func(path, op string)) *Watcher {
	w, _ := fsnotify.NewWatcher()
	ww := &Watcher{
		w:      w,
		cb:     cb,
		timers: make(map[string]*time.Timer),
		done:   make(chan struct{}),
	}
	go ww.run()
	return ww
}

func (w *Watcher) Add(path string) error { return w.w.Add(path) }
func (w *Watcher) Remove(path string)    { w.w.Remove(path) }
func (w *Watcher) Close()                { close(w.done); w.w.Close() }

func (w *Watcher) run() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			w.debounce(ev)
		case _, ok := <-w.w.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) debounce(ev fsnotify.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	op := ev.Op.String()
	path := ev.Name

	if t, ok := w.timers[path]; ok {
		t.Reset(debounceDur)
		return
	}

	w.timers[path] = time.AfterFunc(debounceDur, func() {
		w.mu.Lock()
		delete(w.timers, path)
		w.mu.Unlock()
		w.cb(path, op)
	})
}
