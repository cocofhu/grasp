package handlers

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func writeAdmissionErr(c *gin.Context, err error) {
	var admission *services.AdmissionError
	if errors.As(err, &admission) {
		c.JSON(admission.Status, gin.H{"error": admission.Message, "code": admission.Code})
		return
	}
	_ = c.Error(err)
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}

func (h *Handlers) admission() *services.RunAdmission {
	return services.NewRunAdmission(h.Runs.DB(), h.Eng)
}
