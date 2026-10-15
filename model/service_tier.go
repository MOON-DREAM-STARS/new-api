package model

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Channel aggregation (service tiers) storage.
//
// Upstream channels stay in the channels table; aggregation only stores
// routing rules that reference channel ids. Policies keep their tiers and
// strategy as one TEXT JSON document so a policy is versioned and replaced
// atomically on every database.

const (
	ServiceTierEconomy     = "economy"
	ServiceTierBalanced    = "balanced"
	ServiceTierPerformance = "performance"

	ServiceTierPreferenceLowest      = "lowest"
	ServiceTierPreferenceBalanced    = "balanced"
	ServiceTierPreferencePerformance = "performance"
)

// ServiceTierKeys lists the tiers from the cheapest level to the most
// expensive one. Escalation only moves forward in this order.
var ServiceTierKeys = []string{ServiceTierEconomy, ServiceTierBalanced, ServiceTierPerformance}

// ServiceTierLevel returns the tier's position in ServiceTierKeys, or -1.
func ServiceTierLevel(tier string) int {
	return slices.Index(ServiceTierKeys, tier)
}

var (
	ErrServiceTierVersionConflict = common.NewMessage("The configuration was changed by someone else. Reload and try again.")
	ErrServiceTierNotFound        = common.NewMessage("The configuration no longer exists. Reload and try again.")
	ErrModelFamilyKeyExists       = common.NewMessage("A model family with this key already exists")
)

