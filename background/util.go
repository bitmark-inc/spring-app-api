package main

import (
	"context"
	"github.com/bitmark-inc/spring-app-api/store"
)

type statSaver struct {
	store store.FBDataStore
	queue []store.FbData
}

func newStatSaver(fbstore store.FBDataStore) *statSaver {
	return &statSaver{
		store: fbstore,
		queue: make([]store.FbData, 0),
	}
}

func (s *statSaver) save(key string, timestamp int64, data []byte) error {
	s.queue = append(s.queue, store.FbData{
		Key:       key,
		Timestamp: timestamp,
		Data:      data,
	})

	if len(s.queue) < 25 {
		return nil
	}

	if err := s.flush(); err != nil {
		return err
	}

	s.queue = make([]store.FbData, 0)
	return nil
}

func (s *statSaver) flush() error {
	ctx := context.Background()
	if len(s.queue) > 0 {
		err := s.store.AddFBStats(ctx, s.queue)
		return err
	}
	return nil
}
