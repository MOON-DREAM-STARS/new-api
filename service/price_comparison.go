package service

import (
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// The price comparison page lists the lowest price of the popular models of
// each model family next to a baseline model, so users can read "0.40X of
// <baseline>" at a glance. The administrator chooses the models, the baseline
// and the display rules; every logged-in user may read the page.

const (
	PriceBasisPeak    = "peak"
	PriceBasisOffPeak = "off_peak"

	PriceCurrencyUSD = "USD"
	PriceCurrencyCNY = "CNY"

	PriceFormatMultiplier = "multiplier"
	PriceFormatPercent    = "percent"

	DefaultPriceComparisonBaseline = "deepseek-v4.1-flash"

	priceComparisonMaxModelsPerFamily = 10
	priceComparisonMaxModelNameLength = 255
)

type PriceComparisonFamilyPick struct {
	FamilyID int      `json:"family_id"`
	Models   []string `json:"models"`
}

// PriceComparisonSettings is the administrator configuration of the page.
type PriceComparisonSettings struct {
	// BaselineModel is the model every price is compared with. Its own list
	// price is used, without service tier or group discounts.
	BaselineModel string `json:"baseline_model"`
	// PriceBasis picks the peak or the off-peak price of models that charge
	// by time of day; other models have one price.
	PriceBasis  string `json:"price_basis"`
	Currency    string `json:"currency"`
	RatioFormat string `json:"ratio_format"`
	// Families lists the chosen models, in display order, per model family.
	Families []PriceComparisonFamilyPick `json:"families"`
}

func DefaultPriceComparisonSettings() PriceComparisonSettings {
	return PriceComparisonSettings{
		BaselineModel: DefaultPriceComparisonBaseline,
		PriceBasis:    PriceBasisPeak,
		Currency:      PriceCurrencyUSD,
		RatioFormat:   PriceFormatMultiplier,
		Families:      []PriceComparisonFamilyPick{},
	}
}

func validPriceBasis(value string) bool {
	return value == PriceBasisPeak || value == PriceBasisOffPeak
}

func validPriceCurrency(value string) bool {
	return value == PriceCurrencyUSD || value == PriceCurrencyCNY
}

func validPriceFormat(value string) bool {
	return value == PriceFormatMultiplier || value == PriceFormatPercent
}

// GetPriceComparisonSettings returns the stored settings. A missing or damaged
// value falls back to the defaults field by field, so the page keeps working.
func GetPriceComparisonSettings() PriceComparisonSettings {
	settings := DefaultPriceComparisonSettings()
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[model.PriceComparisonOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if strings.TrimSpace(raw) == "" {
		return settings
	}
	var stored PriceComparisonSettings
	if err := common.UnmarshalJsonStr(raw, &stored); err != nil {
		common.SysError(common.LogText("invalid price comparison settings: %s", err.Error()))
		return settings
	}
	if baseline := strings.TrimSpace(stored.BaselineModel); baseline != "" {
		settings.BaselineModel = baseline
	}
	if validPriceBasis(stored.PriceBasis) {
		settings.PriceBasis = stored.PriceBasis
	}
	if validPriceCurrency(stored.Currency) {
		settings.Currency = stored.Currency
	}
	if validPriceFormat(stored.RatioFormat) {
		settings.RatioFormat = stored.RatioFormat
	}
	for _, pick := range stored.Families {
		if pick.FamilyID > 0 && len(pick.Models) > 0 {
			settings.Families = append(settings.Families, pick)
		}
	}
	return settings
}

// NormalizePriceComparisonSettings validates the administrator input and
// returns its canonical form. Errors are web console messages.
func NormalizePriceComparisonSettings(input PriceComparisonSettings) (PriceComparisonSettings, error) {
	result := PriceComparisonSettings{Families: []PriceComparisonFamilyPick{}}
	result.BaselineModel = strings.TrimSpace(input.BaselineModel)
	if result.BaselineModel == "" {
		return result, common.NewMessage("Choose a baseline model")
	}
	if len(result.BaselineModel) > priceComparisonMaxModelNameLength {
		return result, common.NewMessage("The model name is too long")
	}
	if !modelHasPricing(result.BaselineModel) {
		return result, common.NewMessage("The baseline model has no price. Choose a model with a price")
	}
	if !validPriceBasis(input.PriceBasis) || !validPriceCurrency(input.Currency) || !validPriceFormat(input.RatioFormat) {
		return result, common.NewMessage("Invalid price display option")
	}
	result.PriceBasis, result.Currency, result.RatioFormat = input.PriceBasis, input.Currency, input.RatioFormat

	snapshot, err := currentServiceTierSnapshot()
	if err != nil {
		return result, err
	}
	picks := make(map[int][]string)
	for _, pick := range input.Families {
		if _, duplicated := picks[pick.FamilyID]; duplicated {
			return result, common.NewMessage("Each model family can be listed only once")
		}
		if snapshot == nil || snapshot.familyByID[pick.FamilyID] == nil {
			return result, common.NewMessage("A selected model family no longer exists. Reload the page and try again.")
		}
		if len(pick.Models) > priceComparisonMaxModelsPerFamily {
			return result, common.NewMessage("Choose at most {{max}} models per model family", map[string]any{"max": priceComparisonMaxModelsPerFamily})
		}
		names := make([]string, 0, len(pick.Models))
		for _, name := range pick.Models {
			name = strings.TrimSpace(name)
			if name == "" || slices.Contains(names, name) {
				continue
			}
			if snapshot.familyIDOf(name) != pick.FamilyID {
				return result, common.NewMessage("{{model}} is not a member of this model family", map[string]any{"model": name})
			}
			names = append(names, name)
		}
		picks[pick.FamilyID] = names
	}
	if snapshot != nil {
		for _, family := range snapshot.families {
			if names := picks[family.Id]; len(names) > 0 {
				result.Families = append(result.Families, PriceComparisonFamilyPick{FamilyID: family.Id, Models: names})
			}
		}
	}
	return result, nil
}

// SavePriceComparisonSettings stores the normalized settings.
func SavePriceComparisonSettings(input PriceComparisonSettings) (PriceComparisonSettings, error) {
	settings, err := NormalizePriceComparisonSettings(input)
	if err != nil {
		return settings, err
	}
	encoded, err := common.Marshal(settings)
	if err != nil {
		return settings, err
	}
	if err := model.SaveOption(model.PriceComparisonOptionKey, string(encoded)); err != nil {
		return settings, err
	}
	return settings, nil
}

// PriceComparisonPrice is the configuration the browser needs to read the list
// price of a model: ratios for ratio billing, an expression for tiered billing.
// Request-priced models are shown as not comparable.
type PriceComparisonPrice struct {
	Priced          bool    `json:"priced"`
	QuotaType       int     `json:"quota_type"`
	ModelRatio      float64 `json:"model_ratio"`
	ModelPrice      float64 `json:"model_price"`
	CompletionRatio float64 `json:"completion_ratio"`
	BillingMode     string  `json:"billing_mode,omitempty"`
	BillingExpr     string  `json:"billing_expr,omitempty"`
}

// modelHasPricing reports whether the model has a price of its own: a fixed
// price, a model ratio, or a billing expression. The self-use fallback ratio
// does not count.
func modelHasPricing(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if ratio_setting.HasConfiguredModelRatio(modelName) {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expression, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expression) != ""
}

// priceConfigOf reads the pricing of one model the way the pricing catalog does.
func priceConfigOf(modelName string) PriceComparisonPrice {
	price := PriceComparisonPrice{Priced: modelHasPricing(modelName)}
	if modelPrice, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		price.QuotaType = 1
		price.ModelPrice = modelPrice
	} else {
		price.ModelRatio, _, _ = ratio_setting.GetModelRatio(modelName)
		price.CompletionRatio = ratio_setting.GetCompletionRatio(modelName)
	}
	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		if expression, ok := billing_setting.GetBillingExpr(modelName); ok && strings.TrimSpace(expression) != "" {
			price.BillingMode = billing_setting.BillingModeTieredExpr
			price.BillingExpr = expression
		}
	}
	return price
}

