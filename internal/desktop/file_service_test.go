package desktop

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuanFaied/qryvanta/internal/sqlfile"
)

type fakeDialogs struct {
	open, save string
	err        error
	gotName    string
	gotDir     string
}

func (f *fakeDialogs) OpenSQL(dir string) (string, error) {
	f.gotDir = dir
	return f.open, f.err
}

func (f *fakeDialogs) SaveSQL(dir, name string) (string, error) {
	f.gotDir, f.gotName = dir, name
	return f.save, f.err
}

func newTestFileService(d *fakeDialogs) *FileService {
	return newFileService(d, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFileServiceOpenReadsAndAllowsSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.sql")
	if err := sqlfile.Write(path, "SELECT 1;", sqlfile.UTF16LE); err != nil {
		t.Fatal(err)
	}
	d := &fakeDialogs{open: path}
	s := newTestFileService(d)

	res := s.Open()
	if res.Error != "" || res.Document == nil {
		t.Fatalf("Open = %+v", res)
	}
	if res.Document.Content != "SELECT 1;" || res.Document.Name != "report.sql" || res.Document.Encoding != sqlfile.UTF16LE {
		t.Fatalf("document = %+v", res.Document)
	}

	saved := s.Save(path, "SELECT 2;", res.Document.Encoding)
	if saved.Error != "" {
		t.Fatalf("Save = %+v", saved)
	}
	doc, err := sqlfile.Read(path)
	if err != nil || doc.Text != "SELECT 2;" || doc.Encoding != sqlfile.UTF16LE {
		t.Fatalf("after save: %+v, %v", doc, err)
	}

	// The next dialog starts in the folder of the last file.
	s.Open()
	if d.gotDir != dir {
		t.Errorf("dialog dir = %q, want %q", d.gotDir, dir)
	}
}

func TestFileServiceSaveRejectsPathsNotChosenByTheUser(t *testing.T) {
	target := filepath.Join(t.TempDir(), "victim.txt")
	s := newTestFileService(&fakeDialogs{})
	res := s.Save(target, "DROP TABLE x", sqlfile.UTF8)
	if res.Error == "" {
		t.Fatal("Save to an unknown path should fail")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file was written: %v", err)
	}
}

func TestFileServiceCancelledDialogs(t *testing.T) {
	s := newTestFileService(&fakeDialogs{})
	if res := s.Open(); !res.Cancelled || res.Error != "" {
		t.Errorf("Open = %+v", res)
	}
	if res := s.SaveAs("Query 1", "x", ""); !res.Cancelled || res.Error != "" {
		t.Errorf("SaveAs = %+v", res)
	}
}

func TestFileServiceDialogErrorsAreReported(t *testing.T) {
	s := newTestFileService(&fakeDialogs{err: errors.New("no display")})
	if res := s.Open(); !strings.Contains(res.Error, "no display") {
		t.Errorf("Open = %+v", res)
	}
}

func TestFileServiceSaveAsAddsExtensionAndDefaultsToUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	d := &fakeDialogs{save: filepath.Join(dir, "orders")}
	s := newTestFileService(d)

	res := s.SaveAs("Query 1", "SELECT 1;", "")
	if res.Error != "" {
		t.Fatalf("SaveAs = %+v", res)
	}
	if d.gotName != "Query 1.sql" {
		t.Errorf("suggested name = %q", d.gotName)
	}
	want := filepath.Join(dir, "orders.sql")
	if res.Path != want || res.Encoding != sqlfile.UTF8BOM {
		t.Fatalf("SaveAs = %+v", res)
	}
	data, err := os.ReadFile(want)
	if err != nil || string(data) != "\xef\xbb\xbfSELECT 1;" {
		t.Fatalf("file = %q, %v", data, err)
	}
	// The chosen path can now be saved without a dialog.
	if again := s.Save(want, "SELECT 2;", res.Encoding); again.Error != "" {
		t.Fatalf("Save = %+v", again)
	}
}

func TestFileServiceSaveAsFallsBackWhenEncodingCannotStoreText(t *testing.T) {
	dir := t.TempDir()
	s := newTestFileService(&fakeDialogs{save: filepath.Join(dir, "legacy.sql")})
	res := s.SaveAs("legacy.sql", "SELECT N'東京';", sqlfile.Windows1252)
	if res.Error != "" || res.Encoding != sqlfile.UTF8BOM {
		t.Fatalf("SaveAs = %+v", res)
	}
}

func TestFileServiceOpenReportsMissingFile(t *testing.T) {
	s := newTestFileService(&fakeDialogs{open: filepath.Join(t.TempDir(), "gone.sql")})
	if res := s.Open(); !strings.Contains(res.Error, "no longer exists") {
		t.Fatalf("Open = %+v", res)
	}
}

func TestWindowServiceInterceptsCloseOnlyWithUnsavedChanges(t *testing.T) {
	s := NewWindowService()
	prompts := 0
	s.emitPrompt = func() { prompts++ }

	if s.interceptClose() {
		t.Fatal("close without unsaved changes should not be blocked")
	}
	s.SetUnsavedChanges(true)
	if !s.interceptClose() || prompts != 1 {
		t.Fatalf("close with unsaved changes: prompts = %d", prompts)
	}
	s.Close() // no window attached: only records the decision
	if s.interceptClose() || prompts != 1 {
		t.Fatal("close after the user confirmed should not be blocked")
	}
}
