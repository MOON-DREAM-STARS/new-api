package service

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Channel aggregation (service tiers): configuration snapshot, validation
// and the tier chain shared by routing, the user preview and the
// administrator's route test. Routing state for one request lives in
// service_tier_route.go.

const (
	ServiceTierErrorRateLimited     = "rate_limited"
	ServiceTierErrorServer          = "server_error"
	ServiceTierErrorTimeout         = "timeout"
	ServiceTierErrorChannel         = "channel_error"
	ServiceTierErrorClient          = "client_error"
	ServiceTierErrorInvalidResponse = "invalid_response"
	ServiceTierErrorLocal           = "local_rejection"

	serviceTierMaxAttempts      = 10
	serviceTierMaxTotalSeconds  = 3600
	serviceTierMaxRatio         = 1000
	serviceTierMaxWeight        = 10000
	serviceTierMaxPriority      = 1_000_000
	serviceTierMaxCooldownCount = 20
)

// ServiceTierRelayableErrors are the error categories an administrator may
// allow to move a request to another channel. Client errors, local
// rejections and malformed 2xx responses never move.
var ServiceTierRelayableErrors = []string{
	ServiceTierErrorRateLimited,
	ServiceTierErrorServer,
	ServiceTierErrorTimeout,
	ServiceTierErrorChannel,
}

// ServiceTierSettings is a user's normalized tier choice.
type ServiceTierSettings struct {
	EnabledTiers      []string `json:"enabled_tiers"`
	Preference        string   `json:"preference"`
	AllowEscalation   bool     `json:"allow_escalation"`
	MaxEscalationTier string   `json:"max_escalation_tier"`
	Version           int64    `json:"version"`
	// IsDefault marks settings the user never saved.
	IsDefault bool `json:"is_default"`
}

// DefaultServiceTierSettings applies to users who never saved tier
// settings: every tier may serve, the cheapest offered tier starts and
// requests never escalate, so enabling aggregation cannot raise the price of
// a request above its cheapest offered tier.
func DefaultServiceTierSettings() ServiceTierSettings {
	return ServiceTierSettings{
		EnabledTiers: slices.Clone(model.ServiceTierKeys),
		Preference:   model.ServiceTierPreferenceLowest,
		IsDefault:    true,
	}
}

// NormalizeServiceTierSettings validates a user's choice and returns its
// canonical form. Errors are web console messages.
func NormalizeServiceTierSettings(input ServiceTierSettings) (ServiceTierSettings, error) {
	result := ServiceTierSettings{Version: input.Version}
	for _, tier := range model.ServiceTierKeys {
		if slices.Contains(input.EnabledTiers, tier) {
			result.EnabledTiers = append(result.EnabledTiers, tier)
		}
	}
	for _, tier := range input.EnabledTiers {
		if model.ServiceTierLevel(tier) < 0 {
			return result, common.NewMessage("Unknown service tier: {{tier}}", map[string]any{"tier": tier})
		}
	}
	if len(result.EnabledTiers) == 0 {
		return result, common.NewMessage("Enable at least one service tier")
	}
	switch input.Preference {
	case model.ServiceTierPreferenceLowest, model.ServiceTierPreferenceBalanced, model.ServiceTierPreferencePerformance:
		result.Preference = input.Preference
	default:
		return result, common.NewMessage("Choose a default preference")
	}
	start := serviceTierPreferenceStart(result)
	if !slices.Contains(result.EnabledTiers, start) {
		return result, common.NewMessage("The preferred tier is turned off. Turn it on or choose another preference")
	}
	if !input.AllowEscalation {
		return result, nil
	}
	if !slices.Contains(result.EnabledTiers, input.MaxEscalationTier) || model.ServiceTierLevel(input.MaxEscalationTier) <= model.ServiceTierLevel(start) {
		return result, common.NewMessage("Choose a highest escalation tier that is turned on and above the preferred tier")
	}
	result.AllowEscalation = true
	result.MaxEscalationTier = input.MaxEscalationTier
	return result, nil
}

// serviceTierPreferenceStart is the tier a preference starts from before
// model availability is considered. "lowest" starts from the cheapest tier
// the user enabled.
func serviceTierPreferenceStart(settings ServiceTierSettings) string {
	switch settings.Preference {
	case model.ServiceTierPreferenceBalanced:
		return model.ServiceTierBalanced
	case model.ServiceTierPreferencePerformance:
		return model.ServiceTierPerformance
	}
	if len(settings.EnabledTiers) == 0 {
		return ""
	}
	return settings.EnabledTiers[0]
}

