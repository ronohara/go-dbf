package godbf

import (
	"bytes"
	"errors"
	"fmt"
)

// NewFromByteArray creates a DbfTable, reading it from a raw byte array, expecting the supplied encoding.
func NewFromByteArray(data []byte, fileEncoding string) (table *DbfTable, newErr error) {
	defer func() {
		if e := recover(); e != nil {
			newErr = fmt.Errorf("%v", e)
		}
	}()

	dt := new(DbfTable)
	dt.UseEncoding(fileEncoding)
	unpackHeader(data, dt)
	unpackRecords(data, dt)
	unpackFooter(data, dt)

	verifyTableAgainstRawBytes(data, dt)

	lockSchema(dt)
	return dt, nil
}

func unpackHeader(s []byte, dt *DbfTable) error {
	dt.fileSignature = s[0]
	dt.SetLastUpdatedFromBytes(s[1:4])
	dt.SetNumberOfRecordsFromBytes(s[4:8])
	dt.SetNumberOfBytesInHeaderFromBytes(s[8:10])
	dt.SetLengthOfEachRecordFromBytes(s[10:12])

	if fieldErr := unpackFields(s, dt); fieldErr != nil {
		return fieldErr
	}
	return nil
}

func unpackFields(s []byte, dt *DbfTable) error {
	// create fieldMap to translate field name to index
	dt.fieldMap = make(map[string]int)

	// Number of fields in dbase table
	dt.numberOfFields = int((dt.numberOfBytesInHeader - 1 - 32) / 32)
	for i := 0; i < dt.numberOfFields; i++ {
		if unpackFieldErr := unpackField(s, dt, i); unpackFieldErr != nil {
			return unpackFieldErr
		}
	}
	return nil
}

func unpackField(s []byte, dt *DbfTable, fieldIndex int) error {
	offset := (fieldIndex * 32) + 32

	fieldName := deriveFieldName(s, dt, offset)

	dt.fieldMap[fieldName] = fieldIndex

	var unpackErr error

	switch s[offset+11] {
	case 'C':
		unpackErr = dt.AddTextField(fieldName, s[offset+16])
	case 'N':
		unpackErr = dt.AddNumberField(fieldName, s[offset+16], s[offset+17])
	case 'F':
		unpackErr = dt.AddFloatField(fieldName, s[offset+16], s[offset+17])
	case 'L':
		unpackErr = dt.AddBooleanField(fieldName)
	case 'D':
		unpackErr = dt.AddDateField(fieldName)
	}

	if unpackErr != nil {
		return unpackErr
	}

	return nil
}

func deriveFieldName(s []byte, dt *DbfTable, offset int) string {
	nameBytes := s[offset : offset+fieldNameByteLength]

	// Max usable field length is 10 bytes, where the 11th should contain the end of field marker.
	endOfFieldIndex := bytes.Index(nameBytes, []byte{endOfFieldNameMarker})
	if endOfFieldIndex == -1 {
		msg := fmt.Sprintf("end-of-field marker missing from field bytes, offset [%d,%d]", offset, offset+fieldNameByteLength)
		panic(errors.New(msg))
	}

	fieldName := dt.decoder.ConvertString(string(nameBytes[:endOfFieldIndex]))
	return fieldName
}

func unpackRecords(data []byte, dt *DbfTable) {
	dt.dataStore = data // TODO: Deprecate?  At least reduce scope to just its records.
}

// expectedContentSize is the size the header implies for the file's content: the
// header itself plus every record it declares. The optional 0x1a end-of-file
// marker sits beyond this.
func expectedContentSize(dt *DbfTable) uint32 {
	return uint32(dt.numberOfBytesInHeader) + dt.numberOfRecords*uint32(dt.lengthOfEachRecord)
}

