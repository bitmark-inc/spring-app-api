package main

import (
	"context"
	"strconv"
	"time"

	"github.com/RichardKnop/machinery/v1/tasks"
	"github.com/getsentry/sentry-go"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"

	fbArchive "github.com/bitmark-inc/spring-app-api/archives/facebook"
	"github.com/bitmark-inc/spring-app-api/protomodel"
	"github.com/bitmark-inc/spring-app-api/schema/facebook"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/bitmark-inc/spring-app-api/timeutil"
)

func (b *BackgroundContext) extractPost(ctx context.Context, accountNumber string, archiveID int64) error {
	jobError := NewArchiveJobError(archiveID, fbArchive.ErrFailToExtractPost)
	logEntity := log.WithField("prefix", "extract_post")
	counter := newPostStatisticCounter()

	saver := newStatSaver(b.fbDataStore)

	lastPostTimestamp := time.Now().Unix()

	// Fetch posts from db
	posts := make([]facebook.PostORM, 0)
	if err := b.ormDB.Preload("MediaItems").
		Preload("Places").
		Preload("Tags").
		Where(&facebook.PostORM{DataOwnerID: accountNumber}).
		Order("timestamp ASC").Find(&posts).Error; err != nil {
		return jobError(err)
	}

	// Save to dynamodb
	for _, p := range posts {
		postType := ""
		media := make([]*protomodel.MediaData, 0)

		if p.MediaAttached {
			postType = "media"
			for _, m := range p.MediaItems {
				mediaType := "photo"
				if m.FilenameExtension == ".mp4" {
					mediaType = "video"
				}
				thumbnailURI := m.ThumbnailURI
				if thumbnailURI == "" {
					thumbnailURI = m.MediaURI
				}
				media = append(media, &protomodel.MediaData{
					Type:      mediaType,
					Source:    m.MediaURI,
					Thumbnail: thumbnailURI,
				})
			}
		} else if p.ExternalContextURL != "" {
			postType = "link"
		} else if p.Post != "" {
			postType = "update"
		} else {
			continue
		}

		var l *protomodel.Location
		if len(p.Places) > 0 {
			firstPlace := p.Places[0]
			lat := firstPlace.Latitude
			long := firstPlace.Longitude
			l = &protomodel.Location{
				Address: firstPlace.Address,
				Coordinate: &protomodel.Coordinate{
					Latitude:  lat,
					Longitude: long,
				},
				Name:      firstPlace.Name,
				CreatedAt: p.Timestamp,
			}

			counter.lastLocation = l.GetCoordinate()
		}

		friends := make([]*protomodel.Tag, 0)
		for _, t := range p.Tags {
			friends = append(friends, &protomodel.Tag{
				Id:   t.FriendID.String(),
				Name: t.FriendName,
			})
		}

		// Add post
		post := &protomodel.Post{
			Timestamp: p.Timestamp,
			Type:      postType,
			Post:      p.Post,
			Id:        p.ID.String(),
			MediaData: media,
			Location:  l,
			Url:       p.ExternalContextURL,
			Title:     p.Title,
			Tags:      friends,
		}

		// Make sure there is not duplicated post saved
		if lastPostTimestamp != p.Timestamp {
			postData, err := proto.Marshal(post)
			if err != nil {
				sentry.CaptureException(err)
				continue
			}

			if err := saver.save(accountNumber+"/post", p.Timestamp, postData); err != nil {
				logEntity.Error(err)
				sentry.CaptureException(err)
				return jobError(err)
			}
			counter.countWeek(post)
			counter.countYear(post)
			counter.countDecade(post)
			counter.LastPostTimestamp = p.Timestamp
			lastPostTimestamp = p.Timestamp
		}
	}

	logEntity.Info("Flushing...")
	// Force to flush current data
	saver.flush()
	counter.flushWeekData()
	counter.flushYearData()
	counter.flushDecadeData()

	// Save stats
	for _, weekStat := range counter.Weeks {
		weekStatData, _ := proto.Marshal(weekStat)
		if err := b.fbDataStore.AddFBStat(ctx, accountNumber+"/post-week-stat", weekStat.PeriodStartedAt, weekStatData); err != nil {
			logEntity.Error(err)
			sentry.CaptureException(err)
			continue
		}
	}
	for _, yearStat := range counter.Years {
		yearStatData, _ := proto.Marshal(yearStat)
		if err := b.fbDataStore.AddFBStat(ctx, accountNumber+"/post-year-stat", yearStat.PeriodStartedAt, yearStatData); err != nil {
			logEntity.Error(err)
			sentry.CaptureException(err)
			continue
		}
	}
	for _, decadeStat := range counter.Decades {
		decadeStatData, _ := proto.Marshal(decadeStat)
		if err := b.fbDataStore.AddFBStat(ctx, accountNumber+"/post-decade-stat", decadeStat.PeriodStartedAt, decadeStatData); err != nil {
			logEntity.Error(err)
			sentry.CaptureException(err)
			continue
		}
	}

	// Calculate original location
	accountMetadata := map[string]interface{}{}
	if counter.lastLocation != nil {
		logEntity.Info("Parsing location")
		geoCodingData, err := b.geoServiceClient.ReverseGeocode(ctx,
			counter.lastLocation.Latitude,
			counter.lastLocation.Longitude)
		if err != nil {
			return jobError(err)
		}

		accountMetadata["original_location"] = geoCodingData.Address.CountryCode
	}

	logEntity.Info("Update to db with account number: ", accountNumber)
	// Get user and update
	if _, err := b.store.UpdateAccountMetadata(ctx, &store.AccountQueryParam{
		AccountNumber: &accountNumber,
	}, accountMetadata); err != nil {
		return jobError(err)
	}

	logEntity.Info("Enqueue parsing reaction")
	_, err := server.SendTask(&tasks.Signature{
		Name: jobAnalyzeReactions,
		Args: []tasks.Arg{
			{
				Type:  "string",
				Value: accountNumber,
			},
			{
				Type:  "int64",
				Value: archiveID,
			},
		},
	})
	if err != nil {
		return jobError(err)
	}

	logEntity.Info("Finish...")

	return nil
}