// ServiceTierStep is one tier a request may use, in the order it is tried.
type ServiceTierStep struct {
	Tier       string  `json:"tier"`
	Ratio      float64 `json:"ratio"`
	Escalation bool    `json:"escalation"`
	channels   []model.AggregationTierChannel
}

// BuildServiceTierChain resolves which tiers a request may use and in which
// order. It is the single rule behind routing, the user's strategy preview and
// the administrator's route test:
//
//   - a tier must be offered by the policy (enabled with a positive ratio)
//     and turned on by the user;
//   - "lowest" starts at the cheapest such tier, "balanced"/"performance"
//     start at that tier; a preferred tier the policy does not offer is
//     skipped and only escalation may follow it;
//   - escalation is opt-in, only moves to more expensive tiers and never
//     past MaxEscalationTier.
//
// The returned preferred tier names the starting point the preference asked
// for, even when the policy does not offer it.
func BuildServiceTierChain(config model.AggregationPolicyConfig, settings ServiceTierSettings) ([]ServiceTierStep, string) {
	offered := func(tier string) bool {
		tierConfig, ok := config.Tiers[tier]
		return ok && tierConfig.Enabled && tierConfig.Ratio > 0 && slices.Contains(settings.EnabledTiers, tier)
	}
	preferred := serviceTierPreferenceStart(settings)
	if settings.Preference == model.ServiceTierPreferenceLowest {
		for _, tier := range settings.EnabledTiers {
			if offered(tier) {
				preferred = tier
				break
			}
		}
	}
	steps := make([]ServiceTierStep, 0, len(model.ServiceTierKeys))
	appendStep := func(tier string, escalation bool) {
		tierConfig := config.Tiers[tier]
		channels := make([]model.AggregationTierChannel, 0, len(tierConfig.Channels))
		for _, entry := range tierConfig.Channels {
			if entry.Enabled {
				channels = append(channels, entry)
			}
		}
		steps = append(steps, ServiceTierStep{Tier: tier, Ratio: tierConfig.Ratio, Escalation: escalation, channels: channels})
	}
	if offered(preferred) {
		appendStep(preferred, false)
	}
	if settings.AllowEscalation {
		start, ceiling := model.ServiceTierLevel(preferred), model.ServiceTierLevel(settings.MaxEscalationTier)
		for level, tier := range model.ServiceTierKeys {
			if level > start && level <= ceiling && offered(tier) {
				appendStep(tier, true)
			}
		}
	}
	return steps, preferred
}

// serviceTierCeilingRatio is the highest ratio a chain may charge.
func serviceTierCeilingRatio(steps []ServiceTierStep) float64 {
	ceiling := 0.0
	for _, step := range steps {
		ceiling = max(ceiling, step.Ratio)
	}
	return ceiling
}

// ---------------------------------------------------------------------------
// Configuration snapshot
// ---------------------------------------------------------------------------

type serviceTierPolicy struct {
	Record model.AggregationPolicy
	Config model.AggregationPolicyConfig
}

type serviceTierSnapshot struct {
	loadedAt     time.Time
	families     []model.ModelFamily
	familyByID   map[int]*model.ModelFamily
	memberFamily map[string]int
	members      map[int][]string
	defaults     map[int]*serviceTierPolicy
	overrides    map[int]map[string]*serviceTierPolicy
}

var (
	serviceTierSnapshotValue atomic.Pointer[serviceTierSnapshot]
	serviceTierReloadMutex   sync.Mutex
)

// serviceTierSnapshotTTL bounds how long another node's configuration
// change takes to apply. Writes on this node refresh immediately.
func serviceTierSnapshotTTL() time.Duration {
	if !common.MemoryCacheEnabled {
		return 3 * time.Second
	}
	return time.Duration(max(common.SyncFrequency, 1)) * time.Second
}

