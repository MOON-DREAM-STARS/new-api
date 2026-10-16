package controller

import (
	"bytes"
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelsDevResponse struct {
	Success    bool              `json:"success"`
	Message    string            `json:"message"`
	MessageKey string            `json:"message_key"`
	Data       common.RawMessage `json:"data"`
}

// modelsDevSource serves a models.dev document that a test can change.
type modelsDevSource struct {
	mu     sync.Mutex
	doc    map[string]any
	status int
}

func devCost(input, output float64, extra map[string]any) map[string]any {
	cost := map[string]any{"input": input, "output": output}
	maps.Copy(cost, extra)
	return cost
}

func devModel(name, release string, cost map[string]any, extra map[string]any) map[string]any {
	entry := map[string]any{
		"id": name, "name": name, "release_date": release, "cost": cost,
		"modalities": map[string]any{"output": []string{"text"}},
	}
	maps.Copy(entry, extra)
	return entry
}

func devProvider(name string, models map[string]any) map[string]any {
	return map[string]any{"id": name, "name": name, "models": models}
}

func newModelsDevDocument() map[string]any {
	return map[string]any{
		"anthropic": devProvider("Anthropic", map[string]any{
			"claude-sonnet-4-5": devModel("Claude Sonnet 4.5", "2025-09-29", devCost(3, 15, map[string]any{"cache_read": 0.3, "cache_write": 3.75}), nil),
			"claude-haiku-4-5":  devModel("Claude Haiku 4.5", "2025-10-15", devCost(1, 5, map[string]any{"cache_read": 0.1, "cache_write": 1.25}), nil),
			"claude-3-opus":     devModel("Claude 3 Opus", "2024-03-04", devCost(15, 75, nil), map[string]any{"status": "deprecated"}),
		}),
		// A reseller that is cheaper than the vendor.
		"openrouter": devProvider("OpenRouter", map[string]any{
			"anthropic/claude-sonnet-4.5": devModel("Claude Sonnet 4.5", "2025-09-29", devCost(2, 10, nil), map[string]any{"canonical_model_id": "anthropic/claude-sonnet-4-5"}),
		}),
		"openai": devProvider("OpenAI", map[string]any{
			"gpt-5.4": devModel("GPT-5.4", "2026-03-05", devCost(2.5, 15, map[string]any{
				"cache_read": 0.25,
				"tiers":      []any{map[string]any{"input": 5, "output": 22.5, "cache_read": 0.5, "tier": map[string]any{"type": "context", "size": 272000}}},
			}), nil),
			"gpt-4o":                 devModel("GPT-4o", "2024-05-13", devCost(2.5, 10, map[string]any{"cache_read": 1.25}), nil),
			"gpt-image-1":            devModel("GPT Image 1", "2025-04-23", devCost(5, 40, nil), nil),
			"text-embedding-3-large": devModel("Embedding", "2024-01-25", devCost(0.13, 0, nil), nil),
		}),
		"google": devProvider("Google", map[string]any{
			"gemini-3.6-pro": devModel("Gemini 2.5 Pro", "2025-06-17", devCost(1.25, 10, map[string]any{
				"cache_read":        0.125,
				"context_over_200k": map[string]any{"input": 2.5, "output": 15, "cache_read": 0.25},
			}), nil),
		}),
		"deepseek": devProvider("DeepSeek", map[string]any{
			"deepseek-v4-pro": devModel("DeepSeek V4 Pro", "2026-08-12", devCost(0.66, 1.98, map[string]any{"cache_read": 0.022}), nil),
			"deepseek-free":   devModel("DeepSeek Free", "2026-08-12", devCost(0, 0, nil), nil),
		}),
		"zai": devProvider("Z.AI", map[string]any{
			"glm-5": devModel("GLM-5", "2026-02-12", devCost(1, 3.2, map[string]any{"cache_read": 0.2, "cache_write": 0}), nil),
		}),
		"siliconflow": devProvider("SiliconFlow", map[string]any{
			"Qwen/Qwen3-Coder": devModel("Qwen3 Coder", "2026-01-01", devCost(0.5, 1, nil), nil),
		}),
		// A vendor that also hosts the model of another vendor is a reseller for it.
		"alibaba": devProvider("Alibaba", map[string]any{
			"kimi-k2.6": devModel("Kimi K2.6", "2026-04-21", devCost(0.9, 3.8, nil), nil),
		}),
		"moonshotai": devProvider("Moonshot", map[string]any{
			"kimi-k2.6": devModel("Kimi K2.6", "2026-04-21", devCost(0.95, 4, nil), nil),
		}),
		// The vendor of LongCat is Meituan in the canonical ids of models.dev.
		"longcat": devProvider("LongCat", map[string]any{
			"LongCat-2.0": devModel("LongCat 2.0", "2026-05-01", devCost(0.75, 2.95, nil), map[string]any{"canonical_model_id": "meituan/longcat-2.0"}),
		}),
		"hosta": devProvider("Host A", map[string]any{
			"vendor/qwen3-coder": devModel("Qwen3 Coder", "2026-01-01", devCost(0.3, 0.9, nil), nil),
		}),
	}
}

func (s *modelsDevSource) setCost(provider, modelID string, input, output float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	models := s.doc[provider].(map[string]any)["models"].(map[string]any)
	cost := models[modelID].(map[string]any)["cost"].(map[string]any)
	cost["input"], cost["output"] = input, output
	service.ResetModelsDevCatalogCache()
}

func (s *modelsDevSource) setStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	service.ResetModelsDevCatalogCache()
}

