package api

import (
	"net/http"
	"time"

	"github.com/bitmark-inc/spring-app-api/protomodel"
	"github.com/bitmark-inc/spring-app-api/store"
	"github.com/gin-gonic/gin"
)

type fbIncomeInfo struct {
	Income float64
	From   int64
	To     int64
}

func getTotalFBIncomeForDataPeriod(lookupRange []fbIncomePeriod, from, to int64) fbIncomeInfo {
	amount := 0.0

	if len(lookupRange) == 0 {
		return fbIncomeInfo{
			Income: 0.0,
			From:   0,
			To:     0,
		}
	}

	firstDayTimestamp := from
	if from < lookupRange[0].StartedAt {
		firstDayTimestamp = lookupRange[0].StartedAt
		from = firstDayTimestamp
	}

	quarterIndex := 0
	for {
		currentQuarter := lookupRange[quarterIndex]

		if from > currentQuarter.EndedAt { // our of current quarter, check next quarter
			quarterIndex++
		} else { // from is in current quarter
			amount += currentQuarter.QuarterAmount / 90
			from += 24 * 60 * 60 // next day
		}

		if from > to || quarterIndex >= len(lookupRange) {
			break
		}
	}

	return fbIncomeInfo{
		Income: amount,
		From:   firstDayTimestamp,
		To:     to,
	}
}

func (s *Server) getFBIncomeFromUserData(account *store.Account, from, to int64) fbIncomeInfo {
	if f, ok := account.Metadata["first_activity_timestamp"].(float64); ok {
		if int64(f) > from {
			from = int64(f)
		}
	}

	if t, ok := account.Metadata["last_activity_timestamp"].(float64); ok {
		if int64(t) < to {
			to = int64(t)
		}
	}

	if from > to {
		return fbIncomeInfo{
			Income: -1,
			From:   0,
			To:     0,
		}
	}

	countryCode := ""
	if c, ok := account.Metadata["original_location"].(string); ok {
		countryCode = c
	}

	// Logic: if there is no country code, it's world-wide area
	// if it's us/canada, then area is us-canada
	// if it's europe or asia, then area is either
	// fallback to rest if can not look it up
	var lookupRange []fbIncomePeriod

	if countryCode == "" {
		lookupRange = s.areaFBIncomeMap.WorldWide
	} else if countryCode == "us" || countryCode == "ca" {
		lookupRange = s.areaFBIncomeMap.USCanada
	} else {
		if continent, ok := s.countryContinentMap[countryCode]; ok {
			if continent == "Europe" {
				lookupRange = s.areaFBIncomeMap.Europe
			} else if continent == "Asia" {
				lookupRange = s.areaFBIncomeMap.AsiaPacific
			} else {
				lookupRange = s.areaFBIncomeMap.Rest
			}
		} else {
			lookupRange = s.areaFBIncomeMap.Rest
		}
	}

	return getTotalFBIncomeForDataPeriod(lookupRange, from, to)
}

func (s *Server) getInsight(c *gin.Context) {
	account := c.MustGet("account").(*store.Account)

	var params struct {
		StartedAt int64 `form:"started_at"`
		EndedAt   int64 `form:"ended_at"`
	}

	if err := c.BindQuery(&params); err != nil {
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	if params.EndedAt == 0 {
		params.EndedAt = time.Now().Unix()
	}

	if params.StartedAt > params.EndedAt {
		abortWithEncoding(c, http.StatusBadRequest, errorInvalidParameters)
		return
	}

	// fb income for data
	fbIncome := s.getFBIncomeFromUserData(account, params.StartedAt, params.EndedAt)

	responseWithEncoding(c, http.StatusOK, &protomodel.InsightResponse{
		Result: &protomodel.Insight{
			FbIncome:     fbIncome.Income,
			FbIncomeFrom: fbIncome.From,
			FbIncomeTo:   fbIncome.To,
		},
	})
}
