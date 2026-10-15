package store

// RelationalSQLDialect contains the syntax decisions that vary between SQL
// engines. Driver code supplies its own identifier quoting and bind markers.
type RelationalSQLDialect struct {
	QuoteIdentifier func(string) string
	BindVariable    func(int) string
	Pagination      func(limit, offset int) string
	KeysetPredicate func(quotedColumn, bindVariable string) string
}