func buildServiceTierSnapshot(store *model.ServiceTierStore) *serviceTierSnapshot {
	snapshot := &serviceTierSnapshot{
		loadedAt:     time.Now(),
		families:     store.Families,
		familyByID:   make(map[int]*model.ModelFamily, len(store.Families)),
		memberFamily: make(map[string]int, len(store.Members)),
		members:      make(map[int][]string),
		defaults:     make(map[int]*serviceTierPolicy),
		overrides:    make(map[int]map[string]*serviceTierPolicy),
	}
	for i := range snapshot.families {
		snapshot.familyByID[snapshot.families[i].Id] = &snapshot.families[i]
	}
	for _, member := range store.Members {
		if _, ok := snapshot.familyByID[member.FamilyId]; !ok {
			continue
		}
		snapshot.memberFamily[member.ModelName] = member.FamilyId
		snapshot.members[member.FamilyId] = append(snapshot.members[member.FamilyId], member.ModelName)
	}
	for _, record := range store.Policies {
		config, err := record.GetConfig()
		if err != nil {
			// An unreadable policy routes nothing rather than routing with
			// defaults the administrator never saved.
			common.SysError(common.LogText("invalid aggregation policy config (policy_id=%d): %s", record.Id, err.Error()))
			continue
		}
		policy := &serviceTierPolicy{Record: record, Config: config}
		if record.ModelName == "" {
			snapshot.defaults[record.FamilyId] = policy
			continue
		}
		if snapshot.memberFamily[record.ModelName] != record.FamilyId {
			continue
		}
		if snapshot.overrides[record.FamilyId] == nil {
			snapshot.overrides[record.FamilyId] = make(map[string]*serviceTierPolicy)
		}
		snapshot.overrides[record.FamilyId][record.ModelName] = policy
	}
	return snapshot
}

// RefreshServiceTierCache reloads the aggregation configuration now.
func RefreshServiceTierCache() error {
	serviceTierReloadMutex.Lock()
	defer serviceTierReloadMutex.Unlock()
	return refreshServiceTierCacheLocked()
}

func refreshServiceTierCacheLocked() error {
	store, err := model.LoadServiceTierStore()
	if err != nil {
		return err
	}
	serviceTierSnapshotValue.Store(buildServiceTierSnapshot(store))
	return nil
}

// currentServiceTierSnapshot returns the configuration, reloading it when it
// is older than the TTL. One request reloads while the others keep using the
// previous snapshot. A failed first load is returned as an error because an
// empty snapshot is not evidence that no aggregation policy exists.
func currentServiceTierSnapshot() (*serviceTierSnapshot, error) {
	snapshot := serviceTierSnapshotValue.Load()
	if snapshot != nil && time.Since(snapshot.loadedAt) < serviceTierSnapshotTTL() {
		return snapshot, nil
	}
	if snapshot == nil {
		serviceTierReloadMutex.Lock()
		defer serviceTierReloadMutex.Unlock()
	} else if !serviceTierReloadMutex.TryLock() {
		return snapshot, nil
	} else {
		defer serviceTierReloadMutex.Unlock()
	}
	if latest := serviceTierSnapshotValue.Load(); latest != nil && time.Since(latest.loadedAt) < serviceTierSnapshotTTL() {
		return latest, nil
	}
	if model.DB == nil {
		return snapshot, nil
	}
	if err := refreshServiceTierCacheLocked(); err != nil {
		common.SysError(common.LogText("failed to load channel aggregation config: %s", err.Error()))
		if snapshot == nil {
			// A failed first read is not evidence that no policy exists. Leave
			// the cache unset so relay callers can fail closed and retry.
			return nil, err
		}
		// Keep the last known configuration during a transient refresh error.
		// Copy before updating loadedAt because snapshots are immutable once stored.
		stale := *snapshot
		stale.loadedAt = time.Now()
		serviceTierSnapshotValue.Store(&stale)
		return &stale, nil
	}
	return serviceTierSnapshotValue.Load(), nil
}

// resolve returns the model's family and effective policy. A model override
// replaces the family default as a whole.
func (s *serviceTierSnapshot) resolve(modelName string) (*model.ModelFamily, *serviceTierPolicy, string) {
	matched := modelName
	familyID, ok := s.memberFamily[modelName]
	if !ok {
		normalized := ratio_setting.RoutingMatchModelName(modelName)
		if normalized == "" || normalized == modelName {
			return nil, nil, ""
		}
		if familyID, ok = s.memberFamily[normalized]; !ok {
			return nil, nil, ""
		}
		matched = normalized
	}
	family := s.familyByID[familyID]
	if override := s.overrides[familyID][matched]; override != nil {
		return family, override, "model"
	}
	if policy := s.defaults[familyID]; policy != nil {
		return family, policy, "family"
	}
	return family, nil, ""
}

// ---------------------------------------------------------------------------
// User settings
// ---------------------------------------------------------------------------

func serviceTierSettingsCacheKey(userID int) string {
	return fmt.Sprintf("service_tier:user:%d", userID)
}

