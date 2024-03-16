package contenttype_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/middleware/contenttype"
	"github.com/stretchr/testify/assert"
)

func TestNewContentType(t *testing.T) {

	tests := []struct {
		name                string
		expectedContentType string
	}{
		{
			name:                "Successful",
			expectedContentType: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			req, err := http.NewRequest("GET", "/api/v1/test", nil)
			if err != nil {
				t.Fatal(err)
			}

			watcher := httptest.NewRecorder()
			handler := contenttype.NewContentType(http.HandlerFunc(handlerFunc))

			handler.ServeHTTP(watcher, req)

			assert.Contains(t, watcher.Result().Header["Content-Type"], tt.expectedContentType)

		})
	}

}

func handlerFunc(w http.ResponseWriter, r *http.Request) {
	//nolint not checked
	w.Write([]byte("{\"message\": \"OK\"}"))
}
