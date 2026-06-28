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
// JSON text and binary values as base64, matching the JSON response. The
// rows are already materialized in memory (bounded by the query limit),
// so WriteAll keeps a single error path.
func writeCSV(w http.ResponseWriter, cols []string, rows []map[string]any) error {
	records := make([][]string, 0, len(rows)+1)
	records = append(records, cols)

	for _, row := range rows {
		record := make([]string, len(cols))
		for i, col := range cols {
			record[i] = csvCell(row[col])
		}

		records = append(records, record)
	}

	cw := csv.NewWriter(w)
	if err := cw.WriteAll(records); err != nil {
		return fmt.Errorf("write csv: %w", err)
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
