package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/jackc/pgx/v4"
)

// AddFBArchive to add an archive record from an account
func (p *PGStore) AddFBArchive(ctx context.Context, accountNumber string, starting, ending time.Time) (*store.FBArchive, error) {
	var fbArchive store.FBArchive

	q := psql.
		Insert("fbm.fbarchive").
		Columns("account_number", "file_key", "starting_time", "ending_time").
		Values(accountNumber, "", starting, ending).
		Suffix("RETURNING id, account_number, file_key, starting_time, ending_time, analyzed_task_id, content_hash, processing_status, created_at, updated_at")

	st, val, _ := q.ToSql()

	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&fbArchive.ID,
			&fbArchive.AccountNumber,
			&fbArchive.S3Key,
			&fbArchive.StartingTime,
			&fbArchive.EndingTime,
			&fbArchive.AnalyzedTaskID,
			&fbArchive.ContentHash,
			&fbArchive.ProcessingStatus,
			&fbArchive.CreatedAt,
			&fbArchive.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return &fbArchive, nil
}

// UpdateFBArchiveStatus to update status for a particular fb archive record with s3 key
func (p *PGStore) UpdateFBArchiveStatus(ctx context.Context, params *store.FBArchiveQueryParam, values *store.FBArchiveQueryParam) ([]store.FBArchive, error) {
	q := psql.Update("fbm.fbarchive").
		Set("updated_at", time.Now()).
		Suffix("RETURNING id, account_number, file_key, starting_time, ending_time, analyzed_task_id, content_hash, processing_status, created_at, updated_at")

	if params.ID != nil {
		q = q.Where(sq.Eq{"id": *params.ID})
	}

	if params.AccountNumber != nil {
		q = q.Where(sq.Eq{"account_number": *params.AccountNumber})
	}

	if params.S3Key != nil {
		q = q.Where(sq.Eq{"file_key": *params.S3Key})
	}

	if values.S3Key != nil {
		q = q.Set("file_key", *values.S3Key)
	}

	if values.Status != nil {
		q = q.Set("processing_status", *values.Status)
	}

	if values.AnalyzedID != nil {
		q = q.Set("analyzed_task_id", *values.AnalyzedID)
	}

	if values.ContentHash != nil {
		q = q.Set("content_hash", *values.ContentHash)
	}

	st, val, _ := q.ToSql()

	rows, err := p.pool.Query(ctx, st, val...)
	if err != nil {
		return nil, err
	}

	fbarchives := make([]store.FBArchive, 0)

	for rows.Next() {
		var fbArchive store.FBArchive

		if rows.Scan(&fbArchive.ID,
			&fbArchive.AccountNumber,
			&fbArchive.S3Key,
			&fbArchive.StartingTime,
			&fbArchive.EndingTime,
			&fbArchive.AnalyzedTaskID,
			&fbArchive.ContentHash,
			&fbArchive.ProcessingStatus,
			&fbArchive.CreatedAt,
			&fbArchive.UpdatedAt); err != nil {
			return nil, err
		}

		fbarchives = append(fbarchives, fbArchive)
	}

	return fbarchives, nil
}

func (p *PGStore) GetFBArchives(ctx context.Context, params *store.FBArchiveQueryParam) ([]store.FBArchive, error) {
	q := psql.Select(`id, account_number, file_key, starting_time, ending_time, analyzed_task_id,
					  content_hash, processing_status, processing_error, created_at, updated_at`).
		From("fbm.fbarchive")

	if params.ID != nil {
		q = q.Where(sq.Eq{"id": *params.ID})
	}

	if params.S3Key != nil {
		q = q.Where(sq.Eq{"file_key": *params.S3Key})
	}

	if params.AccountNumber != nil {
		q = q.Where(sq.Eq{"account_number": *params.AccountNumber})
	}

	if params.Status != nil {
		q = q.Where(sq.Eq{"processing_status": *params.Status})
	}

	st, val, _ := q.ToSql()

	rows, err := p.pool.Query(ctx, st, val...)
	if err != nil {
		return nil, err
	}

	fbarchives := make([]store.FBArchive, 0)

	for rows.Next() {
		var fbArchive store.FBArchive

		if rows.Scan(&fbArchive.ID,
			&fbArchive.AccountNumber,
			&fbArchive.S3Key,
			&fbArchive.StartingTime,
			&fbArchive.EndingTime,
			&fbArchive.AnalyzedTaskID,
			&fbArchive.ContentHash,
			&fbArchive.ProcessingStatus,
			&fbArchive.ProcessingError,
			&fbArchive.CreatedAt,
			&fbArchive.UpdatedAt); err != nil {
			return nil, err
		}

		fbarchives = append(fbarchives, fbArchive)
	}

	return fbarchives, nil
}

func (p *PGStore) InvalidFBArchive(ctx context.Context, params *store.FBArchiveQueryParam) error {

	if params.ID == nil {
		return fmt.Errorf("archive id is required to invalid a archive")
	}

	if params.Error == nil {
		return fmt.Errorf("message is required to invalid a archive")
	}

	b, err := json.Marshal(params.Error)
	if err != nil {
		return err
	}

	q := psql.Update("fbm.fbarchive").
		Set("updated_at", time.Now()).
		Set("processing_status", &store.FBArchiveStatusInvalid).
		Set("processing_error", b).
		Where(sq.Eq{"id": *params.ID})

	st, val, _ := q.ToSql()

	_, err = p.pool.Exec(ctx, st, val...)
	return err
}

func (p *PGStore) DeleteFBArchives(ctx context.Context, params *store.FBArchiveQueryParam) error {
	q := psql.Delete("fbm.fbarchive")

	if params.ID != nil {
		q = q.Where(sq.Eq{"id": *params.ID})
	}

	if params.S3Key != nil {
		q = q.Where(sq.Eq{"file_key": *params.S3Key})
	}

	if params.AccountNumber != nil {
		q = q.Where(sq.Eq{"account_number": *params.AccountNumber})
	}

	if params.Status != nil {
		q = q.Where(sq.Eq{"processing_status": *params.Status})
	}

	st, val, _ := q.ToSql()

	_, err := p.pool.Exec(ctx, st, val...)
	return err
}

// AddFBStat to add a FB stat
func (p *PGStore) AddFBStat(ctx context.Context, key string, value interface{}) error {
	q := psql.Insert("fbm.fbdata").Values(key, value)
	st, val, _ := q.ToSql()
	_, err := p.pool.Exec(ctx, st, val...)
	return err
}

// GetFBStat to get a FB stat
func (p *PGStore) GetFBStat(ctx context.Context, key string) (interface{}, error) {
	q := psql.Select("fbm.fbdata").Where(sq.Eq{"data_name": key})
	st, val, _ := q.ToSql()

	var data interface{}

	if err := p.pool.
		QueryRow(ctx, st, val...).
		Scan(&data); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	return data, nil
}