const serviceTierSettingsCacheSetScript = `
local cached = redis.call('GET', KEYS[1])
if cached then
  local ok, current = pcall(cjson.decode, cached)
  if ok and tonumber(current.version or 0) >= tonumber(ARGV[1]) then
    return 0
  end
end
local ttl = tonumber(ARGV[3])
if ttl and ttl > 0 then
  redis.call('SET', KEYS[1], ARGV[2], 'EX', ttl)
else
  redis.call('SET', KEYS[1], ARGV[2])
end
return 1
`

func cacheServiceTierSettingsIfNewer(userID int, settings ServiceTierSettings) error {
	if common.RDB == nil {
		return fmt.Errorf("redis client is unavailable")
	}
	data, err := common.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = common.RDB.Eval(context.Background(), serviceTierSettingsCacheSetScript,
		[]string{serviceTierSettingsCacheKey(userID)}, settings.Version,
		string(data), common.RedisKeyCacheSeconds()).Result()
	return err
}

// GetUserServiceTierSettings returns the user's saved settings or the
// defaults. Redis cache writes are ordered by the persisted row version.
func GetUserServiceTierSettings(userID int) (ServiceTierSettings, error) {
	if common.RedisEnabled {
		if cached, err := common.RedisGet(serviceTierSettingsCacheKey(userID)); err == nil && cached != "" {
			var settings ServiceTierSettings
			if common.UnmarshalJsonStr(cached, &settings) == nil {
				return settings, nil
			}
		}
	}
	row, found, err := model.GetUserServiceTierSetting(userID, 0)
	if err != nil {
		return ServiceTierSettings{}, err
	}
	settings := DefaultServiceTierSettings()
	if found {
		settings = ServiceTierSettings{
			EnabledTiers:      row.GetEnabledTiers(),
			Preference:        row.Preference,
			AllowEscalation:   row.AllowEscalation,
			MaxEscalationTier: row.MaxEscalationTier,
			Version:           row.Version,
		}
	}
	if common.RedisEnabled {
		if err := cacheServiceTierSettingsIfNewer(userID, settings); err != nil {
			common.SysError(common.LogText("failed to cache service tier settings (user_id=%d): %s", userID, err.Error()))
		}
	}
	return settings, nil
}

// SaveUserServiceTierSettings validates and stores the user's settings when
// expectedVersion still matches the stored version (0 for a first save).
func SaveUserServiceTierSettings(userID int, input ServiceTierSettings, expectedVersion int64) (ServiceTierSettings, error) {
	settings, err := NormalizeServiceTierSettings(input)
	if err != nil {
		return settings, err
	}
	row := &model.UserServiceTierSetting{
		UserId:            userID,
		Preference:        settings.Preference,
		AllowEscalation:   settings.AllowEscalation,
		MaxEscalationTier: settings.MaxEscalationTier,
	}
	row.SetEnabledTiers(settings.EnabledTiers)
	if err := model.SaveUserServiceTierSetting(row, expectedVersion); err != nil {
		return settings, err
	}
	settings.Version = row.Version
	if common.RedisEnabled {
		if err := cacheServiceTierSettingsIfNewer(userID, settings); err != nil {
			common.SysError(common.LogText("failed to cache service tier settings (user_id=%d): %s", userID, err.Error()))
		}
	}
	return settings, nil
}

// ---------------------------------------------------------------------------
// Administrator policy validation
// ---------------------------------------------------------------------------

// ServiceTierPolicyInput is an administrator's policy as submitted.
type ServiceTierPolicyInput struct {
	FamilyID  int                           `json:"family_id"`
	ModelName string                        `json:"model_name"`
	Enabled   bool                          `json:"enabled"`
	Config    model.AggregationPolicyConfig `json:"config"`
	Version   int64                         `json:"version"`
}