type ModelFamily struct {
	Id          int    `json:"id"`
	Key         string `json:"key" gorm:"type:varchar(64);uniqueIndex;not null"`
	Name        string `json:"name" gorm:"type:varchar(128);not null"`
	Description string `json:"description" gorm:"type:varchar(255)"`
	// MatchPatterns is a JSON array of lower-case name fragments that help the
	// administrator find candidate members. Membership itself is explicit.
	MatchPatterns string `json:"-" gorm:"type:text"`
	SortOrder     int    `json:"sort_order"`
	Version       int64  `json:"version" gorm:"bigint;not null;default:1"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

func (f *ModelFamily) GetMatchPatterns() []string {
	patterns := make([]string, 0)
	if strings.TrimSpace(f.MatchPatterns) == "" {
		return patterns
	}
	if err := common.UnmarshalJsonStr(f.MatchPatterns, &patterns); err != nil {
		common.SysError(common.LogText("invalid model family match patterns (family_id=%d): %s", f.Id, err.Error()))
		return []string{}
	}
	return patterns
}

func (f *ModelFamily) SetMatchPatterns(patterns []string) error {
	if patterns == nil {
		patterns = []string{}
	}
	data, err := common.Marshal(patterns)
	if err != nil {
		return err
	}
	f.MatchPatterns = string(data)
	return nil
}

type ModelFamilyMember struct {
	Id        int    `json:"id"`
	FamilyId  int    `json:"family_id" gorm:"index;not null"`
	ModelName string `json:"model_name" gorm:"type:varchar(255);uniqueIndex;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

// AggregationPolicy is either a family default (ModelName == "") or a
// single-model override. A disabled override keeps that model on ordinary
// group routing even when the family default is enabled.
type AggregationPolicy struct {
	Id        int    `json:"id"`
	FamilyId  int    `json:"family_id" gorm:"not null;uniqueIndex:idx_aggregation_policy_scope,priority:1"`
	ModelName string `json:"model_name" gorm:"type:varchar(255);not null;default:'';uniqueIndex:idx_aggregation_policy_scope,priority:2"`
	Enabled   bool   `json:"enabled"`
	Config    string `json:"-" gorm:"type:text"`
	Version   int64  `json:"version" gorm:"bigint;not null;default:1"`
	UpdatedBy int    `json:"updated_by"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint"`
}

type AggregationTierChannel struct {
	ChannelId int   `json:"channel_id"`
	Priority  int64 `json:"priority"`
	Weight    int   `json:"weight"`
	Enabled   bool  `json:"enabled"`
}

type AggregationTierConfig struct {
	Enabled  bool                     `json:"enabled"`
	Ratio    float64                  `json:"ratio"`
	Channels []AggregationTierChannel `json:"channels"`
}

// AggregationStrategy controls failover inside one request.
type AggregationStrategy struct {
	// SameTierFailover allows another channel of the same tier after a
	// retryable failure. Same-tier failover never counts as escalation.
	SameTierFailover bool `json:"same_tier_failover"`
	// MaxAttempts bounds upstream attempts per request across all tiers.
	MaxAttempts int `json:"max_attempts"`
	// MaxTotalSeconds stops new attempts once the request has run this long.
	// Zero disables the limit.
	MaxTotalSeconds int `json:"max_total_seconds"`
	// RetryOn lists the error categories that may move to another channel.
	RetryOn []string `json:"retry_on"`
	// SkipFailedChannels excludes channels that already failed in the request.
	SkipFailedChannels bool `json:"skip_failed_channels"`
	// CooldownFailures consecutive retryable failures put a channel into a
	// local cooldown for CooldownSeconds. Zero disables the cooldown.
	CooldownFailures int `json:"cooldown_failures"`
	CooldownSeconds  int `json:"cooldown_seconds"`
	// UserGroups restricts the policy to these user groups. Empty means all.
	UserGroups []string `json:"user_groups"`
}

type AggregationPolicyConfig struct {
	Tiers    map[string]AggregationTierConfig `json:"tiers"`
	Strategy AggregationStrategy              `json:"strategy"`
}

func (p *AggregationPolicy) GetConfig() (AggregationPolicyConfig, error) {
	config := AggregationPolicyConfig{}
	if strings.TrimSpace(p.Config) == "" {
		return config, nil
	}
	err := common.UnmarshalJsonStr(p.Config, &config)
	return config, err
}

func (p *AggregationPolicy) SetConfig(config AggregationPolicyConfig) error {
	data, err := common.Marshal(config)
	if err != nil {
		return err
	}
	p.Config = string(data)
	return nil
}

// UserServiceTierSetting stores a user's tier choices. TokenId 0 is the user
// default; a positive TokenId is reserved for per-key overrides.
type UserServiceTierSetting struct {
	Id                int    `json:"id"`
	UserId            int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_service_tier_scope,priority:1"`
	TokenId           int    `json:"token_id" gorm:"not null;default:0;uniqueIndex:idx_user_service_tier_scope,priority:2"`
	EnabledTiers      string `json:"enabled_tiers" gorm:"type:varchar(64);not null"`
	Preference        string `json:"preference" gorm:"type:varchar(16);not null"`
	AllowEscalation   bool   `json:"allow_escalation"`
	MaxEscalationTier string `json:"max_escalation_tier" gorm:"type:varchar(16);not null;default:''"`
	Version           int64  `json:"version" gorm:"bigint;not null;default:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint"`
}

func (s *UserServiceTierSetting) GetEnabledTiers() []string {
	tiers := make([]string, 0, len(ServiceTierKeys))
	for tier := range strings.SplitSeq(s.EnabledTiers, ",") {
		tier = strings.TrimSpace(tier)
		if ServiceTierLevel(tier) >= 0 && !slices.Contains(tiers, tier) {
			tiers = append(tiers, tier)
		}
	}
	slices.SortFunc(tiers, func(a, b string) int { return ServiceTierLevel(a) - ServiceTierLevel(b) })
	return tiers
}

func (s *UserServiceTierSetting) SetEnabledTiers(tiers []string) {
	ordered := make([]string, 0, len(ServiceTierKeys))
	for _, tier := range ServiceTierKeys {
		if slices.Contains(tiers, tier) {
			ordered = append(ordered, tier)
		}
	}
	s.EnabledTiers = strings.Join(ordered, ",")
}

// migrateServiceTierTables creates the aggregation tables. The default
// families are seeded only when the family table is created, so families an
// administrator deleted are never recreated by a restart.
func migrateServiceTierTables(db *gorm.DB) error {
	fresh := !db.Migrator().HasTable(&ModelFamily{})
	if err := db.AutoMigrate(&ModelFamily{}, &ModelFamilyMember{}, &AggregationPolicy{}, &UserServiceTierSetting{}); err != nil {
		return err
	}
	if !fresh {
		return nil
	}
	return seedDefaultModelFamilies(db)
}

func seedDefaultModelFamilies(db *gorm.DB) error {
	now := common.GetTimestamp()
	defaults := []struct {
		key, name, description string
		patterns               []string
	}{
		{"deepseek", "DeepSeek", "DeepSeek models", []string{"deepseek"}},
		{"qwen", "Qwen", "Qwen (Tongyi Qianwen) models", []string{"qwen", "qwq", "qvq"}},
		{"glm", "GLM", "Zhipu GLM models", []string{"glm"}},
		{"kimi", "Kimi", "Moonshot Kimi models", []string{"kimi", "moonshot"}},
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for index, item := range defaults {
			family := ModelFamily{Key: item.key, Name: item.name, Description: item.description, SortOrder: index + 1, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := family.SetMatchPatterns(item.patterns); err != nil {
				return err
			}
			if err := tx.Create(&family).Error; err != nil {
				return err
			}
			if item.key != "deepseek" {
				continue
			}
			// An editable, disabled example: it routes nothing until an
			// administrator adds channels and enables it.
			policy := AggregationPolicy{FamilyId: family.Id, Enabled: false, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := policy.SetConfig(DefaultAggregationPolicyConfig(map[string]float64{
				ServiceTierEconomy: 0.15, ServiceTierBalanced: 0.20, ServiceTierPerformance: 0.40,
			})); err != nil {
				return err
			}
			if err := tx.Create(&policy).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DefaultAggregationPolicyConfig returns an empty policy template. Tiers
// start disabled and without channels.
func DefaultAggregationPolicyConfig(ratios map[string]float64) AggregationPolicyConfig {
	tiers := make(map[string]AggregationTierConfig, len(ServiceTierKeys))
	for _, tier := range ServiceTierKeys {
		ratio := ratios[tier]
		if ratio <= 0 {
			ratio = 1
		}
		tiers[tier] = AggregationTierConfig{Enabled: false, Ratio: ratio, Channels: []AggregationTierChannel{}}
	}
	return AggregationPolicyConfig{
		Tiers: tiers,
		Strategy: AggregationStrategy{
			SameTierFailover:   true,
			MaxAttempts:        4,
			MaxTotalSeconds:    120,
			RetryOn:            []string{"rate_limited", "server_error", "timeout", "channel_error"},
			SkipFailedChannels: true,
			CooldownFailures:   0,
			CooldownSeconds:    60,
			UserGroups:         []string{},
		},
	}
}

// ServiceTierStore is the full aggregation configuration as stored.
type ServiceTierStore struct {
	Families []ModelFamily
	Members  []ModelFamilyMember
	Policies []AggregationPolicy
}

func LoadServiceTierStore() (*ServiceTierStore, error) {
	store := &ServiceTierStore{}
	if err := DB.Order("sort_order asc, id asc").Find(&store.Families).Error; err != nil {
		return nil, err
	}
	if err := DB.Order("id asc").Find(&store.Members).Error; err != nil {
		return nil, err
	}
	if err := DB.Order("id asc").Find(&store.Policies).Error; err != nil {
		return nil, err
	}
	return store, nil
}

func CreateModelFamily(family *ModelFamily) error {
	now := common.GetTimestamp()
	family.Id = 0
	family.Version = 1
	family.CreatedAt, family.UpdatedAt = now, now
	return DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&ModelFamily{}).Where(map[string]any{"key": family.Key}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrModelFamilyKeyExists
		}
		return tx.Create(family).Error
	})
}

// UpdateModelFamily changes the descriptive fields when the stored version
// still equals expectedVersion.
func UpdateModelFamily(family *ModelFamily, expectedVersion int64) error {
	result := DB.Model(&ModelFamily{}).
		Where("id = ? AND version = ?", family.Id, expectedVersion).
		Updates(map[string]any{
			"name":           family.Name,
			"description":    family.Description,
			"match_patterns": family.MatchPatterns,
			"sort_order":     family.SortOrder,
			"version":        expectedVersion + 1,
			"updated_at":     common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return serviceTierMissingOrConflictTx(DB, &ModelFamily{}, family.Id)
	}
	family.Version = expectedVersion + 1
	return nil
}

// DeleteModelFamily removes the family with its members and policies.
func DeleteModelFamily(id int, expectedVersion int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&ModelFamily{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return serviceTierMissingOrConflictTx(tx, &ModelFamily{}, id)
		}
		if err := tx.Where("family_id = ?", id).Delete(&ModelFamilyMember{}).Error; err != nil {
			return err
		}
		return tx.Where("family_id = ?", id).Delete(&AggregationPolicy{}).Error
	})
}

// ReplaceModelFamilyMembers sets the family's member list. Overrides of
// models that leave the family are deleted with them. It returns the number
// of removed overrides.
func ReplaceModelFamilyMembers(familyId int, expectedVersion int64, models []string) (int64, error) {
	var removedOverrides int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ModelFamily{}).
			Where("id = ? AND version = ?", familyId, expectedVersion).
			Updates(map[string]any{"version": expectedVersion + 1, "updated_at": common.GetTimestamp()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return serviceTierMissingOrConflictTx(tx, &ModelFamily{}, familyId)
		}
		if len(models) > 0 {
			var conflicts []string
			if err := tx.Model(&ModelFamilyMember{}).
				Where("family_id <> ? AND model_name IN ?", familyId, models).
				Order("model_name asc").
				Pluck("model_name", &conflicts).Error; err != nil {
				return err
			}
			if len(conflicts) > 0 {
				// A model belongs to at most one family.
				return common.NewMessage("These models already belong to another family: {{models}}", map[string]any{"models": strings.Join(conflicts, ", ")})
			}
		}
		if err := tx.Where("family_id = ?", familyId).Delete(&ModelFamilyMember{}).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		for _, name := range models {
			if err := tx.Create(&ModelFamilyMember{FamilyId: familyId, ModelName: name, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		orphanQuery := tx.Where("family_id = ? AND model_name <> ?", familyId, "")
		if len(models) > 0 {
			orphanQuery = orphanQuery.Where("model_name NOT IN ?", models)
		}
		deleted := orphanQuery.Delete(&AggregationPolicy{})
		if deleted.Error != nil {
			return deleted.Error
		}
		removedOverrides = deleted.RowsAffected
		return nil
	})
	return removedOverrides, err
}

// SaveAggregationPolicy creates the policy when expectedVersion is 0 and
// otherwise replaces it when the stored version still matches.
func SaveAggregationPolicy(policy *AggregationPolicy, expectedVersion int64) error {
	now := common.GetTimestamp()
	scope := map[string]any{"family_id": policy.FamilyId, "model_name": policy.ModelName}
	if expectedVersion == 0 {
		policy.Id = 0
		policy.Version = 1
		policy.CreatedAt, policy.UpdatedAt = now, now
		return createVersionedRow(&AggregationPolicy{}, scope, policy)
	}
	result := DB.Model(&AggregationPolicy{}).
		Where(scope).Where("version = ?", expectedVersion).
		Updates(map[string]any{
			"enabled":    policy.Enabled,
			"config":     policy.Config,
			"version":    expectedVersion + 1,
			"updated_by": policy.UpdatedBy,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return missingOrConflict(&AggregationPolicy{}, scope)
	}
	policy.Version = expectedVersion + 1
	policy.UpdatedAt = now
	var stored AggregationPolicy
	if err := DB.Select("id", "created_at").Where(scope).First(&stored).Error; err == nil {
		policy.Id, policy.CreatedAt = stored.Id, stored.CreatedAt
	}
	return nil
}

// DeleteAggregationPolicy deletes a single-model override. Family defaults
// are disabled instead of deleted.
func DeleteAggregationPolicy(familyId int, modelName string, expectedVersion int64) error {
	if modelName == "" {
		return common.NewMessage("The family default rule cannot be deleted; disable it instead")
	}
	scope := map[string]any{"family_id": familyId, "model_name": modelName}
	result := DB.Where(scope).Where("version = ?", expectedVersion).Delete(&AggregationPolicy{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return missingOrConflict(&AggregationPolicy{}, scope)
	}
	return nil
}

func GetUserServiceTierSetting(userId int, tokenId int) (*UserServiceTierSetting, bool, error) {
	var setting UserServiceTierSetting
	err := DB.Where("user_id = ? AND token_id = ?", userId, tokenId).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &setting, true, nil
}

// SaveUserServiceTierSetting creates the row when expectedVersion is 0 and
// otherwise updates it when the stored version still matches.
func SaveUserServiceTierSetting(setting *UserServiceTierSetting, expectedVersion int64) error {
	now := common.GetTimestamp()
	scope := map[string]any{"user_id": setting.UserId, "token_id": setting.TokenId}
	if expectedVersion == 0 {
		setting.Id = 0
		setting.Version = 1
		setting.CreatedAt, setting.UpdatedAt = now, now
		return createVersionedRow(&UserServiceTierSetting{}, scope, setting)
	}
	result := DB.Model(&UserServiceTierSetting{}).
		Where(scope).Where("version = ?", expectedVersion).
		Updates(map[string]any{
			"enabled_tiers":       setting.EnabledTiers,
			"preference":          setting.Preference,
			"allow_escalation":    setting.AllowEscalation,
			"max_escalation_tier": setting.MaxEscalationTier,
			"version":             expectedVersion + 1,
			"updated_at":          now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrServiceTierVersionConflict
	}
	setting.Version = expectedVersion + 1
	setting.UpdatedAt = now
	return nil
}

// createVersionedRow inserts the first version of a row that is unique by
// scope. A concurrent first save loses with a version conflict instead of a
// raw unique-constraint error.
func createVersionedRow(table any, scope map[string]any, row any) error {
	var count int64
	if err := DB.Model(table).Where(scope).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrServiceTierVersionConflict
	}
	if err := DB.Create(row).Error; err != nil {
		if recount := DB.Model(table).Where(scope).Count(&count); recount.Error == nil && count > 0 {
			return ErrServiceTierVersionConflict
		}
		return err
	}
	return nil
}

func missingOrConflict(table any, scope map[string]any) error {
	var count int64
	if err := DB.Model(table).Where(scope).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrServiceTierNotFound
	}
	return ErrServiceTierVersionConflict
}

func serviceTierMissingOrConflictTx(tx *gorm.DB, row any, id int) error {
	var count int64
	if err := tx.Model(row).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrServiceTierNotFound
	}
	return ErrServiceTierVersionConflict
}
