package main

import (
	"context"
	"errors"
	"math"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/bitmark-inc/spring-app-api/protomodel"
	"github.com/bitmark-inc/spring-app-api/timeutil"
	"github.com/getsentry/sentry-go"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
)

func (b *BackgroundContext) extractSentiment(ctx context.Context, accountNumber string, archiveid int64) (err error) {
	logEntry := log.WithField("prefix", "extract_sentiment")

	defer func() error {
		if err == nil {
			logEntry.Info("Finish parsing sentiments")

			server.SendTask(&tasks.Signature{
				Name: jobAnalyzeReactions,
				Args: []tasks.Arg{
					{
						Type:  "string",
						Value: accountNumber,
					},
					{
						Type:  "int64",
						Value: archiveid,
					},
				},
			})
		}
		return err
	}()

	saver := newStatSaver(b.fbDataStore)
	counter := newSentimentStatCounter(ctx, logEntry, saver, accountNumber)

	// Get first post to get the starting timestamp
	firstPost, err := b.bitSocialClient.GetFirstPost(ctx, accountNumber)
	if err != nil {
		logEntry.Error(err)
		sentry.CaptureException(errors.New("Request first post failed for onwer " + accountNumber))
		return err
	}

	// This user has no post at all, no sentiment to calculate
	if firstPost == nil {
		return nil
	}

	// Get last post to get the ending timestamp
	lastPost, err := b.bitSocialClient.GetLastPost(ctx, accountNumber)
	if err != nil {
		logEntry.Error(err)
		sentry.CaptureException(errors.New("Request last post failed for onwer " + accountNumber))
		return err
	}

	// last post can not be nil
	if lastPost == nil {
		err := errors.New("Last post can not be nil")
		logEntry.Error(err)
		sentry.CaptureException(err)
		return err
	}

	timestampOffset := timeutil.AbsWeek(firstPost.Timestamp)
	nextWeek := timeutil.AbsWeek(lastPost.Timestamp) + 7*24*60*60
	toEndOfWeek := int64(7*24*60*60 - 1)

	for {
		data, err := b.bitSocialClient.GetLast7DaysOfSentiment(ctx, accountNumber, timestampOffset+toEndOfWeek)
		if err != nil {
			return err
		}
		logEntry.Debug(data)

		if err := counter.count(timestampOffset, data.Score); err != nil {
			logEntry.Error(err)
			sentry.CaptureException(err)
			return err
		}

		timestampOffset += 7 * 24 * 60 * 60 // means next week
		if timestampOffset >= nextWeek {
			break
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

	return nil
}

type sentimentStat struct {
	Usage           *protomodel.Usage
	SubPeriodValues []float64
	IsSaved         bool
}

type sentimentStatCounter struct {
	lastWeekStat      *sentimentStat
	currentWeekStat   *sentimentStat
	lastYearStat      *sentimentStat
	currentYearStat   *sentimentStat
	lastDecadeStat    *sentimentStat
	currentDecadeStat *sentimentStat
	ctx               context.Context
	saver             *statSaver
	log               *log.Entry
	accountNumber     string
}

func newSentimentStatCounter(ctx context.Context, log *log.Entry, saver *statSaver, accountNumber string) *sentimentStatCounter {
	return &sentimentStatCounter{
		ctx:           ctx,
		saver:         saver,
		log:           log,
		accountNumber: accountNumber,
	}
}

func (s *sentimentStatCounter) count(timestamp int64, sentimentValue float64) error {
	if err := s.countWeek(timestamp, sentimentValue); err != nil {
		return err
	}
	if err := s.countYear(timestamp, sentimentValue); err != nil {
		return err
	}
	if err := s.countDecade(timestamp, sentimentValue); err != nil {
		return err
	}
	return nil
}

func (s *sentimentStatCounter) flush() error {
	if err := s.flushStat("week", s.currentWeekStat, s.lastWeekStat); err != nil {
		return err
	}
	if err := s.flushStat("year", s.currentYearStat, s.lastYearStat); err != nil {
		return err
	}
	if err := s.flushStat("decade", s.currentDecadeStat, s.lastDecadeStat); err != nil {
		return err
	}
	return nil
}

func (s *sentimentStatCounter) flushStat(period string, currentStat *sentimentStat, lastStat *sentimentStat) error {
	if currentStat != nil && !currentStat.IsSaved {
		currentStat.Usage.Value = s.averageSentiment(currentStat.SubPeriodValues)

		lastSentiment := 0.0
		if lastStat != nil {
			lastSentiment = lastStat.Usage.Value
		}
		currentStat.Usage.DiffFromPrevious = timeutil.GetDiff(currentStat.Usage.Value, lastSentiment)

		statData, _ := proto.Marshal(currentStat.Usage)
		if err := s.saver.save(s.accountNumber+"/sentiment-"+period+"-stat", currentStat.Usage.PeriodStartedAt, statData); err != nil {
			return err
		}
		currentStat.IsSaved = true
	}
	return nil
}

func (s *sentimentStatCounter) averageSentiment(sentiments []float64) float64 {
	totalSentiment := 0.0
	for _, v := range sentiments {
		totalSentiment += v
	}
	averageSentiment := totalSentiment / float64(len(sentiments))
	return math.Round(averageSentiment)
}

func (s *sentimentStatCounter) createEmptyStat(period string, timestamp int64) *sentimentStat {
	return &sentimentStat{
		Usage: &protomodel.Usage{
			SectionName:     "sentiment",
			Period:          period,
			PeriodStartedAt: timeutil.AbsPeriod(period, timestamp),
		},
		IsSaved:         false,
		SubPeriodValues: make([]float64, 0),
	}
}

func (s *sentimentStatCounter) countWeek(timestamp int64, sentimentValue float64) error {
	periodTimestamp := timeutil.AbsWeek(timestamp)

	// flush the current period to give space for next period
	if s.currentWeekStat != nil && s.currentWeekStat.Usage.PeriodStartedAt != periodTimestamp {
		if err := s.flushStat("week", s.currentWeekStat, s.lastWeekStat); err != nil {
			return err
		}
		s.lastWeekStat = s.currentWeekStat
		s.currentWeekStat = nil
	}

	// no current period, let's create a new one
	if s.currentWeekStat == nil {
		s.currentWeekStat = s.createEmptyStat("week", timestamp)
	}

	s.currentWeekStat.SubPeriodValues = append(s.currentWeekStat.SubPeriodValues, sentimentValue)
	return nil
}

func (s *sentimentStatCounter) countYear(timestamp int64, sentimentValue float64) error {
	periodTimestamp := timeutil.AbsYear(timestamp)

	// flush the current period to give space for next period
	if s.currentYearStat != nil && s.currentYearStat.Usage.PeriodStartedAt != periodTimestamp {
		if err := s.flushStat("year", s.currentYearStat, s.lastYearStat); err != nil {
			return err
		}
		s.lastYearStat = s.currentYearStat
		s.currentYearStat = nil
	}

	// no current period, let's create a new one
	if s.currentYearStat == nil {
		s.currentYearStat = s.createEmptyStat("year", timestamp)
	}

	s.currentYearStat.SubPeriodValues = append(s.currentYearStat.SubPeriodValues, sentimentValue)
	return nil
}

func (s *sentimentStatCounter) countDecade(timestamp int64, sentimentValue float64) error {
	periodTimestamp := timeutil.AbsDecade(timestamp)

	// New decade, let's save current decade before continuing to aggregate
	if s.currentDecadeStat != nil && s.currentDecadeStat.Usage.PeriodStartedAt != periodTimestamp {
		if err := s.flushStat("decade", s.currentDecadeStat, s.lastDecadeStat); err != nil {
			return err
		}
		s.lastDecadeStat = s.currentDecadeStat
		s.currentDecadeStat = nil
	}

	// The first time this function is call
	if s.currentDecadeStat == nil {
		s.currentDecadeStat = s.createEmptyStat("decade", timestamp)
	}

	s.currentDecadeStat.SubPeriodValues = append(s.currentDecadeStat.SubPeriodValues, sentimentValue)
	return nil
}
