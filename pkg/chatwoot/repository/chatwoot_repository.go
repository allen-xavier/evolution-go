package chatwoot_repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	chatwoot_model "github.com/EvolutionAPI/evolution-go/pkg/chatwoot/model"
	"gorm.io/gorm"
)

type ChatwootRepository interface {
	SaveConfig(config *chatwoot_model.ChatwootConfig) error
	GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error)

	SaveBinding(binding *chatwoot_model.ChatwootBinding) error
	GetBindingByRemoteJID(instanceID string, remoteJID string) (*chatwoot_model.ChatwootBinding, error)
	GetBindingByConversationID(instanceID string, conversationID int) (*chatwoot_model.ChatwootBinding, error)
}

type chatwootRepository struct {
	db *gorm.DB
}

func (r *chatwootRepository) SaveConfig(config *chatwoot_model.ChatwootConfig) error {
	return r.db.Save(config).Error
}

func (r *chatwootRepository) GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error) {
	var config chatwoot_model.ChatwootConfig
	err := r.db.Where("instance_id = ?", instanceID).First(&config).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &config, nil
}

func (r *chatwootRepository) SaveBinding(binding *chatwoot_model.ChatwootBinding) error {
	if err := r.db.Save(binding).Error; err != nil {
		if !isUndefinedColumnErr(err, "remote_jid") {
			return err
		}

		// Compatibility fallback for legacy schemas that used remotejid/remote_j_id.
		for _, legacyColumn := range []string{"remote_j_id", "remotejid"} {
			if !r.db.Migrator().HasColumn("chatwoot_bindings", legacyColumn) {
				continue
			}

			now := time.Now()
			payload := map[string]interface{}{
				"instance_id":     binding.InstanceID,
				legacyColumn:      binding.RemoteJID,
				"contact_id":      binding.ContactID,
				"conversation_id": binding.ConversationID,
				"source_id":       binding.SourceID,
				"updated_at":      now,
			}

			// Try update first.
			updateResult := r.db.Table("chatwoot_bindings").
				Where(fmt.Sprintf("instance_id = ? AND %s = ?", legacyColumn), binding.InstanceID, binding.RemoteJID).
				Updates(payload)
			if updateResult.Error == nil && updateResult.RowsAffected > 0 {
				return nil
			}

			// If no row matched, create one.
			payload["created_at"] = now
			createErr := r.db.Table("chatwoot_bindings").Create(payload).Error
			if createErr == nil {
				return nil
			}
		}
		return err
	}

	return nil
}

func (r *chatwootRepository) GetBindingByRemoteJID(instanceID string, remoteJID string) (*chatwoot_model.ChatwootBinding, error) {
	columns := r.remoteJIDLookupColumns()
	var lastErr error

	for _, column := range columns {
		var binding chatwoot_model.ChatwootBinding
		err := r.db.Where(fmt.Sprintf("instance_id = ? AND %s = ?", column), instanceID, remoteJID).First(&binding).Error
		if err == nil {
			return &binding, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if isUndefinedColumnErr(err, column) {
			lastErr = err
			continue
		}
		return nil, err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func (r *chatwootRepository) GetBindingByConversationID(instanceID string, conversationID int) (*chatwoot_model.ChatwootBinding, error) {
	columns := r.conversationIDLookupColumns()
	var lastErr error

	for _, column := range columns {
		var binding chatwoot_model.ChatwootBinding
		err := r.db.Where(fmt.Sprintf("instance_id = ? AND %s = ?", column), instanceID, conversationID).First(&binding).Error
		if err == nil {
			return &binding, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if isUndefinedColumnErr(err, column) {
			lastErr = err
			continue
		}
		return nil, err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func NewChatwootRepository(db *gorm.DB) ChatwootRepository {
	return &chatwootRepository{db: db}
}

func (r *chatwootRepository) remoteJIDLookupColumns() []string {
	return resolveExistingColumns(r.db, "chatwoot_bindings", []string{"remote_jid", "remote_j_id", "remotejid"})
}

func (r *chatwootRepository) conversationIDLookupColumns() []string {
	return resolveExistingColumns(r.db, "chatwoot_bindings", []string{"conversation_id", "conversationid"})
}

func resolveExistingColumns(db *gorm.DB, table string, candidates []string) []string {
	var columns []string
	for _, c := range candidates {
		if db.Migrator().HasColumn(table, c) {
			columns = append(columns, c)
		}
	}
	if len(columns) == 0 {
		return candidates
	}
	return columns
}

func isUndefinedColumnErr(err error, column string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	col := strings.ToLower(strings.TrimSpace(column))
	return strings.Contains(msg, fmt.Sprintf("column \"%s\" does not exist", col))
}
