package sqlfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = "SELECT N'Müller — 東京' AS name;\r\nGO\r\n"

func TestDecodeDetectsEncodings(t *testing.T) {
	for _, enc := range []Encoding{UTF8, UTF8BOM, UTF16LE, UTF16BE} {
		t.Run(string(enc), func(t *testing.T) {
			data, err := Encode(sample, enc)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Encoding != enc || doc.Text != sample {
				t.Fatalf("Decode = (%q, %s), want (%q, %s)", doc.Text, doc.Encoding, sample, enc)
			}
		})
	}
}

func TestEncodeWritesByteOrderMarks(t *testing.T) {
	cases := map[Encoding][]byte{
		UTF8BOM: {0xEF, 0xBB, 0xBF, 'A'},
		UTF16LE: {0xFF, 0xFE, 'A', 0},
		UTF16BE: {0xFE, 0xFF, 0, 'A'},
		UTF8:    {'A'},
	}
	for enc, want := range cases {
		got, err := Encode("A", enc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("Encode(%s) = % x, want % x", enc, got, want)
		}
	}
}

func TestDecodeFallsBackToWindows1252(t *testing.T) {
	// "café" in Windows-1252: é is 0xE9, which is not valid UTF-8 on its own.
	doc, err := Decode([]byte("SELECT 'caf\xe9'"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Encoding != Windows1252 || doc.Text != "SELECT 'café'" {
		t.Fatalf("Decode = (%q, %s)", doc.Text, doc.Encoding)
	}
	back, err := Encode(doc.Text, Windows1252)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != "SELECT 'caf\xe9'" {
		t.Fatalf("round trip = % x", back)
	}
}

func TestEncodeWindows1252RejectsUnrepresentableText(t *testing.T) {
	_, err := Encode("SELECT N'東京'", Windows1252)
	if err == nil || !strings.Contains(err.Error(), "Save As") {
		t.Fatalf("err = %v, want a Save As hint", err)
	}
}

func TestEncodeRejectsUnknownEncoding(t *testing.T) {
	if _, err := Encode("x", "ebcdic"); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeRejectsBrokenUTF8AfterBOM(t *testing.T) {
	if _, err := Decode([]byte{0xEF, 0xBB, 0xBF, 0xFF}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestWriteAndReadRoundTripKeepsEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.sql")
	if err := Write(path, sample, UTF16LE); err != nil {
		t.Fatal(err)
	}
	doc, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Text != sample || doc.Encoding != UTF16LE {
		t.Fatalf("Read = (%q, %s)", doc.Text, doc.Encoding)
	}
}

func TestReadRejectsLargeFilesAndFolders(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); err == nil {
		t.Fatal("reading a folder should fail")
	}
	big := filepath.Join(dir, "big.sql")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := Read(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}