func priceComparisonExchangeRate() float64 {
	if rate := operation_setting.USDExchangeRate; rate > 0 {
		return rate
	}
	return 1
}

type PriceComparisonModel struct {
	ModelName string               `json:"model_name"`
	Price     PriceComparisonPrice `json:"price"`
	// PriceRatio multiplies the list price for this user: the ratio of the
	// lowest service tier the model offers, or the lowest group ratio.
	PriceRatio float64 `json:"price_ratio"`
	// Tier names the service tier behind PriceRatio, empty for plain routing.
	Tier string `json:"tier,omitempty"`
}

type PriceComparisonFamily struct {
	Id     int                    `json:"id"`
	Key    string                 `json:"key"`
	Name   string                 `json:"name"`
	Models []PriceComparisonModel `json:"models"`
}

type PriceComparisonView struct {
	Settings        PriceComparisonSettings `json:"settings"`
	USDExchangeRate float64                 `json:"usd_exchange_rate"`
	Baseline        PriceComparisonModel    `json:"baseline"`
	Families        []PriceComparisonFamily `json:"families"`
}

// lowestOfferedTier returns the cheapest service tier that the aggregation
// policy of the model offers to the user group. A tier ratio replaces the group
// ratio when a request is billed, so the tier ratio is the price ratio.
func (s *serviceTierSnapshot) lowestOfferedTier(modelName string, userGroup string) (string, float64, bool) {
	family, policy, _ := s.resolve(modelName)
	if family == nil || policy == nil || !policy.Record.Enabled {
		return "", 0, false
	}
	if groups := policy.Config.Strategy.UserGroups; len(groups) > 0 && !slices.Contains(groups, userGroup) {
		return "", 0, false
	}
	for _, tier := range model.ServiceTierKeys {
		if config := policy.Config.Tiers[tier]; config.Enabled && config.Ratio > 0 {
			return tier, config.Ratio, true
		}
	}
	return "", 0, false
}

