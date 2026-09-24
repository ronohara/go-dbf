package godbf

import (
	"encoding/binary"
	"testing"

	"github.com/axgle/mahonia"
	"github.com/onsi/gomega"
)

// Every name in the table has to resolve in mahonia: these strings are handed to
// it directly, and an unresolvable label would decode nothing while looking fine.
func TestLanguageDriverEncodingsAreResolvable(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	for driver, encoding := range languageDriverEncodings {
		decoder := mahonia.NewDecoder(encoding)
		g.Expect(decoder).ToNot(gomega.BeNil(),
			"language driver 0x%02x maps to %q, which mahonia cannot resolve", driver, encoding)
	}
}

// The detection has to make the same choice the reference implementation makes,
// including its fallback for a byte it does not know.
func TestDetectEncodingMatchesReference(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	cases := []struct {
		driver byte
		want   string
	}{
		{0x00, "ascii"},
		{0x01, "IBM437"},
		{0x02, "IBM850"},
		{0x03, "windows-1252"},
		{0x57, "windows-1252"}, // the byte ZillowNeighborhoods-NY.dbf carries
		{0x58, "windows-1252"},
		{0x59, "windows-1252"},
		{0xC8, "windows-1250"},
		{0x7B, "Shift_JIS"},
		{0x4E, fallbackEncoding}, // Korean: mahonia has no charmap, so this falls back
		{0xFF, fallbackEncoding}, // unknown, so the same guess dbfread makes
	}

	for _, c := range cases {
		data := make([]byte, 32)
		data[languageDriverOffset] = c.driver

		got, err := DetectEncoding(data)
		g.Expect(err).To(gomega.BeNil())
		g.Expect(got).To(gomega.Equal(c.want), "language driver 0x%02x", c.driver)
	}
}

func TestDetectEncoding_TooShortIsAnError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	_, err := DetectEncoding(make([]byte, languageDriverOffset))
	g.Expect(err).ToNot(gomega.BeNil())
}

// The point of the function: a real file, read without telling it the encoding.
func TestNewFromByteArrayWithDetectedEncoding(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	data, readErr := readFile(validTestFile)
	g.Expect(readErr).To(gomega.BeNil())

	table, err := NewFromByteArrayWithDetectedEncoding(data)
	g.Expect(err).To(gomega.BeNil())
	g.Expect(table.NumberOfRecords()).To(gomega.Equal(3))
	g.Expect(table.FieldNames()).To(gomega.HaveLen(5))
}

func TestNewFromFileWithDetectedEncoding(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	table, err := NewFromFileWithDetectedEncoding(validTestFile)
	g.Expect(err).To(gomega.BeNil())
	g.Expect(table.NumberOfRecords()).To(gomega.Equal(3))
}

// A header declaring a code page must actually change how text is decoded, not
// merely be recorded.
func TestDetectedEncodingIsUsedForDecoding(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	data, readErr := readFile(validTestFile)
	g.Expect(readErr).To(gomega.BeNil())

	// validFile.dbf declares 0x00. Point it at Windows-1252 and put into the first
	// text field a byte that only Windows-1252 decodes: 0xe9, 'é'. The field sits
	// at record offset 2 — after the deletion flag and TESTBOOL — as the header's
	// own field descriptors lay the record out.
	data[languageDriverOffset] = 0x57
	headerSize := int(binary.LittleEndian.Uint16(data[8:10]))
	const firstTextFieldOffset = 2
	data[headerSize+firstTextFieldOffset] = 0xE9

	table, err := NewFromByteArrayWithDetectedEncoding(data)
	g.Expect(err).To(gomega.BeNil())

	value, fieldErr := table.FieldValueByName(0, "TESTTEXT")
	g.Expect(fieldErr).To(gomega.BeNil())
	g.Expect(value).To(gomega.HavePrefix("é"))
}
