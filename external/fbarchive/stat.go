package fbarchive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"

	log "github.com/sirupsen/logrus"
)

// CountStat shows stat count for each item in
// posts / reactions stats API from parser server
type CountStat struct {
	SystemAvg float64 `json:"sys_avg"`
	Count     int64   `json:"count"`
}

// GetPostsStats to get posts stats from parser server
func (c *Client) GetPostsStats(ctx context.Context, start, end int64, publicKey string) (map[string]CountStat, error) {
	r, _ := c.createRequest(ctx, http.MethodGet,
		fmt.Sprintf("/posts_stats?start=%d&end=%d&data_owner=%s", start, end, publicKey),
		nil)
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return nil, err
	}

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}
	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from bitsocial server")

	decoder := json.NewDecoder(resp.Body)
	var respBody struct {
		Stats map[string]CountStat `json:"stats"`
	}

	if err := decoder.Decode(&respBody); err != nil {
		return nil, err
	}

	return respBody.Stats, nil
}

// GetReactionsStats to get posts stats from parser server
func (c *Client) GetReactionsStats(ctx context.Context, start, end int64, publicKey string) (map[string]CountStat, error) {
	r, _ := c.createRequest(ctx, http.MethodGet,
		fmt.Sprintf("/reactions_stats?start=%d&end=%d&data_owner=%s", start, end, publicKey),
		nil)
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return nil, err
	}

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}
	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from bitsocial server")

	decoder := json.NewDecoder(resp.Body)
	var respBody struct {
		Stats map[string]CountStat `json:"stats"`
	}

	if err := decoder.Decode(&respBody); err != nil {
		return nil, err
	}

	return respBody.Stats, nil
}
