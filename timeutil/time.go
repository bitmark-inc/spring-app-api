package timeutil

import (
	"time"
)

// AbsDay to find start time of the week of a given time
// timestamp is unix time in second
// days start from 12:00 AM
func AbsDay(timestamp int64) int64 {
	t := time.Unix(timestamp, 0).UTC()
	year, month, day := t.Date()
	absDay := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return absDay.Unix()
}

// AbsWeek to find start time of the week of a given time
// timestamp is unix time in second
// weekdays start from Sunday
func AbsWeek(timestamp int64) int64 {
	t := time.Unix(timestamp, 0).UTC()
	weekday := time.Duration(t.Weekday())
	year, month, day := t.Date()
	absDay := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	startWeekDay := absDay.Add(0 - weekday*time.Hour*24)
	return startWeekDay.Unix()
}

// AbsMonth to find start time of the month of a given time
// timestamp is unix time in second
// days start from 12:00 AM
func AbsMonth(timestamp int64) int64 {
	t := time.Unix(timestamp, 0).UTC()
	year, month, _ := t.Date()
	absDay := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	return absDay.Unix()
}

// AbsYear to find start time of the year of a given time
// timestamp is unix time in second
// years start from Jan 1st
func AbsYear(timestamp int64) int64 {
	t := time.Unix(timestamp, 0).UTC()
	year := t.Year()
	absDay := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	return absDay.Unix()
}

// AbsDecade to find start time of the decade of a given time
// timestamp is unix time in second
func AbsDecade(timestamp int64) int64 {
	t := time.Unix(timestamp, 0).UTC()
	year := t.Year()
	absYear := year % 10
	absDay := time.Date(year-absYear, 1, 1, 0, 0, 0, 0, time.UTC)
	return absDay.Unix()
}

// AbsPeriod find start time of the period in seconds
func AbsPeriod(period string, timestamp int64) int64 {
	switch period {
	case "week":
		return AbsWeek(timestamp)
	case "month":
		return AbsMonth(timestamp)
	case "year":
		return AbsYear(timestamp)
	case "decade":
		return AbsDecade(timestamp)
	default:
		return timestamp
	}
}

// TimestampToDateString to format timestamp in unix second
// format to app's string format
func TimestampToDateString(timestamp int64) string {
	t := time.Unix(timestamp, 0).UTC()
	return t.Format("2006-01-02")
}

//  GetDiff to get difference of timestamp in unix sencond format
func GetDiff(current, last float64) float64 {
	var difference float64
	if last != 0 {
		difference = (current - last) / last
	} else if current == 0 {
		difference = 0
	} else {
		difference = 1
	}
	return difference
}
