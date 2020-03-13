package postgres

import (
	"context"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v4"

	"github.com/bitmark-inc/spring-app-api/store"
)

func (p *PGStore) InsertAccount(ctx context.Context, accountNumber string, encPubKey []byte, metadata map[string]interface{}) (*store.Account, error) {
	var account store.Account

	if encPubKey == nil {
		encPubKey = []byte{}
	}

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
			&account.UpdatedAt,
			&account.Deleting); err != nil {
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
			&account.UpdatedAt,
			&account.Deleting); err != nil {
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
			&account.UpdatedAt,
			&account.Deleting); err != nil {
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

func (p *PGStore) DeleteAccount(ctx context.Context, accountNumber string) error {
	q := psql.
		Delete("fbm.account").
		Where(sq.Eq{"account_number": accountNumber})

	st, val, _ := q.ToSql()

	_, err := p.pool.
		Exec(ctx, st, val...)

	return err
}