// NormalizeServiceTierPolicy validates a policy against the stored families
// and channels and returns warnings that do not block saving.
func NormalizeServiceTierPolicy(input ServiceTierPolicyInput, store *model.ServiceTierStore, channels map[int]*model.Channel) (model.AggregationPolicyConfig, []*common.Message, error) {
	config := model.AggregationPolicyConfig{Tiers: make(map[string]model.AggregationTierConfig, len(model.ServiceTierKeys)), Strategy: input.Config.Strategy}
	warnings := make([]*common.Message, 0)
	var family *model.ModelFamily
	for i := range store.Families {
		if store.Families[i].Id == input.FamilyID {
			family = &store.Families[i]
		}
	}
	if family == nil {
		return config, nil, model.ErrServiceTierNotFound
	}
	members := make([]string, 0)
	for _, member := range store.Members {
		if member.FamilyId == input.FamilyID {
			members = append(members, member.ModelName)
		}
	}
	if input.ModelName != "" && !slices.Contains(members, input.ModelName) {
		return config, nil, common.NewMessage("Model {{model}} is not a member of this family", map[string]any{"model": input.ModelName})
	}
	for tier := range input.Config.Tiers {
		if model.ServiceTierLevel(tier) < 0 {
			return config, nil, common.NewMessage("Unknown service tier: {{tier}}", map[string]any{"tier": tier})
		}
	}

	previousRatio, previousTier := 0.0, ""
	enabledTiers := 0
	for _, tier := range model.ServiceTierKeys {
		tierInput, ok := input.Config.Tiers[tier]
		if !ok {
			tierInput = model.AggregationTierConfig{Ratio: 1}
		}
		if math.IsNaN(tierInput.Ratio) || math.IsInf(tierInput.Ratio, 0) || tierInput.Ratio <= 0 || tierInput.Ratio > serviceTierMaxRatio {
			return config, nil, common.NewMessage("The ratio of tier {{tier}} must be greater than 0 and at most 1000", map[string]any{"tier": tier})
		}
		normalized := model.AggregationTierConfig{Enabled: tierInput.Enabled, Ratio: tierInput.Ratio, Channels: make([]model.AggregationTierChannel, 0, len(tierInput.Channels))}
		enabledChannels := 0
		for _, entry := range tierInput.Channels {
			channel, exists := channels[entry.ChannelId]
			if !exists {
				return config, nil, common.NewMessage("Channel #{{id}} does not exist", map[string]any{"id": entry.ChannelId})
			}
			if slices.ContainsFunc(normalized.Channels, func(existing model.AggregationTierChannel) bool { return existing.ChannelId == entry.ChannelId }) {
				return config, nil, common.NewMessage("Channel #{{id}} is listed twice in tier {{tier}}", map[string]any{"id": entry.ChannelId, "tier": tier})
			}
			if entry.Weight < 0 || entry.Weight > serviceTierMaxWeight {
				return config, nil, common.NewMessage("Channel weight must be between 0 and 10000")
			}
			if entry.Priority < -serviceTierMaxPriority || entry.Priority > serviceTierMaxPriority {
				return config, nil, common.NewMessage("Channel priority must be between -1000000 and 1000000")
			}
			supported := serviceTierSupportedMembers(channel, members)
			if input.ModelName != "" && !slices.Contains(supported, input.ModelName) {
				return config, nil, common.NewMessage("Channel #{{id}} {{name}} does not support model {{model}}", map[string]any{"id": channel.Id, "name": channel.Name, "model": input.ModelName})
			}
			if input.ModelName == "" && len(supported) == 0 {
				return config, nil, common.NewMessage("Channel #{{id}} {{name}} supports none of the models in this family", map[string]any{"id": channel.Id, "name": channel.Name})
			}
			if entry.Enabled {
				enabledChannels++
			}
			normalized.Channels = append(normalized.Channels, entry)
		}
		slices.SortStableFunc(normalized.Channels, func(a, b model.AggregationTierChannel) int {
			if a.Priority != b.Priority {
				if a.Priority > b.Priority {
					return -1
				}
				return 1
			}
			return b.Weight - a.Weight
		})
		if normalized.Enabled {
			enabledTiers++
			if input.Enabled && enabledChannels == 0 {
				return config, nil, common.NewMessage("Enabled tier {{tier}} needs at least one enabled channel", map[string]any{"tier": tier})
			}
			if previousTier != "" && normalized.Ratio < previousRatio {
				return config, nil, common.NewMessage("Tier ratios must not decrease from economy to performance ({{tier}} is cheaper than {{previous}})", map[string]any{"tier": tier, "previous": previousTier})
			}
			previousRatio, previousTier = normalized.Ratio, tier
			if input.ModelName == "" {
				uncovered := slices.DeleteFunc(slices.Clone(members), func(member string) bool {
					return slices.ContainsFunc(normalized.Channels, func(entry model.AggregationTierChannel) bool {
						return entry.Enabled && slices.Contains(serviceTierSupportedMembers(channels[entry.ChannelId], []string{member}), member)
					})
				})
				if len(uncovered) > 0 {
					warnings = append(warnings, common.NewMessage("No channel in tier {{tier}} serves: {{models}}", map[string]any{"tier": tier, "models": strings.Join(uncovered, ", ")}))
				}
			}
		}
		config.Tiers[tier] = normalized
	}
	if input.Enabled && enabledTiers == 0 {
		return config, nil, common.NewMessage("Enable at least one tier before enabling the rule")
	}

	strategy := &config.Strategy
	if strategy.MaxAttempts < 1 || strategy.MaxAttempts > serviceTierMaxAttempts {
		return config, nil, common.NewMessage("Max attempts must be between 1 and 10")
	}
	if strategy.MaxTotalSeconds < 0 || strategy.MaxTotalSeconds > serviceTierMaxTotalSeconds {
		return config, nil, common.NewMessage("Max total duration must be between 0 and 3600 seconds")
	}
	retryOn := make([]string, 0, len(ServiceTierRelayableErrors))
	for _, category := range strategy.RetryOn {
		if !slices.Contains(ServiceTierRelayableErrors, category) {
			return config, nil, common.NewMessage("Unknown error category: {{category}}", map[string]any{"category": category})
		}
		if !slices.Contains(retryOn, category) {
			retryOn = append(retryOn, category)
		}
	}
	strategy.RetryOn = retryOn
	if strategy.CooldownFailures < 0 || strategy.CooldownFailures > serviceTierMaxCooldownCount {
		return config, nil, common.NewMessage("Cooldown failure count must be between 0 and 20")
	}
	if strategy.CooldownSeconds < 0 || strategy.CooldownSeconds > serviceTierMaxTotalSeconds || strategy.CooldownFailures > 0 && strategy.CooldownSeconds == 0 {
		return config, nil, common.NewMessage("Cooldown duration must be between 1 and 3600 seconds")
	}
	groups := make([]string, 0, len(strategy.UserGroups))
	for _, group := range strategy.UserGroups {
		group = strings.TrimSpace(group)
		if group == "" || slices.Contains(groups, group) {
			continue
		}
		if !ratio_setting.ContainsGroupRatio(group) {
			return config, nil, common.NewMessage("User group {{group}} does not exist", map[string]any{"group": group})
		}
		groups = append(groups, group)
	}
	strategy.UserGroups = groups
	return config, warnings, nil
}