type postStatisticCounter struct {
	Weeks   []*protomodel.Usage
	Years   []*protomodel.Usage
	Decades []*protomodel.Usage

	WeekTypePeriodsMap   map[string]map[string]int64
	YearTypePeriodsMap   map[string]map[string]int64
	DecadeTypePeriodsMap map[string]map[string]int64

	WeekFriendPeriodsMap   map[string]map[string]int64
	YearFriendPeriodsMap   map[string]map[string]int64
	DecadeFriendPeriodsMap map[string]map[string]int64

	WeekPlacePeriodsMap   map[string]map[string]int64
	YearPlacePeriodsMap   map[string]map[string]int64
	DecadePlacePeriodsMap map[string]map[string]int64

	// LastPostTimestamp a flag to check duplicated item
	LastPostTimestamp int64

	// to cache the current week, year or decade
	currentWeek   int64
	currentYear   int64
	currentDecade int64

	// to cache the last period timestamp
	lastWeek   int64
	lastYear   int64
	lastDecade int64

	// to count the type overral of a week, year or decade
	currentWeekTypeMap   map[string]int64
	currentYearTypeMap   map[string]int64
	currentDecadeTypeMap map[string]int64

	// to cache the total post of last period
	lastTotalPostOfWeek   int64
	lastTotalPostOfYear   int64
	lastTotalPostOfDecade int64

	lastLocation *protomodel.Coordinate
}

func plusOneValue(m *map[string]int64, key string) {
	if v, ok := (*m)[key]; ok {
		(*m)[key] = v + 1
	} else {
		(*m)[key] = 1
	}
}

func addOneToArray(m *map[string]int64, key string) {
	if v, ok := (*m)[key]; ok {
		(*m)[key] = v + 1
	} else {
		(*m)[key] = 1
	}
}

func getMap(m map[string]map[string]int64, key string) *map[string]int64 {
	if v, ok := m[key]; ok {
		return &v
	}

	v := make(map[string]int64)
	m[key] = v
	return &v
}

func newPostStatisticCounter() *postStatisticCounter {
	return &postStatisticCounter{
		WeekTypePeriodsMap:   make(map[string]map[string]int64),
		YearTypePeriodsMap:   make(map[string]map[string]int64),
		DecadeTypePeriodsMap: make(map[string]map[string]int64),

		WeekFriendPeriodsMap:   make(map[string]map[string]int64),
		YearFriendPeriodsMap:   make(map[string]map[string]int64),
		DecadeFriendPeriodsMap: make(map[string]map[string]int64),

		WeekPlacePeriodsMap:   make(map[string]map[string]int64),
		YearPlacePeriodsMap:   make(map[string]map[string]int64),
		DecadePlacePeriodsMap: make(map[string]map[string]int64),

		currentWeekTypeMap:   make(map[string]int64),
		currentYearTypeMap:   make(map[string]int64),
		currentDecadeTypeMap: make(map[string]int64),

		Weeks:   make([]*protomodel.Usage, 0),
		Years:   make([]*protomodel.Usage, 0),
		Decades: make([]*protomodel.Usage, 0),

		lastLocation:      nil,
		LastPostTimestamp: time.Now().Unix(),
	}
}

