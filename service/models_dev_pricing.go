package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// The models.dev import writes the list prices of models.dev into the billing
// expressions of this site. It never overwrites a price that somebody set: a
// model with a price is locked until an administrator unlocks it, and only the
// prices that this import wrote itself (and nobody changed since) are updated.

const (
	ModelsDevSourceMapping  = "mapping"
	ModelsDevSourceSelected = "selected"
	ModelsDevSourceSite     = "site"
	ModelsDevSourceCommon   = "common"

	ModelsDevStatusNew       = "new"
	ModelsDevStatusUpdate    = "update"
	ModelsDevStatusUnchanged = "unchanged"
	ModelsDevStatusLocked    = "locked"
	ModelsDevStatusInvalid   = "invalid"

	ModelsDevLockExplicit   = "explicit"
	ModelsDevLockModified   = "modified"
	ModelsDevLockConfigured = "configured"
	// ModelsDevLockDefault marks a price that is only the built-in default.
	ModelsDevLockDefault = "default"

	// A proposal with a review reason is written only after an administrator
	// approved that model: a change by more than ten times, or the price of a
	// reseller instead of the vendor.
	ModelsDevReviewLargeChange = "large_change"
	ModelsDevReviewReseller    = "reseller"

	ModelsDevTriggerRelease = "release"
	ModelsDevTriggerManual  = "manual"

	modelsDevMaxKeyLength   = 300
	modelsDevMaxNameLength  = 255
	modelsDevMaxSelected    = 1000
	modelsDevMaxExcluded    = 2000
	modelsDevMaxMappings    = 1000
	modelsDevMaxLocks       = 2000
	modelsDevMaxBatchItems  = 300
	modelsDevMaxAutoRetries = 6
	// A change by more than this factor, up or down, needs a human decision.
	modelsDevLargeChangeFactor = 10.0
)

// These errors are web console messages, so the console shows them translated.
var (
	ErrModelsDevBusy        error = common.NewMessage("A models.dev sync is already running")
	ErrModelsDevStalePlan   error = common.NewMessage("The models.dev data or the prices changed since the preview. Preview again")
	ErrModelsDevNothingUndo error = common.NewMessage("There is no models.dev sync to undo")
	ErrModelsDevConflict    error = common.NewMessage("Model prices changed during the import. Preview again")
)

// ModelsDevConfig is what the administrator chose to import.
type ModelsDevConfig struct {
	// AutoSyncEnabled imports once for every new release of the software.
	AutoSyncEnabled bool `json:"auto_sync_enabled"`
	// IncludeSiteModels imports the models of this site that models.dev lists
	// under the same name.
	IncludeSiteModels bool `json:"include_site_models"`
	// IncludeCommonModels imports the recent models of the popular model
	// families, under their models.dev names.
	IncludeCommonModels bool `json:"include_common_models"`
	// SelectedKeys are models.dev entries chosen by hand.
	SelectedKeys []string `json:"selected_keys"`
	// ExcludedKeys are entries the automatic choices leave out.
	ExcludedKeys []string `json:"excluded_keys"`
	// Mappings tie a model of this site to the models.dev entry that prices it.
	Mappings map[string]string `json:"mappings"`
	// LockedModels never change; UnlockedModels may change although they have a
	// price.
	LockedModels   []string `json:"locked_models"`
	UnlockedModels []string `json:"unlocked_models"`
}

func DefaultModelsDevConfig() ModelsDevConfig {
	return ModelsDevConfig{
		IncludeSiteModels: true, IncludeCommonModels: true,
		SelectedKeys: []string{}, ExcludedKeys: []string{}, Mappings: map[string]string{},
		LockedModels: []string{}, UnlockedModels: []string{},
	}
}

// ModelsDevSummary counts what one import did.
type ModelsDevSummary struct {
	Matched       int `json:"matched"`
	Created       int `json:"created"`
	Updated       int `json:"updated"`
	Unchanged     int `json:"unchanged"`
	Locked        int `json:"locked"`
	SkippedReview int `json:"skipped_review"`
	Invalid       int `json:"invalid"`
	Unmatched     int `json:"unmatched"`
}

// ModelsDevManaged remembers a price that the import wrote. While the version
// of the model still equals Version nobody changed it since.
type ModelsDevManaged struct {
	Key      string  `json:"key"`
	Version  string  `json:"version"`
	Input    float64 `json:"input"`
	Output   float64 `json:"output"`
	SyncedAt int64   `json:"synced_at"`
}

type ModelsDevBatchItem struct {
	Model string `json:"model"`
	Key   string `json:"key"`
	// Before is everything configured for the model before the import.
	Before        model.PricingValues `json:"before"`
	AfterVersion  string              `json:"after_version"`
	ManagedBefore *ModelsDevManaged   `json:"managed_before,omitempty"`
}

// ModelsDevBatch is the latest import, kept to undo it.
type ModelsDevBatch struct {
	At        int64                `json:"at"`
	Trigger   string               `json:"trigger"`
	Version   string               `json:"version"`
	Items     []ModelsDevBatchItem `json:"items"`
	Truncated int                  `json:"truncated,omitempty"`
}

