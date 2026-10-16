package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// models.dev publishes the list prices of the models of many providers as one
// public JSON document. The catalog flattens it into text models with a token
// price, the way CC Switch does, so that the prices can be imported into the
// billing expressions of this site.

const (
	modelsDevDefaultURL = "https://models.dev/api.json"
	// ModelsDevURLEnv overrides the address of the document; it exists for tests
	// and for deployments that mirror the data.
	ModelsDevURLEnv           = "MODELS_DEV_API_URL"
	modelsDevFetchTimeout     = 20 * time.Second
	modelsDevMaxBodyBytes     = 32 << 20
	modelsDevCacheTTL         = time.Hour
	modelsDevCommonPerFamily  = 6
	modelsDevTierEntryMaxSize = int64(1) << 40
)

// ModelsDevTier is the price above a context size.
type ModelsDevTier struct {
	Size       int64    `json:"size"`
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

// ModelsDevEntry is one priced text model of one provider. Prices are US
// dollars per million tokens.
type ModelsDevEntry struct {
	// Key is "provider/model id", unique in the document.
	Key          string `json:"key"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	// NormalizedID is the model id without namespace, variant and case, so that
	// the same model listed by several providers shares one id.
	NormalizedID string `json:"normalized_id"`
	ModelName    string `json:"model_name"`
	ReleaseDate  string `json:"release_date"`
	// Official marks the first-party listing of the model: the vendor itself
	// rather than a reseller or cloud host.
	Official bool `json:"official"`
	// Common marks the recent models of the popular model families.
	Common     bool            `json:"common"`
	Input      float64         `json:"input"`
	Output     float64         `json:"output"`
	CacheRead  *float64        `json:"cache_read,omitempty"`
	CacheWrite *float64        `json:"cache_write,omitempty"`
	Tiers      []ModelsDevTier `json:"tiers,omitempty"`
}

// ModelsDevProviderFacet counts the entries of one provider.
type ModelsDevProviderFacet struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ModelsDevCatalog is the flattened document.
type ModelsDevCatalog struct {
	Entries   []ModelsDevEntry
	Providers []ModelsDevProviderFacet
	FetchedAt int64
	// Hash identifies the content, so a preview can tell that the data changed.
	Hash string

	byKey   map[string]int
	byNorm  map[string][]int
	byLoose map[string][]int
}

type modelsDevRawPrice struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type modelsDevRawCost struct {
	modelsDevRawPrice
	Tiers []struct {
		modelsDevRawPrice
		Tier struct {
			Type string  `json:"type"`
			Size float64 `json:"size"`
		} `json:"tier"`
	} `json:"tiers"`
	ContextOver200k *modelsDevRawPrice `json:"context_over_200k"`
}

type modelsDevRawModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	ReleaseDate      string            `json:"release_date"`
	Status           string            `json:"status"`
	CanonicalModelID string            `json:"canonical_model_id"`
	Cost             *modelsDevRawCost `json:"cost"`
	Modalities       struct {
		Output []string `json:"output"`
	} `json:"modalities"`
}

type modelsDevRawProvider struct {
	ID     string                       `json:"id"`
	Name   string                       `json:"name"`
	Models map[string]modelsDevRawModel `json:"models"`
}

// normalizeModelsDevID drops the namespace ("vendor/"), the variant (":tag")
// and the "[1m]" marker, and lower-cases the rest, so that one model listed by
// several providers lands on one id. It is the id normalization of CC Switch.
func normalizeModelsDevID(modelID string) string {
	if index := strings.LastIndex(modelID, "/"); index >= 0 {
		modelID = modelID[index+1:]
	}
	if index := strings.Index(modelID, ":"); index >= 0 {
		modelID = modelID[:index]
	}
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(modelID), "@", "-"))
	if strings.HasSuffix(normalized, "[1m]") {
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, "[1m]"))
	}
	return normalized
}

// looseModelsDevID also treats "." and "-" alike, so that "claude-sonnet-4.5"
// and "claude-sonnet-4-5" name the same model.
func looseModelsDevID(normalized string) string {
	return strings.ReplaceAll(normalized, ".", "-")
}

var modelsDevNonTextMarkers = []string{
	"audio", "deprecated", "embedding", "image", "moderation", "realtime", "transcribe", "tts", "video",
}

func isModelsDevTextModel(modelID string, model modelsDevRawModel) bool {
	if strings.EqualFold(model.Status, "deprecated") {
		return false
	}
	if len(model.Modalities.Output) > 0 {
		hasText := false
		for _, modality := range model.Modalities.Output {
			switch strings.ToLower(modality) {
			case "text":
				hasText = true
			case "audio", "image", "video":
				return false
			}
		}
		if !hasText {
			return false
		}
	}
	searchable := strings.ToLower(modelID + " " + model.Name)
	for _, marker := range modelsDevNonTextMarkers {
		if strings.Contains(searchable, marker) {
			return false
		}
	}
	return true
}

// modelsDevCommonRule picks the popular models of one model family: the
// recent models of its vendor.
type modelsDevCommonRule struct {
	id        string
	providers []string
	prefixes  []string
}

var modelsDevCommonRules = []modelsDevCommonRule{
	{"claude", []string{"anthropic"}, []string{"claude-"}},
	{"gpt", []string{"openai"}, []string{"gpt-", "o1-", "o3-", "o4-"}},
	{"gemini", []string{"google"}, []string{"gemini-"}},
	{"grok", []string{"xai"}, []string{"grok-"}},
	{"deepseek", []string{"deepseek"}, []string{"deepseek-"}},
	{"qwen", []string{"alibaba"}, []string{"qwen"}},
	{"mimo", []string{"xiaomi"}, []string{"mimo-"}},
	{"longcat", []string{"longcat"}, []string{"longcat-"}},
	{"kimi", []string{"moonshotai"}, []string{"kimi-"}},
	{"minimax", []string{"minimax-cn"}, []string{"minimax-m"}},
	{"glm", []string{"zai"}, []string{"glm-"}},
}

func (rule modelsDevCommonRule) matches(entry ModelsDevEntry) bool {
	if !slices.Contains(rule.providers, entry.ProviderID) {
		return false
	}
	modelID := strings.ToLower(entry.ModelID)
	for _, prefix := range rule.prefixes {
		if strings.HasPrefix(modelID, prefix) {
			return true
		}
	}
	return false
}

// modelsDevVendors maps the providers that sell their own models to the vendor
// namespace used by canonical model ids.
var modelsDevVendors = map[string]string{
	"anthropic": "anthropic", "openai": "openai", "google": "google", "xai": "xai",
	"deepseek": "deepseek", "alibaba": "alibaba", "alibaba-cn": "alibaba",
	"moonshotai": "moonshotai", "moonshotai-cn": "moonshotai", "zai": "zhipuai",
	"zhipuai": "zhipuai", "minimax": "minimax", "minimax-cn": "minimax",
	"xiaomi": "xiaomi", "longcat": "meituan", "mistral": "mistral", "cohere": "cohere",
	"perplexity": "perplexity", "ai21": "ai21", "meta": "meta",
}

// modelsDevModelVendor names the vendor of a model by its id, for the model
// families that are known.
func modelsDevModelVendor(normalizedID string) (string, bool) {
	for _, rule := range modelsDevCommonRules {
		for _, prefix := range rule.prefixes {
			if strings.HasPrefix(normalizedID, prefix) {
				vendor, ok := modelsDevVendors[rule.providers[0]]
				return vendor, ok
			}
		}
	}
	return "", false
}

// isModelsDevOfficial tells the first-party listing of a model from the
// listings of resellers and cloud hosts. A host lists vendors' models under a
// canonical id that names the vendor; a vendor that also hosts the models of
// another vendor is a reseller for those.
func isModelsDevOfficial(providerID string, canonicalID string, normalizedID string) bool {
	canonicalVendor, _, hasCanonical := strings.Cut(canonicalID, "/")
	vendor, firstParty := modelsDevVendors[providerID]
	if !firstParty {
		return hasCanonical && canonicalVendor == providerID
	}
	if hasCanonical {
		return canonicalVendor == vendor
	}
	modelVendor, known := modelsDevModelVendor(normalizedID)
	return !known || modelVendor == vendor
}

func validModelsDevPrice(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0
}

func optionalModelsDevPrice(value *float64) *float64 {
	if !validModelsDevPrice(value) {
		return nil
	}
	result := *value
	return &result
}

// modelsDevTiers reads the context tiers of a cost: the tiers list, or the
// older fixed "over 200k" price when the list is missing.
func modelsDevTiers(cost *modelsDevRawCost) []ModelsDevTier {
	var tiers []ModelsDevTier
	for _, raw := range cost.Tiers {
		size := int64(raw.Tier.Size)
		if raw.Tier.Type != "context" || size <= 0 || size > modelsDevTierEntryMaxSize {
			continue
		}
		if !validModelsDevPrice(raw.Input) || !validModelsDevPrice(raw.Output) {
			continue
		}
		tiers = append(tiers, ModelsDevTier{
			Size: size, Input: *raw.Input, Output: *raw.Output,
			CacheRead: optionalModelsDevPrice(raw.CacheRead), CacheWrite: optionalModelsDevPrice(raw.CacheWrite),
		})
	}
	if len(tiers) == 0 && cost.ContextOver200k != nil {
		legacy := cost.ContextOver200k
		if validModelsDevPrice(legacy.Input) && validModelsDevPrice(legacy.Output) {
			tiers = append(tiers, ModelsDevTier{
				Size: 200000, Input: *legacy.Input, Output: *legacy.Output,
				CacheRead: optionalModelsDevPrice(legacy.CacheRead), CacheWrite: optionalModelsDevPrice(legacy.CacheWrite),
			})
		}
	}
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].Size < tiers[j].Size })
	return slices.CompactFunc(tiers, func(a, b ModelsDevTier) bool { return a.Size == b.Size })
}

// BlendedPrice is the price of a million tokens at three input tokens per
// output token, the weight used to compare models.
func (entry ModelsDevEntry) BlendedPrice() float64 {
	return (3*entry.Input + entry.Output) / 4
}

// flattenModelsDev turns the document into the priced text models. Free models
// are left out: a price of zero is never imported.
func flattenModelsDev(raw map[string]modelsDevRawProvider) []ModelsDevEntry {
	var entries []ModelsDevEntry
	for providerKey, provider := range raw {
		providerName := provider.Name
		if providerName == "" {
			providerName = providerKey
		}
		for modelID, model := range provider.Models {
			if model.Cost == nil || !isModelsDevTextModel(modelID, model) {
				continue
			}
			cost := model.Cost
			if !validModelsDevPrice(cost.Input) && !validModelsDevPrice(cost.Output) {
				continue
			}
			entry := ModelsDevEntry{
				Key: providerKey + "/" + modelID, ProviderID: providerKey, ProviderName: providerName,
				ModelID: modelID, NormalizedID: normalizeModelsDevID(modelID), ModelName: model.Name,
				ReleaseDate: model.ReleaseDate,
				Official:    isModelsDevOfficial(providerKey, model.CanonicalModelID, normalizeModelsDevID(modelID)),
				CacheRead:   optionalModelsDevPrice(cost.CacheRead), CacheWrite: optionalModelsDevPrice(cost.CacheWrite),
			}
			if validModelsDevPrice(cost.Input) {
				entry.Input = *cost.Input
			}
			if validModelsDevPrice(cost.Output) {
				entry.Output = *cost.Output
			}
			if entry.NormalizedID == "" || entry.Input+entry.Output <= 0 {
				continue
			}
			if entry.ModelName == "" {
				entry.ModelName = modelID
			}
			entry.Tiers = modelsDevTiers(cost)
			entries = append(entries, entry)
		}
	}
	// Newest first; a stable order for equal dates keeps the catalog repeatable.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ReleaseDate != entries[j].ReleaseDate {
			return entries[i].ReleaseDate > entries[j].ReleaseDate
		}
		if entries[i].ModelName != entries[j].ModelName {
			return entries[i].ModelName < entries[j].ModelName
		}
		return entries[i].Key < entries[j].Key
	})
	markCommonModelsDev(entries)
	return entries
}

// markCommonModelsDev flags the newest models of each popular model family.
func markCommonModelsDev(entries []ModelsDevEntry) {
	for _, rule := range modelsDevCommonRules {
		count := 0
		seen := make(map[string]bool)
		for index := range entries {
			if count >= modelsDevCommonPerFamily {
				break
			}
			entry := &entries[index]
			if !rule.matches(*entry) || seen[entry.NormalizedID] {
				continue
			}
			seen[entry.NormalizedID] = true
			entry.Common = true
			count++
		}
	}
}

func newModelsDevCatalog(entries []ModelsDevEntry, fetchedAt int64) *ModelsDevCatalog {
	catalog := &ModelsDevCatalog{
		Entries: entries, FetchedAt: fetchedAt,
		byKey:   make(map[string]int, len(entries)),
		byNorm:  make(map[string][]int),
		byLoose: make(map[string][]int),
	}
	providers := make(map[string]*ModelsDevProviderFacet)
	hasher := sha256.New()
	for index, entry := range entries {
		catalog.byKey[entry.Key] = index
		catalog.byNorm[entry.NormalizedID] = append(catalog.byNorm[entry.NormalizedID], index)
		loose := looseModelsDevID(entry.NormalizedID)
		catalog.byLoose[loose] = append(catalog.byLoose[loose], index)
		facet := providers[entry.ProviderID]
		if facet == nil {
			facet = &ModelsDevProviderFacet{ID: entry.ProviderID, Name: entry.ProviderName}
			providers[entry.ProviderID] = facet
		}
		facet.Count++
		fmt.Fprintf(hasher, "%s|%v|%v|%v|%v|%d;", entry.Key, entry.Input, entry.Output, entry.CacheRead, entry.CacheWrite, len(entry.Tiers))
		for _, tier := range entry.Tiers {
			fmt.Fprintf(hasher, "%d,%v,%v,%v,%v;", tier.Size, tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite)
		}
	}
	catalog.Hash = fmt.Sprintf("%x", hasher.Sum(nil))[:16]
	for _, facet := range providers {
		catalog.Providers = append(catalog.Providers, *facet)
	}
	sort.Slice(catalog.Providers, func(i, j int) bool {
		if catalog.Providers[i].Count != catalog.Providers[j].Count {
			return catalog.Providers[i].Count > catalog.Providers[j].Count
		}
		return catalog.Providers[i].ID < catalog.Providers[j].ID
	})
	return catalog
}

// Entry returns the entry with the given key.
func (catalog *ModelsDevCatalog) Entry(key string) (ModelsDevEntry, bool) {
	index, ok := catalog.byKey[key]
	if !ok {
		return ModelsDevEntry{}, false
	}
	return catalog.Entries[index], true
}

// betterModelsDevEntry says whether a is the one to use before b: the
// first-party listing first, then the lower price.
func betterModelsDevEntry(a, b ModelsDevEntry) bool {
	if a.Official != b.Official {
		return a.Official
	}
	if a.BlendedPrice() != b.BlendedPrice() {
		return a.BlendedPrice() < b.BlendedPrice()
	}
	return a.Key < b.Key
}

func (catalog *ModelsDevCatalog) best(indexes []int) (ModelsDevEntry, bool) {
	var best ModelsDevEntry
	found := false
	for _, index := range indexes {
		if entry := catalog.Entries[index]; !found || betterModelsDevEntry(entry, best) {
			best, found = entry, true
		}
	}
	return best, found
}

// Match finds the entry of a model by its name, treating dots and dashes alike
// ("claude-sonnet-4.5" is "claude-sonnet-4-5"). Among the providers that list
// the model the first-party listing wins, then the lowest price.
func (catalog *ModelsDevCatalog) Match(modelName string) (ModelsDevEntry, bool) {
	normalized := normalizeModelsDevID(modelName)
	if normalized == "" {
		return ModelsDevEntry{}, false
	}
	return catalog.best(catalog.byLoose[looseModelsDevID(normalized)])
}

type modelsDevCatalogCache struct {
	mu      sync.Mutex
	url     string
	catalog *ModelsDevCatalog
}

var modelsDevCache modelsDevCatalogCache

// ResetModelsDevCatalogCache forgets the downloaded document. Tests use it to
// serve another document.
func ResetModelsDevCatalogCache() {
	modelsDevCache.mu.Lock()
	defer modelsDevCache.mu.Unlock()
	modelsDevCache.catalog = nil
}

func modelsDevSourceURL() string {
	return common.GetEnvOrDefaultString(ModelsDevURLEnv, modelsDevDefaultURL)
}

func newModelsDevHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 15 * time.Second
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig.Clone()
	}
	return &http.Client{Transport: transport}
}

func downloadModelsDev(ctx context.Context, sourceURL string) (map[string]modelsDevRawProvider, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsDevFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := newModelsDevHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models.dev: HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, modelsDevMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read models.dev: %w", err)
	}
	if len(body) > modelsDevMaxBodyBytes {
		return nil, fmt.Errorf("models.dev response is larger than %d MB", modelsDevMaxBodyBytes>>20)
	}
	var raw map[string]modelsDevRawProvider
	if err := common.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode models.dev: %w", err)
	}
	return raw, nil
}

// GetModelsDevCatalog returns the flattened document, downloading it when the
// copy in memory is older than an hour or force is set.
func GetModelsDevCatalog(ctx context.Context, force bool) (*ModelsDevCatalog, error) {
	modelsDevCache.mu.Lock()
	defer modelsDevCache.mu.Unlock()
	sourceURL := modelsDevSourceURL()
	if cached := modelsDevCache.catalog; cached != nil && !force && modelsDevCache.url == sourceURL &&
		time.Since(time.Unix(cached.FetchedAt, 0)) < modelsDevCacheTTL {
		return cached, nil
	}
	raw, err := downloadModelsDev(ctx, sourceURL)
	if err != nil {
		return nil, err
	}
	entries := flattenModelsDev(raw)
	if len(entries) == 0 {
		return nil, fmt.Errorf("models.dev has no priced models")
	}
	modelsDevCache.catalog = newModelsDevCatalog(entries, common.GetTimestamp())
	modelsDevCache.url = sourceURL
	return modelsDevCache.catalog, nil
}

// ModelsDevSearch filters the entries of the catalog for a picker.
type ModelsDevSearch struct {
	Query        string
	Provider     string
	OnlyCommon   bool
	OnlyOfficial bool
}

// modelsDevSearchRank puts the entries whose id is the query first, then the
// ones whose id starts with it, then the rest.
func modelsDevSearchRank(entry ModelsDevEntry, query string) int {
	id := looseModelsDevID(entry.NormalizedID)
	switch {
	case query == "" || id == query:
		return 0
	case strings.HasPrefix(id, query):
		return 1
	default:
		return 2
	}
}

// Search returns the entries that match. Every word of the query must occur in
// the key or the name ("." and "-" count alike). The entries that fit the query
// best come first, then the listings of the vendors themselves, then the
// popular models; the catalog order, newest first, settles the rest.
func (catalog *ModelsDevCatalog) Search(search ModelsDevSearch) []ModelsDevEntry {
	query := looseModelsDevID(strings.ToLower(strings.TrimSpace(search.Query)))
	words := strings.Fields(query)
	var result []ModelsDevEntry
	for _, entry := range catalog.Entries {
		if search.Provider != "" && entry.ProviderID != search.Provider {
			continue
		}
		if (search.OnlyCommon && !entry.Common) || (search.OnlyOfficial && !entry.Official) {
			continue
		}
		haystack := looseModelsDevID(strings.ToLower(entry.Key + " " + entry.ModelName))
		matches := true
		for _, word := range words {
			matches = matches && strings.Contains(haystack, word)
		}
		if matches {
			result = append(result, entry)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if rankA, rankB := modelsDevSearchRank(a, query), modelsDevSearchRank(b, query); rankA != rankB {
			return rankA < rankB
		}
		if a.Official != b.Official {
			return a.Official
		}
		return a.Common && !b.Common
	})
	return result
}
