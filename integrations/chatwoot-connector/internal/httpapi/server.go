package httpapi

import (
	"crypto/subtle"
	"embed"
	"io"
	"net/http"
	"strings"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	chatwoot_service "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/service"
	"github.com/gin-gonic/gin"
)

const maxWebhookBytes = 100 << 20

//go:embed ui/index.html
var uiFiles embed.FS

func New(service chatwoot_service.ChatwootService, evolutionAPI evolution.API, adminAPIKey string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	chatwoot := router.Group("/chatwoot")
	chatwoot.GET("", uiHandler)
	chatwoot.GET("/", uiHandler)
	chatwoot.POST("/webhook/:instanceId", webhookHandler(service))

	protected := chatwoot.Group("")
	protected.Use(auth(adminAPIKey))
	protected.POST("/set/:instanceId", setHandler(service))
	protected.GET("/find/:instanceId", findHandler(service))
	protected.GET("/instances", instancesHandler(evolutionAPI))

	return router
}

func uiHandler(c *gin.Context) {
	content, err := uiFiles.ReadFile("ui/index.html")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", content)
}

type instanceView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	JID              string `json:"jid"`
	Connected        bool   `json:"connected"`
	DisconnectReason string `json:"disconnectReason,omitempty"`
}

func instancesHandler(evolutionAPI evolution.API) gin.HandlerFunc {
	return func(c *gin.Context) {
		instances, err := evolutionAPI.ListInstances(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		views := make([]instanceView, 0, len(instances))
		for _, instance := range instances {
			views = append(views, instanceView{
				ID:               instance.Id,
				Name:             instance.Name,
				JID:              instance.Jid,
				Connected:        instance.Connected,
				DisconnectReason: instance.DisconnectReason,
			})
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": views})
	}
}

func auth(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := strings.TrimSpace(c.GetHeader("apikey"))
		if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
			return
		}
		c.Next()
	}
}

func setHandler(service chatwoot_service.ChatwootService) gin.HandlerFunc {
	return func(c *gin.Context) {
		instanceID := strings.TrimSpace(c.Param("instanceId"))
		if instanceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "instanceId is required"})
			return
		}
		var payload chatwoot_model.SetChatwootPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		config, err := service.Set(instanceID, &payload)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": config})
	}
}

func findHandler(service chatwoot_service.ChatwootService) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := service.Find(strings.TrimSpace(c.Param("instanceId")))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": config})
	}
}

func webhookHandler(service chatwoot_service.ChatwootService) gin.HandlerFunc {
	return func(c *gin.Context) {
		instanceID := strings.TrimSpace(c.Param("instanceId"))
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBytes))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if err := service.HandleWebhook(instanceID, c.Request.Header, body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "received"})
	}
}
