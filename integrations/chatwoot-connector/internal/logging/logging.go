package logging

import (
	"fmt"
	"log/slog"
)

// Manager keeps the logging calls instance-aware without coupling the connector
// to Evolution Go's internal logger package.
type Manager struct {
	logger *slog.Logger
}

type InstanceLogger struct {
	logger     *slog.Logger
	instanceID string
}

func New(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{logger: logger}
}

func (m *Manager) GetLogger(instanceID string) *InstanceLogger {
	return &InstanceLogger{logger: m.logger, instanceID: instanceID}
}

func (l *InstanceLogger) LogInfo(format string, args ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, args...), "instance_id", l.instanceID)
}

func (l *InstanceLogger) LogWarn(format string, args ...interface{}) {
	l.logger.Warn(fmt.Sprintf(format, args...), "instance_id", l.instanceID)
}

func (l *InstanceLogger) LogError(format string, args ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, args...), "instance_id", l.instanceID)
}
