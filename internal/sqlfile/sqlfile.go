// Package sqlfile reads and writes .sql files for the editor.
//
// SQL scripts come from many tools: SSMS and Azure Data Studio write UTF-8 with a byte-order
// mark, older SSMS versions and some generators write UTF-16, and legacy scripts are often in
// the Windows ANSI code page. Read detects the encoding so the text shows correctly, and Write
// saves it back in the same encoding so opening and saving a file never changes how other
// tools (sqlcmd, deployment pipelines) read it.
package sqlfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/TuanFaied/qryvanta/internal/fsutil"
)

// Encoding names a file encoding. The values are part of the frontend contract.
type Encoding string

const (
	// UTF8BOM is UTF-8 with a byte-order mark, which SSMS writes and sqlcmd recognises
	// without extra flags. New files are saved in this encoding.
	UTF8BOM Encoding = "utf-8-bom"
	UTF8    Encoding = "utf-8"
	UTF16LE Encoding = "utf-16le"
	UTF16BE Encoding = "utf-16be"
	// Windows1252 is the Western European ANSI code page, used for files that are not valid
	// UTF-8 and have no byte-order mark.
	Windows1252 Encoding = "windows-1252"
)

// DefaultEncoding is used for files that have not been saved before.
const DefaultEncoding = UTF8BOM

// MaxSize is the largest file Read accepts. A query editor is not the place for a multi-
// hundred-megabyte data script, and loading one would freeze the webview.
const MaxSize = 16 << 20

// ErrTooLarge is returned for files over MaxSize.
var ErrTooLarge = fmt.Errorf("the file is larger than %d MB", MaxSize>>20)

// ErrUnsupportedEncoding is returned by Write for an unknown encoding name.
var ErrUnsupportedEncoding = errors.New("unsupported file encoding")

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// Document is a decoded file.
type Document struct {
	Text     string
	Encoding Encoding
}

// Read loads the file at path and decodes it.
func Read(path string) (Document, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Document{}, err
	}
	if info.IsDir() {
		return Document{}, fmt.Errorf("%s is a folder", info.Name())
	}
	if info.Size() > MaxSize {
		return Document{}, ErrTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	if len(data) > MaxSize {
		return Document{}, ErrTooLarge
	}
	return Decode(data)
}

// Decode detects the encoding of data and returns its text.
func Decode(data []byte) (Document, error) {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		rest := data[len(bomUTF8):]
		if !utf8.Valid(rest) {
			return Document{}, errors.New("the file starts with a UTF-8 byte-order mark but is not valid UTF-8")
		}
		return Document{Text: string(rest), Encoding: UTF8BOM}, nil
	case bytes.HasPrefix(data, bomUTF16LE):
		return decodeWith(data, UTF16LE)
	case bytes.HasPrefix(data, bomUTF16BE):
		return decodeWith(data, UTF16BE)
	case utf8.Valid(data):
		return Document{Text: string(data), Encoding: UTF8}, nil
	default:
		return decodeWith(data, Windows1252)
	}
}

func decodeWith(data []byte, enc Encoding) (Document, error) {
	codec, err := codecFor(enc)
	if err != nil {
		return Document{}, err
	}
	text, err := codec.NewDecoder().Bytes(data)
	if err != nil {
		return Document{}, fmt.Errorf("decode %s: %w", enc, err)
	}
	return Document{Text: string(text), Encoding: enc}, nil
}

// Encode converts text to bytes in enc, including the byte-order mark the encoding uses.
func Encode(text string, enc Encoding) ([]byte, error) {
	switch enc {
	case UTF8:
		return []byte(text), nil
	case UTF8BOM:
		return append(append([]byte{}, bomUTF8...), text...), nil
	}
	codec, err := codecFor(enc)
	if err != nil {
		return nil, err
	}
	out, err := codec.NewEncoder().Bytes([]byte(text))
	if err != nil {
		if enc == Windows1252 {
			return nil, errors.New("the text contains characters that the file's Windows-1252 encoding cannot store; " +
				"use Save As to save it as UTF-8")
		}
		return nil, fmt.Errorf("encode %s: %w", enc, err)
	}
	return out, nil
}

func codecFor(enc Encoding) (encoding.Encoding, error) {
	switch enc {
	case UTF16LE:
		// ExpectBOM: the decoder strips the mark and the encoder writes it.
		return unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM), nil
	case UTF16BE:
		return unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM), nil
	case Windows1252:
		return charmap.Windows1252, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedEncoding, enc)
	}
}

// Write saves text to path in enc, atomically: a failed save never leaves a truncated script.
func Write(path, text string, enc Encoding) error {
	data, err := Encode(text, enc)
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}
