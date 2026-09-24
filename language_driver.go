package godbf

import (
	"fmt"
)

// languageDriverOffset is where a dBase header records the code page it was
// written in: byte 29 of the file, the "language driver" byte. Until this file
// existed, every caller had to know that and pass the encoding in itself.
const languageDriverOffset = 29

// fallbackEncoding is used when the language driver byte is one this table does
// not know, or when the file declares 0x00. It mirrors the reference
// implementation (dbfread's dbf.py), which falls back to 'ascii' in the same two
// cases.
//
// Where the two sides differ in that case: a byte above 0x7f read as ascii makes
// Python's strict codec raise UnicodeDecodeError, while mahonia's ascii charmap
// substitutes U+FFFD. Neither invents a character — one refuses the input and the
// other returns it with visible replacements — and for pure ASCII data, which is
// what a file declaring no code page almost always holds, they agree exactly.
const fallbackEncoding = "ascii"

// languageDriverEncodings maps the header's language driver byte to the name of
// the encoding to read character fields with.
//
// The byte-to-code-page choices are dbfread's (dbfread/codepages.py), so that
// both backends decode the same file the same way. The names are not dbfread's
// Python codec names, because these strings go straight to mahonia, which
// registers its charmaps under its own names: the single-byte DOS pages are
// "IBM437" style where mahonia has them and "ibm-857_P100-1995" style where it
// does not, the cp125x family is "windows-125x", cp932 is "Shift_JIS", cp936
// "GBK", cp950 "Big5", cp874 "windows-874", cp1252 is "windows-1252" (mahonia
// does not accept "cp1252"), mac_roman is "macintosh", and mac_latin2 is
// "x-mac-ce", the Mac Central European charmap.
//
// Two bytes are deliberately absent, and a file declaring them falls back to
// fallbackEncoding: 0x4E and 0x79, the Korean code pages. mahonia ships no Korean
// charmap, so there is no name to put here — the Python side reads those files
// and this side cannot, which is a gap rather than a translation problem, and is
// recorded rather than papered over with a codec that would quietly mangle the
// text.
//
// TestLanguageDriverEncodingsAreResolvable asserts every name below resolves in
// mahonia, so a wrong label fails the test run rather than the file being read.
var languageDriverEncodings = map[byte]string{
	0x00: "ascii",
	0x01: "IBM437",
	0x02: "IBM850",
	0x03: "windows-1252",
	0x04: "macintosh",
	0x08: "ibm-865_P100-1995",
	0x09: "IBM437",
	0x0A: "IBM850",
	0x0B: "IBM437",
	0x0D: "IBM437",
	0x0E: "IBM850",
	0x0F: "IBM437",
	0x10: "IBM850",
	0x11: "IBM437",
	0x12: "IBM850",
	0x13: "Shift_JIS",
	0x14: "IBM850",
	0x15: "IBM437",
	0x16: "IBM850",
	0x17: "ibm-865_P100-1995",
	0x18: "IBM437",
	0x19: "IBM437",
	0x1A: "IBM850",
	0x1B: "IBM437",
	0x1C: "ibm-863_P100-1995",
	0x1D: "IBM850",
	0x1F: "IBM852",
	0x22: "IBM852",
	0x23: "IBM852",
	0x24: "ibm-860_P100-1995",
	0x25: "IBM850",
	0x26: "IBM866",
	0x37: "IBM850",
	0x40: "IBM852",
	0x4D: "GBK",
	// 0x4E Korean (Windows) — omitted, see the note above.
	0x4F: "Big5",
	0x50: "windows-874",
	0x57: "windows-1252",
	0x58: "windows-1252",
	0x59: "windows-1252",
	0x64: "IBM852",
	0x65: "IBM866",
	0x66: "ibm-865_P100-1995",
	0x67: "ibm-861_P100-1995",
	0x6A: "IBM737",
	0x6B: "ibm-857_P100-1995",
	0x78: "Big5",
	// 0x79 Korean (Windows) — omitted, see the note above.
	0x7A: "GBK",
	0x7B: "Shift_JIS",
	0x7C: "windows-874",
	0x7D: "windows-1255",
	0x7E: "windows-1256",
	0x96: "x-mac-cyrillic",
	0x97: "x-mac-ce",
	0x98: "x-mac-greek",
	0xC8: "windows-1250",
	0xC9: "windows-1251",
	0xCA: "windows-1254",
	0xCB: "windows-1253",
}

// DetectEncoding returns the encoding a dBase file declares for its character
// fields, read from the language driver byte in its header.
//
// A language driver this table does not know falls back to fallbackEncoding
// rather than failing, matching the reference implementation: that is a guess,
// but it is the same guess the Python side makes. Input too short to carry a
// header is an error, since there is no byte to read.
func DetectEncoding(data []byte) (string, error) {
	if len(data) <= languageDriverOffset {
		return "", fmt.Errorf("cannot detect encoding: %d bytes is too short to hold a dbf header (needs more than %d)",
			len(data), languageDriverOffset)
	}

	if encoding, known := languageDriverEncodings[data[languageDriverOffset]]; known {
		return encoding, nil
	}

	return fallbackEncoding, nil
}

// NewFromByteArrayWithDetectedEncoding reads a dBase file held in memory, taking
// the character encoding from the file's own header rather than requiring the
// caller to pass one. It is the counterpart of NewFromByteArray for callers
// holding bytes of unknown origin — an uploaded file, say — who would otherwise
// have to reach into byte 29 themselves.
func NewFromByteArrayWithDetectedEncoding(data []byte) (table *DbfTable, newErr error) {
	encoding, encodingErr := DetectEncoding(data)
	if encodingErr != nil {
		return nil, encodingErr
	}
	return NewFromByteArray(data, encoding)
}

// NewFromFileWithDetectedEncoding is NewFromByteArrayWithDetectedEncoding for a
// file on disk.
func NewFromFileWithDetectedEncoding(fileName string) (table *DbfTable, newErr error) {
	data, readErr := readFile(fileName)
	if readErr != nil {
		return nil, readErr
	}
	return NewFromByteArrayWithDetectedEncoding(data)
}