// ModelsDevState is the outcome of the imports. The importer writes it and
// nobody else, so a slow import cannot restore stale switches.
type ModelsDevState struct {
	LastSyncAt      int64                       `json:"last_sync_at"`
	LastSyncVersion string                      `json:"last_sync_version"`
	LastTrigger     string                      `json:"last_trigger"`
	LastSummary     ModelsDevSummary            `json:"last_summary"`
	LastError       string                      `json:"last_error,omitempty"`
	LastErrorAt     int64                       `json:"last_error_at,omitempty"`
	AttemptVersion  string                      `json:"attempt_version,omitempty"`
	Attempts        int                         `json:"attempts,omitempty"`
	Managed         map[string]ModelsDevManaged `json:"managed"`
	LastBatch       *ModelsDevBatch             `json:"last_batch,omitempty"`
}

func uniqueSortedStrings(values []string, maxLength int, limit int, tooMany string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(result, value) {
			continue
		}
		if len(value) > maxLength {
			return nil, common.NewMessage("The model name is too long")
		}
		result = append(result, value)
	}
	if len(result) > limit {
		return nil, common.NewMessage(tooMany)
	}
	sort.Strings(result)
	return result, nil
}

// NormalizeModelsDevConfig validates the input of an administrator and
// returns its canonical form.
func NormalizeModelsDevConfig(input ModelsDevConfig) (ModelsDevConfig, error) {
	config := ModelsDevConfig{
		AutoSyncEnabled: input.AutoSyncEnabled, IncludeSiteModels: input.IncludeSiteModels,
		IncludeCommonModels: input.IncludeCommonModels, Mappings: map[string]string{},
	}
	var err error
	if config.SelectedKeys, err = uniqueSortedStrings(input.SelectedKeys, modelsDevMaxKeyLength, modelsDevMaxSelected, "Too many selected models"); err != nil {
		return config, err
	}
	if config.ExcludedKeys, err = uniqueSortedStrings(input.ExcludedKeys, modelsDevMaxKeyLength, modelsDevMaxExcluded, "Too many excluded models"); err != nil {
		return config, err
	}
	if config.LockedModels, err = uniqueSortedStrings(input.LockedModels, modelsDevMaxNameLength, modelsDevMaxLocks, "Too many locked models"); err != nil {
		return config, err
	}
	if config.UnlockedModels, err = uniqueSortedStrings(input.UnlockedModels, modelsDevMaxNameLength, modelsDevMaxLocks, "Too many unlocked models"); err != nil {
		return config, err
	}
	for _, name := range config.LockedModels {
		if slices.Contains(config.UnlockedModels, name) {
			return config, common.NewMessage("{{model}} cannot be locked and unlocked at once", map[string]any{"model": name})
		}
	}
	if len(input.Mappings) > modelsDevMaxMappings {
		return config, common.NewMessage("Too many model mappings")
	}
	for name, key := range input.Mappings {
		name, key = strings.TrimSpace(name), strings.TrimSpace(key)
		if name == "" || key == "" {
			continue
		}
		if len(name) > modelsDevMaxNameLength || len(key) > modelsDevMaxKeyLength {
			return config, common.NewMessage("The model name is too long")
		}
		config.Mappings[name] = key
	}
	return config, nil
}

func decodeModelsDevConfig(raw string) ModelsDevConfig {
	config := DefaultModelsDevConfig()
	if strings.TrimSpace(raw) == "" {
		return config
	}
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		common.SysError(common.LogText("invalid models.dev config: %s", err.Error()))
		return DefaultModelsDevConfig()
	}
	if config.SelectedKeys == nil {
		config.SelectedKeys = []string{}
	}
	if config.ExcludedKeys == nil {
		config.ExcludedKeys = []string{}
	}
	if config.LockedModels == nil {
		config.LockedModels = []string{}
	}
	if config.UnlockedModels == nil {
		config.UnlockedModels = []string{}
	}
	if config.Mappings == nil {
		config.Mappings = map[string]string{}
	}
	return config
}

func decodeModelsDevState(raw string) ModelsDevState {
	var state ModelsDevState
	if strings.TrimSpace(raw) != "" {
		if err := common.UnmarshalJsonStr(raw, &state); err != nil {
			common.SysError(common.LogText("invalid models.dev state: %s", err.Error()))
			state = ModelsDevState{}
		}
	}
	if state.Managed == nil {
		state.Managed = map[string]ModelsDevManaged{}
	}
	return state
}

func cachedOption(key string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[key]
}

// GetModelsDevConfig reads the configuration from the database, the copy that
// every write starts from.
func GetModelsDevConfig() (ModelsDevConfig, error) {
	raw, _, err := model.GetOptionFromDB(model.ModelsDevConfigOptionKey)
	if err != nil {
		return DefaultModelsDevConfig(), err
	}
	return decodeModelsDevConfig(raw), nil
}

