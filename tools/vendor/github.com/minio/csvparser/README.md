# csvparser

```
// Package csv reads and writes comma-separated values (CSV) files.
// There are many kinds of CSV files; this package supports the format
// described in RFC 4180.
//
// A csv file contains zero or more records of one or more fields per record.
// Each record is separated by the newline character. The final record may
// optionally be followed by a newline character.
//
//      field1,field2,field3
//
// White space is considered part of a field.
//
// Carriage returns before newline characters are silently removed.
//
// Blank lines are ignored. A line with only whitespace characters (excluding
// the ending newline character) is not considered a blank line.
//
// Fields which start and stop with the quote character " are called
// quoted-fields. The beginning and ending quote are not part of the
// field.
//
// The source:
//
//      normal string,"quoted-field"
//
// results in the fields
//
//      {`normal string`, `quoted-field`}
//
// Within a quoted-field a quote character followed by a second quote
// character is considered a single quote.
//
//      "the ""word"" is true","a ""quoted-field"""
//
// results in
//
//      {`the "word" is true`, `a "quoted-field"`}
//
// Newlines and commas may be included in a quoted-field
//
//      "Multi-line
//      field","comma is ,"
//
// results in
//
//      {`Multi-line
//      field`, `comma is ,`}
//
```

This package is forked from encoding/csv from Go upstream, original package
is under feature freeze and we had to expand the scope of CSV RFC for
S3 Select CSV support.

```
// Modified to be used with MinIO. Main modifications include
// - Configurable 'quote' parameter
// - Performance improvements
//    benchmark                                            old ns/op     new ns/op     delta
//    BenchmarkRead-8                                      2807          2189          -22.02%
//    BenchmarkReadWithFieldsPerRecord-8                   2802          2179          -22.23%
//    BenchmarkReadWithoutFieldsPerRecord-8                2824          2181          -22.77%
//    BenchmarkReadLargeFields-8                           3584          3371          -5.94%
//    BenchmarkReadReuseRecord-8                           2044          1480          -27.59%
//    BenchmarkReadReuseRecordWithFieldsPerRecord-8        2056          1483          -27.87%
//    BenchmarkReadReuseRecordWithoutFieldsPerRecord-8     2047          1482          -27.60%
//    BenchmarkReadReuseRecordLargeFields-8                2777          2594          -6.59%
```
