package facebook

import (
	"encoding/json"
	"fmt"

	"golang.org/x/text/encoding/charmap"
)

// UTF-8 string wrongfully decoded as Latin-1
// https://stackoverflow.com/questions/50008296/facebook-json-badly-encoded
type MojibakeString string

func (s *MojibakeString) UnmarshalJSON(data []byte) error {
	var rawStr string
	if err := json.Unmarshal(data, &rawStr); err != nil {
		return fmt.Errorf("corrupted text: %s", err)
	}

	rs := []rune(rawStr)
	bs := []byte{}
	for _, r := range rs {
		b, ok := charmap.ISO8859_1.EncodeRune(r)
		if !ok {
			return fmt.Errorf("text not latin-1 encoded")
		}
		bs = append(bs, b)
	}
	*s = MojibakeString(bs)

	return nil
}