// GetModelsDevState reads the state from the database.
func GetModelsDevState() (ModelsDevState, error) {
	raw, _, err := model.GetOptionFromDB(model.ModelsDevStateOptionKey)
	if err != nil {
		return ModelsDevState{Managed: map[string]ModelsDevManaged{}}, err
	}
	return decodeModelsDevState(raw), nil
}

func saveModelsDevState(state ModelsDevState) error {
	encoded, err := common.Marshal(state)
	if err != nil {
		return err
	}
	return model.SaveOption(model.ModelsDevStateOptionKey, string(encoded))
}

// SaveModelsDevConfig stores the configuration an administrator edited.
func SaveModelsDevConfig(input ModelsDevConfig) (ModelsDevConfig, error) {
	config, err := NormalizeModelsDevConfig(input)
	if err != nil {
		return config, err
	}
	encoded, err := common.Marshal(config)
	if err != nil {
		return config, err
	}
	if err := model.SaveOption(model.ModelsDevConfigOptionKey, string(encoded)); err != nil {
		return config, err
	}
	return config, nil
}

// ModelsDevAutoSyncDue tells whether the scheduler should import now: the
// switch is on, and this version of the software has not imported yet. A
// failing import is tried again for a while, then left to the administrator.
// It reads the copy in memory, so the scheduler does not query the database.
func ModelsDevAutoSyncDue() bool {
	config := decodeModelsDevConfig(cachedOption(model.ModelsDevConfigOptionKey))
	if !config.AutoSyncEnabled {
		return false
	}
	state := decodeModelsDevState(cachedOption(model.ModelsDevStateOptionKey))
	if state.LastSyncVersion == common.Version {
		return false
	}
	return state.AttemptVersion != common.Version || state.Attempts < modelsDevMaxAutoRetries
}

// ModelsDevAutoSyncFirstTry tells whether no import was tried for this version
// yet. The scheduler does not make that first try wait for the interval that
// spaces the retries, so a release imports on its first start even when the
// release before it imported a few minutes ago.
func ModelsDevAutoSyncFirstTry() bool {
	state := decodeModelsDevState(cachedOption(model.ModelsDevStateOptionKey))
	return state.AttemptVersion != common.Version
}

