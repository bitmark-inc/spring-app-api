package main

import (
	"context"
	"strconv"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/bitmark-inc/spring-app-api/protomodel"
	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/bitmark-inc/spring-app-api/timeutil"
	"github.com/getsentry/sentry-go"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
)

func (b *BackgroundContext) extractReaction(ctx context.Context, accountNumber string, archiveid int64) error {
	logEntry := log.WithField("prefix", "extract_reaction")

	saver := newStatSaver(b.fbDataStore)
	counter := newReactionStatCounter(ctx, logEntry, saver, accountNumber)

	var lastTimestamp int64

	// Save to db & count
	reactions := make([]facebook.ReactionORM, 0)
	b.ormDB.Where(&facebook.ReactionORM{DataOwnerID: accountNumber}).
		Order("timestamp ASC").Find(&reactions)

	for _, reaction := range reactions {
		if lastTimestamp == reaction.Timestamp {
			continue
		}
		lastTimestamp = reaction.Timestamp

		reactionData, _ := proto.Marshal(&protomodel.Reaction{
			ReactionId: reaction.ID.String(),
			Timestamp:  reaction.Timestamp,
			Title:      reaction.Title,
			Actor:      reaction.Actor,
			Reaction:   reaction.Reaction,
		})
		if err := saver.save(accountNumber+"/reaction", reaction.Timestamp, reactionData); err != nil {
			logEntry.Error(err)
			sentry.CaptureException(err)
			return err
		}

		if err := counter.count(reaction); err != nil {
			logEntry.Error(err)
			sentry.CaptureException(err)
			return err
		}
	}

	if err := counter.flush(); err != nil {
		logEntry.Error(err)
		sentry.CaptureException(err)
		return err
	}
	if err := saver.flush(); err != nil {
		logEntry.Error(err)
		sentry.CaptureException(err)
		return err
	}

	logEntry.Info("Enqueue parsing time meta")
	server.SendTask(&tasks.Signature{
		Name: jobExtractTimeMetadata,
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: accountNumber,
			},
		},
	})

	logEntry.Info("Enqueue push notification")
	server.SendTask(&tasks.Signature{
		Name: jobNotificationFinish,
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: accountNumber,
			},
		},
	})

	// Mark the archive is processed
	if _, err := b.store.UpdateFBArchiveStatus(ctx, &store.FBArchiveQueryParam{
		ID: &archiveid,
	}, &store.FBArchiveQueryParam{
		Status: &store.FBArchiveStatusProcessed,
	}); err != nil {
		logEntry.Error(err)
		return err
	}

	logEntry.Info("Finish parsing reactions")

	return nil
}

type reactionStat struct {
	Reaction *protomodel.Usage
	IsSaved  bool
}

type reactionStatCounter struct {
	lastWeekStat      *reactionStat
	currentWeekStat   *reactionStat
	lastYearStat      *reactionStat
	currentYearStat   *reactionStat
	lastDecadeStat    *reactionStat
	currentDecadeStat *reactionStat
	accountNumber     string
	ctx               context.Context
	saver             *statSaver
	log               *log.Entry
}

func newReactionStatCounter(ctx context.Context, log *log.Entry, saver *statSaver, accountNumber string) *reactionStatCounter {
	return &reactionStatCounter{
		ctx:           ctx,
		saver:         saver,
		log:           log,
		accountNumber: accountNumber,
	}
}

func (r *reactionStatCounter) createEmptyStat(period string, timestamp int64) *reactionStat {
	return &reactionStat{
		Reaction: &protomodel.Usage{
			SectionName:     "reaction",
			Period:          period,
			PeriodStartedAt: timeutil.AbsPeriod(period, timestamp),
			Groups: &protomodel.Group{
				Type: &protomodel.PeriodData{
					Data: make(map[string]int64),
				},
				SubPeriod: make([]*protomodel.PeriodData, 0),
			},
		},
		IsSaved: false,
	}
}

func (r *reactionStatCounter) flushStat(period string, currentStat *reactionStat, lastStat *reactionStat) error {
	if currentStat != nil && !currentStat.IsSaved {
		// Calculate the difference
		var lastQuantity int64
		if lastStat != nil {
			lastQuantity = lastStat.Reaction.Quantity
		}
		currentStat.Reaction.DiffFromPrevious = timeutil.GetDiff(float64(currentStat.Reaction.Quantity), float64(lastQuantity))

		statData, _ := proto.Marshal(currentStat.Reaction)

		// Save data
		if err := r.saver.save(r.accountNumber+"/reaction-"+period+"-stat", currentStat.Reaction.PeriodStartedAt, statData); err != nil {
			return err
		}
		currentStat.IsSaved = true
	}
	return nil
}

func (r *reactionStatCounter) flush() error {
	if err := r.flushStat("week", r.currentWeekStat, r.lastWeekStat); err != nil {
		return err
	}
	if err := r.flushStat("year", r.currentYearStat, r.lastYearStat); err != nil {
		return err
	}
	if err := r.flushStat("decade", r.currentDecadeStat, r.lastDecadeStat); err != nil {
		return err
	}
	return nil
}

