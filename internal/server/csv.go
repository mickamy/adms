package server

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// wantsCSV reports whether the Accept header asks for CSV. It is a plain
// token match with no q-value weighting, so application/json and */*
// keep the default JSON response.
func wantsCSV(accept string) bool {
	for part := range strings.SplitSeq(accept, ",") {
		media := strings.TrimSpace(part)
		if i := strings.IndexByte(media, ';'); i >= 0 {
			media = strings.TrimSpace(media[:i])
		}

		if media == "text/csv" {
			return true
		}
	}

	return false
}

// writeCSV renders scanned rows as RFC 4180 CSV. The header row follows
// the column projection order; embedded relations are emitted as their
// JSON text and binary values as base64, matching the JSON response.
func writeCSV(w http.ResponseWriter, cols []string, rows []map[string]any) error {
	cw := csv.NewWriter(w)

	if err := cw.Write(cols); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	record := make([]string, len(cols))
	for _, row := range rows {
		for i, col := range cols {
			record[i] = csvCell(row[col])
		}

		if err := cw.Write(record); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}

	cw.Flush()

	if err := cw.Error(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	return nil
}

// csvCell formats a scanned value for a CSV cell. The type set mirrors
// what rowsToJSON produces: strings, json.RawMessage for embeds, []byte
// for non-UTF-8 binary, and the driver scalar types otherwise.
func csvCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.RawMessage:
		return string(x)
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
