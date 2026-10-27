package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// ReplaceFile is declared here as documentation of the service method the upload
// handler relies on; see importx.Service.ReplaceFile.

func derefInt64Or(v *int64, def int64) int64 {
	if v == nil {
		return def
	}
	return *v
}

func auditEntryExport(p *rbac.Principal, entityType, format string) audit.Entry {
	return audit.Entry{
		OrganizationID: &p.OrganizationID, ActorUserID: &p.UserID,
		Action: string(audit.ActionExportExecuted), EntityType: entityType,
		Metadata: map[string]any{"format": format},
	}
}

// streamCSVExport writes an entity set as CSV.
//
// The header is derived from the first row rather than a fixed column list, so a
// new column appears in exports automatically. Values are written with encoding/
// csv, which handles quoting, so a value containing a comma or a newline cannot
// break the structure.
func (s *Server) streamCSVExport(w http.ResponseWriter, r *http.Request, p *rbac.Principal, entityType string) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	headerWritten := false
	_, err := s.deps.Imports.Export(r.Context(), p, entityType, "csv", func(row map[string]any) error {
		if !headerWritten {
			columns := make([]string, 0, len(row))
			for k := range row {
				columns = append(columns, k)
			}
			sort.Strings(columns)
			if err := cw.Write(columns); err != nil {
				return err
			}
			headerWritten = true
		}
		columns := make([]string, 0, len(row))
		for k := range row {
			columns = append(columns, k)
		}
		sort.Strings(columns)
		values := make([]string, len(columns))
		for i, col := range columns {
			values[i] = formatCell(row[col])
		}
		return cw.Write(values)
	})
	cw.Flush()
	return err
}

// streamJSONLExport writes an entity set as newline-delimited JSON.
func (s *Server) streamJSONLExport(w http.ResponseWriter, r *http.Request, p *rbac.Principal, entityType string) error {
	enc := json.NewEncoder(w)
	_, err := s.deps.Imports.Export(r.Context(), p, entityType, "jsonl", func(row map[string]any) error {
		return enc.Encode(row)
	})
	return err
}

func formatCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		return t.Format(time.RFC3339)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
