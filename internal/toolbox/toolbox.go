package toolbox

import (
	"encoding/json"
	"time"
)

// TimeNowUTC returns time as string in RFC3339 format w/o timezone
func TimeNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.999999999")
}

// OutputBasicLogString attempts to output log in a JSON format otherwise will default to plain text
func OutputBasicLogString(level, message string) string {
	type ServerBasicError struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		TimeStamp string `json:"ts"`
	}

	basicError := ServerBasicError{
		Level:     level,
		Message:   message,
		TimeStamp: TimeNowUTC(),
	}

	marshalledJson, err := json.Marshal(basicError)
	if err != nil {
		return message
	}

	return string(marshalledJson)
}