func (s *modelsDevSource) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != 0 {
		w.WriteHeader(s.status)
		return
	}
	body, _ := common.Marshal(s.doc)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func TestModelsDevPricing(t *testing.T) {
	f := newServiceTierFixture(t)
	previousOptions, previousVersion := common.OptionMap, common.Version
	common.OptionMap = map[string]string{}
	common.Version = "v-test-1"
	source := &modelsDevSource{doc: newModelsDevDocument()}
	server := httptest.NewServer(source)
	t.Setenv(service.ModelsDevURLEnv, server.URL)
	service.ResetModelsDevCatalogCache()
	t.Cleanup(func() {
		server.Close()
		service.ResetModelsDevCatalogCache()
		common.OptionMap, common.Version = previousOptions, previousVersion
	})

	// The models of this site that no channel serves are family members.
	members := map[string][]string{
		"claude": {"claude-sonnet-4.5"}, "gpt": {"gpt-5.4", "gpt-4o"}, "gemini": {"gemini-3.6-pro"}, "glm": {"glm-5"},
		"qwen": {"qwen-plus", "qwen3-coder"}, "kimi": {"kimi-k2.6"},
		"deepseek": {"deepseek-v4-pro", "deepseek-ratio", "deepseek-tiered", "deepseek-v4.1-flash"},
	}
	store, err := model.LoadServiceTierStore()
	require.NoError(t, err)
	for _, family := range store.Families {
		if names, ok := members[family.Key]; ok {
			_, err = model.ReplaceModelFamilyMembers(family.Id, family.Version, names)
			require.NoError(t, err)
		}
	}
	refreshChannelAggregation()

	call := func(handler gin.HandlerFunc, method, target, body string) modelsDevResponse {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, target, bytes.NewBufferString(body))
		c.Set("id", f.user.Id)
		c.Set("role", common.RoleRootUser)
		handler(c)
		var response modelsDevResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		return response
	}
	decode := func(response modelsDevResponse, target any) {
		t.Helper()
		require.True(t, response.Success, response.Message)
		require.NoError(t, common.Unmarshal(response.Data, target))
	}
	configure := func(config service.ModelsDevConfig) {
		t.Helper()
		body, err := common.Marshal(config)
		require.NoError(t, err)
		response := call(UpdateModelsDevConfig, http.MethodPut, "/api/option/models_dev/config", string(body))
		require.True(t, response.Success, response.Message)
	}
	preview := func() service.ModelsDevPlan {
		t.Helper()
		var plan service.ModelsDevPlan
		decode(call(PreviewModelsDev, http.MethodPost, "/api/option/models_dev/preview", ""), &plan)
		return plan
	}
	rowOf := func(plan service.ModelsDevPlan, name string) service.ModelsDevRow {
		t.Helper()
		for _, row := range plan.Rows {
			if row.Model == name {
				return row
			}
		}
		require.Failf(t, "missing row", "no row for %s", name)
		return service.ModelsDevRow{}
	}
	sync := func(body string) (modelsDevResponse, service.ModelsDevSyncResult) {
		t.Helper()
		response := call(SyncModelsDev, http.MethodPost, "/api/option/models_dev/sync", body)
		var result service.ModelsDevSyncResult
		if response.Success {
			decode(response, &result)
		}
		return response, result
	}
	configured := func(name string) model.PricingValues {
		t.Helper()
		snapshot, err := model.GetModelPricingSnapshot([]string{name})
		require.NoError(t, err)
		return snapshot.Entries[0].Configured
	}
	setPricing := func(name string, values model.PricingValues) {
		t.Helper()
		snapshot, err := model.GetModelPricingSnapshot([]string{name})
		require.NoError(t, err)
		require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: name, ExpectedVersion: snapshot.Entries[0].Version, Pricing: values}}))
	}
	expression := func(name string) string {
		text, _ := configured(name)["billing_setting.billing_expr"].(string)
		return text
	}

	// Two models of this site already have a price of their own.
	setPricing("gpt-5.4", model.PricingValues{"ModelRatio": 1.25, "CompletionRatio": 4.0})
	setPricing("deepseek-v4.1-flash", model.PricingValues{
		"billing_setting.billing_mode": "tiered_expr",
		"billing_setting.billing_expr": `tier("base", p * 0.2857 + c * 1.1429)`,
	})
	settings := service.DefaultModelsDevConfig()
	settings.IncludeCommonModels = false
	settings.Mappings = map[string]string{"deepseek-v4.1-flash": "deepseek/deepseek-v4-pro"}

	t.Run("the catalog keeps priced text models and marks the first-party listing", func(t *testing.T) {
		var page service.ModelsDevCatalogPage
		decode(call(GetModelsDevCatalog, http.MethodGet, "/api/option/models_dev/catalog?limit=200", ""), &page)
		official := map[string]bool{}
		for _, item := range page.Items {
			official[item.Key] = item.Official
		}
		for _, absent := range []string{"anthropic/claude-3-opus", "openai/gpt-image-1", "openai/text-embedding-3-large", "deepseek/deepseek-free"} {
			assert.NotContains(t, official, absent)
		}
		assert.True(t, official["anthropic/claude-sonnet-4-5"])
		assert.False(t, official["openrouter/anthropic/claude-sonnet-4.5"], "a reseller is not the vendor")
		assert.False(t, official["alibaba/kimi-k2.6"], "a vendor is a reseller of the models of another vendor")
		assert.True(t, official["moonshotai/kimi-k2.6"])
		assert.True(t, official["longcat/LongCat-2.0"], "the canonical vendor of LongCat is Meituan")
		assert.Equal(t, 13, page.Total)

		var found service.ModelsDevCatalogPage
		decode(call(GetModelsDevCatalog, http.MethodGet, "/api/option/models_dev/catalog?q=qwen3&provider=hosta", ""), &found)
		require.Len(t, found.Items, 1)
		assert.Equal(t, "hosta/vendor/qwen3-coder", found.Items[0].Key)
		assert.Equal(t, []string{"qwen3-coder"}, found.Items[0].SiteModels, "the model of this site that the entry prices by name")
		// The vendor's listing comes before a reseller's, dots count as dashes, and every word must occur.
		var sonnets service.ModelsDevCatalogPage
		decode(call(GetModelsDevCatalog, http.MethodGet, "/api/option/models_dev/catalog?q=claude-sonnet-4.5", ""), &sonnets)
		require.Len(t, sonnets.Items, 2)
		assert.Equal(t, "anthropic/claude-sonnet-4-5", sonnets.Items[0].Key)
		assert.Equal(t, "openrouter/anthropic/claude-sonnet-4.5", sonnets.Items[1].Key)
		var words service.ModelsDevCatalogPage
		decode(call(GetModelsDevCatalog, http.MethodGet, "/api/option/models_dev/catalog?q=openrouter%20sonnet", ""), &words)
		require.Len(t, words.Items, 1)
		assert.Equal(t, "openrouter/anthropic/claude-sonnet-4.5", words.Items[0].Key)
		var popular service.ModelsDevCatalogPage
		decode(call(GetModelsDevCatalog, http.MethodGet, "/api/option/models_dev/catalog?common=1&limit=200", ""), &popular)
		for _, item := range popular.Items {
			assert.True(t, item.Common)
		}
		assert.Equal(t, 9, popular.Total, "the newest models of the popular model families")
	})

	t.Run("the preview prefers the vendor and keeps prices that are set", func(t *testing.T) {
		configure(settings)
		plan := preview()
		sonnet := rowOf(plan, "claude-sonnet-4.5")
		assert.Equal(t, "anthropic/claude-sonnet-4-5", sonnet.Key, "the vendor wins over the cheaper reseller")
		assert.True(t, sonnet.Official)
		assert.Equal(t, service.ModelsDevStatusNew, sonnet.Status)
		assert.Equal(t, `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`, sonnet.Expression, "Claude names get the one hour cache price")

		gpt := rowOf(plan, "gpt-5.4")
		assert.Equal(t, service.ModelsDevStatusLocked, gpt.Status)
		assert.Equal(t, service.ModelsDevLockConfigured, gpt.LockReason)
		assert.Equal(t, `len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)`, gpt.Expression)
		assert.Equal(t, `len <= 200000 ? tier("standard", p * 1.25 + c * 10 + cr * 0.125) : tier("long_context", p * 2.5 + c * 15 + cr * 0.25)`, rowOf(plan, "gemini-3.6-pro").Expression)
		assert.Equal(t, `tier("base", p * 1 + c * 3.2 + cr * 0.2 + cc * 0)`, rowOf(plan, "glm-5").Expression, "a free cache write stays free")
		coder := rowOf(plan, "qwen3-coder")
		assert.Equal(t, "hosta/vendor/qwen3-coder", coder.Key, "without a vendor listing the cheapest one is used")
		assert.Equal(t, service.ModelsDevReviewReseller, coder.Review, "a reseller's price waits for a decision")
		assert.Equal(t, "moonshotai/kimi-k2.6", rowOf(plan, "kimi-k2.6").Key, "the vendor wins over a cheaper vendor that resells the model")

		mapped := rowOf(plan, "deepseek-v4.1-flash")
		assert.Equal(t, service.ModelsDevSourceMapping, mapped.Source)
		assert.Equal(t, service.ModelsDevStatusLocked, mapped.Status, "a mapped model keeps its own price too")
		assert.NotContains(t, plan.Unmatched, "deepseek-v4.1-flash")
		assert.ElementsMatch(t, []string{"qwen-plus", "deepseek-ratio", "deepseek-tiered", "gpt-plain"}, plan.Unmatched)
		builtin := rowOf(plan, "gpt-4o")
		assert.Equal(t, service.ModelsDevStatusLocked, builtin.Status)
		assert.Equal(t, service.ModelsDevLockDefault, builtin.LockReason, "the built-in price of the software is a price too")
		assert.Equal(t, 3, plan.Summary.Locked)
	})

	t.Run("a stale preview is refused", func(t *testing.T) {
		response, _ := sync(`{"plan_hash":"deadbeef"}`)
		assert.False(t, response.Success)
		assert.Equal(t, "The models.dev data or the prices changed since the preview. Preview again", response.MessageKey)
	})

	t.Run("the import writes expressions and leaves the prices that are set", func(t *testing.T) {
		plan := preview()
		response, result := sync(`{"plan_hash":"` + plan.Hash + `"}`)
		require.True(t, response.Success, response.Message)
		assert.ElementsMatch(t, []string{"claude-sonnet-4.5", "gemini-3.6-pro", "glm-5", "kimi-k2.6", "deepseek-v4-pro"}, result.Written)
		assert.Equal(t, []string{"qwen3-coder"}, result.SkippedReview, "the price of a reseller is not written unasked")
		assert.Equal(t, 5, result.Summary.Created)
		assert.Equal(t, `tier("base", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)`, expression("claude-sonnet-4.5"))
		assert.Equal(t, "tiered_expr", configured("glm-5")["billing_setting.billing_mode"])
		gpt := configured("gpt-5.4")
		assert.NotContains(t, gpt, "billing_setting.billing_expr")
		assert.Equal(t, 1.25, gpt["ModelRatio"])
		assert.Equal(t, `tier("base", p * 0.2857 + c * 1.1429)`, expression("deepseek-v4.1-flash"))

		var overview service.ModelsDevOverview
		decode(call(GetModelsDevOverview, http.MethodGet, "/api/option/models_dev/state", ""), &overview)
		assert.Equal(t, 5, overview.State.ManagedCount)
		assert.Equal(t, 5, overview.State.UndoCount)
		assert.Equal(t, "v-test-1", overview.State.LastSyncVersion)
		assert.Equal(t, service.ModelsDevTriggerManual, overview.State.LastTrigger)
	})

	t.Run("an administrator approves the price of a reseller", func(t *testing.T) {
		_, result := sync(`{"approved":["qwen3-coder"]}`)
		assert.Equal(t, []string{"qwen3-coder"}, result.Written)
		assert.Equal(t, `tier("base", p * 0.3 + c * 0.9)`, expression("qwen3-coder"))
	})

	t.Run("a price the import wrote follows models.dev until somebody changes it", func(t *testing.T) {
		source.setCost("anthropic", "claude-sonnet-4-5", 3.5, 15)
		sonnet := rowOf(preview(), "claude-sonnet-4.5")
		assert.Equal(t, service.ModelsDevStatusUpdate, sonnet.Status)
		assert.InDelta(t, (3*3.5+15)/(3*3.0+15), sonnet.Change, 0.0001)
		assert.Empty(t, sonnet.Review)
		_, result := sync("")
		assert.Equal(t, []string{"claude-sonnet-4.5"}, result.Written)
		assert.Contains(t, expression("claude-sonnet-4.5"), "p * 3.5")
		assert.Equal(t, service.ModelsDevStatusUnchanged, rowOf(preview(), "glm-5").Status)

		edited := configured("glm-5")
		edited["billing_setting.billing_expr"] = `tier("base", p * 2 + c * 6)`
		setPricing("glm-5", edited)
		source.setCost("zai", "glm-5", 1.1, 3.2)
		glm := rowOf(preview(), "glm-5")
		assert.Equal(t, service.ModelsDevStatusLocked, glm.Status)
		assert.Equal(t, service.ModelsDevLockModified, glm.LockReason, "a price changed after the import is the administrator's")
		_, result = sync("")
		assert.NotContains(t, result.Written, "glm-5")
		assert.Equal(t, `tier("base", p * 2 + c * 6)`, expression("glm-5"))
	})

	t.Run("a change by more than ten times waits for a decision", func(t *testing.T) {
		source.setCost("deepseek", "deepseek-v4-pro", 13.2, 39.6)
		pro := rowOf(preview(), "deepseek-v4-pro")
		assert.Equal(t, service.ModelsDevReviewLargeChange, pro.Review)
		assert.InDelta(t, 20.0, pro.Change, 0.001)
		_, result := sync("")
		assert.Equal(t, []string{"deepseek-v4-pro"}, result.SkippedReview)
		assert.Empty(t, result.Written)
		assert.Contains(t, expression("deepseek-v4-pro"), "p * 0.66")
		_, result = sync(`{"approved":["deepseek-v4-pro"]}`)
		assert.Equal(t, []string{"deepseek-v4-pro"}, result.Written)
		assert.Contains(t, expression("deepseek-v4-pro"), "p * 13.2")
	})

	t.Run("the settings refuse a model that is locked and unlocked", func(t *testing.T) {
		bad := settings
		bad.LockedModels, bad.UnlockedModels = []string{"x"}, []string{"x"}
		body, err := common.Marshal(bad)
		require.NoError(t, err)
		response := call(UpdateModelsDevConfig, http.MethodPut, "/api/option/models_dev/config", string(body))
		assert.False(t, response.Success)
		assert.Equal(t, "{{model}} cannot be locked and unlocked at once", response.MessageKey)
	})

	t.Run("an administrator unlocks or locks a model", func(t *testing.T) {
		settings.UnlockedModels, settings.LockedModels = []string{"gpt-5.4"}, []string{"qwen3-coder"}
		configure(settings)
		plan := preview()
		assert.Equal(t, service.ModelsDevStatusNew, rowOf(plan, "gpt-5.4").Status)
		qwen := rowOf(plan, "qwen3-coder")
		assert.Equal(t, service.ModelsDevStatusLocked, qwen.Status)
		assert.Equal(t, service.ModelsDevLockExplicit, qwen.LockReason)

		_, result := sync(`{"models":["gpt-5.4"]}`)
		assert.Equal(t, []string{"gpt-5.4"}, result.Written)
		gpt := configured("gpt-5.4")
		assert.Equal(t, 1.25, gpt["ModelRatio"], "the old price stays, to go back to it")
		assert.Contains(t, expression("gpt-5.4"), "len <= 272000")
	})

	t.Run("undo puts the replaced prices back and keeps what was edited since", func(t *testing.T) {
		var undone service.ModelsDevUndoResult
		decode(call(UndoModelsDevSync, http.MethodPost, "/api/option/models_dev/undo", ""), &undone)
		assert.Equal(t, []string{"gpt-5.4"}, undone.Restored)
		gpt := configured("gpt-5.4")
		assert.NotContains(t, gpt, "billing_setting.billing_expr")
		assert.Equal(t, 1.25, gpt["ModelRatio"])
		again := call(UndoModelsDevSync, http.MethodPost, "/api/option/models_dev/undo", "")
		assert.False(t, again.Success)
		assert.Equal(t, "There is no models.dev sync to undo", again.MessageKey)

		settings.UnlockedModels, settings.LockedModels = nil, nil
		configure(settings)
		source.setCost("google", "gemini-3.6-pro", 1.5, 10)
		_, result := sync("")
		require.Equal(t, []string{"gemini-3.6-pro"}, result.Written)
		edited := configured("gemini-3.6-pro")
		edited["billing_setting.billing_expr"] = `tier("base", p * 9 + c * 9)`
		setPricing("gemini-3.6-pro", edited)
		var conflicted service.ModelsDevUndoResult
		decode(call(UndoModelsDevSync, http.MethodPost, "/api/option/models_dev/undo", ""), &conflicted)
		assert.Empty(t, conflicted.Restored)
		assert.Equal(t, []string{"gemini-3.6-pro"}, conflicted.Conflicts)
		assert.Equal(t, `tier("base", p * 9 + c * 9)`, expression("gemini-3.6-pro"))
	})

	t.Run("a manual import does not start beside the automatic one", func(t *testing.T) {
		task, err := model.CreateSystemTask(model.SystemTaskTypeModelsDevPricing, nil, nil)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, model.DB.Delete(&model.SystemTask{}, task.ID).Error) })
		response, _ := sync("")
		assert.False(t, response.Success)
		assert.Equal(t, "A models.dev sync is already running", response.MessageKey)
		undo := call(UndoModelsDevSync, http.MethodPost, "/api/option/models_dev/undo", "")
		assert.Equal(t, "A models.dev sync is already running", undo.MessageKey)
	})

	t.Run("a new release imports once when the switch is on", func(t *testing.T) {
		common.Version = "v-test-2"
		assert.False(t, service.ModelsDevAutoSyncDue(), "the switch is off")
		settings.AutoSyncEnabled = true
		configure(settings)
		require.True(t, service.ModelsDevAutoSyncDue())
		handler := modelsDevPricingHandler{}
		assert.Zero(t, handler.Interval(), "the first try of a release does not wait for the retry interval")

		source.setStatus(http.StatusInternalServerError)
		for attempt := 0; attempt < 6; attempt++ {
			require.True(t, service.ModelsDevAutoSyncDue(), "attempt %d", attempt+1)
			_, err := service.RunModelsDevAutoSync(context.Background())
			require.Error(t, err)
			assert.Equal(t, time.Hour, handler.Interval(), "a retry waits for the interval")
		}
		assert.False(t, service.ModelsDevAutoSyncDue(), "a failing import is given up after a few tries")
		var failed service.ModelsDevOverview
		decode(call(GetModelsDevOverview, http.MethodGet, "/api/option/models_dev/state", ""), &failed)
		assert.Contains(t, failed.State.LastError, "HTTP 500")
		assert.NotEqual(t, "v-test-2", failed.State.LastSyncVersion)

		common.Version = "v-test-3"
		source.setStatus(0)
		require.True(t, service.ModelsDevAutoSyncDue(), "the next release tries again")
		assert.Zero(t, handler.Interval(), "and does not wait for the retries of the release before")
		result, err := service.RunModelsDevAutoSync(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, service.ModelsDevAutoSyncDue(), "this release has imported")
		var done service.ModelsDevOverview
		decode(call(GetModelsDevOverview, http.MethodGet, "/api/option/models_dev/state", ""), &done)
		assert.Equal(t, "v-test-3", done.State.LastSyncVersion)
		assert.Equal(t, service.ModelsDevTriggerRelease, done.State.LastTrigger)
		assert.Empty(t, done.State.LastError)

		again, err := service.RunModelsDevAutoSync(context.Background())
		require.NoError(t, err)
		assert.Empty(t, again.Written, "a second task of an imported release has nothing to do")
		var kept service.ModelsDevOverview
		decode(call(GetModelsDevOverview, http.MethodGet, "/api/option/models_dev/state", ""), &kept)
		assert.Equal(t, done.State.LastSummary, kept.State.LastSummary, "and leaves the summary of the import alone")
		assert.Equal(t, done.State.LastSyncAt, kept.State.LastSyncAt)

		settings.AutoSyncEnabled = false
		configure(settings)
		common.Version = "v-test-4"
		assert.False(t, service.ModelsDevAutoSyncDue(), "switching it off stops the import")
	})

	t.Run("the editor gets the expression of one entry", func(t *testing.T) {
		var result service.ModelsDevExpressionResult
		decode(call(GetModelsDevExpression, http.MethodGet, "/api/option/models_dev/expression?key=anthropic/claude-haiku-4-5&model=my-claude", ""), &result)
		assert.Equal(t, `tier("base", p * 1 + c * 5 + cr * 0.1 + cc * 1.25 + cc1h * 2)`, result.Expression)
		assert.Equal(t, "models.dev does not list this model", call(GetModelsDevExpression, http.MethodGet, "/api/option/models_dev/expression?key=none/none", "").MessageKey)
		assert.Equal(t, "Choose a models.dev model", call(GetModelsDevExpression, http.MethodGet, "/api/option/models_dev/expression", "").MessageKey)
	})

	t.Run("common models are priced under their models.dev names", func(t *testing.T) {
		settings.IncludeCommonModels = true
		configure(settings)
		plan := preview()
		haiku := rowOf(plan, "claude-haiku-4-5")
		assert.Equal(t, service.ModelsDevSourceCommon, haiku.Source)
		assert.Equal(t, service.ModelsDevStatusNew, haiku.Status)
		assert.Equal(t, service.ModelsDevSourceSite, rowOf(plan, "deepseek-v4-pro").Source, "a model of this site keeps its own choice")
		longcat := rowOf(plan, "longcat-2.0")
		assert.Equal(t, service.ModelsDevSourceCommon, longcat.Source)
		assert.True(t, longcat.Official)
		assert.Empty(t, longcat.Review, "the vendor's own price needs no approval")
	})
}
