package model

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Managed options hold JSON documents that one dedicated endpoint or background
// task owns: the price comparison page settings and the models.dev pricing
// import. They are ordinary rows of the options table and entries of the
// OptionMap, but their readers go through these helpers so that a write on
// another node is visible at once where a stale copy would matter.

const (
	// PriceComparisonOptionKey stores the settings of the price comparison page.
	PriceComparisonOptionKey = "PriceComparisonSettings"
	// ModelsDevConfigOptionKey stores which models the models.dev import covers.
	ModelsDevConfigOptionKey = "ModelsDevPricingConfig"
	// ModelsDevStateOptionKey stores the outcome of the latest models.dev
	// import. It is written by the importer only and kept separate from the
	// configuration so a slow import cannot restore stale switches.
	ModelsDevStateOptionKey = "ModelsDevPricingState"
)

// GetOptionFromDB returns the stored value of key and whether the row exists.
func GetOptionFromDB(key string) (string, bool, error) {
	var rows []Option
	if err := DB.Where(&Option{Key: key}).Limit(1).Find(&rows).Error; err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].Value, true, nil
}

// SaveOption stores value under key and refreshes this node's OptionMap.
func SaveOption(key string, value string) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&Option{}).Where(&Option{Key: key}).Update("value", value)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected > 0 {
			return nil
		}
		// An unchanged value reports no affected rows on MySQL, so the insert
		// must tolerate the existing row.
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: key, Value: value}).Error
	})
	if err != nil {
		return err
	}
	return updateOptionMap(key, value)
}
