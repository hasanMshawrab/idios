// Package query holds the read models: one function per endpoint row set,
// over the read pool, returning flat structs named for the screen.
package query

import "strings"

// Page bounds a list. A zero Limit means no bound was configured and the
// function returns everything.
type Page struct{ Limit int }

// StateAttention is the list state that is not a row's own: open, or closed
// within the attention window and never acknowledged or dismissed.
const StateAttention = "attention"

// stateExpr derives an incident's wire state. Dismissal outranks every other
// flag, and a closed row reports why it closed. It reads columns of the
// incidents table under the alias i.
const stateExpr = `CASE WHEN i.dismissed_at IS NOT NULL THEN 'dismissed'
     WHEN i.closed_at IS NOT NULL THEN i.close_reason
     WHEN i.acknowledged_at IS NOT NULL THEN 'acknowledged'
     ELSE 'open' END`

// limitClause asks for one row past the limit, which is how truncate tells a
// full page from a truncated one.
func limitClause(p Page, args []any) (string, []any) {
	if p.Limit <= 0 {
		return "", args
	}
	return " LIMIT ?", append(args, p.Limit+1)
}

func truncate[T any](rows []T, p Page) ([]T, bool) {
	if p.Limit <= 0 || len(rows) <= p.Limit {
		return rows, false
	}
	return rows[:p.Limit], true
}

// clusterFilter returns an IN predicate on column for ids, or "" when ids is
// empty, meaning every cluster. Values travel as placeholders.
func clusterFilter(column string, ids []int64, args []any) (string, []any) {
	if len(ids) == 0 {
		return "", args
	}
	for _, id := range ids {
		args = append(args, id)
	}
	return column + " IN (" + strings.TrimPrefix(strings.Repeat(",?", len(ids)), ",") + ")", args
}

// categoryRank orders the categories worst first, the order the
// application's sidebar lists them; a category outside the list sorts
// behind every one in it. col is the column or alias holding the
// category.
func categoryRank(col string) string {
	return "CASE " + col + " WHEN 'crash' THEN 0 WHEN 'oom' THEN 1 WHEN 'unclean_exit' THEN 2" +
		" WHEN 'image_pull' THEN 3 WHEN 'config' THEN 4 WHEN 'probe' THEN 5 WHEN 'scheduling' THEN 6" +
		" WHEN 'stuck' THEN 7 WHEN 'node_pressure' THEN 8 WHEN 'rescheduled' THEN 9 WHEN 'job_failed' THEN 10" +
		" ELSE 11 END"
}

func where(predicates []string) string {
	if len(predicates) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(predicates, " AND ")
}