func unpackFooter(data []byte, dt *DbfTable) {
	// Default to the canonical marker and take the file's own only when the file
	// actually ends in one. The marker is optional, so a file of exactly the size
	// the header implies is complete — and the byte it ends on is a record's last,
	// not a footer. Normalising here keeps two things right: the footer check,
	// which would otherwise compare that record byte against 0x1a, and
	// SaveToFile, which appends dt.eofMarker to whatever it writes.
	dt.eofMarker = eofMarker
	if uint32(len(data)) == expectedContentSize(dt)+1 {
		dt.eofMarker = data[len(data)-1]
	}
}

func verifyTableAgainstRawBytes(s []byte, dt *DbfTable) {
	verifyTableAgainstRawHeader(s, dt)
	verifyTableAgainstRawFooter(s, dt)
}

// verifyTableAgainstRawFooter rejects a file that ends with a byte other than the
// marker. A file with no marker at all is not rejected here — unpackFooter has
// already given it the canonical one, because the byte it ends on belongs to a
// record — and a file with too many bytes is caught by the size check.
func verifyTableAgainstRawFooter(s []byte, dt *DbfTable) {
	if dt.eofMarker != eofMarker {
		panic(fmt.Errorf("encoded footer is %v, but actual footer is %d", eofMarker, s[len(s)-1]))
	}
}

func verifyTableAgainstRawHeader(s []byte, dt *DbfTable) {
	verifyByteArraySizeAgainstExpected(s, dt)
}

// verifyByteArraySizeAgainstExpected checks that the file holds exactly the
// header and the records the header declares, so that a truncated file is
// rejected rather than read as if complete.
//
// The 0x1a end-of-file marker is optional: dBase writes it, plenty of other
// tools do not. A file one byte short of the size with the marker is therefore
// complete, not truncated — ZillowNeighborhoods-NY.dbf in the Workbench
// sampledata is exactly that, and refusing it was this library's bug. Anything
// else still fails: bytes beyond the marker are as wrong as bytes missing.
func verifyByteArraySizeAgainstExpected(s []byte, dt *DbfTable) {
	contentSize := expectedContentSize(dt)
	withMarkerSize := contentSize + 1
	actualSize := uint32(len(s))
	if actualSize != contentSize && actualSize != withMarkerSize {
		panic(fmt.Errorf("encoded content is %d bytes, but header expected %d (%d without the end-of-file marker)", actualSize, withMarkerSize, contentSize))
	}
}

func lockSchema(dt *DbfTable) {
	dt.schemaLocked = true // Schema changes no longer permitted
}

// New creates a new dbase table from scratch for the given character encoding
func New(encoding string) (table *DbfTable) {
	dt := new(DbfTable)

	// read dbase table header information
	dt.fileSignature = 0x03
	dt.RefreshLastUpdated()
	dt.numberOfRecords = 0
	dt.numberOfBytesInHeader = 32
	dt.lengthOfEachRecord = 0

	dt.UseEncoding(encoding)
	dt.createdFromScratch = true
	// create fieldMap to translate field name to index
	dt.fieldMap = make(map[string]int)
	dt.schemaLocked = false

	// Number of fields in dbase table
	dt.numberOfFields = 0
	dt.eofMarker = eofMarker

	s := make([]byte, dt.numberOfBytesInHeader)

	// set DbfTable dataStore slice that will store the complete file in memory
	dt.dataStore = s

	dt.dataStore[0] = dt.fileSignature
	dt.dataStore[1] = dt.updateYear
	dt.dataStore[2] = dt.updateMonth
	dt.dataStore[3] = dt.updateDay

	// no MDX file (index upon demand)
	dt.dataStore[28] = 0x00

	// set dbase language driver
	// Huston we have problem!
	// There is no easy way to deal with encoding issues. At least at the moment
	// I will try to find archaic encoding code defined by dbase standard (if there is any)
	// for given encoding. If none match I will go with default ANSI.
	//
	// Despite this flag in set in dbase file, I will continue to use provide encoding for
	// everything except this file encoding flag.
	//
	// Why? To make sure at least if you know the real encoding you can process text accordingly.

	if code, ok := encodingTable[lookup[encoding]]; ok {
		dt.dataStore[29] = code
	} else {
		dt.dataStore[29] = 0x57 // ANSI
	}

	dt.updateDataStore()

	return dt
}
