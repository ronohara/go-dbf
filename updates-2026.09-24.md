# Fork updates — 2026-09-24

Three changes on top of commit `8b6d3c8` ("Own the fork: module path, go directive,
README"), all prompted by using the library for real: reading a dBase file that
arrived as a user upload in the Statisticians Workbench.

**Status: released as `v1.0.1`** — commit `9b7a5c7`, annotated tag pushed, and the
published module fetched back from the module proxy and exercised against the same
fixture this work was driven by (579 records, detected encoding `windows-1252`).

Two notes on the history around it:

- these changes were first described as sitting on top of a `v1.0.0` tag. That tag was
deleted on 2026-09-24 rather than published: it predates these fixes, so publishing it
would have advertised a reader that refuses a file without the end-of-file marker. The
commit it pointed at is untouched, and `v1.0.1` is now the fork's only tag.
- the module proxy's `@latest` still resolves to an earlier cached pseudo-version.
Pin `v1.0.1` rather than relying on `@latest` until that cache expires.

---

## 1. A file without the optional end-of-file marker is now readable

### What was wrong

`verifyByteArraySizeAgainstExpected` (`interpreter.go`) required a file's length to
be exactly `header + records × recordSize + 1` — the trailing `0x1a` end-of-file
marker counted — and panicked when it was anything else.

That byte is **optional**. dBase writes it; plenty of other tools do not. A real
file that omits it was refused outright:

```
$ ./probe ZillowNeighborhoods-NY.dbf
parse error: encoded content is 232372 bytes, but header expected 232373
```

The file is exactly `header + records` (193 + 579 × 401 = 232,372). The one byte the
library expected beyond that is the marker, which is not content — nothing was
missing from the file.

### The change

- A new `expectedContentSize(dt *DbfTable) uint32` helper computes header-plus-records
  once, so the arithmetic lives in one place.
- `verifyByteArraySizeAgainstExpected` now accepts **that** size or **that size plus
  one**, and its message names both expected values.
- Everything else still fails: a file with bytes missing is still truncated, and a
  file whose header disagrees with its length is still inconsistent.

### The second half, which mattered more

`unpackFooter` used to take the file's last byte as the end-of-file marker
unconditionally. Once the size check tolerated a marker-less file, that byte became a
**record's** last byte, which breaks two things:

1. the footer check (`verifyTableAgainstRawFooter`), which compares that byte against
   `0x1a` and would fail;
2. `SaveToFile`, which appends `dt.eofMarker` to whatever it writes (`table.go:324`) —
   so loading a marker-less file and saving it would have written a record byte into
   the footer, silently corrupting the output.

`unpackFooter` now defaults to the canonical `0x1a`, and takes the file's own byte
only when the file actually ends in one.

### Verified

| Case | Result |
| --- | --- |
| `ZillowNeighborhoods-NY.dbf` as delivered (no marker) | loads — 579 records, 5 fields, correct values |
| `testdata/validFile.dbf` (marker present) | still loads |
| `testdata/lessThanActualRecords.dbf` (header disagrees with length) | still refused |
| a truncated copy of the Zillow file | still refused |

---

## 2. Encoding detection — callers no longer read byte 29

### What was wrong

A dBase header records its code page in the **language driver byte at offset 29**.
The reference implementation (`dbfread`) reads it and picks the encoding itself. This
library took the encoding as a parameter and never looked at that byte, so any caller
wanting reference behaviour had to read byte 29 and re-implement the mapping — the
reference's logic living in the consumer, which is the wrong place for it.

### What was added

`language_driver.go`:

```go
func DetectEncoding(data []byte) (string, error)

func NewFromByteArrayWithDetectedEncoding(data []byte) (*DbfTable, error)
func NewFromFileWithDetectedEncoding(fileName string) (*DbfTable, error)
```

`DetectEncoding` returns the declared encoding; `ascii` when the byte is one the table
does not know (the same fallback `dbfread` makes); and an error when the input is too
short to hold a header. The two constructors are the reading entry points, and the
byte-array one is what an upload path wants:

```go
table, err := godbf.NewFromByteArrayWithDetectedEncoding(uploadBytes)
```

### The table

`languageDriverEncodings` maps 60 language-driver bytes to encodings. The
byte-to-code-page **choices** are `dbfread`'s (`dbfread/codepages.py`), so both sides
select the same codec. The **names** are not `dbfread`'s, because these strings are
handed straight to mahonia.

### What this work exposed

- **mahonia does not accept Python codec names.** `"cp1252"` and `"cp437"` do not
  resolve. mahonia registers charmaps as `windows-1252`, `IBM437`, and — for pages it
  has no short name for — ICU-style names such as `ibm-857_P100-1995`. A test asserts
  that every name in the table resolves, so a wrong label fails the test run instead of
  a file being read wrongly.
- **Two code pages cannot be represented at all.** mahonia ships no Korean charmap, so
  `0x4E` and `0x79` are deliberately absent and documented. A file declaring them falls
  back rather than being decoded with a codec that would quietly mangle the text.
  `dbfread` reads those files; this library cannot. A gap, recorded rather than hidden.
- **`mac_latin2`** (`0x97`) maps to mahonia's `x-mac-ce`, the Mac Central European
  charmap — a closer match than the ISO-8859-2 stand-in first considered.
- **The fallback differs visibly in one case.** With no usable code page, Python's
  strict `ascii` raises on a byte above `0x7f`, where mahonia substitutes `U+FFFD`.
  Neither invents a character; for pure ASCII data — what such a file almost always
  holds — the two agree exactly.

### Verified

Six tests in `language_driver_test.go`:

- every name in the table resolves in mahonia;
- detection matches the reference's choices for real byte values, including its
  fallback for an unknown byte;
- an input too short to hold a header is an error;
- both constructors read a real fixture with no encoding argument;
- the declared code page genuinely changes decoding — a `0xe9` byte becomes `é` under
  `0x57`.

End to end, against the reference: the same file read by this library with internal
detection and by `dbfread` produced **identical output for all 580 lines** (header row
plus 579 records). This library reported `detected encoding: windows-1252`; `dbfread`
independently chose `cp1252`.

---

## 3. The test suite was order-dependent, and no longer is

### What was wrong

Six tests in `io_test.go` replace package-level indirection variables to simulate
failures — `reader` (default `io.ReadFull`) and `fsWrapper` (default `osFileSystem{}`) —
and never restored them. Any test running afterwards that reads a file gets the stub.

It had never shown because nothing sorting after `io_test.go` read a file. Adding
`language_driver_test.go`, which sorts after it, turned the suite red with
`i'm a little error teapot` — the stub's error — in three otherwise unrelated tests.
Running those tests with `-run` passed, which is what identified the cause as ordering
rather than logic.

### The change

Each of the six now restores its default through `t.Cleanup`:

```go
reader = panicReader
t.Cleanup(func() { reader = io.ReadFull })
```

Their assertions are unchanged, so the stubs are still exercised — the failure each one
simulates now fails *its own* test rather than the next test's.

---

## Verification summary

- `go build ./...` — exit 0
- `go vet ./...` — exit 0
- `go test ./...` — passes; `ok github.com/ronohara/go-dbf`
- Full-suite run, not `-run` filters, since change 3 exists precisely to make that
  distinction stop mattering.

## Known gaps

- Korean code pages (`0x4E`, `0x79`) cannot be decoded: mahonia has no Korean charmap.
  Such files fall back to `ascii` where the Python side reads them as `cp949`.
- Files whose language-driver byte is unknown fall back to `ascii`, which is a guess —
  the same guess `dbfread` makes.
- Memo fields (`.dbt`) are still unsupported; the reader handles C, N, F, L and D.

## Files changed

| File | Change |
| --- | --- |
| `interpreter.go` | Accept a file of exactly header-plus-records as complete; normalise a missing end-of-file marker to `0x1a` |
| `language_driver.go` | **new** — language-driver table, `DetectEncoding`, and the two detecting constructors |
| `language_driver_test.go` | **new** — six tests, including the one that keeps the table honest |
| `io_test.go` | Restore the stubbed globals after the six tests that replace them |