func (r *reactionStatCounter) count(reaction facebook.ReactionORM) error {
	if err := r.countWeek(reaction); err != nil {
		return err
	}
	if err := r.countYear(reaction); err != nil {
		return err
	}
	if err := r.countDecade(reaction); err != nil {
		return err
	}
	return nil
}

func (r *reactionStatCounter) countWeek(reaction facebook.ReactionORM) error {
	periodTimestamp := timeutil.AbsWeek(reaction.Timestamp)

	// Release the current period if next period has come
	if r.currentWeekStat != nil && r.currentWeekStat.Reaction.PeriodStartedAt != periodTimestamp {
		if err := r.flushStat("week", r.currentWeekStat, r.lastWeekStat); err != nil {
			return err
		}
		r.lastWeekStat = r.currentWeekStat
		r.currentWeekStat = nil
	}

	// no data for current period yet, let's create one
	if r.currentWeekStat == nil {
		r.currentWeekStat = r.createEmptyStat("week", periodTimestamp)
	}

	r.currentWeekStat.Reaction.Quantity++
	plusOneValue(&r.currentWeekStat.Reaction.Groups.Type.Data, reaction.Reaction)

	subPeriod := r.currentWeekStat.Reaction.Groups.SubPeriod
	subPeriodTimestamp := timeutil.AbsDay(reaction.Timestamp)
	needNewSubPeriod := len(subPeriod) == 0 || subPeriod[len(subPeriod)-1].Name != strconv.FormatInt(subPeriodTimestamp, 10)

	if needNewSubPeriod {
		subPeriod = append(subPeriod, &protomodel.PeriodData{
			Name: strconv.FormatInt(subPeriodTimestamp, 10),
			Data: make(map[string]int64),
		})
	}
	plusOneValue(&subPeriod[len(subPeriod)-1].Data, reaction.Reaction)
	r.currentWeekStat.Reaction.Groups.SubPeriod = subPeriod

	return nil
}

func (r *reactionStatCounter) countYear(reaction facebook.ReactionORM) error {
	periodTimestamp := timeutil.AbsYear(reaction.Timestamp)

	// Release the current period if next period has come
	if r.currentYearStat != nil && r.currentYearStat.Reaction.PeriodStartedAt != periodTimestamp {
		if err := r.flushStat("year", r.currentYearStat, r.lastYearStat); err != nil {
			return err
		}
		r.lastYearStat = r.currentYearStat
		r.currentYearStat = nil
	}

	// no data for current period yet, let's create one
	if r.currentYearStat == nil {
		r.currentYearStat = r.createEmptyStat("year", periodTimestamp)
	}

	r.currentYearStat.Reaction.Quantity++
	plusOneValue(&r.currentYearStat.Reaction.Groups.Type.Data, reaction.Reaction)

	subPeriod := r.currentYearStat.Reaction.Groups.SubPeriod
	subPeriodTimestamp := timeutil.AbsMonth(reaction.Timestamp)
	needNewSubPeriod := len(subPeriod) == 0 || subPeriod[len(subPeriod)-1].Name != strconv.FormatInt(subPeriodTimestamp, 10)

	if needNewSubPeriod {
		subPeriod = append(subPeriod, &protomodel.PeriodData{
			Name: strconv.FormatInt(subPeriodTimestamp, 10),
			Data: make(map[string]int64),
		})
	}
	plusOneValue(&subPeriod[len(subPeriod)-1].Data, reaction.Reaction)
	r.currentYearStat.Reaction.Groups.SubPeriod = subPeriod

	return nil
}

func (r *reactionStatCounter) countDecade(reaction facebook.ReactionORM) error {
	periodTimestamp := timeutil.AbsDecade(reaction.Timestamp)

	// Release the current period if next period has come
	if r.currentDecadeStat != nil && r.currentDecadeStat.Reaction.PeriodStartedAt != periodTimestamp {
		log.Debug("Current period started at: ", periodTimestamp)
		if err := r.flushStat("decade", r.currentDecadeStat, r.lastDecadeStat); err != nil {
			return err
		}
		r.lastDecadeStat = r.currentDecadeStat
		r.currentDecadeStat = nil
	}

	// no data for current period yet, let's create one
	if r.currentDecadeStat == nil {
		r.currentDecadeStat = r.createEmptyStat("decade", periodTimestamp)
	}

	r.currentDecadeStat.Reaction.Quantity++
	plusOneValue(&r.currentDecadeStat.Reaction.Groups.Type.Data, reaction.Reaction)

	subPeriod := r.currentDecadeStat.Reaction.Groups.SubPeriod
	subPeriodTimestamp := timeutil.AbsYear(reaction.Timestamp)
	needNewSubPeriod := len(subPeriod) == 0 || subPeriod[len(subPeriod)-1].Name != strconv.FormatInt(subPeriodTimestamp, 10)

	if needNewSubPeriod {
		subPeriod = append(subPeriod, &protomodel.PeriodData{
			Name: strconv.FormatInt(subPeriodTimestamp, 10),
			Data: make(map[string]int64),
		})
	}
	plusOneValue(&subPeriod[len(subPeriod)-1].Data, reaction.Reaction)
	r.currentDecadeStat.Reaction.Groups.SubPeriod = subPeriod

	return nil
}
