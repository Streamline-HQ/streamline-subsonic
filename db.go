package main

import (
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/datahearth/streamline/extensions/sdk"
)

// inChunk bounds the IN list of one statement, well under SQLite's limit on
// bound parameters.
const inChunk = 500

// row is one result row. The host sends numbers as float64, text as string
// and NULL as nil; the accessors turn a NULL or a mismatched value into the
// zero value, so a column read never panics.
type row []any

func (r row) str(i int) string {
	s, _ := r[i].(string)
	return s
}

func (r row) int(i int) int64 {
	f, _ := r[i].(float64)
	return int64(f)
}

func (r row) uint(i int) uint32 {
	return uint32(min(max(r.int(i), 0), math.MaxUint32))
}

func (r row) bool(i int) bool { return r.int(i) != 0 }

// time reads a stored or core timestamp; the zero time for NULL or text that
// is not one.
func (r row) time(i int) time.Time { return parseTime(r.str(i)) }

func query(sql string, args ...any) ([]row, error) {
	res, err := sdk.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	rows := make([]row, len(res.Rows))
	for i, r := range res.Rows {
		rows[i] = r
	}
	return rows, nil
}

// queryRow returns the first row, and false when there is none.
func queryRow(sql string, args ...any) (row, bool, error) {
	rows, err := query(sql, args...)
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	return rows[0], true, nil
}

func exec(sql string, args ...any) (sdk.SQLResult, error) {
	res, err := sdk.Exec(sql, args...)
	if err != nil {
		return sdk.SQLResult{}, err
	}
	if res.Error != "" {
		return sdk.SQLResult{}, errors.New(res.Error)
	}
	return res, nil
}

// tx runs fn inside one transaction on the request's connection and rolls it
// back when fn fails. IMMEDIATE takes the write lock at once, so fn never
// meets a busy database halfway.
func tx(fn func() error) error {
	if _, err := exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if err := fn(); err != nil {
		if _, rbErr := exec("ROLLBACK"); rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}
	_, err := exec("COMMIT")
	return err
}

// placeholders is n comma-separated "?".
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func idArgs(ids []uint32) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// queryIn runs sql once per chunk of ids and joins the rows. The text "{in}"
// in sql stands for the placeholders of the chunk, which come last among the
// arguments, after lead.
func queryIn(sql string, ids []uint32, lead ...any) ([]row, error) {
	var out []row
	for chunk := range slices.Chunk(ids, inChunk) {
		rows, err := query(
			strings.Replace(sql, "{in}", placeholders(len(chunk)), 1),
			append(slices.Clone(lead), idArgs(chunk)...)...,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// storedTime is how this extension's own tables keep a time: UTC, to the
// millisecond, so text order is time order.
const storedTime = "2006-01-02T15:04:05.000Z"

func nowStored() string { return time.Now().UTC().Format(storedTime) }

func formatStored(t time.Time) string { return t.UTC().Format(storedTime) }

// timestamp is a time as a response carries it.
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func optTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return timestamp(t)
}

var timeLayouts = []string{
	storedTime,
	time.RFC3339Nano,
	// The core database keeps a time as Go prints one.
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

// parseTime reads a time from this extension's tables or from a core view.
// The zero time stands for NULL, an empty string and anything unreadable.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// coreMillis is an SQL expression for a core time column, which holds a time
// as Go prints one ("2006-01-02 15:04:05.999999999 -0700 MST", in whatever
// zone it was written), as Unix milliseconds. The abbreviation after the
// offset is ignored, and a value with no offset reads as UTC.
func coreMillis(col string) string {
	rest := "substr(" + col + ", 20)"
	pos := "(19 + instr(" + rest + ", ' '))"
	offset := "CASE WHEN instr(" + rest + ", ' ') = 0 THEN '+00:00' ELSE " +
		"(CASE substr(" + col + ", " + pos + " + 1, 1) WHEN '-' THEN '+' ELSE '-' END)" +
		" || substr(" + col + ", " + pos + " + 2, 2) || ':' || substr(" + col + ", " + pos + " + 4, 2) END"
	fraction := "CASE WHEN substr(" + col + ", 20, 1) = '.' THEN " +
		"CAST('0' || substr(" + col + ", 20, instr(" + rest + " || ' ', ' ') - 1) AS REAL) ELSE 0 END"
	return "((CAST(strftime('%s', substr(" + col + ", 1, 19), " + offset +
		") AS INTEGER) + " + fraction + ") * 1000)"
}