func (sc *postStatisticCounter) flushWeekData() {
	// Sub periods
	subPeriods := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.WeekTypePeriodsMap {
		subPeriods = append(subPeriods, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Friends
	friends := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.WeekFriendPeriodsMap {
		friends = append(friends, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Places
	places := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.WeekPlacePeriodsMap {
		places = append(places, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Calculate the current total
	var currentTotal int64 = 0
	for _, count := range sc.currentWeekTypeMap {
		currentTotal += count
	}

	// Calculate the difference if last week has data
	difference := 1.0
	if sc.lastWeek == timeutil.AbsWeek(sc.currentWeek-1) {
		difference = timeutil.GetDiff(float64(currentTotal), float64(sc.lastTotalPostOfWeek))
	}
	sc.lastTotalPostOfWeek = currentTotal

	weekStatisticData := &protomodel.Usage{
		SectionName:      "post",
		Period:           "week",
		Quantity:         currentTotal,
		PeriodStartedAt:  sc.currentWeek,
		DiffFromPrevious: difference,
		Groups: &protomodel.Group{
			Type: &protomodel.PeriodData{
				Data: sc.currentWeekTypeMap,
			},
			SubPeriod: subPeriods,
			Friend:    friends,
			Place:     places,
		},
	}

	sc.Weeks = append(sc.Weeks, weekStatisticData)

	// Clean obsolete data
	sc.WeekTypePeriodsMap = make(map[string]map[string]int64)
	sc.WeekFriendPeriodsMap = make(map[string]map[string]int64)
	sc.WeekPlacePeriodsMap = make(map[string]map[string]int64)
	sc.currentWeekTypeMap = make(map[string]int64)
}

func (sc *postStatisticCounter) countWeek(r *protomodel.Post) {
	// Skip duplicated items
	if sc.LastPostTimestamp == r.Timestamp {
		return
	}

	week := timeutil.AbsWeek(r.Timestamp)
	if sc.currentWeek == 0 {
		sc.lastWeek = 0
		sc.currentWeek = week
	}

	if week != sc.currentWeek {
		// Flush data
		sc.flushWeekData()

		// Set current week
		sc.lastWeek = sc.currentWeek
		sc.currentWeek = week
	}

	// parse sub periods of days, friends, places in a week
	plusOneValue(&sc.currentWeekTypeMap, r.Type)
	weekTypeMap := getMap(sc.WeekTypePeriodsMap, strconv.FormatInt(timeutil.AbsDay(r.Timestamp), 10))
	plusOneValue(weekTypeMap, r.Type)

	if r.Location != nil {
		weekPlaceMap := getMap(sc.WeekPlacePeriodsMap, r.Location.Name)
		plusOneValue(weekPlaceMap, r.Type)
	}

	for _, f := range r.Tags {
		weekFriendMap := getMap(sc.WeekFriendPeriodsMap, f.Name)
		plusOneValue(weekFriendMap, r.Type)
	}
}

func (sc *postStatisticCounter) flushYearData() {
	// Sub periods
	subPeriods := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.YearTypePeriodsMap {
		subPeriods = append(subPeriods, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Friends
	friends := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.YearFriendPeriodsMap {
		friends = append(friends, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Places
	places := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.YearPlacePeriodsMap {
		places = append(places, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Count the difference with last period
	var currentTotal int64 = 0
	for _, count := range sc.currentYearTypeMap {
		currentTotal += count
	}

	// Calculate the difference if last week has data
	difference := 1.0
	if sc.lastYear == timeutil.AbsYear(sc.currentYear-1) {
		difference = timeutil.GetDiff(float64(currentTotal), float64(sc.lastTotalPostOfYear))
	}
	sc.lastTotalPostOfYear = currentTotal

	yearStatisticData := &protomodel.Usage{
		SectionName:      "post",
		Period:           "year",
		Quantity:         currentTotal,
		PeriodStartedAt:  sc.currentYear,
		DiffFromPrevious: difference,
		Groups: &protomodel.Group{
			Type: &protomodel.PeriodData{
				Data: sc.currentYearTypeMap,
			},
			SubPeriod: subPeriods,
			Friend:    friends,
			Place:     places,
		},
	}

	sc.Years = append(sc.Years, yearStatisticData)

	// Clean obsolete data
	sc.YearTypePeriodsMap = make(map[string]map[string]int64)
	sc.YearFriendPeriodsMap = make(map[string]map[string]int64)
	sc.YearPlacePeriodsMap = make(map[string]map[string]int64)
	sc.currentYearTypeMap = make(map[string]int64)
}

func (sc *postStatisticCounter) countYear(r *protomodel.Post) {
	// Skip duplicated items
	if sc.LastPostTimestamp == r.Timestamp {
		return
	}

	year := timeutil.AbsYear(r.Timestamp)
	if sc.currentYear == 0 {
		sc.lastYear = 0
		sc.currentYear = year
	}

	if year != sc.currentYear {
		// Flush data
		sc.flushYearData()

		// Set current year
		sc.lastYear = sc.currentYear
		sc.currentYear = year
	}

	// parse sub periods of days, friends, places in a year
	plusOneValue(&sc.currentYearTypeMap, r.Type)
	yearTypeMap := getMap(sc.YearTypePeriodsMap, strconv.FormatInt(timeutil.AbsMonth(r.Timestamp), 10))
	plusOneValue(yearTypeMap, r.Type)

	if r.Location != nil {
		yearPlaceMap := getMap(sc.YearPlacePeriodsMap, r.Location.Name)
		plusOneValue(yearPlaceMap, r.Type)
	}

	for _, f := range r.Tags {
		yearFriendMap := getMap(sc.YearFriendPeriodsMap, f.Name)
		plusOneValue(yearFriendMap, r.Type)
	}
}

func (sc *postStatisticCounter) flushDecadeData() {
	// Sub periods
	subPeriods := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.DecadeTypePeriodsMap {
		subPeriods = append(subPeriods, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Friends
	friends := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.DecadeFriendPeriodsMap {
		friends = append(friends, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Places
	places := make([]*protomodel.PeriodData, 0)
	for name, dayData := range sc.DecadePlacePeriodsMap {
		places = append(places, &protomodel.PeriodData{
			Name: name,
			Data: dayData,
		})
	}

	// Count the difference with last period
	var currentTotal int64 = 0
	for _, count := range sc.currentDecadeTypeMap {
		currentTotal += count
	}

	// Calculate the difference if last week has data
	difference := 1.0
	if sc.lastDecade == timeutil.AbsDecade(sc.currentDecade-1) {
		difference = timeutil.GetDiff(float64(currentTotal), float64(sc.lastTotalPostOfDecade))
	}
	sc.lastTotalPostOfDecade = currentTotal

	decadeStatisticData := &protomodel.Usage{
		SectionName:      "post",
		Period:           "decade",
		Quantity:         currentTotal,
		PeriodStartedAt:  sc.currentDecade,
		DiffFromPrevious: difference,
		Groups: &protomodel.Group{
			Type: &protomodel.PeriodData{
				Data: sc.currentDecadeTypeMap,
			},
			SubPeriod: subPeriods,
			Friend:    friends,
			Place:     places,
		},
	}

	sc.Decades = append(sc.Decades, decadeStatisticData)

	// Clean obsolete data
	sc.DecadeTypePeriodsMap = make(map[string]map[string]int64)
	sc.DecadeFriendPeriodsMap = make(map[string]map[string]int64)
	sc.DecadePlacePeriodsMap = make(map[string]map[string]int64)
	sc.currentDecadeTypeMap = make(map[string]int64)
}

func (sc *postStatisticCounter) countDecade(r *protomodel.Post) {
	// Skip duplicated items
	if sc.LastPostTimestamp == r.Timestamp {
		return
	}

	decade := timeutil.AbsDecade(r.Timestamp)
	if sc.currentDecade == 0 {
		sc.lastDecade = 0
		sc.currentDecade = decade
	}

	if decade != sc.currentDecade {
		// Flush data
		sc.flushDecadeData()

		// Set current decade
		sc.lastDecade = sc.currentDecade
		sc.currentDecade = decade
	}

	// parse sub periods of days, friends, places in a decade
	plusOneValue(&sc.currentDecadeTypeMap, r.Type)
	decadeTypeMap := getMap(sc.DecadeTypePeriodsMap, strconv.FormatInt(timeutil.AbsYear(r.Timestamp), 10))
	plusOneValue(decadeTypeMap, r.Type)

	if r.Location != nil {
		decadePlaceMap := getMap(sc.DecadePlacePeriodsMap, r.Location.Name)
		plusOneValue(decadePlaceMap, r.Type)
	}

	for _, f := range r.Tags {
		decadeFriendMap := getMap(sc.DecadeFriendPeriodsMap, f.Name)
		plusOneValue(decadeFriendMap, r.Type)
	}
}
