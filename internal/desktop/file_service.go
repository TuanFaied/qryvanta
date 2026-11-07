package desktop

import (
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/TuanFaied/qryvanta/internal/app"
	"github.com/TuanFaied/qryvanta/internal/sqlfile"
)

// FileDocument is an opened .sql file.
type FileDocument struct {
	Path     string           `json:"path"`
	Name     string           `json:"name"`
	Content  string           `json:"content"`
	Encoding sqlfile.Encoding `json:"encoding"`
}

// OpenFileResult is the outcome of Open. Errors are data so the UI can show them inline.
type OpenFileResult struct {
	Cancelled bool          `json:"cancelled"`
	Document  *FileDocument `json:"document"`
	Error     string        `json:"error"`
}

// SaveFileResult is the outcome of Save and SaveAs. Encoding is the encoding actually
// written, which SaveAs may change (see SaveAs).
type SaveFileResult struct {
	Cancelled bool             `json:"cancelled"`
	Path      string           `json:"path"`
	Name      string           `json:"name"`
	Encoding  sqlfile.Encoding `json:"encoding"`
	Error     string           `json:"error"`
}

// FileDialogs shows the native open and save dialogs. It returns an empty path when the
// user cancels.
type FileDialogs interface {
	OpenSQL(dir string) (string, error)
	SaveSQL(dir, name string) (string, error)
}

// FileService opens and saves .sql files through native dialogs.
//
// The frontend never names a path the user did not choose: Save only writes to a path that
// came from an Open or Save As dialog in this run, so a compromised or buggy page cannot
// overwrite arbitrary files.
type FileService struct {
	dialogs FileDialogs
	log     *slog.Logger

	mu      sync.Mutex
	allowed map[string]bool
	lastDir string
}

// NewFileService creates the service with the Wails native dialogs.
func NewFileService(core *app.Core) *FileService {
	return newFileService(wailsDialogs{}, core.ComponentLogger("files"))
}

func newFileService(d FileDialogs, log *slog.Logger) *FileService {
	return &FileService{dialogs: d, log: log, allowed: map[string]bool{}}
}

// Open asks for a .sql file and reads it.
func (s *FileService) Open() OpenFileResult {
	path, err := s.dialogs.OpenSQL(s.dir())
	if err != nil {
		s.log.Warn("open file dialog failed", "err", err)
		return OpenFileResult{Error: "The open dialog could not be shown: " + err.Error()}
	}
	if path == "" {
		return OpenFileResult{Cancelled: true}
	}
	doc, err := sqlfile.Read(path)
	if err != nil {
		return OpenFileResult{Error: readError(path, err)}
	}
	s.remember(path)
	s.log.Debug("opened file", "name", filepath.Base(path), "encoding", doc.Encoding)
	return OpenFileResult{Document: &FileDocument{
		Path:     path,
		Name:     filepath.Base(path),
		Content:  doc.Text,
		Encoding: doc.Encoding,
	}}
}

// Save writes content to a file opened or saved earlier, in its encoding.
func (s *FileService) Save(path, content string, encoding sqlfile.Encoding) SaveFileResult {
	path = filepath.Clean(path)
	if !s.isAllowed(path) {
		return SaveFileResult{Error: "Qryvanta can only save to a file you opened or chose with Save As."}
	}
	if encoding == "" {
		encoding = sqlfile.DefaultEncoding
	}
	if err := sqlfile.Write(path, content, encoding); err != nil {
		return SaveFileResult{Error: writeError(path, err)}
	}
	return SaveFileResult{Path: path, Name: filepath.Base(path), Encoding: encoding}
}

// SaveAs asks for a file name and writes content there. It keeps encoding when that
// encoding can store the text, and otherwise (a Windows-1252 file that now contains, say,
// Japanese text) writes UTF-8 with a byte-order mark. New documents pass an empty encoding
// and are written as UTF-8 with a byte-order mark, which SSMS and sqlcmd both read correctly.
func (s *FileService) SaveAs(suggestedName, content string, encoding sqlfile.Encoding) SaveFileResult {
	path, err := s.dialogs.SaveSQL(s.dir(), withSQLExtension(suggestedName))
	if err != nil {
		s.log.Warn("save file dialog failed", "err", err)
		return SaveFileResult{Error: "The save dialog could not be shown: " + err.Error()}
	}
	if path == "" {
		return SaveFileResult{Cancelled: true}
	}
	path = filepath.Clean(withSQLExtension(path))

	if encoding == "" {
		encoding = sqlfile.DefaultEncoding
	}
	if _, err := sqlfile.Encode(content, encoding); err != nil {
		encoding = sqlfile.DefaultEncoding
	}
	if err := sqlfile.Write(path, content, encoding); err != nil {
		return SaveFileResult{Error: writeError(path, err)}
	}
	s.remember(path)
	return SaveFileResult{Path: path, Name: filepath.Base(path), Encoding: encoding}
}

func (s *FileService) remember(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowed[filepath.Clean(path)] = true
	s.lastDir = filepath.Dir(path)
}

func (s *FileService) isAllowed(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowed[path]
}

func (s *FileService) dir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDir
}

// withSQLExtension adds .sql to a name without an extension.
func withSQLExtension(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Query.sql"
	}
	if filepath.Ext(name) == "" {
		return name + ".sql"
	}
	return name
}

func readError(path string, err error) string {
	name := filepath.Base(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return name + " no longer exists."
	case errors.Is(err, fs.ErrPermission):
		return "You do not have permission to read " + name + "."
	case errors.Is(err, sqlfile.ErrTooLarge):
		return name + " is too large to open in the editor (" + sqlfile.ErrTooLarge.Error() + ")."
	default:
		return "Could not open " + name + ": " + err.Error()
	}
}

func writeError(path string, err error) string {
	name := filepath.Base(path)
	if errors.Is(err, fs.ErrPermission) {
		return "You do not have permission to write " + name + ". It may be read-only or open in another program."
	}
	return "Could not save " + name + ": " + err.Error()
}

// wailsDialogs shows the native dialogs attached to the current window.
type wailsDialogs struct{}

func (wailsDialogs) OpenSQL(dir string) (string, error) {
	a := application.Get()
	d := a.Dialog.OpenFile().
		SetTitle("Open SQL file").
		CanChooseFiles(true).
		AddFilter("SQL files (*.sql)", "*.sql").
		AddFilter("All files (*.*)", "*.*")
	if dir != "" {
		d.SetDirectory(dir)
	}
	if w := a.Window.Current(); w != nil {
		d.AttachToWindow(w)
	}
	return d.PromptForSingleSelection()
}

func (wailsDialogs) SaveSQL(dir, name string) (string, error) {
	a := application.Get()
	d := a.Dialog.SaveFile().
		SetMessage("Save SQL file").
		SetFilename(name).
		CanCreateDirectories(true).
		AddFilter("SQL files (*.sql)", "*.sql").
		AddFilter("All files (*.*)", "*.*")
	if dir != "" {
		d.SetDirectory(dir)
	}
	if w := a.Window.Current(); w != nil {
		d.AttachToWindow(w)
	}
	return d.PromptForSingleSelection()
}
