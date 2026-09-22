// Package migrations 将版本化 SQL 内嵌到迁移程序，供 Goose 按顺序执行
package migrations

import "embed"

// Files 保存数据库迁移使用的嵌入式 SQL 文件
//
//go:embed *.sql
var Files embed.FS