// lowestGroupRatio is the best group ratio among the groups of the user that
// offer the model, and 1 when no group does. A group that costs nothing is no
// price to compare, so it is skipped.
func lowestGroupRatio(userGroup string, modelName string, usable map[string]string) float64 {
	best := math.Inf(1)
	for group := range usable {
		if group == "auto" || !model.GroupHasEnabledModel(group, modelName) {
			continue
		}
		if ratio := GetUserGroupRatio(userGroup, group); ratio > 0 {
			best = math.Min(best, ratio)
		}
	}
	if math.IsInf(best, 1) {
		return 1
	}
	return best
}

// GetPriceComparisonView builds the page for one user. Only models that the
// user can use are listed, and no group or channel information leaves the server.
func GetPriceComparisonView(userGroup string) (*PriceComparisonView, error) {
	snapshot, err := currentServiceTierSnapshot()
	if err != nil {
		return nil, err
	}
	settings := GetPriceComparisonSettings()
	view := &PriceComparisonView{
		Settings:        settings,
		USDExchangeRate: priceComparisonExchangeRate(),
		Baseline:        PriceComparisonModel{ModelName: settings.BaselineModel, Price: priceConfigOf(settings.BaselineModel), PriceRatio: 1},
		Families:        []PriceComparisonFamily{},
	}
	if snapshot == nil {
		return view, nil
	}

	usable := GetUserUsableGroups(userGroup)
	groups := make([]string, 0, len(usable))
	for group := range usable {
		groups = append(groups, group)
	}
	slices.Sort(groups)
	enabled := make(map[string]struct{})
	for _, name := range GetGroupsEnabledModels(groups) {
		enabled[name] = struct{}{}
	}
	picks := make(map[int][]string, len(settings.Families))
	for _, pick := range settings.Families {
		picks[pick.FamilyID] = pick.Models
	}

	for _, family := range snapshot.families {
		item := PriceComparisonFamily{Id: family.Id, Key: family.Key, Name: family.Name, Models: []PriceComparisonModel{}}
		for _, name := range picks[family.Id] {
			if _, ok := enabled[name]; !ok || snapshot.familyIDOf(name) != family.Id {
				continue
			}
			price := priceConfigOf(name)
			if !price.Priced {
				continue
			}
			entry := PriceComparisonModel{ModelName: name, Price: price}
			if tier, ratio, ok := snapshot.lowestOfferedTier(name, userGroup); ok {
				entry.Tier, entry.PriceRatio = tier, ratio
			} else {
				entry.PriceRatio = lowestGroupRatio(userGroup, name, usable)
			}
			item.Models = append(item.Models, entry)
		}
		if len(item.Models) > 0 {
			view.Families = append(view.Families, item)
		}
	}
	return view, nil
}

type PriceComparisonAdminFamily struct {
	Id   int    `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type PriceComparisonAdminModel struct {
	ModelName string               `json:"model_name"`
	Price     PriceComparisonPrice `json:"price"`
	// Enabled is true when at least one group offers the model.
	Enabled bool `json:"enabled"`
	// FamilyID is 0 for models that belong to no family.
	FamilyID int `json:"family_id"`
}

type PriceComparisonAdminView struct {
	Settings        PriceComparisonSettings      `json:"settings"`
	USDExchangeRate float64                      `json:"usd_exchange_rate"`
	Families        []PriceComparisonAdminFamily `json:"families"`
	Models          []PriceComparisonAdminModel  `json:"models"`
}

// GetPriceComparisonAdminView lists everything the administrator can choose
// from: the models that some group offers and every member of every model family.
func GetPriceComparisonAdminView() (*PriceComparisonAdminView, error) {
	snapshot, err := currentServiceTierSnapshot()
	if err != nil {
		return nil, err
	}
	view := &PriceComparisonAdminView{
		Settings:        GetPriceComparisonSettings(),
		USDExchangeRate: priceComparisonExchangeRate(),
		Families:        []PriceComparisonAdminFamily{},
		Models:          []PriceComparisonAdminModel{},
	}
	enabled := make(map[string]struct{})
	names := make(map[string]struct{})
	for _, pricing := range model.GetPricing() {
		enabled[pricing.ModelName] = struct{}{}
		names[pricing.ModelName] = struct{}{}
	}
	if snapshot != nil {
		for _, family := range snapshot.families {
			view.Families = append(view.Families, PriceComparisonAdminFamily{Id: family.Id, Key: family.Key, Name: family.Name})
		}
		for name := range snapshot.memberFamily {
			names[name] = struct{}{}
		}
	}
	names[view.Settings.BaselineModel] = struct{}{}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	slices.Sort(sorted)
	for _, name := range sorted {
		_, isEnabled := enabled[name]
		familyID := 0
		if snapshot != nil {
			familyID = snapshot.familyIDOf(name)
		}
		view.Models = append(view.Models, PriceComparisonAdminModel{
			ModelName: name, Price: priceConfigOf(name), Enabled: isEnabled, FamilyID: familyID,
		})
	}
	return view, nil
}
