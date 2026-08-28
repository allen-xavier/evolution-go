package httpapi

import (
	"context"
	"crypto/subtle"
	"embed"
	"io"
	"net/http"
	"strings"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/proxymanager"
	chatwoot_service "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/service"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/watchdog"
	"github.com/gin-gonic/gin"
)

const maxWebhookBytes = 100 << 20

//go:embed ui/index.html
var uiFiles embed.FS

type ProxyManager interface {
	Get(instanceID string) (*proxymanager.ConfigView, error)
	Set(ctx context.Context, instanceID string, input proxymanager.ConfigInput) (*proxymanager.ConfigView, error)
	Remove(ctx context.Context, instanceID string) error
	Test(ctx context.Context, instanceID string, input proxymanager.ConfigInput) (*proxymanager.TestResult, error)
}

type WatchdogController interface {
	Status() watchdog.StatusView
	SetConfig(ctx context.Context, enabled bool, webhookURL string) error
}

func New(service chatwoot_service.ChatwootService, evolutionAPI evolution.API, proxyManager ProxyManager, watchdogCtl WatchdogController, adminAPIKey string) *gin.Engine {
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
	protected.GET("/proxy/:instanceId", getProxyHandler(proxyManager))
	protected.PUT("/proxy/:instanceId", setProxyHandler(proxyManager))
	protected.DELETE("/proxy/:instanceId", removeProxyHandler(proxyManager))
	protected.POST("/proxy/:instanceId/test", testProxyHandler(proxyManager))
	if watchdogCtl != nil {
		protected.GET("/watchdog", getWatchdogHandler(watchdogCtl))
		protected.PUT("/watchdog", putWatchdogHandler(watchdogCtl))
	}

	return router
}

func getWatchdogHandler(watchdogCtl WatchdogController) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": watchdogCtl.Status()})
	}
}

func putWatchdogHandler(watchdogCtl WatchdogController) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload struct {
			Enabled    bool   `json:"enabled"`
			WebhookURL string `json:"webhookUrl"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := watchdogCtl.SetConfig(c.Request.Context(), payload.Enabled, payload.WebhookURL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": watchdogCtl.Status()})
	}
}

func getProxyHandler(manager ProxyManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, err := manager.Get(strings.TrimSpace(c.Param("instanceId")))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": config})
	}
}

func setProxyHandler(manager ProxyManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input proxymanager.ConfigInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		config, err := manager.Set(c.Request.Context(), strings.TrimSpace(c.Param("instanceId")), input)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": config})
	}
}

func removeProxyHandler(manager ProxyManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := manager.Remove(c.Request.Context(), strings.TrimSpace(c.Param("instanceId"))); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	}
}

func testProxyHandler(manager ProxyManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input proxymanager.ConfigInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		result, err := manager.Test(c.Request.Context(), strings.TrimSpace(c.Param("instanceId")), input)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": result})
	}
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
