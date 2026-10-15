package service

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Views for the administrator's channel aggregation page and the user's
// service tier page. Administrator views carry channel details; user views
// carry only tiers, ratios and family names.

type ServiceTierFamilyView struct {
	Id             int      `json:"id"`
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	SortOrder      int      `json:"sort_order"`
	MatchPatterns  []string `json:"match_patterns"`
	Version        int64    `json:"version"`
	Members        []string `json:"members"`
	DefaultEnabled bool     `json:"default_enabled"`
	OverrideCount  int      `json:"override_count"`
}

type ServiceTierModelView struct {
	Name     string `json:"name"`
	FamilyID int    `json:"family_id"`
}

type ServiceTierOverview struct {
	Tiers           []string                `json:"tiers"`
	ErrorCategories []string                `json:"error_categories"`
	Families        []ServiceTierFamilyView `json:"families"`
	Models          []ServiceTierModelView  `json:"models"`
	Groups          []string                `json:"groups"`
}

type ServiceTierPolicyView struct {
	Id        int                           `json:"id"`
	FamilyID  int                           `json:"family_id"`
	ModelName string                        `json:"model_name"`
	Enabled   bool                          `json:"enabled"`
	Version   int64                         `json:"version"`
	UpdatedAt int64                         `json:"updated_at"`
	UpdatedBy int                           `json:"updated_by"`
	Config    model.AggregationPolicyConfig `json:"config"`
}

type ServiceTierChannelView struct {
	Id               int                      `json:"id"`
	Name             string                   `json:"name"`
	Type             int                      `json:"type"`
	Status           int                      `json:"status"`
	Group            string                   `json:"group"`
	Tag              string                   `json:"tag,omitempty"`
	ResponseTime     int                      `json:"response_time"`
	TestTime         int64                    `json:"test_time"`
	StatusReason     string                   `json:"status_reason,omitempty"`
	StatusTime       int64                    `json:"status_time,omitempty"`
	SupportedMembers int                      `json:"supported_members"`
	SupportsModel    bool                     `json:"supports_model"`
	Health           ServiceTierChannelHealth `json:"health"`
}

type ServiceTierFamilyDetail struct {
	Family    ServiceTierFamilyView         `json:"family"`
	Default   *ServiceTierPolicyView        `json:"default"`
	Overrides []ServiceTierPolicyView       `json:"overrides"`
	Template  model.AggregationPolicyConfig `json:"template"`
	Channels  []ServiceTierChannelView      `json:"channels"`
}

func serviceTierFamilyViews(store *model.ServiceTierStore) []ServiceTierFamilyView {
	views := make([]ServiceTierFamilyView, 0, len(store.Families))
	for _, family := range store.Families {
		view := ServiceTierFamilyView{
			Id: family.Id, Key: family.Key, Name: family.Name, Description: family.Description,
			SortOrder: family.SortOrder, MatchPatterns: family.GetMatchPatterns(), Version: family.Version,
			Members: make([]string, 0),
		}
		for _, member := range store.Members {
			if member.FamilyId == family.Id {
				view.Members = append(view.Members, member.ModelName)
			}
		}
		slices.Sort(view.Members)
		for _, policy := range store.Policies {
			if policy.FamilyId != family.Id {
				continue
			}
			if policy.ModelName == "" {
				view.DefaultEnabled = policy.Enabled
			} else if slices.Contains(view.Members, policy.ModelName) {
				view.OverrideCount++
			}
		}
		views = append(views, view)
	}
	return views
}

