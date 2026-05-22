package chatwoot_handler

import (
	"io"
	"net/http"

	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	chatwoot_service "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/service"
	"github.com/gin-gonic/gin"
)

type ChatwootHandler interface {
	Set(c *gin.Context)
	Find(c *gin.Context)
	Webhook(c *gin.Context)
}

type chatwootHandler struct {
	service chatwoot_service.ChatwootService
}

func (h *chatwootHandler) Set(c *gin.Context) {
	instanceID := c.Param("instanceId")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	var payload chatwoot_model.SetChatwootPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	cfg, err := h.service.Set(instanceID, &payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    cfg,
	})
}

func (h *chatwootHandler) Find(c *gin.Context) {
	instanceID := c.Param("instanceId")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	cfg, err := h.service.Find(instanceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data":    cfg,
	})
}

func (h *chatwootHandler) Webhook(c *gin.Context) {
	instanceID := c.Param("instanceId")
	if instanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if err := h.service.HandleWebhook(instanceID, c.Request.Header, body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "received"})
}

func NewChatwootHandler(service chatwoot_service.ChatwootService) ChatwootHandler {
	return &chatwootHandler{
		service: service,
	}
}
