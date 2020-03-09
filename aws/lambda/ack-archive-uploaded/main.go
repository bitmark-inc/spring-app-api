// main.go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var httpClient *http.Client = &http.Client{
	Timeout: time.Second * 10,
}

func handler(ctx context.Context, s3Event events.S3Event) {
	for _, record := range s3Event.Records {
		s3 := record.S3
		log.Printf("[%s] Bucket = %s, Key = %s \n", record.EventTime, s3.Bucket.Name, s3.Object.Key)

		b, err := json.Marshal(map[string]interface{}{
			"file_key":    s3.Object.Key,
			"uploaded_at": record.EventTime.Unix(),
		})

		if err != nil {
			log.Printf("fail to generate message body. error: %s", err.Error())
			continue
		}

		serverURL := os.Getenv("SERVER_URL")
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/secret/ack-archive-uploaded", serverURL), bytes.NewBuffer(b))
		req.Header.Add("API-TOKEN", os.Getenv("SERVER_API_TOKEN"))

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Printf("fail to ack uploaded archive. error: %s", err.Error())
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			b, err := httputil.DumpResponse(resp, false)
			if err != nil {
				log.Printf("fail to ack uploaded archive. error: %s", err.Error())
			} else {
				log.Printf(string(b))
			}
		}
	}
}

func main() {
	// Make the handler available for Remote Procedure Call by AWS Lambda
	lambda.Start(handler)
}
