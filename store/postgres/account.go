package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v4"

	sq "github.com/Masterminds/squirrel"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/google/uuid"
)

func (p *PGStore) InsertAccount(ctx context.Context, accountNumber string, encPubKey []byte, metadata map[string]interface{}) (*store.Account, error) {
	var account store.Account

	values := map[string]interface{}{
		"account_number": accountNumber,
		"enc_pub_key":    encPubKey,
	}
	if metadata != nil {
		values["metadata"] = metadata
	}

	q := psql.
		Insert("fbm.account").
		SetMap(values).
		Suffix("RETURNING *")

	st, val, _ := q.ToSql()

	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&account.AccountNumber,
			&account.EncryptionPublicKey,
			&account.Metadata,
			&account.CreatedAt,
			&account.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return &account, nil
}

func (p *PGStore) QueryAccount(ctx context.Context, params *store.AccountQueryParam) (*store.Account, error) {
	q := psql.Select("*").From("fbm.account")

	if params.AccountNumber != nil {
		q = q.Where(sq.Eq{"account_number": *params.AccountNumber})
	}

	st, val, _ := q.ToSql()
	var account store.Account

	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&account.AccountNumber,
			&account.EncryptionPublicKey,
			&account.Metadata,
			&account.CreatedAt,
			&account.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return &account, nil
}

func (p *PGStore) UpdateAccountMetadata(ctx context.Context, params *store.AccountQueryParam, metadata map[string]interface{}) (*store.Account, error) {
	q1 := psql.Select("*").From("fbm.account")

	if params.AccountNumber != nil {
		q1 = q1.Where(sq.Eq{"account_number": *params.AccountNumber})
	}

	st, val, _ := q1.ToSql()
	var account store.Account

	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&account.AccountNumber,
			&account.EncryptionPublicKey,
			&account.Metadata,
			&account.CreatedAt,
			&account.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	for k, v := range metadata {
		account.Metadata[k] = v
	}

	q2 := psql.Update("fbm.account").Set("metadata", account.Metadata)
	if params.AccountNumber != nil {
		q2 = q2.Where(sq.Eq{"account_number": *params.AccountNumber})
	}

	st, val, _ = q2.ToSql()
	t, err := p.pool.Exec(ctx, st, val...)
	if err != nil {
		return nil, err
	}

	if t.RowsAffected() == 1 {
		return &account, nil
	}

	return nil, nil
}

func (p *PGStore) AddToken(ctx context.Context, accountNumber string, info map[string]interface{}, expire time.Duration) (*store.Token, error) {
	tokenString := uuid.New().String()

	q := psql.
		Insert("fbm.token").
		Columns("id", "account_number", "info", "expired_at").
		Values(tokenString, accountNumber, info, time.Now().Add(expire)).
		Suffix("RETURNING *")

	st, val, _ := q.ToSql()

	var token store.Token
	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&token.Token,
			&token.AccountNumber,
			&token.Info,
			&token.CreatedAt,
			&token.ExpireAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return &token, nil
}

func (p *PGStore) UseToken(ctx context.Context, token string) (*store.Account, map[string]interface{}, error) {
	var accountNumber string
	var info map[string]interface{}

	q := psql.
		Delete("fbm.token").
		Where(sq.Eq{"id": token}).
		Where(sq.GtOrEq{"expired_at": time.Now()}).
		Suffix("RETURNING account_number, info")

	st, val, _ := q.ToSql()

	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&accountNumber,
			&info); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, nil
		}

		return nil, nil, err
	}

	account, err := p.QueryAccount(ctx, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	})
	if err != nil {
		return nil, nil, err
	}

	return account, info, nil
}