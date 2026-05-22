package query

import (
	"fmt"
	"strings"
)

// parseSelect parses a select expression such as "col1,col2" or "*".
// Phase 3 accepts only flat column lists; the alias and embedded
// relation syntax ("posts(id,title)", "author:users(id,name)") is
// deferred to Phase 4 and rejected here.
func parseSelect(s string) ([]SelectItem, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	fields := strings.Split(s, ",")
	items := make([]SelectItem, 0, len(fields))
	for _, f := range fields {
		col := strings.TrimSpace(f)
		if col == "" {
			return nil, fmt.Errorf("empty select item in %q", s)
		}

		if strings.ContainsAny(col, "()") {
			return nil, fmt.Errorf("embedded select %q is not supported yet", col)
		}

		if strings.Contains(col, ":") {
			return nil, fmt.Errorf("aliased select %q is not supported yet", col)
		}

		items = append(items, SelectItem{Column: col})
	}

	return items, nil
}
