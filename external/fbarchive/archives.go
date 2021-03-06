package fbarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"os"

	log "github.com/sirupsen/logrus"
)

func (c *Client) Login(ctx context.Context, username, password string) error {
	body := &bytes.Buffer{}
	encoder := json.NewEncoder(body)
	if err := encoder.Encode(map[string]interface{}{
		"username": username,
		"password": password,
	}); err != nil {
		return err
	}

	r, _ := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/auth/token/login", body)
	r.Header.Add("Content-Type", "application/json")
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}

	var respBody struct {
		AuthToken string `json:"auth_token"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&respBody); err != nil {
		return err
	}

	c.token = respBody.AuthToken

	return nil
}

func (c *Client) NewDataOwner(ctx context.Context, publicKey string) error {
	r, _ := c.createRequest(ctx, "POST", "/data_owners/", map[string]string{
		"public_key": publicKey,
	})
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}

	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from data analysis server")

	if resp.StatusCode < 300 {
		return nil
	}

	return errors.New("error when creating data owner")
}

func (c *Client) GetDataOwnerStatus(ctx context.Context, publicKey string) (string, error) {
	r, _ := c.createRequest(ctx, "GET", "/data_owners/"+publicKey, nil)
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}

	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from data analysis server")

	decoder := json.NewDecoder(resp.Body)
	var respBody struct {
		PublicKey string `json:"public_key"`
		Status    string `json:"status"`
	}

	if err := decoder.Decode(&respBody); err != nil {
		return "", err
	}

	return respBody.Status, nil
}

func (c *Client) DeleteDataOwner(ctx context.Context, publicKey string) error {
	r, _ := c.createRequest(ctx, http.MethodDelete, "/data_owners/"+publicKey, nil)
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}

	if resp.StatusCode < 300 {
		return nil
	}

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}
	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from bitsocial server")

	return errors.New("error when deleting data owner")
}

func (c *Client) UploadArchives(ctx context.Context, file *os.File, dataOwner string) (string, error) {
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "data.zip")
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.WriteField("data_owner", dataOwner); err != nil {
		return "", err
	}
	err = writer.Close()
	if err != nil {
		return "", err
	}

	r, _ := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/archives/", body)
	r.Header.Add("Content-Type", "multipart/form-data")
	r.Header.Add("Authorization", "Token "+c.token)
	r.Header.Set("Content-Type", writer.FormDataContentType())

	reqDumpByte, err := httputil.DumpRequest(r, false)
	if err != nil {
		log.Error(err)
	}

	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("req", string(reqDumpByte)).Debug("request to data analysis server")

	resp, err := c.httpClient.Do(r)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}

	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from data analysis server")

	decoder := json.NewDecoder(resp.Body)
	var respBody struct {
		ID string `json:"id"`
	}

	if err := decoder.Decode(&respBody); err != nil {
		return "", err
	}

	return respBody.ID, nil
}

func (c *Client) TriggerParsing(ctx context.Context, archiveID, dataOwner string) (string, error) {
	r, _ := c.createRequest(ctx, http.MethodPost, "/tasks/extraction/", map[string]string{
		"archive":    archiveID,
		"data_owner": dataOwner,
	})
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Print out the response in console log
	dumpBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Error(err)
	}

	log.WithContext(ctx).WithField("prefix", "fbarchive").WithField("resp", string(dumpBytes)).Debug("response from data analysis server")

	decoder := json.NewDecoder(resp.Body)
	var respBody struct {
		ID        string `json:"id"`
		DataOwner string `json:"data_owner"`
		Archive   string `json:"archive"`
		Status    string `json:"status"`
	}

	if err := decoder.Decode(&respBody); err != nil {
		return "", err
	}

	return respBody.ID, nil
}

func (c *Client) GetArchiveTaskStatus(ctx context.Context, taskID string) (string, error) {
	r, _ := c.createRequest(ctx, "GET", "/tasks/"+taskID, nil)
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return "", err
	}

	decoder := json.NewDecoder(resp.Body)
	var respBody struct {
		ID        string `json:"id"`
		DataOwner string `json:"data_owner"`
		Archive   string `json:"archive"`
		Status    string `json:"status"`
	}

	if err := decoder.Decode(&respBody); err != nil {
		return "", err
	}
	defer resp.Body.Close()

	return respBody.Status, nil
}

func (c *Client) IsReady() bool {
	return c.token != ""
}
