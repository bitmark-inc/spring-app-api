package facebook

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"unicode"
)

type result struct {
	Output []string `json:"output"`
}

func isASCII(s string) bool {
	for _, c := range s {
		if c > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// DeepAI only uses Enlight corpus for training the sentiment model,
// so the analysis is only applied to content which only contains ASCII characters.
// For content which contains non-ASCII characters, it will labled as "Neutral".
func sentiment(text string) string {
	if !isASCII(text) {
		return "0"
	}

	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	writer.WriteField("text", text)
	if err := writer.Close(); err != nil {
		return "0"
	}

	req, _ := http.NewRequest("POST", "https://api.deepai.org/api/sentiment-analysis", payload)
	req.Header.Add("api-key", os.Getenv("DEEPAI_API_TOKEN"))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "0"
	}
	defer resp.Body.Close()

	var r result
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "0"
	}

	var scores []string
	for _, s := range r.Output {
		switch s {
		case "Verypositive":
			scores = append(scores, "2")
		case "Positive":
			scores = append(scores, "1")
		case "Neutral":
			scores = append(scores, "0")
		case "Negative":
			scores = append(scores, "-1")
		case "Verynegative":
			scores = append(scores, "-2")
		}
	}
	return strings.Join(scores, ",")
}
