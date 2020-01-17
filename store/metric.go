package store

import (
	"context"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v4"
)

func (p *PGStore) CountAccountCreation(ctx context.Context, from, to time.Time) (map[string]int, error) {
	q := psql.Select("count(*)").
		From("fbm.account")

	if !from.IsZero() {
		q = q.Where(sq.GtOrEq{"created_at": from})
	}

	if !to.IsZero() {
		q = q.Where(sq.LtOrEq{"created_at": to})
	}

	queryfunc := func(platform string) (int, error) {
		finalQuery := q.Where(sq.Eq{"metadata->>'platform'": platform})
		st, val, _ := finalQuery.ToSql()
		var count int
		if err := p.pool.
			QueryRow(ctx, st, val...).
			Scan(&count); err != nil {
			if err == pgx.ErrNoRows {
				return 0, nil
			}

			return 0, err
		}

		return count, nil
	}

	result := make(map[string]int)

	// Query for iOS
	countIOS, err := queryfunc("ios")
	if err != nil {
		return nil, err
	}
	result["ios"] = countIOS

	// Query for Android
	countAndroid, err := queryfunc("android")
	if err != nil {
		return nil, err
	}
	result["android"] = countAndroid

	return result, nil
}
