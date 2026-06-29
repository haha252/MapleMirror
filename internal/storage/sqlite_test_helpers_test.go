package storage

import (
	"database/sql"
	"testing"
)

func assertTable(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, table string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("缺少数据表 %s：%v", table, err)
	}
}

func assertIndex(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, index string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("缺少索引 %s：%v", index, err)
	}
}

func assertColumn(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return
		}
	}
	t.Fatalf("缺少数据列 %s.%s", table, column)
}

func assertColumnDefault(t *testing.T, db *sql.DB, table, column, want string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			if defaultValue != want {
				t.Fatalf("%s.%s default=%v want %s", table, column, defaultValue, want)
			}
			return
		}
	}
	t.Fatalf("缺少数据列 %s.%s", table, column)
}

func assertDBVersion(t *testing.T, db *sql.DB, kind string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT version FROM database_version WHERE kind = ?`, kind).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s database version=%d want=%d", kind, got, want)
	}
}
