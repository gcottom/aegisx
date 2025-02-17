package handlers

import (
	"github.com/gcottom/aegisx/services"
	"github.com/gin-gonic/gin"
)

type MainHandler struct {
	ExecutorService *services.ExecuterService
}

func (h *MainHandler) Execute(c *gin.Context) {
	var req ExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, err := h.ExecutorService.NewExecution(c, req.Prompt)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "initiated", "executerID": id, "url": "http://localhost:8080/runtime/" + id})
}

func (h *MainHandler) Stop(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(400, gin.H{"error": "missing ID"})
		return
	}
	err := h.ExecutorService.StopRuntime(c, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": "stopped"})
}

func (h *MainHandler) Status(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(400, gin.H{"error": "missing ID"})
		return
	}
	status, err := h.ExecutorService.GetRuntimeStatus(c, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, *status)
}
