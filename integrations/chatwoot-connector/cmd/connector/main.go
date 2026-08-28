package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/broker"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/evolution"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/httpapi"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/logging"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/proxymanager"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/repository"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/service"
	"github.com/allen-xavier/evolution-go-chatwoot-connector/internal/watchdog"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("connector stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := requiredEnv("CONNECTOR_DATABASE_URL")
	connectorAPIKey := requiredEnv("CONNECTOR_API_KEY")
	evolutionURL := envOrDefault("EVOLUTION_API_URL", "http://evolution_go:4000")
	evolutionAPIKey := requiredEnv("EVOLUTION_API_KEY")
	amqpURL := requiredEnv("AMQP_URL")
	port := envOrDefault("PORT", "4100")
	proxyRequired := !strings.EqualFold(envOrDefault("CONNECTOR_PROXY_REQUIRED", "true"), "false")
	proxyCollisionAction := strings.ToLower(strings.TrimSpace(envOrDefault("PROXY_COLLISION_ACTION", "alert")))
	if proxyCollisionAction != "alert" && proxyCollisionAction != "quarantine" {
		return fmt.Errorf("PROXY_COLLISION_ACTION must be alert or quarantine")
	}
	proxyMonitorSeconds, err := strconv.Atoi(envOrDefault("PROXY_MONITOR_INTERVAL_SECONDS", "60"))
	if err != nil || proxyMonitorSeconds < 15 {
		return fmt.Errorf("PROXY_MONITOR_INTERVAL_SECONDS must be an integer greater than or equal to 15")
	}

	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	if err := db.AutoMigrate(
		&model.ChatwootConfig{},
		&model.ChatwootBinding{},
		&model.ChatwootIdentityAlias{},
		&model.ChatwootOutboundJob{},
		&model.ConnectorSetting{},
		&proxymanager.TestRecord{},
	); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := migrateLegacyBindings(db); err != nil {
		return fmt.Errorf("migrate legacy chatwoot bindings: %w", err)
	}

	evolutionClient, err := evolution.NewClient(evolutionURL, evolutionAPIKey)
	if err != nil {
		return err
	}
	repo := repository.NewChatwootRepository(db)
	chatwootService := service.NewChatwootService(
		repo,
		evolutionClient,
		nil,
		logging.New(logger),
	)
	proxyManager := proxymanager.New(proxymanager.NewGormRepository(db), evolutionClient, proxyRequired)
	proxyManager.SetQuarantineOnUnsafe(proxyCollisionAction == "quarantine")
	watchdogSvc := watchdog.New(repo, repo, evolutionClient, logging.New(logger))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go chatwootService.Run(ctx)
	go watchdogSvc.Run(ctx)
	go proxyManager.RunMonitor(ctx, time.Duration(proxyMonitorSeconds)*time.Second, func(err error) {
		logger.Error("proxy safety monitor failed", "error", err)
	})

	go func() {
		handler := func(raw []byte) error {
			watchdogSvc.Observe(raw)
			return chatwootService.HandleEvolutionEvent(raw)
		}
		if err := broker.Run(ctx, amqpURL, logger, handler); err != nil {
			logger.Error("rabbitmq consumer stopped", "error", err)
			stop()
		}
	}()

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.New(chatwootService, evolutionClient, proxyManager, watchdogSvc, connectorAPIKey),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       70 * time.Second,
		WriteTimeout:      70 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		logger.Info("http server started", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func migrateLegacyBindings(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.ChatwootBinding{}) {
		return nil
	}
	if !db.Migrator().HasColumn("chatwoot_bindings", "remote_jid") {
		if err := db.Exec("ALTER TABLE chatwoot_bindings ADD COLUMN IF NOT EXISTS remote_jid VARCHAR(191)").Error; err != nil {
			return err
		}
	}
	for _, legacyColumn := range []string{"remote_j_id", "remotejid"} {
		if !db.Migrator().HasColumn("chatwoot_bindings", legacyColumn) {
			continue
		}
		query := fmt.Sprintf(
			"UPDATE chatwoot_bindings SET remote_jid = %s WHERE (remote_jid IS NULL OR remote_jid = '') AND %s IS NOT NULL AND %s <> ''",
			legacyColumn,
			legacyColumn,
			legacyColumn,
		)
		if err := db.Exec(query).Error; err != nil {
			return err
		}
		break
	}
	return db.Exec("CREATE INDEX IF NOT EXISTS idx_chatwoot_bindings_instance_remote_jid ON chatwoot_bindings (instance_id, remote_jid)").Error
}

func requiredEnv(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		panic(key + " is required")
	}
	return value
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
