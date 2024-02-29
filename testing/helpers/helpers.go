package responsehelpers

import (
	"encoding/json"
	"io/ioutil"
	"net/http/httptest"
)

// UnmarshalResponseBody reads the response body from the recorder
// then attempts to unmarshal it with expected response struct
func UnmarshalResponseBody(recorder *httptest.ResponseRecorder, expectedResponseDefinition interface{}) error {

	content, err := ioutil.ReadAll(recorder.Result().Body)
	if err != nil {
		return err
	}

	if err = json.Unmarshal(content, expectedResponseDefinition); err != nil {
		return err
	}

	return nil
}
