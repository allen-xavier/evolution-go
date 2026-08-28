package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	chatwoot_model "github.com/allen-xavier/evolution-go-chatwoot-connector/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	identityAliasJIDColumn     = "alias_j_id"
	identityCanonicalJIDColumn = "canonical_j_id"
)

type ChatwootRepository interface {
	SaveConfig(config *chatwoot_model.ChatwootConfig) error
	GetConfig(instanceID string) (*chatwoot_model.ChatwootConfig, error)

	SaveBinding(binding *chatwoot_model.ChatwootBinding) error
	DeleteBinding(binding *chatwoot_model.ChatwootBinding) error
	GetBindingByRemoteJID(instanceID string, remoteJID string) (*chatwoot_model.ChatwootBinding, error)
	GetBindingByConversationID(instanceID string, conversationID int) (*chatwoot_model.ChatwootBinding, error)

	SaveIdentityAlias(instanceID string, aliasJID string, canonicalJID string) error
	ResolveIdentityAlias(instanceID string, aliasJID string) (string, error)

	EnqueueOutboundJob(job *chatwoot_model.ChatwootOutboundJob) error
	ListDueOutboundJobs(now time.Time, limit int) ([]chatwoot_model.ChatwootOutboundJob, error)
	SaveOutboundJob(job *chatwoot_model.ChatwootOutboundJob) error
	DeleteOutboundJob(job *chatwoot_model.ChatwootOutboundJob) error

	GetSetting(key string) (string, error)
	SetSetting(key string, value string) error
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

func (r *chatwootRepository) DeleteBinding(binding *chatwoot_model.ChatwootBinding) error {
	if binding == nil || binding.ID == 0 {
		return nil
	}
	return r.db.Delete(binding).Error
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

func (r *chatwootRepository) SaveIdentityAlias(instanceID string, aliasJID string, canonicalJID string) error {
	alias := chatwoot_model.ChatwootIdentityAlias{
		InstanceID:   strings.TrimSpace(instanceID),
		AliasJID:     strings.TrimSpace(aliasJID),
		CanonicalJID: strings.TrimSpace(canonicalJID),
	}
	if alias.InstanceID == "" || alias.AliasJID == "" || alias.CanonicalJID == "" {
		return errors.New("identity alias requires instance, alias jid and canonical jid")
	}

	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "instance_id"},
			{Name: identityAliasJIDColumn},
		},
		DoUpdates: clause.AssignmentColumns([]string{identityCanonicalJIDColumn, "updated_at"}),
	}).Create(&alias).Error
}

func (r *chatwootRepository) ResolveIdentityAlias(instanceID string, aliasJID string) (string, error) {
	var alias chatwoot_model.ChatwootIdentityAlias
	err := r.db.Where("instance_id = ? AND "+identityAliasJIDColumn+" = ?", strings.TrimSpace(instanceID), strings.TrimSpace(aliasJID)).
		First(&alias).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(alias.CanonicalJID), nil
}

func (r *chatwootRepository) EnqueueOutboundJob(job *chatwoot_model.ChatwootOutboundJob) error {
	if job == nil {
		return errors.New("outbound job is required")
	}
	if strings.TrimSpace(job.InstanceID) == "" || strings.TrimSpace(job.ChatwootMessageID) == "" {
		return errors.New("outbound job requires instance and Chatwoot message ID")
	}
	if job.NextAttemptAt.IsZero() {
		job.NextAttemptAt = time.Now()
	}

	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "instance_id"},
			{Name: "chatwoot_message_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"payload":         job.Payload,
			"attempts":        job.Attempts,
			"next_attempt_at": job.NextAttemptAt,
			"last_error":      job.LastError,
			"failed_at":       job.FailedAt,
			"updated_at":      time.Now(),
		}),
	}).Create(job).Error
}

func (r *chatwootRepository) ListDueOutboundJobs(now time.Time, limit int) ([]chatwoot_model.ChatwootOutboundJob, error) {
	if limit <= 0 {
		limit = 20
	}
	var jobs []chatwoot_model.ChatwootOutboundJob
	err := r.db.
		Where("failed_at IS NULL AND next_attempt_at <= ?", now).
		Order("next_attempt_at ASC, id ASC").
		Limit(limit).
		Find(&jobs).Error
	return jobs, err
}

func (r *chatwootRepository) SaveOutboundJob(job *chatwoot_model.ChatwootOutboundJob) error {
	if job == nil {
		return errors.New("outbound job is required")
	}
	return r.db.Save(job).Error
}

func (r *chatwootRepository) DeleteOutboundJob(job *chatwoot_model.ChatwootOutboundJob) error {
	if job == nil || job.ID == 0 {
		return nil
	}
	return r.db.Delete(job).Error
}

func NewChatwootRepository(db *gorm.DB) ChatwootRepository {
	return &chatwootRepository{db: db}
}

func (r *chatwootRepository) GetSetting(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("setting key is required")
	}
	var setting chatwoot_model.ConnectorSetting
	err := r.db.Where("key = ?", key).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *chatwootRepository) SetSetting(key string, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("setting key is required")
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&chatwoot_model.ConnectorSetting{Key: key, Value: value}).Error
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