// serviceTierSupportedMembers returns the members the channel's model list
// serves, matching routing-normalized names as channel selection does.
func serviceTierSupportedMembers(channel *model.Channel, members []string) []string {
	supported := make([]string, 0, len(members))
	if channel == nil {
		return supported
	}
	models := channel.GetModels()
	for i := range models {
		models[i] = strings.TrimSpace(models[i])
	}
	for _, member := range members {
		if slices.Contains(models, member) {
			supported = append(supported, member)
			continue
		}
		if normalized := ratio_setting.RoutingMatchModelName(member); normalized != member && slices.Contains(models, normalized) {
			supported = append(supported, member)
		}
	}
	return supported
}

// SaveServiceTierPolicy validates and stores a policy, then refreshes the
// routing snapshot on this node.
func SaveServiceTierPolicy(input ServiceTierPolicyInput, operatorID int) (*model.AggregationPolicy, []*common.Message, error) {
	store, err := model.LoadServiceTierStore()
	if err != nil {
		return nil, nil, err
	}
	channels, err := loadServiceTierChannels()
	if err != nil {
		return nil, nil, err
	}
	config, warnings, err := NormalizeServiceTierPolicy(input, store, channels)
	if err != nil {
		return nil, nil, err
	}
	policy := &model.AggregationPolicy{FamilyId: input.FamilyID, ModelName: input.ModelName, Enabled: input.Enabled, UpdatedBy: operatorID}
	if err := policy.SetConfig(config); err != nil {
		return nil, nil, err
	}
	if err := model.SaveAggregationPolicy(policy, input.Version); err != nil {
		return nil, nil, err
	}
	if err := RefreshServiceTierCache(); err != nil {
		common.SysError(common.LogText("failed to refresh channel aggregation config: %s", err.Error()))
	}
	return policy, warnings, nil
}

// loadServiceTierChannels reads every channel without its key.
func loadServiceTierChannels() (map[int]*model.Channel, error) {
	var channels []*model.Channel
	if err := model.DB.Omit("key").Find(&channels).Error; err != nil {
		return nil, err
	}
	result := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		result[channel.Id] = channel
	}
	return result, nil
}
