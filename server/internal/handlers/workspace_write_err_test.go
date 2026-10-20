package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func TestWriteWorkspaceErrBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		err  error
		code int
	}{
		{services.ErrEmptyWorkspaceName, http.StatusBadRequest},
		{services.ErrSecretPlaceholderOnNewKey, http.StatusBadRequest},
		{services.ErrSecretPlaceholderOnPublicValue, http.StatusBadRequest},
		{services.ErrUnknownModelDisplayNameTooLong, http.StatusBadRequest},
		{services.ErrWorkspaceMissing, http.StatusNotFound},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeWorkspaceErr(c, tc.err)
		if w.Code != tc.code {
			t.Fatalf("%v: got %d want %d body=%s", tc.err, w.Code, tc.code, w.Body.String())
		}
	}
}
