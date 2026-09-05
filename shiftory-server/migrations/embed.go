package migrations

import "embed"

// Files contains the SQL schema migrations used by the API, worker, and tests.
//
//go:embed *.sql
var Files embed.FS
