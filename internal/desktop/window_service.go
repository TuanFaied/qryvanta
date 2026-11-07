package desktop

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// EventCloseRequested is sent to the frontend when the user closes the window while an
// editor tab has unsaved changes. The frontend asks what to do and calls
// WindowService.Close to close anyway.
const EventCloseRequested = "qryvanta:close-requested"

func init() {
	application.RegisterEvent[application.Void](EventCloseRequested)
}

// WindowService lets the frontend protect unsaved work when the window is closed.
type WindowService struct {
	mu         sync.Mutex
	unsaved    bool
	closing    bool
	window     *application.WebviewWindow
	emitPrompt func()
}

// NewWindowService creates the service. AttachWindow connects it to the main window.
func NewWindowService() *WindowService {
	return &WindowService{}
}

// AttachWindow installs s's close hook on the main window. It is a function, not a method,
// so that Wails does not expose it to the frontend.
func AttachWindow(s *WindowService, a *application.App, w *application.WebviewWindow) {
	s.mu.Lock()
	s.window = w
	s.emitPrompt = func() { a.Event.Emit(EventCloseRequested) }
	s.mu.Unlock()

	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if s.interceptClose() {
			e.Cancel()
		}
	})
}

// SetUnsavedChanges records whether any editor tab has unsaved changes.
func (s *WindowService) SetUnsavedChanges(unsaved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unsaved = unsaved
}

// Close closes the window without asking again, after the user chose to discard or save.
func (s *WindowService) Close() {
	s.mu.Lock()
	s.closing = true
	w := s.window
	s.mu.Unlock()
	if w != nil {
		w.Close()
	}
}

// interceptClose reports whether a close must be cancelled, and if so asks the frontend.
func (s *WindowService) interceptClose() bool {
	s.mu.Lock()
	block := s.unsaved && !s.closing
	prompt := s.emitPrompt
	s.mu.Unlock()
	if block && prompt != nil {
		prompt()
	}
	return block
}