// modelsDevSiteModels lists the models of this site: the ones some group
// offers and the members of the model families.
func modelsDevSiteModels() []string {
	names := make(map[string]struct{})
	for _, pricing := range model.GetPricing() {
		names[pricing.ModelName] = struct{}{}
	}
	if snapshot, err := currentServiceTierSnapshot(); err == nil && snapshot != nil {
		for name := range snapshot.memberFamily {
			names[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

type modelsDevTarget struct {
	model    string
	source   string
	priority int
	entry    ModelsDevEntry
	// missing is set when a mapping names an entry the catalog no longer has.
	missing string
}

func modelsDevSourcePriority(source string) int {
	switch source {
	case ModelsDevSourceMapping:
		return 4
	case ModelsDevSourceSelected:
		return 3
	case ModelsDevSourceSite:
		return 2
	default:
		return 1
	}
}

// resolveModelsDevTargets decides which entry prices which model name. A name
// that several choices reach keeps the strongest: a mapping, then a hand
// picked entry, then a model of this site, then a common model.
func resolveModelsDevTargets(catalog *ModelsDevCatalog, config ModelsDevConfig, siteModels []string) ([]modelsDevTarget, []string) {
	chosen := make(map[string]modelsDevTarget)
	offer := func(target modelsDevTarget) {
		target.priority = modelsDevSourcePriority(target.source)
		current, exists := chosen[target.model]
		if !exists || target.priority > current.priority ||
			(target.priority == current.priority && target.missing == "" && current.missing == "" && betterModelsDevEntry(target.entry, current.entry)) {
			chosen[target.model] = target
		}
	}
	excluded := make(map[string]bool, len(config.ExcludedKeys))
	for _, key := range config.ExcludedKeys {
		excluded[key] = true
	}
	if config.IncludeCommonModels {
		for _, entry := range catalog.Entries {
			if entry.Common && !excluded[entry.Key] {
				offer(modelsDevTarget{model: entry.NormalizedID, source: ModelsDevSourceCommon, entry: entry})
			}
		}
	}
	if config.IncludeSiteModels {
		for _, name := range siteModels {
			if entry, ok := catalog.Match(name); ok && !excluded[entry.Key] {
				offer(modelsDevTarget{model: name, source: ModelsDevSourceSite, entry: entry})
			}
		}
	}
	for _, key := range config.SelectedKeys {
		if entry, ok := catalog.Entry(key); ok {
			offer(modelsDevTarget{model: entry.NormalizedID, source: ModelsDevSourceSelected, entry: entry})
		}
	}
	for name, key := range config.Mappings {
		if entry, ok := catalog.Entry(key); ok {
			offer(modelsDevTarget{model: name, source: ModelsDevSourceMapping, entry: entry})
		} else {
			offer(modelsDevTarget{model: name, source: ModelsDevSourceMapping, missing: key})
		}
	}
	targets := make([]modelsDevTarget, 0, len(chosen))
	for _, target := range chosen {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].model < targets[j].model })
	var unmatched []string
	for _, name := range siteModels {
		if _, ok := chosen[name]; !ok {
			unmatched = append(unmatched, name)
		}
	}
	return targets, unmatched
}

var modelsDevRunMu sync.Mutex

// ModelsDevRow is the proposal for one model name.
type ModelsDevRow struct {
	Model        string   `json:"model"`
	Key          string   `json:"key,omitempty"`
	Source       string   `json:"source"`
	ProviderID   string   `json:"provider_id,omitempty"`
	ProviderName string   `json:"provider_name,omitempty"`
	Official     bool     `json:"official,omitempty"`
	Status       string   `json:"status"`
	LockReason   string   `json:"lock_reason,omitempty"`
	Review       string   `json:"review,omitempty"`
	Change       float64  `json:"change,omitempty"`
	Input        float64  `json:"input"`
	Output       float64  `json:"output"`
	CacheRead    *float64 `json:"cache_read,omitempty"`
	CacheWrite   *float64 `json:"cache_write,omitempty"`
	Tiers        int      `json:"tiers,omitempty"`
	Expression   string   `json:"expression,omitempty"`
	// Version is the pricing version the proposal was made against.
	Version string `json:"version"`
	// Current is the price the model has now.
	Current PriceComparisonPrice `json:"current"`
	Error   string               `json:"error,omitempty"`
}

// ModelsDevPlan is what an import would do.
type ModelsDevPlan struct {
	Hash        string           `json:"hash"`
	CatalogHash string           `json:"catalog_hash"`
	FetchedAt   int64            `json:"fetched_at"`
	Rows        []ModelsDevRow   `json:"rows"`
	Unmatched   []string         `json:"unmatched"`
	Summary     ModelsDevSummary `json:"summary"`

	configured map[string]model.PricingValues
}

// isBuiltinPrice tells whether everything configured for a model is the
// built-in default ratio of the software, which nobody changed.
func isBuiltinPrice(configured model.PricingValues, name string, defaults map[string]map[string]float64) bool {
	if len(configured) == 0 {
		return false
	}
	for key, value := range configured {
		number, isNumber := value.(float64)
		builtin, hasBuiltin := defaults[key][name]
		if !isNumber || !hasBuiltin || number != builtin {
			return false
		}
	}
	return true
}

func hasModelsDevPrice(configured model.PricingValues, name string) bool {
	if _, ok := configured["ModelPrice"]; ok {
		return true
	}
	if _, ok := configured["ModelRatio"]; ok {
		return true
	}
	if expression, _ := configured["billing_setting.billing_expr"].(string); strings.TrimSpace(expression) != "" {
		return true
	}
	if variants, _ := configured[billing_setting.PluginBillingExprOption].(map[string]any); len(variants) > 0 {
		return true
	}
	return false
}

// modelsDevLock decides whether the import must leave a model alone. An
// explicit choice of the administrator comes first. Then a price the import
// wrote and nobody changed may be rewritten, while a price somebody set, or
// changed after the import, is kept.
func modelsDevLock(name string, config ModelsDevConfig, state ModelsDevState, entry model.ModelPricingEntry, defaults map[string]map[string]float64) (bool, string) {
	if slices.Contains(config.LockedModels, name) {
		return true, ModelsDevLockExplicit
	}
	if slices.Contains(config.UnlockedModels, name) {
		return false, ""
	}
	if managed, ok := state.Managed[name]; ok {
		if managed.Version == entry.Version {
			return false, ""
		}
		return true, ModelsDevLockModified
	}
	if hasModelsDevPrice(entry.Configured, name) {
		if isBuiltinPrice(entry.Configured, name, defaults) {
			return true, ModelsDevLockDefault
		}
		return true, ModelsDevLockConfigured
	}
	if _, builtin := billing_setting.GetBuiltinBillingExpr(name); builtin {
		return true, ModelsDevLockDefault
	}
	return false, ""
}

func modelsDevRowRank(status string) int {
	switch status {
	case ModelsDevStatusNew:
		return 0
	case ModelsDevStatusUpdate:
		return 1
	case ModelsDevStatusLocked:
		return 2
	case ModelsDevStatusInvalid:
		return 3
	default:
		return 4
	}
}

func modelsDevRowFor(target modelsDevTarget, config ModelsDevConfig, state ModelsDevState, pricing model.ModelPricingEntry, defaults map[string]map[string]float64) ModelsDevRow {
	row := ModelsDevRow{
		Model: target.model, Source: target.source, Version: pricing.Version, Current: priceConfigOf(target.model),
	}
	if target.missing != "" {
		row.Key, row.Status = target.missing, ModelsDevStatusInvalid
		row.Error = "models.dev no longer lists this model"
		return row
	}
	entry := target.entry
	row.Key, row.ProviderID, row.ProviderName, row.Official = entry.Key, entry.ProviderID, entry.ProviderName, entry.Official
	row.Input, row.Output, row.CacheRead, row.CacheWrite, row.Tiers = entry.Input, entry.Output, entry.CacheRead, entry.CacheWrite, len(entry.Tiers)
	expression, err := BuildModelsDevExpression(entry, target.model)
	if err != nil {
		row.Status, row.Error = ModelsDevStatusInvalid, err.Error()
		return row
	}
	row.Expression = expression
	if locked, reason := modelsDevLock(target.model, config, state, pricing, defaults); locked {
		row.Status, row.LockReason = ModelsDevStatusLocked, reason
		return row
	}
	managed, wasImported := state.Managed[target.model]
	current, _ := pricing.Configured["billing_setting.billing_expr"].(string)
	switch {
	case wasImported && current == expression && pricing.Configured["billing_setting.billing_mode"] == "tiered_expr":
		row.Status = ModelsDevStatusUnchanged
	case wasImported:
		row.Status = ModelsDevStatusUpdate
	default:
		row.Status = ModelsDevStatusNew
	}
	if wasImported && managed.Input+managed.Output > 0 && row.Status != ModelsDevStatusUnchanged {
		before := (3*managed.Input + managed.Output) / 4
		row.Change = entry.BlendedPrice() / before
		if row.Change > modelsDevLargeChangeFactor || row.Change < 1/modelsDevLargeChangeFactor {
			row.Review = ModelsDevReviewLargeChange
		}
	}
	// A price from a reseller picked by name is cheaper than the vendor's
	// more often than not; a person decides whether to sell at it.
	if row.Review == "" && !entry.Official && (target.source == ModelsDevSourceSite || target.source == ModelsDevSourceCommon) {
		row.Review = ModelsDevReviewReseller
	}
	return row
}

func newModelsDevPlan(catalog *ModelsDevCatalog, config ModelsDevConfig, state ModelsDevState, siteModels []string) (*ModelsDevPlan, error) {
	targets, unmatched := resolveModelsDevTargets(catalog, config, siteModels)
	plan := &ModelsDevPlan{
		CatalogHash: catalog.Hash, FetchedAt: catalog.FetchedAt, Rows: []ModelsDevRow{},
		Unmatched: unmatched, configured: map[string]model.PricingValues{},
	}
	if plan.Unmatched == nil {
		plan.Unmatched = []string{}
	}
	pricing := make(map[string]model.ModelPricingEntry, len(targets))
	if len(targets) > 0 {
		names := make([]string, 0, len(targets))
		for _, target := range targets {
			names = append(names, target.model)
		}
		snapshot, err := model.GetModelPricingSnapshot(names)
		if err != nil {
			return nil, err
		}
		for _, entry := range snapshot.Entries {
			pricing[entry.ModelName] = entry
		}
	}
	defaults := ratio_setting.GetDefaultPricingMaps()
	hasher := sha256.New()
	fmt.Fprintf(hasher, "%s;", catalog.Hash)
	for _, target := range targets {
		entry := pricing[target.model]
		row := modelsDevRowFor(target, config, state, entry, defaults)
		plan.Rows = append(plan.Rows, row)
		plan.configured[target.model] = entry.Configured
		fmt.Fprintf(hasher, "%s|%s|%s|%s|%s;", row.Model, row.Key, row.Status, row.Version, row.Expression)
		switch row.Status {
		case ModelsDevStatusNew:
			plan.Summary.Created++
		case ModelsDevStatusUpdate:
			plan.Summary.Updated++
		case ModelsDevStatusUnchanged:
			plan.Summary.Unchanged++
		case ModelsDevStatusLocked:
			plan.Summary.Locked++
		default:
			plan.Summary.Invalid++
		}
	}
	plan.Summary.Matched = len(plan.Rows)
	plan.Summary.Unmatched = len(plan.Unmatched)
	plan.Hash = fmt.Sprintf("%x", hasher.Sum(nil))[:16]
	sort.SliceStable(plan.Rows, func(i, j int) bool {
		return modelsDevRowRank(plan.Rows[i].Status) < modelsDevRowRank(plan.Rows[j].Status)
	})
	return plan, nil
}

// PreviewModelsDev works out what an import would do, without changing anything.
func PreviewModelsDev(ctx context.Context, refresh bool) (*ModelsDevPlan, error) {
	catalog, err := GetModelsDevCatalog(ctx, refresh)
	if err != nil {
		return nil, err
	}
	config, err := GetModelsDevConfig()
	if err != nil {
		return nil, err
	}
	state, err := GetModelsDevState()
	if err != nil {
		return nil, err
	}
	return newModelsDevPlan(catalog, config, state, modelsDevSiteModels())
}

// ModelsDevSyncOptions says which part of the plan to write.
type ModelsDevSyncOptions struct {
	Trigger string
	Refresh bool
	// PlanHash is the hash of the preview the administrator looked at; the
	// import refuses to run when the plan has changed since.
	PlanHash string
	// Models limits the import to these names; empty means every writable row.
	Models []string
	// Approved lists the models whose proposal needs a decision and that the
	// administrator accepted.
	Approved []string
}

// ModelsDevSyncResult reports one import.
type ModelsDevSyncResult struct {
	At            int64            `json:"at"`
	Summary       ModelsDevSummary `json:"summary"`
	Written       []string         `json:"written"`
	SkippedReview []string         `json:"skipped_review"`
}

func clonePricingValues(values model.PricingValues) model.PricingValues {
	result := make(model.PricingValues, len(values)+2)
	for key, value := range values {
		if nested, ok := value.(map[string]any); ok {
			copied := make(map[string]any, len(nested))
			maps.Copy(copied, nested)
			value = copied
		}
		result[key] = value
	}
	return result
}

// SyncModelsDev imports the prices of the plan. Only one import runs at a time.
func SyncModelsDev(ctx context.Context, opts ModelsDevSyncOptions) (*ModelsDevSyncResult, error) {
	if !modelsDevRunMu.TryLock() {
		return nil, ErrModelsDevBusy
	}
	defer modelsDevRunMu.Unlock()
	return syncModelsDevLocked(ctx, opts)
}

func syncModelsDevLocked(ctx context.Context, opts ModelsDevSyncOptions) (*ModelsDevSyncResult, error) {
	var result *ModelsDevSyncResult
	var err error
	// A price edited while the import runs makes its write fail as a whole;
	// planning again against the new prices settles it.
	for attempt := 0; attempt < 3; attempt++ {
		result, err = runModelsDevSync(ctx, opts)
		if !errors.Is(err, model.ErrModelPricingConflict) || opts.PlanHash != "" {
			break
		}
	}
	if errors.Is(err, model.ErrModelPricingConflict) {
		err = ErrModelsDevConflict
	}
	if err != nil && !errors.Is(err, ErrModelsDevStalePlan) {
		recordModelsDevFailure(opts.Trigger, err)
	}
	return result, err
}

func recordModelsDevFailure(trigger string, syncErr error) {
	state, err := GetModelsDevState()
	if err != nil {
		return
	}
	state.LastError, state.LastErrorAt, state.LastTrigger = syncErr.Error(), common.GetTimestamp(), trigger
	if saveErr := saveModelsDevState(state); saveErr != nil {
		common.SysError(common.LogText("failed to record the models.dev error: %s", saveErr.Error()))
	}
}

func runModelsDevSync(ctx context.Context, opts ModelsDevSyncOptions) (*ModelsDevSyncResult, error) {
	catalog, err := GetModelsDevCatalog(ctx, opts.Refresh)
	if err != nil {
		return nil, err
	}
	config, err := GetModelsDevConfig()
	if err != nil {
		return nil, err
	}
	state, err := GetModelsDevState()
	if err != nil {
		return nil, err
	}
	plan, err := newModelsDevPlan(catalog, config, state, modelsDevSiteModels())
	if err != nil {
		return nil, err
	}
	if opts.PlanHash != "" && opts.PlanHash != plan.Hash {
		return nil, ErrModelsDevStalePlan
	}
	now := common.GetTimestamp()
	result := &ModelsDevSyncResult{At: now, Summary: plan.Summary, Written: []string{}, SkippedReview: []string{}}
	var changes []model.ModelPricingChange
	var rows []ModelsDevRow
	for _, row := range plan.Rows {
		if row.Status != ModelsDevStatusNew && row.Status != ModelsDevStatusUpdate {
			continue
		}
		if len(opts.Models) > 0 && !slices.Contains(opts.Models, row.Model) {
			continue
		}
		if row.Review != "" && !slices.Contains(opts.Approved, row.Model) {
			result.SkippedReview = append(result.SkippedReview, row.Model)
			continue
		}
		draft := clonePricingValues(plan.configured[row.Model])
		draft["billing_setting.billing_mode"] = billing_setting.BillingModeTieredExpr
		draft["billing_setting.billing_expr"] = row.Expression
		changes = append(changes, model.ModelPricingChange{ModelName: row.Model, ExpectedVersion: row.Version, Pricing: draft})
		rows = append(rows, row)
	}
	result.Summary.SkippedReview = len(result.SkippedReview)
	result.Summary.Created, result.Summary.Updated = 0, 0
	for _, row := range rows {
		if row.Status == ModelsDevStatusNew {
			result.Summary.Created++
		} else {
			result.Summary.Updated++
		}
		result.Written = append(result.Written, row.Model)
	}
	batch := &ModelsDevBatch{At: now, Trigger: opts.Trigger, Version: common.Version}
	if len(changes) > 0 {
		if err := model.UpdateModelPricing(changes); err != nil {
			return nil, err
		}
		after, err := model.GetModelPricingSnapshot(result.Written)
		if err != nil {
			return nil, err
		}
		versions := make(map[string]string, len(after.Entries))
		for _, entry := range after.Entries {
			versions[entry.ModelName] = entry.Version
		}
		for _, row := range rows {
			previous, wasManaged := state.Managed[row.Model]
			if len(batch.Items) < modelsDevMaxBatchItems {
				item := ModelsDevBatchItem{
					Model: row.Model, Key: row.Key, AfterVersion: versions[row.Model],
					Before: clonePricingValues(plan.configured[row.Model]),
				}
				if wasManaged {
					item.ManagedBefore = &previous
				}
				batch.Items = append(batch.Items, item)
			} else {
				batch.Truncated++
			}
			state.Managed[row.Model] = ModelsDevManaged{
				Key: row.Key, Version: versions[row.Model], Input: row.Input, Output: row.Output, SyncedAt: now,
			}
		}
		state.LastBatch = batch
	}
	state.LastSyncAt, state.LastTrigger, state.LastSummary = now, opts.Trigger, result.Summary
	state.LastError, state.LastErrorAt = "", 0
	if len(opts.Models) == 0 {
		state.LastSyncVersion = common.Version
	}
	if err := saveModelsDevState(state); err != nil {
		return nil, err
	}
	return result, nil
}

// RunModelsDevAutoSync is the import of a new release: the system task calls it
// when the switch is on and this version has not imported yet.
func RunModelsDevAutoSync(ctx context.Context) (*ModelsDevSyncResult, error) {
	if !modelsDevRunMu.TryLock() {
		return nil, ErrModelsDevBusy
	}
	defer modelsDevRunMu.Unlock()
	config, err := GetModelsDevConfig()
	if err != nil {
		return nil, err
	}
	if !config.AutoSyncEnabled {
		return &ModelsDevSyncResult{Written: []string{}, SkippedReview: []string{}}, nil
	}
	state, err := GetModelsDevState()
	if err != nil {
		return nil, err
	}
	if state.LastSyncVersion == common.Version {
		// Another node, or the administrator, imported for this version while
		// the task waited; a second pass would only overwrite its summary.
		return &ModelsDevSyncResult{Written: []string{}, SkippedReview: []string{}}, nil
	}
	if state.AttemptVersion != common.Version {
		state.AttemptVersion, state.Attempts = common.Version, 0
	}
	state.Attempts++
	if err := saveModelsDevState(state); err != nil {
		return nil, err
	}
	return syncModelsDevLocked(ctx, ModelsDevSyncOptions{Trigger: ModelsDevTriggerRelease})
}

// ModelsDevUndoResult reports an undo. A model that somebody edited after the
// import keeps its price and is listed as a conflict.
type ModelsDevUndoResult struct {
	Restored  []string `json:"restored"`
	Conflicts []string `json:"conflicts"`
}

// UndoModelsDevSync puts the prices back that the latest import replaced.
func UndoModelsDevSync() (*ModelsDevUndoResult, error) {
	if !modelsDevRunMu.TryLock() {
		return nil, ErrModelsDevBusy
	}
	defer modelsDevRunMu.Unlock()
	state, err := GetModelsDevState()
	if err != nil {
		return nil, err
	}
	batch := state.LastBatch
	if batch == nil || len(batch.Items) == 0 {
		return nil, ErrModelsDevNothingUndo
	}
	names := make([]string, 0, len(batch.Items))
	for _, item := range batch.Items {
		names = append(names, item.Model)
	}
	snapshot, err := model.GetModelPricingSnapshot(names)
	if err != nil {
		return nil, err
	}
	versions := make(map[string]string, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		versions[entry.ModelName] = entry.Version
	}
	result := &ModelsDevUndoResult{Restored: []string{}, Conflicts: []string{}}
	var changes []model.ModelPricingChange
	var restored []ModelsDevBatchItem
	for _, item := range batch.Items {
		if versions[item.Model] != item.AfterVersion {
			result.Conflicts = append(result.Conflicts, item.Model)
			continue
		}
		before := item.Before
		if before == nil {
			before = model.PricingValues{}
		}
		changes = append(changes, model.ModelPricingChange{ModelName: item.Model, ExpectedVersion: item.AfterVersion, Pricing: before})
		restored = append(restored, item)
	}
	if len(changes) > 0 {
		if err := model.UpdateModelPricing(changes); err != nil {
			if errors.Is(err, model.ErrModelPricingConflict) {
				return nil, ErrModelsDevConflict
			}
			return nil, err
		}
	}
	for _, item := range restored {
		result.Restored = append(result.Restored, item.Model)
		if item.ManagedBefore != nil {
			state.Managed[item.Model] = *item.ManagedBefore
		} else {
			delete(state.Managed, item.Model)
		}
	}
	state.LastBatch = nil
	// The undo is the decision of the administrator for this release: the
	// automatic import must not redo it.
	state.LastSyncVersion = common.Version
	if err := saveModelsDevState(state); err != nil {
		return nil, err
	}
	return result, nil
}

// ModelsDevStateView is the part of the state the settings page shows.
type ModelsDevStateView struct {
	LastSyncAt      int64            `json:"last_sync_at"`
	LastSyncVersion string           `json:"last_sync_version"`
	LastTrigger     string           `json:"last_trigger"`
	LastSummary     ModelsDevSummary `json:"last_summary"`
	LastError       string           `json:"last_error,omitempty"`
	LastErrorAt     int64            `json:"last_error_at,omitempty"`
	ManagedCount    int              `json:"managed_count"`
	UndoCount       int              `json:"undo_count"`
	UndoAt          int64            `json:"undo_at,omitempty"`
}

type ModelsDevOverview struct {
	Config         ModelsDevConfig    `json:"config"`
	State          ModelsDevStateView `json:"state"`
	SourceURL      string             `json:"source_url"`
	CurrentVersion string             `json:"current_version"`
	// AutoSyncDue is true when the next scheduler pass imports.
	AutoSyncDue bool `json:"auto_sync_due"`
}

func GetModelsDevOverview() (*ModelsDevOverview, error) {
	config, err := GetModelsDevConfig()
	if err != nil {
		return nil, err
	}
	state, err := GetModelsDevState()
	if err != nil {
		return nil, err
	}
	view := ModelsDevStateView{
		LastSyncAt: state.LastSyncAt, LastSyncVersion: state.LastSyncVersion, LastTrigger: state.LastTrigger,
		LastSummary: state.LastSummary, LastError: state.LastError, LastErrorAt: state.LastErrorAt,
		ManagedCount: len(state.Managed),
	}
	if state.LastBatch != nil {
		view.UndoCount = len(state.LastBatch.Items)
		view.UndoAt = state.LastBatch.At
	}
	return &ModelsDevOverview{
		Config: config, State: view, SourceURL: modelsDevSourceURL(), CurrentVersion: common.Version,
		AutoSyncDue: ModelsDevAutoSyncDue(),
	}, nil
}

// ModelsDevCatalogItem is an entry together with what the configuration says
// about it.
type ModelsDevCatalogItem struct {
	ModelsDevEntry
	Selected bool `json:"selected"`
	Excluded bool `json:"excluded"`
	// SiteModels are the models of this site that the entry prices by name.
	SiteModels []string `json:"site_models,omitempty"`
	// MappedFrom are the models of this site mapped to the entry by hand.
	MappedFrom []string `json:"mapped_from,omitempty"`
}

type ModelsDevCatalogPage struct {
	Total     int                      `json:"total"`
	Items     []ModelsDevCatalogItem   `json:"items"`
	Providers []ModelsDevProviderFacet `json:"providers"`
	FetchedAt int64                    `json:"fetched_at"`
	Hash      string                   `json:"hash"`
}

// ListModelsDevCatalog returns one page of the entries that match the search.
func ListModelsDevCatalog(ctx context.Context, search ModelsDevSearch, limit int, offset int, refresh bool) (*ModelsDevCatalogPage, error) {
	catalog, err := GetModelsDevCatalog(ctx, refresh)
	if err != nil {
		return nil, err
	}
	config, err := GetModelsDevConfig()
	if err != nil {
		return nil, err
	}
	matches := catalog.Search(search)
	page := &ModelsDevCatalogPage{
		Total: len(matches), Items: []ModelsDevCatalogItem{}, Providers: catalog.Providers,
		FetchedAt: catalog.FetchedAt, Hash: catalog.Hash,
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 || offset > len(matches) {
		offset = len(matches)
	}
	end := min(offset+limit, len(matches))
	siteByKey := make(map[string][]string)
	for _, name := range modelsDevSiteModels() {
		if entry, ok := catalog.Match(name); ok {
			siteByKey[entry.Key] = append(siteByKey[entry.Key], name)
		}
	}
	mappedByKey := make(map[string][]string)
	for name, key := range config.Mappings {
		mappedByKey[key] = append(mappedByKey[key], name)
	}
	for _, entry := range matches[offset:end] {
		item := ModelsDevCatalogItem{
			ModelsDevEntry: entry, Selected: slices.Contains(config.SelectedKeys, entry.Key),
			Excluded: slices.Contains(config.ExcludedKeys, entry.Key), SiteModels: siteByKey[entry.Key], MappedFrom: mappedByKey[entry.Key],
		}
		sort.Strings(item.MappedFrom)
		page.Items = append(page.Items, item)
	}
	return page, nil
}

// ModelsDevExpressionResult is the expression of one entry for one model name.
type ModelsDevExpressionResult struct {
	Entry      ModelsDevEntry `json:"entry"`
	Expression string         `json:"expression"`
}

// ModelsDevExpression writes the prices of one entry as the billing
// expression of a model, for the editor of the model price.
func ModelsDevExpression(ctx context.Context, key string, billingName string) (*ModelsDevExpressionResult, error) {
	catalog, err := GetModelsDevCatalog(ctx, false)
	if err != nil {
		return nil, err
	}
	entry, ok := catalog.Entry(key)
	if !ok {
		return nil, common.NewMessage("models.dev does not list this model")
	}
	expression, err := BuildModelsDevExpression(entry, billingName)
	if err != nil {
		return nil, err
	}
	return &ModelsDevExpressionResult{Entry: entry, Expression: expression}, nil
}
