package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeQueryRower struct {
	query string
	args  []any
	row   pgx.Row
}

func (f *fakeQueryRower) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.query = sql
	f.args = args
	return f.row
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for index, value := range r.values {
		switch target := dest[index].(type) {
		case *int64:
			*target = value.(int64)
		case *string:
			*target = value.(string)
		case *time.Time:
			*target = value.(time.Time)
		}
	}
	return nil
}

func TestFindByAddress(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	db := &fakeQueryRower{row: fakeRow{values: []any{int64(7), "0xabc", "alice", now, now}}}
	repository := NewUserRepository(db)

	user, err := repository.FindByAddress(context.Background(), "0xAbC")
	if err != nil {
		t.Fatalf("FindByAddress() error = %v", err)
	}
	if user.ID != 7 || user.Address != "0xabc" || user.Username != "alice" {
		t.Fatalf("user = %+v", user)
	}
	if !strings.Contains(db.query, "LOWER(address)") || db.args[0] != "0xAbC" {
		t.Fatalf("query or args were not recorded correctly: %q %#v", db.query, db.args)
	}
}

func TestCreateReturnsDatabaseError(t *testing.T) {
	db := &fakeQueryRower{row: fakeRow{err: errors.New("database unavailable")}}
	_, err := NewUserRepository(db).Create(context.Background(), "0xabc", "alice")
	if err == nil || err.Error() != "database unavailable" {
		t.Fatalf("Create() error = %v", err)
	}
}
