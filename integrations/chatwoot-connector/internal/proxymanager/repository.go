package proxymanager

import (
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) GetInstanceProxy(instanceID string) (string, error) {
	var row struct {
		Proxy sql.NullString
	}
	result := r.db.Table("instances").
		Select("proxy").
		Where("id = ?", instanceID).
		Take(&row)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("instance %q was not found", instanceID)
		}
		return "", result.Error
	}
	if !row.Proxy.Valid {
		return "", nil
	}
	return row.Proxy.String, nil
}

func (r *GormRepository) GetProxyTest(instanceID string) (*TestRecord, error) {
	var record TestRecord
	result := r.db.Where("instance_id = ?", instanceID).Take(&record)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &record, nil
}

func (r *GormRepository) FindInstancesByPublicIP(publicIP, exceptInstanceID string) ([]string, error) {
	var instanceIDs []string
	result := r.db.Model(&TestRecord{}).
		Where("public_ip = ? AND instance_id <> ?", publicIP, exceptInstanceID).
		Order("instance_id").
		Pluck("instance_id", &instanceIDs)
	return instanceIDs, result.Error
}

func (r *GormRepository) SaveProxyTest(record *TestRecord) error {
	return r.db.Save(record).Error
}

func (r *GormRepository) DeleteProxyTest(instanceID string) error {
	return r.db.Where("instance_id = ?", instanceID).Delete(&TestRecord{}).Error
}

func (r *GormRepository) ListInstanceProxies() ([]InstanceProxy, error) {
	var rows []struct {
		ID    string
		Proxy sql.NullString
	}
	result := r.db.Table("instances").
		Select("id, proxy").
		Where("proxy IS NOT NULL AND proxy <> ''").
		Order("id").
		Scan(&rows)
	if result.Error != nil {
		return nil, result.Error
	}
	instances := make([]InstanceProxy, 0, len(rows))
	for _, row := range rows {
		if row.Proxy.Valid {
			instances = append(instances, InstanceProxy{InstanceID: row.ID, RawProxy: row.Proxy.String})
		}
	}
	return instances, nil
}