// GetServiceTierOverview lists families, every known model name and the
// user groups a policy can be limited to.
func GetServiceTierOverview() (*ServiceTierOverview, error) {
	store, err := model.LoadServiceTierStore()
	if err != nil {
		return nil, err
	}
	channels, err := loadServiceTierChannels()
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{})
	for _, channel := range channels {
		for _, name := range channel.GetModels() {
			if name = strings.TrimSpace(name); name != "" {
				names[name] = struct{}{}
			}
		}
	}
	var metadataNames []string
	if err := model.DB.Model(&model.Model{}).Pluck("model_name", &metadataNames).Error; err != nil {
		return nil, err
	}
	for _, name := range metadataNames {
		if name = strings.TrimSpace(name); name != "" {
			names[name] = struct{}{}
		}
	}
	memberFamily := make(map[string]int, len(store.Members))
	for _, member := range store.Members {
		memberFamily[member.ModelName] = member.FamilyId
		names[member.ModelName] = struct{}{}
	}
	models := make([]ServiceTierModelView, 0, len(names))
	for name := range names {
		models = append(models, ServiceTierModelView{Name: name, FamilyID: memberFamily[name]})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	groups := make([]string, 0)
	for group := range ratio_setting.GetGroupRatioCopy() {
		groups = append(groups, group)
	}
	slices.Sort(groups)
	return &ServiceTierOverview{
		Tiers:           slices.Clone(model.ServiceTierKeys),
		ErrorCategories: slices.Clone(ServiceTierRelayableErrors),
		Families:        serviceTierFamilyViews(store),
		Models:          models,
		Groups:          groups,
	}, nil
}

func serviceTierChannelView(channel *model.Channel, members []string, modelName string, now time.Time) ServiceTierChannelView {
	view := ServiceTierChannelView{
		Id: channel.Id, Name: channel.Name, Type: channel.Type, Status: channel.Status, Group: channel.Group,
		Tag: channel.GetTag(), ResponseTime: channel.ResponseTime, TestTime: channel.TestTime,
		Health: serviceTierHealth.view(channel.Id, now),
	}
	if channel.Status != common.ChannelStatusEnabled {
		info := channel.GetOtherInfo()
		if reason, ok := info["status_reason"].(string); ok {
			view.StatusReason = reason
		}
		if at, ok := info["status_time"].(float64); ok {
			view.StatusTime = int64(at)
		}
	}
	supported := serviceTierSupportedMembers(channel, members)
	view.SupportedMembers = len(supported)
	view.SupportsModel = modelName != "" && slices.Contains(supported, modelName)
	return view
}

// GetServiceTierFamilyDetail returns a family's policies and the channels
// they reference.
func GetServiceTierFamilyDetail(familyID int) (*ServiceTierFamilyDetail, error) {
	store, err := model.LoadServiceTierStore()
	if err != nil {
		return nil, err
	}
	var detail *ServiceTierFamilyDetail
	for _, view := range serviceTierFamilyViews(store) {
		if view.Id == familyID {
			detail = &ServiceTierFamilyDetail{Family: view, Overrides: make([]ServiceTierPolicyView, 0), Template: model.DefaultAggregationPolicyConfig(nil)}
		}
	}
	if detail == nil {
		return nil, model.ErrServiceTierNotFound
	}
	referenced := make(map[int]struct{})
	for _, policy := range store.Policies {
		if policy.FamilyId != familyID {
			continue
		}
		if policy.ModelName != "" && !slices.Contains(detail.Family.Members, policy.ModelName) {
			continue
		}
		config, err := policy.GetConfig()
		if err != nil {
			return nil, err
		}
		view := ServiceTierPolicyView{Id: policy.Id, FamilyID: policy.FamilyId, ModelName: policy.ModelName, Enabled: policy.Enabled, Version: policy.Version, UpdatedAt: policy.UpdatedAt, UpdatedBy: policy.UpdatedBy, Config: config}
		for _, tier := range config.Tiers {
			for _, entry := range tier.Channels {
				referenced[entry.ChannelId] = struct{}{}
			}
		}
		if policy.ModelName == "" {
			detail.Default = &view
		} else {
			detail.Overrides = append(detail.Overrides, view)
		}
	}
	channels, err := loadServiceTierChannels()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	detail.Channels = make([]ServiceTierChannelView, 0, len(referenced))
	for id := range referenced {
		if channel, ok := channels[id]; ok {
			detail.Channels = append(detail.Channels, serviceTierChannelView(channel, detail.Family.Members, "", now))
		}
	}
	sort.Slice(detail.Channels, func(i, j int) bool { return detail.Channels[i].Id < detail.Channels[j].Id })
	return detail, nil
}

