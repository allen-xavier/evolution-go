package chatwoot_repository

import (
	"errors"

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
	return r.db.Save(binding).Error
}

func (r *chatwootRepository) GetBindingByRemoteJID(instanceID string, remoteJID string) (*chatwoot_model.ChatwootBinding, error) {
	var binding chatwoot_model.ChatwootBinding
	err := r.db.Where("instance_id = ? AND remote_jid = ?", instanceID, remoteJID).First(&binding).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &binding, nil
}

func (r *chatwootRepository) GetBindingByConversationID(instanceID string, conversationID int) (*chatwoot_model.ChatwootBinding, error) {
	var binding chatwoot_model.ChatwootBinding
	err := r.db.Where("instance_id = ? AND conversation_id = ?", instanceID, conversationID).First(&binding).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &binding, nil
}

func NewChatwootRepository(db *gorm.DB) ChatwootRepository {
	return &chatwootRepository{db: db}
}