// ListServiceTierChannelOptions lists the channels an administrator can add
// to a tier, with how many family members (or whether the override's model)
// each one serves.
func ListServiceTierChannelOptions(familyID int, modelName string) ([]ServiceTierChannelView, error) {
	store, err := model.LoadServiceTierStore()
	if err != nil {
		return nil, err
	}
	members := make([]string, 0)
	for _, member := range store.Members {
		if member.FamilyId == familyID {
			members = append(members, member.ModelName)
		}
	}
	if modelName != "" && !slices.Contains(members, modelName) {
		members = append(members, modelName)
	}
	channels, err := loadServiceTierChannels()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	views := make([]ServiceTierChannelView, 0, len(channels))
	for _, channel := range channels {
		views = append(views, serviceTierChannelView(channel, members, modelName, now))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Id < views[j].Id })
	return views, nil
}

var serviceTierFamilyKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ServiceTierFamilyInput is an administrator's family edit.
type ServiceTierFamilyInput struct {
	Key           string   `json:"key"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	SortOrder     int      `json:"sort_order"`
	MatchPatterns []string `json:"match_patterns"`
	Version       int64    `json:"version"`
}

// NormalizeServiceTierFamily validates a family edit into a record.
func NormalizeServiceTierFamily(input ServiceTierFamilyInput) (*model.ModelFamily, error) {
	family := &model.ModelFamily{
		Key:         strings.ToLower(strings.TrimSpace(input.Key)),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		SortOrder:   input.SortOrder,
	}
	if !serviceTierFamilyKeyPattern.MatchString(family.Key) {
		return nil, common.NewMessage("The family key may contain only lowercase letters, digits, hyphens and underscores")
	}
	if family.Name == "" || len([]rune(family.Name)) > 128 {
		return nil, common.NewMessage("The family name must be 1 to 128 characters")
	}
	if len([]rune(family.Description)) > 255 {
		return nil, common.NewMessage("The description must be at most 255 characters")
	}
	patterns := make([]string, 0, len(input.MatchPatterns))
	for _, pattern := range input.MatchPatterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" || slices.Contains(patterns, pattern) {
			continue
		}
		if len(pattern) > 64 {
			return nil, common.NewMessage("A match pattern must be at most 64 characters")
		}
		patterns = append(patterns, pattern)
	}
	if len(patterns) > 20 {
		return nil, common.NewMessage("A family can have at most 20 match patterns")
	}
	if err := family.SetMatchPatterns(patterns); err != nil {
		return nil, err
	}
	return family, nil
}

// NormalizeServiceTierMembers cleans a member list.
func NormalizeServiceTierMembers(models []string) ([]string, error) {
	result := make([]string, 0, len(models))
	for _, name := range models {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(result, name) {
			continue
		}
		if len(name) > 255 {
			return nil, common.NewMessage("A model name must be at most 255 characters")
		}
		result = append(result, name)
	}
	if len(result) > 2000 {
		return nil, common.NewMessage("A family can have at most 2000 models")
	}
	slices.Sort(result)
	return result, nil
}

// ---------------------------------------------------------------------------
// Route test
// ---------------------------------------------------------------------------

type ServiceTierSimulationInput struct {
	ModelName string              `json:"model_name"`
	UserGroup string              `json:"user_group"`
	Settings  ServiceTierSettings `json:"settings"`
	// Draft tests an unsaved policy for the model's family default or
	// override instead of the stored one.
	Draft *ServiceTierPolicyInput `json:"draft"`
}

type ServiceTierSimulationChannel struct {
	ChannelID   int     `json:"channel_id"`
	Name        string  `json:"name"`
	Priority    int64   `json:"priority"`
	Weight      int     `json:"weight"`
	Status      int     `json:"status"`
	Eligible    bool    `json:"eligible"`
	Reason      string  `json:"reason,omitempty"`
	Level       int     `json:"level,omitempty"`
	Probability float64 `json:"probability,omitempty"`
}

type ServiceTierSimulationStep struct {
	Tier       string                         `json:"tier"`
	Ratio      float64                        `json:"ratio"`
	Escalation bool                           `json:"escalation"`
	Channels   []ServiceTierSimulationChannel `json:"channels"`
}

type ServiceTierSimulation struct {
	ModelName     string                      `json:"model_name"`
	FamilyName    string                      `json:"family_name,omitempty"`
	PolicyScope   string                      `json:"policy_scope,omitempty"`
	Applies       bool                        `json:"applies"`
	Reason        string                      `json:"reason,omitempty"`
	Settings      ServiceTierSettings         `json:"settings"`
	PreferredTier string                      `json:"preferred_tier,omitempty"`
	CeilingRatio  float64                     `json:"ceiling_ratio,omitempty"`
	MaxAttempts   int                         `json:"max_attempts,omitempty"`
	Steps         []ServiceTierSimulationStep `json:"steps"`
}

// SimulateServiceTierRoute explains how a request would route without
// calling any upstream. It applies the same chain and channel eligibility
// rules as live routing, without request-specific filters, session affinity
// or channels failed during a request.
func SimulateServiceTierRoute(input ServiceTierSimulationInput) (*ServiceTierSimulation, error) {
	settings, err := NormalizeServiceTierSettings(input.Settings)
	if err != nil {
		return nil, err
	}
	result := &ServiceTierSimulation{ModelName: strings.TrimSpace(input.ModelName), Settings: settings, Steps: make([]ServiceTierSimulationStep, 0)}
	store, err := model.LoadServiceTierStore()
	if err != nil {
		return nil, err
	}
	snapshot := buildServiceTierSnapshot(store)
	family, policy, scope := snapshot.resolve(result.ModelName)
	if family == nil {
		result.Reason = "not_member"
		return result, nil
	}
	result.FamilyName = family.Name
	// A draft replaces the rule it edits: an override draft applies to its
	// model, a family default draft to models without an override.
	if draft := input.Draft; draft != nil && (draft.ModelName == result.ModelName || draft.ModelName == "" && scope != "model") {
		channels, err := loadServiceTierChannels()
		if err != nil {
			return nil, err
		}
		draft := *input.Draft
		draft.FamilyID = family.Id
		config, _, err := NormalizeServiceTierPolicy(draft, store, channels)
		if err != nil {
			return nil, err
		}
		policy = &serviceTierPolicy{Record: model.AggregationPolicy{FamilyId: family.Id, ModelName: draft.ModelName, Enabled: draft.Enabled}, Config: config}
		scope = "draft"
	}
	result.PolicyScope = scope
	if policy == nil || !policy.Record.Enabled {
		result.Reason = "policy_disabled"
		return result, nil
	}
	if groups := policy.Config.Strategy.UserGroups; len(groups) > 0 && !slices.Contains(groups, input.UserGroup) {
		result.Reason = "group_not_applicable"
		return result, nil
	}
	steps, preferred := BuildServiceTierChain(policy.Config, settings)
	result.PreferredTier = preferred
	result.MaxAttempts = policy.Config.Strategy.MaxAttempts
	if len(steps) == 0 {
		result.Reason = "no_tier"
		return result, nil
	}
	result.Applies = true
	result.CeilingRatio = serviceTierCeilingRatio(steps)
	channels, err := loadServiceTierChannels()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, step := range steps {
		simulated := ServiceTierSimulationStep{Tier: step.Tier, Ratio: step.Ratio, Escalation: step.Escalation, Channels: make([]ServiceTierSimulationChannel, 0)}
		for _, entry := range policy.Config.Tiers[step.Tier].Channels {
			item := ServiceTierSimulationChannel{ChannelID: entry.ChannelId, Priority: entry.Priority, Weight: entry.Weight}
			channel, exists := channels[entry.ChannelId]
			switch {
			case !exists:
				item.Reason = "channel_missing"
			case !entry.Enabled:
				item.Name, item.Status, item.Reason = channel.Name, channel.Status, "disabled_in_pool"
			case channel.Status != common.ChannelStatusEnabled:
				item.Name, item.Status, item.Reason = channel.Name, channel.Status, "channel_disabled"
			case len(serviceTierSupportedMembers(channel, []string{result.ModelName})) == 0:
				item.Name, item.Status, item.Reason = channel.Name, channel.Status, "model_unsupported"
			default:
				item.Name, item.Status, item.Eligible = channel.Name, channel.Status, true
				if serviceTierHealth.coolingDown(channel.Id, now) {
					item.Reason = "cooling_down"
				}
			}
			simulated.Channels = append(simulated.Channels, item)
		}
		// Eligible channels are tried by priority level; inside a level the
		// first pick is weighted. Probabilities describe that first pick.
		priorities := make([]int64, 0)
		for _, item := range simulated.Channels {
			if item.Eligible && !slices.Contains(priorities, item.Priority) {
				priorities = append(priorities, item.Priority)
			}
		}
		slices.SortFunc(priorities, func(a, b int64) int {
			if a > b {
				return -1
			}
			if a < b {
				return 1
			}
			return 0
		})
		for levelIndex, priority := range priorities {
			total, count := 0, 0
			for _, item := range simulated.Channels {
				if item.Eligible && item.Priority == priority {
					total += item.Weight
					count++
				}
			}
			for i := range simulated.Channels {
				item := &simulated.Channels[i]
				if !item.Eligible || item.Priority != priority {
					continue
				}
				item.Level = levelIndex + 1
				if total > 0 {
					item.Probability = float64(item.Weight) / float64(total)
				} else {
					item.Probability = 1 / float64(count)
				}
			}
		}
		result.Steps = append(result.Steps, simulated)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// User page
// ---------------------------------------------------------------------------

type ServiceTierFamilyPreview struct {
	Family           string             `json:"family"`
	OfferedTiers     map[string]float64 `json:"offered_tiers"`
	ModelOverrides   int                `json:"model_overrides"`
	PreferredTier    string             `json:"preferred_tier"`
	PreferredOffered bool               `json:"preferred_offered"`
	Steps            []ServiceTierStep  `json:"steps"`
	CeilingRatio     float64            `json:"ceiling_ratio"`
}

type ServiceTierPreview struct {
	// Chain is the tier order the settings produce when every tier is
	// offered; Families shows what each family actually offers.
	Chain    []ServiceTierStep          `json:"chain"`
	Families []ServiceTierFamilyPreview `json:"families"`
}

type ServiceTierUserOverview struct {
	Available bool                `json:"available"`
	Settings  ServiceTierSettings `json:"settings"`
	Preview   ServiceTierPreview  `json:"preview"`
}

// PreviewServiceTierSettings applies the routing chain rule to every family
// with an enabled default rule that serves userGroup.
func PreviewServiceTierSettings(settings ServiceTierSettings, userGroup string) ServiceTierPreview {
	generic := model.AggregationPolicyConfig{Tiers: make(map[string]model.AggregationTierConfig, len(model.ServiceTierKeys))}
	for _, tier := range model.ServiceTierKeys {
		generic.Tiers[tier] = model.AggregationTierConfig{Enabled: true, Ratio: 1}
	}
	chain, _ := BuildServiceTierChain(generic, settings)
	for i := range chain {
		chain[i].Ratio = 0
	}
	preview := ServiceTierPreview{Chain: chain, Families: make([]ServiceTierFamilyPreview, 0)}
	snapshot, err := currentServiceTierSnapshot()
	if err != nil || snapshot == nil {
		return preview
	}
	for _, family := range snapshot.families {
		policy := snapshot.defaults[family.Id]
		if policy == nil || !policy.Record.Enabled {
			continue
		}
		if groups := policy.Config.Strategy.UserGroups; len(groups) > 0 && !slices.Contains(groups, userGroup) {
			continue
		}
		item := ServiceTierFamilyPreview{Family: family.Name, OfferedTiers: make(map[string]float64)}
		for _, tier := range model.ServiceTierKeys {
			if tierConfig := policy.Config.Tiers[tier]; tierConfig.Enabled && tierConfig.Ratio > 0 {
				item.OfferedTiers[tier] = tierConfig.Ratio
			}
		}
		for _, override := range snapshot.overrides[family.Id] {
			if override.Record.Enabled {
				item.ModelOverrides++
			}
		}
		item.Steps, item.PreferredTier = BuildServiceTierChain(policy.Config, settings)
		_, item.PreferredOffered = item.OfferedTiers[item.PreferredTier]
		item.CeilingRatio = serviceTierCeilingRatio(item.Steps)
		preview.Families = append(preview.Families, item)
	}
	return preview
}

// GetServiceTierUserOverview returns the user's settings with their preview.
func GetServiceTierUserOverview(userID int, userGroup string) (*ServiceTierUserOverview, error) {
	settings, err := GetUserServiceTierSettings(userID)
	if err != nil {
		return nil, err
	}
	preview := PreviewServiceTierSettings(settings, userGroup)
	return &ServiceTierUserOverview{Available: len(preview.Families) > 0, Settings: settings, Preview: preview}, nil
}
