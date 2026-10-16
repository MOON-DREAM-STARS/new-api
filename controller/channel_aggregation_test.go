package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The chain rule decides which tiers a request may use. It backs live
// routing, the user's strategy preview and the administrator's route test.
func TestServiceTierChainFollowsUserAuthorization(t *testing.T) {
	offerAll := model.AggregationPolicyConfig{Tiers: map[string]model.AggregationTierConfig{
		model.ServiceTierEconomy:     {Enabled: true, Ratio: 0.15},
		model.ServiceTierBalanced:    {Enabled: true, Ratio: 0.20},
		model.ServiceTierPerformance: {Enabled: true, Ratio: 0.40},
	}}
	withoutEconomy := model.AggregationPolicyConfig{Tiers: map[string]model.AggregationTierConfig{
		model.ServiceTierBalanced:    {Enabled: true, Ratio: 0.20},
		model.ServiceTierPerformance: {Enabled: true, Ratio: 0.40},
	}}
	withoutBalanced := model.AggregationPolicyConfig{Tiers: map[string]model.AggregationTierConfig{
		model.ServiceTierEconomy:     {Enabled: true, Ratio: 0.15},
		model.ServiceTierPerformance: {Enabled: true, Ratio: 0.40},
	}}
	eco, bal, perf := model.ServiceTierEconomy, model.ServiceTierBalanced, model.ServiceTierPerformance
	for _, tc := range []struct {
		name      string
		config    model.AggregationPolicyConfig
		settings  service.ServiceTierSettings
		want      []string
		preferred string
	}{
		{"lowest skips a turned-off balanced tier when escalating", offerAll, service.ServiceTierSettings{EnabledTiers: []string{eco, perf}, Preference: "lowest", AllowEscalation: true, MaxEscalationTier: perf}, []string{eco, perf}, eco},
		{"escalation stops at the highest allowed tier", offerAll, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "lowest", AllowEscalation: true, MaxEscalationTier: bal}, []string{eco, bal}, eco},
		{"escalation off keeps the preferred tier only", offerAll, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "lowest"}, []string{eco}, eco},
		{"performance preference never moves down", offerAll, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "performance"}, []string{perf}, perf},
		{"performance preference is not limited by the low-price ceiling", offerAll, service.ServiceTierSettings{EnabledTiers: []string{eco, perf}, Preference: "performance"}, []string{perf}, perf},
		{"lowest starts at the cheapest tier the model offers", withoutEconomy, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "lowest"}, []string{bal}, bal},
		{"a preferred tier the model lacks is never replaced by a cheaper one", withoutBalanced, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "balanced"}, []string{}, bal},
		{"a missing preferred tier may only escalate", withoutBalanced, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "balanced", AllowEscalation: true, MaxEscalationTier: perf}, []string{perf}, bal},
		{"a tier the user turned off is never used", offerAll, service.ServiceTierSettings{EnabledTiers: []string{perf}, Preference: "lowest"}, []string{perf}, perf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps, preferred := service.BuildServiceTierChain(tc.config, tc.settings)
			tiers := make([]string, 0, len(steps))
			for _, step := range steps {
				tiers = append(tiers, step.Tier)
			}
			assert.Equal(t, tc.want, tiers)
			assert.Equal(t, tc.preferred, preferred)
		})
	}
}

func TestServiceTierSettingsValidation(t *testing.T) {
	eco, bal, perf := model.ServiceTierEconomy, model.ServiceTierBalanced, model.ServiceTierPerformance
	for _, tc := range []struct {
		name     string
		settings service.ServiceTierSettings
		message  string
	}{
		{"at least one tier", service.ServiceTierSettings{Preference: "lowest"}, "Enable at least one service tier"},
		{"preferred tier must be on", service.ServiceTierSettings{EnabledTiers: []string{eco, perf}, Preference: "balanced"}, "The preferred tier is turned off. Turn it on or choose another preference"},
		{"ceiling must be above the preferred tier", service.ServiceTierSettings{EnabledTiers: []string{eco, bal}, Preference: "balanced", AllowEscalation: true, MaxEscalationTier: eco}, "Choose a highest escalation tier that is turned on and above the preferred tier"},
		{"ceiling must be turned on", service.ServiceTierSettings{EnabledTiers: []string{eco, bal}, Preference: "lowest", AllowEscalation: true, MaxEscalationTier: perf}, "Choose a highest escalation tier that is turned on and above the preferred tier"},
		{"unknown tier", service.ServiceTierSettings{EnabledTiers: []string{"premium"}, Preference: "lowest"}, "Unknown service tier: {{tier}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.NormalizeServiceTierSettings(tc.settings)
			var message *common.Message
			require.ErrorAs(t, err, &message)
			assert.Equal(t, tc.message, message.Key)
		})
	}
	normalized, err := service.NormalizeServiceTierSettings(service.ServiceTierSettings{EnabledTiers: []string{perf, eco}, Preference: "lowest", MaxEscalationTier: perf})
	require.NoError(t, err)
	assert.Equal(t, []string{eco, perf}, normalized.EnabledTiers)
	assert.Empty(t, normalized.MaxEscalationTier, "a ceiling without escalation is dropped")
}

type serviceTierUpstream struct {
	mu       sync.Mutex
	behavior map[string]string
	calls    map[string]int
}

func (u *serviceTierUpstream) set(behavior map[string]string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.behavior, u.calls = behavior, map[string]int{}
}

func (u *serviceTierUpstream) callCounts() map[string]int {
	u.mu.Lock()
	defer u.mu.Unlock()
	counts := make(map[string]int, len(u.calls))
	for key, count := range u.calls {
		counts[key] = count
	}
	return counts
}

func (u *serviceTierUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	u.mu.Lock()
	u.calls[key]++
	behavior := u.behavior[key]
	u.mu.Unlock()
	body := `{"id":"chatcmpl-tier","object":"chat.completion","created":1,"model":"upstream","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":1000,"total_tokens":2000}}`
	switch behavior {
	case "", "ok":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	case "stream_cut":
		// The response starts, then the upstream connection drops.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `data: {"id":"chatcmpl-tier","object":"chat.completion.chunk","created":1,"model":"upstream","choices":[{"index":0,"delta":{"content":"partial"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	default:
		var status int
		_, _ = fmt.Sscanf(behavior, "%d", &status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"upstream %s","type":"upstream_error"}}`, behavior)
	}
}

type serviceTierFixture struct {
	engine   *gin.Engine
	upstream *serviceTierUpstream
	user     *model.User
	token    *model.Token
	families map[string]model.ModelFamily
	channels map[string]*model.Channel
	settings int64
}

func newServiceTierFixture(t *testing.T) *serviceTierFixture {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousMemory, previousRedis, previousSQLite := common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath
	previousBatch, previousLogs, previousCount, previousQuota := common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken, common.QuotaPerUnit
	previousTrust, previousTimeout := operation_setting.GetQuotaSetting().TrustQuotaUSD, constant.StreamingTimeout
	previousGroups := setting.UserUsableGroups2JSONString()
	previousPrices, previousRatios, previousCompletion := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	previousConfig := config.GlobalConfig.ExportAllConfigs()

	require.NoError(t, i18n.Init())
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled = true, false, false
	common.SQLitePath = filepath.Join(t.TempDir(), "service-tier.db")
	common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken, common.QuotaPerUnit = false, true, false, 500000
	operation_setting.GetQuotaSetting().TrustQuotaUSD, constant.StreamingTimeout = 0, 30
	// The full startup migration creates the aggregation tables and seeds
	// the default families.
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio": `{"default":1}`,
		"billing_setting.billing_mode":    `{"deepseek-tiered":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"deepseek-tiered":"tier(\"base\", p * 1 + c * 1)"}`,
	}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"deepseek-v4-pro":0.002,"qwen-plus":0.002,"gpt-plain":0.002}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"deepseek-ratio":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"deepseek-ratio":1}`))

	fixture := &serviceTierFixture{upstream: &serviceTierUpstream{behavior: map[string]string{}, calls: map[string]int{}}, families: map[string]model.ModelFamily{}, channels: map[string]*model.Channel{}}
	server := httptest.NewServer(fixture.upstream)
	t.Cleanup(func() {
		// Leave no aggregation behind for later tests in this package.
		require.NoError(t, model.DB.Where("1 = 1").Delete(&model.AggregationPolicy{}).Error)
		require.NoError(t, model.DB.Where("1 = 1").Delete(&model.ModelFamilyMember{}).Error)
		require.NoError(t, service.RefreshServiceTierCache())
		server.Close()
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(previousCompletion))
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"group_ratio_setting.group_ratio": previousConfig["group_ratio_setting.group_ratio"],
			"billing_setting.billing_mode":    previousConfig["billing_setting.billing_mode"],
			"billing_setting.billing_expr":    previousConfig["billing_setting.billing_expr"],
		}))
		operation_setting.GetQuotaSetting().TrustQuotaUSD, constant.StreamingTimeout = previousTrust, previousTimeout
		common.BatchUpdateEnabled, common.LogConsumeEnabled, constant.CountToken, common.QuotaPerUnit = previousBatch, previousLogs, previousCount, previousQuota
		common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath = previousMaster, previousMemory, previousRedis, previousSQLite
		common.SetDatabaseTypes(previousMain, previousLog)
		require.NoError(t, sqlDB.Close())
		model.DB, model.LOG_DB = previousDB, previousLogDB
	})

	models := "deepseek-v4-pro,deepseek-ratio,deepseek-tiered,qwen-plus"
	autoBan := 0
	for _, spec := range []struct {
		key      string
		models   string
		priority int64
	}{
		{"eco-a", models, 0}, {"eco-b", models, 0}, {"eco-c", models, 0}, {"bal-a", models, 0}, {"perf-a", models, 0}, {"plain", "gpt-plain", 0}, {"other-model", "gpt-plain", 0},
	} {
		channel := &model.Channel{Name: spec.key, Key: spec.key, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: spec.models, Group: "default", BaseURL: &server.URL, AutoBan: &autoBan, Priority: common.GetPointer(spec.priority)}
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
		fixture.channels[spec.key] = channel
	}
	user := &model.User{Username: "tier-user", Status: common.UserStatusEnabled, Group: "default", Quota: 100000, AuthVersion: 1, Setting: `{"billing_preference":"wallet_only"}`}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "servicetiertoken", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100000}
	require.NoError(t, model.DB.Create(token).Error)
	fixture.user, fixture.token = user, token

	store, err := model.LoadServiceTierStore()
	require.NoError(t, err)
	for _, family := range store.Families {
		fixture.families[family.Key] = family
	}
	deepseek, qwen := fixture.families["deepseek"], fixture.families["qwen"]
	_, err = model.ReplaceModelFamilyMembers(deepseek.Id, deepseek.Version, []string{"deepseek-v4-pro", "deepseek-ratio", "deepseek-tiered"})
	require.NoError(t, err)
	_, err = model.ReplaceModelFamilyMembers(qwen.Id, qwen.Version, []string{"qwen-plus"})
	require.NoError(t, err)

	engine := gin.New()
	relayRoute := func(format types.RelayFormat) gin.HandlerFunc {
		return func(c *gin.Context) { Relay(c, format) }
	}
	engine.POST("/v1/chat/completions", middleware.TokenAuth(), middleware.Distribute(), relayRoute(types.RelayFormatOpenAI))
	engine.POST("/v1/embeddings", middleware.TokenAuth(), middleware.Distribute(), relayRoute(types.RelayFormatEmbedding))
	engine.GET("/v1/models", middleware.TokenAuth(), func(c *gin.Context) { ListModels(c, constant.ChannelTypeOpenAI) })
	fixture.engine = engine
	return fixture
}

type tierPool map[string][]string

// savePolicy stores a family default through the administrator's
// validation path. pool lists channel keys per tier in priority order.
func (f *serviceTierFixture) savePolicy(t *testing.T, family string, pool tierPool, strategy func(*model.AggregationStrategy)) {
	t.Helper()
	ratios := map[string]float64{model.ServiceTierEconomy: 0.15, model.ServiceTierBalanced: 0.20, model.ServiceTierPerformance: 0.40}
	f.savePolicyWithRatios(t, family, pool, ratios, strategy)
}

func (f *serviceTierFixture) savePolicyWithRatios(t *testing.T, family string, pool tierPool, ratios map[string]float64, strategy func(*model.AggregationStrategy)) {
	t.Helper()
	config := model.DefaultAggregationPolicyConfig(ratios)
	for tier, keys := range pool {
		tierConfig := config.Tiers[tier]
		tierConfig.Enabled = true
		for index, key := range keys {
			tierConfig.Channels = append(tierConfig.Channels, model.AggregationTierChannel{ChannelId: f.channels[key].Id, Priority: int64(100 - index), Weight: 1, Enabled: true})
		}
		config.Tiers[tier] = tierConfig
	}
	if strategy != nil {
		strategy(&config.Strategy)
	}
	familyID := f.families[family].Id
	version := int64(0)
	var existing model.AggregationPolicy
	if err := model.DB.Where("family_id = ? AND model_name = ?", familyID, "").First(&existing).Error; err == nil {
		version = existing.Version
	}
	_, _, err := service.SaveServiceTierPolicy(service.ServiceTierPolicyInput{FamilyID: familyID, Enabled: true, Config: config, Version: version}, 1)
	require.NoError(t, err)
}

func (f *serviceTierFixture) saveSettings(t *testing.T, settings service.ServiceTierSettings) {
	t.Helper()
	saved, err := service.SaveUserServiceTierSettings(f.user.Id, settings, f.settings)
	require.NoError(t, err)
	f.settings = saved.Version
}

// reset sets the balances and upstream behavior for one scenario.
func (f *serviceTierFixture) reset(t *testing.T, userQuota int, behavior map[string]string) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", f.user.Id).Update("quota", userQuota).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", f.token.Id).Update("remain_quota", 100000).Error)
	require.NoError(t, model.LOG_DB.Where("1 = 1").Delete(&model.Log{}).Error)
	f.upstream.set(behavior)
}

func (f *serviceTierFixture) request(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer sk-"+f.token.Key)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	f.engine.ServeHTTP(recorder, request)
	return recorder
}

func (f *serviceTierFixture) chat(t *testing.T, modelName string) *httptest.ResponseRecorder {
	t.Helper()
	// The OpenAI service_tier field is forwarded upstream and never selects
	// an aggregation tier.
	return f.request(t, http.MethodPost, "/v1/chat/completions", fmt.Sprintf(`{"model":%q,"service_tier":"priority","messages":[{"role":"user","content":"hi"}]}`, modelName))
}

// settled waits for asynchronous refunds and returns the wallet, the token
// balance and the consume logs.
func (f *serviceTierFixture) settled(t *testing.T, wantUserQuota int) (int, []model.Log) {
	t.Helper()
	require.Eventually(t, func() bool {
		quota, err := model.GetUserQuota(f.user.Id, true)
		return err == nil && quota == wantUserQuota
	}, 3*time.Second, 10*time.Millisecond)
	var token model.Token
	require.NoError(t, model.DB.First(&token, f.token.Id).Error)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Order("id asc").Find(&logs).Error)
	return token.RemainQuota, logs
}

func serviceTierLogInfo(t *testing.T, log model.Log) map[string]any {
	t.Helper()
	other := map[string]any{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	info, ok := other["service_tier"].(map[string]any)
	require.True(t, ok, log.Other)
	return info
}

func TestServiceTierRoutingAndBilling(t *testing.T) {
	f := newServiceTierFixture(t)
	eco, bal, perf := model.ServiceTierEconomy, model.ServiceTierBalanced, model.ServiceTierPerformance
	f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a", "eco-b"}, bal: {"bal-a"}, perf: {"perf-a"}}, nil)
	f.savePolicy(t, "qwen", tierPool{eco: {"eco-c"}}, nil)

	t.Run("default settings use the cheapest tier and charge its ratio", func(t *testing.T) {
		f.reset(t, 10000, nil)
		response := f.chat(t, "deepseek-v4-pro")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		tokenQuota, logs := f.settled(t, 10000-150)
		assert.Equal(t, map[string]int{"eco-a": 1}, f.upstream.callCounts())
		require.Len(t, logs, 1)
		assert.Equal(t, 150, logs[0].Quota, "fixed price 0.002 USD at the economy ratio 0.15")
		assert.Equal(t, 100000-150, tokenQuota)
		info := serviceTierLogInfo(t, logs[0])
		assert.Equal(t, eco, info["final_tier"])
		assert.Equal(t, false, info["escalated"])
		assert.NotContains(t, info, "channel_id", "the public log part carries no channel")
	})

	t.Run("one key serves several families and ordinary models", func(t *testing.T) {
		f.reset(t, 10000, nil)
		for _, name := range []string{"deepseek-v4-pro", "qwen-plus", "gpt-plain"} {
			response := f.chat(t, name)
			require.Equal(t, http.StatusOK, response.Code, name+": "+response.Body.String())
		}
		counts := f.upstream.callCounts()
		assert.Equal(t, 1, counts["eco-a"])
		assert.Equal(t, 1, counts["eco-c"], "qwen follows its own family pool")
		assert.Equal(t, 1, counts["plain"]+counts["other-model"], "a model outside aggregation keeps group routing")
		models := f.request(t, http.MethodGet, "/v1/models", "")
		require.Equal(t, http.StatusOK, models.Code)
		assert.Equal(t, 1, strings.Count(models.Body.String(), `"id":"deepseek-v4-pro"`), "five channels serve the model; it is listed once")
	})

	t.Run("a failed channel hands over to the next channel of the same tier", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "500"})
		response := f.chat(t, "deepseek-v4-pro")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		_, logs := f.settled(t, 10000-150)
		assert.Equal(t, map[string]int{"eco-a": 1, "eco-b": 1}, f.upstream.callCounts())
		require.Len(t, logs, 1)
		assert.Equal(t, 150, logs[0].Quota, "same-tier failover is not an escalation")
		info := serviceTierLogInfo(t, logs[0])
		assert.Equal(t, float64(2), info["attempts"])
		assert.Equal(t, false, info["escalated"])
	})

	f.saveSettings(t, service.ServiceTierSettings{EnabledTiers: []string{eco, perf}, Preference: "lowest", AllowEscalation: true, MaxEscalationTier: perf})

	t.Run("an exhausted tier escalates past a turned-off tier and bills the tier that served", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "500", "eco-b": "429"})
		response := f.chat(t, "deepseek-v4-pro")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		_, logs := f.settled(t, 10000-400)
		assert.Equal(t, map[string]int{"eco-a": 1, "eco-b": 1, "perf-a": 1}, f.upstream.callCounts(), "balanced is off for this user")
		require.Len(t, logs, 1)
		assert.Equal(t, 400, logs[0].Quota)
		info := serviceTierLogInfo(t, logs[0])
		assert.Equal(t, perf, info["final_tier"])
		assert.Equal(t, true, info["escalated"])
		assert.Equal(t, 0.4, info["ratio"])
		other := map[string]any{}
		require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
		adminInfo := other["admin_info"].(map[string]any)
		route := adminInfo["service_tier_route"].(map[string]any)
		attempts := route["attempts"].([]any)
		require.Len(t, attempts, 3)
		assert.Equal(t, "same_tier_failover", attempts[0].(map[string]any)["decision"])
		assert.Equal(t, "tier_escalation", attempts[1].(map[string]any)["decision"])
		assert.Equal(t, "success", attempts[2].(map[string]any)["outcome"])
	})

	t.Run("token ratio and tiered expression billing follow the serving tier", func(t *testing.T) {
		for _, tc := range []struct {
			model string
			quota int
		}{
			{"deepseek-ratio", 800},  // (1000 prompt + 1000 completion) x model ratio 1 x 0.40
			{"deepseek-tiered", 400}, // 2000 USD/1M tokens = 1000 quota x 0.40
		} {
			f.reset(t, 10000, map[string]string{"eco-a": "500", "eco-b": "500"})
			response := f.chat(t, tc.model)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			_, logs := f.settled(t, 10000-tc.quota)
			require.Len(t, logs, 1, tc.model)
			assert.Equal(t, tc.quota, logs[0].Quota, tc.model)
		}
	})

	t.Run("escalation stops before a tier the balance cannot cover", func(t *testing.T) {
		// 300 covers the economy reservation (150) but not performance (400).
		f.reset(t, 300, map[string]string{"eco-a": "500", "eco-b": "500"})
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "insufficient_user_quota")
		_, logs := f.settled(t, 300)
		assert.Zero(t, f.upstream.callCounts()["perf-a"], "the unaffordable tier is never called")
		assert.Empty(t, logs, "nothing is charged")
	})

	t.Run("a response that already started is not switched to another channel", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "stream_cut"})
		response := f.request(t, http.MethodPost, "/v1/chat/completions", `{"model":"deepseek-v4-pro","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		assert.Contains(t, response.Body.String(), "partial")
		counts := f.upstream.callCounts()
		assert.Equal(t, 1, counts["eco-a"])
		assert.Zero(t, counts["eco-b"]+counts["perf-a"])
	})

	t.Run("client errors are not relayed", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "400"})
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusBadRequest, response.Code)
		f.settled(t, 10000)
		assert.Equal(t, map[string]int{"eco-a": 1}, f.upstream.callCounts())
	})

	f.saveSettings(t, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "lowest", AllowEscalation: true, MaxEscalationTier: bal})

	t.Run("escalation never passes the highest allowed tier", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "500", "eco-b": "500", "bal-a": "500"})
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusInternalServerError, response.Code)
		f.settled(t, 10000)
		counts := f.upstream.callCounts()
		assert.Equal(t, 1, counts["bal-a"])
		assert.Zero(t, counts["perf-a"], "performance is enabled but above the ceiling")
	})

	f.saveSettings(t, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "lowest"})

	t.Run("without escalation a request stays in its tier", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "500", "eco-b": "500"})
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusInternalServerError, response.Code)
		f.settled(t, 10000)
		assert.Equal(t, map[string]int{"eco-a": 1, "eco-b": 1}, f.upstream.callCounts())
	})

	f.saveSettings(t, service.ServiceTierSettings{EnabledTiers: []string{eco, bal, perf}, Preference: "performance"})

	t.Run("a performance preference never moves down", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"perf-a": "500"})
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusInternalServerError, response.Code)
		f.settled(t, 10000)
		assert.Equal(t, map[string]int{"perf-a": 1}, f.upstream.callCounts())
	})

	f.saveSettings(t, service.ServiceTierSettings{EnabledTiers: []string{eco}, Preference: "lowest"})

	t.Run("an empty tier fails instead of borrowing another tier", func(t *testing.T) {
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{f.channels["eco-a"].Id, f.channels["eco-b"].Id}).Update("status", common.ChannelStatusManuallyDisabled).Error)
		t.Cleanup(func() {
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{f.channels["eco-a"].Id, f.channels["eco-b"].Id}).Update("status", common.ChannelStatusEnabled).Error)
		})
		f.reset(t, 10000, nil)
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusServiceUnavailable, response.Code)
		assert.Contains(t, response.Body.String(), "deepseek-v4-pro")
		assert.Empty(t, f.upstream.callCounts())
	})

	t.Run("other endpoints cannot reach a model on aggregation", func(t *testing.T) {
		f.reset(t, 10000, nil)
		response := f.request(t, http.MethodPost, "/v1/embeddings", `{"model":"deepseek-v4-pro","input":"hi"}`)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Empty(t, f.upstream.callCounts())
	})

	f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a", "eco-b", "eco-c"}}, func(strategy *model.AggregationStrategy) { strategy.MaxAttempts = 2 })

	t.Run("the attempt budget bounds failover", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "500", "eco-b": "500", "eco-c": "500"})
		response := f.chat(t, "deepseek-v4-pro")
		assert.Equal(t, http.StatusInternalServerError, response.Code)
		f.settled(t, 10000)
		counts := f.upstream.callCounts()
		assert.Equal(t, 2, counts["eco-a"]+counts["eco-b"]+counts["eco-c"])
		for key, count := range counts {
			assert.Equal(t, 1, count, "%s is not retried within the request", key)
		}
	})

	t.Run("concurrent requests are charged exactly once each", func(t *testing.T) {
		f.reset(t, 10000, map[string]string{"eco-a": "500"})
		var wg sync.WaitGroup
		for range 6 {
			wg.Go(func() {
				response := f.chat(t, "deepseek-v4-pro")
				assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
			})
		}
		wg.Wait()
		tokenQuota, logs := f.settled(t, 10000-6*150)
		require.Len(t, logs, 6)
		total := 0
		for _, log := range logs {
			total += log.Quota
		}
		assert.Equal(t, 6*150, total, "usage logs match the wallet")
		assert.Equal(t, 100000-6*150, tokenQuota, "usage logs match the API key balance")
	})
}

func TestServiceTierEscalationPreConsumesFromUnroundedEstimate(t *testing.T) {
	f := newServiceTierFixture(t)
	eco, perf := model.ServiceTierEconomy, model.ServiceTierPerformance
	f.savePolicyWithRatios(t, "deepseek", tierPool{eco: {"eco-a"}, perf: {"perf-a"}}, map[string]float64{
		eco: 0.0001, model.ServiceTierBalanced: 0.20, perf: 0.40,
	}, nil)
	f.saveSettings(t, service.ServiceTierSettings{
		EnabledTiers: []string{eco, perf}, Preference: "lowest", AllowEscalation: true, MaxEscalationTier: perf,
	})
	f.reset(t, 10, map[string]string{"eco-a": "429"})

	response := f.chat(t, "deepseek-v4-pro")
	assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), "insufficient_user_quota")
	f.settled(t, 10)
	assert.Equal(t, map[string]int{"eco-a": 1}, f.upstream.callCounts(), "the high tier must not be called without its reservation")
}

func TestServiceTierAPIs(t *testing.T) {
	f := newServiceTierFixture(t)
	eco, perf := model.ServiceTierEconomy, model.ServiceTierPerformance
	f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a"}, perf: {"perf-a"}}, nil)
	call := func(handler gin.HandlerFunc, method, path, body string) map[string]any {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		c.Set("id", f.user.Id)
		handler(c)
		response := map[string]any{}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		return response
	}

	t.Run("the user page shows tiers and ratios without channels", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/user/service_tier", nil)
		c.Set("id", f.user.Id)
		GetSelfServiceTier(c)
		body := recorder.Body.String()
		assert.Contains(t, body, `"available":true`)
		assert.Contains(t, body, `"is_default":true`)
		assert.Contains(t, body, `"economy":0.15`)
		for _, private := range []string{"eco-a", "perf-a", "channel", f.channels["eco-a"].GetBaseURL()} {
			assert.NotContains(t, body, private)
		}
	})

	t.Run("saving checks the version and the preview follows the saved rule", func(t *testing.T) {
		draft := `{"enabled_tiers":["economy","performance"],"preference":"lowest","allow_escalation":true,"max_escalation_tier":"performance","version":0}`
		preview := call(PreviewSelfServiceTier, http.MethodPost, "/api/user/service_tier/preview", draft)
		require.Equal(t, true, preview["success"], preview)
		chain := preview["data"].(map[string]any)["chain"].([]any)
		require.Len(t, chain, 2)
		assert.Equal(t, perf, chain[1].(map[string]any)["tier"])
		saved := call(UpdateSelfServiceTier, http.MethodPut, "/api/user/service_tier", draft)
		require.Equal(t, true, saved["success"], saved)
		stale := call(UpdateSelfServiceTier, http.MethodPut, "/api/user/service_tier", draft)
		assert.Equal(t, false, stale["success"])
		assert.Equal(t, model.ErrServiceTierVersionConflict.Key, stale["message_key"])
		invalid := call(UpdateSelfServiceTier, http.MethodPut, "/api/user/service_tier", `{"enabled_tiers":["economy"],"preference":"performance","version":1}`)
		assert.Equal(t, false, invalid["success"])
		settings, err := service.GetUserServiceTierSettings(f.user.Id)
		require.NoError(t, err)
		assert.Equal(t, []string{eco, perf}, settings.EnabledTiers, "an invalid save leaves the stored settings")
	})

	t.Run("the user's family list groups the models the user can use and shows no channel", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/user/model_families", nil)
		c.Set("id", f.user.Id)
		GetSelfModelFamilies(c)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Families []struct {
					Key    string   `json:"key"`
					Models []string `json:"models"`
				} `json:"families"`
				UnclassifiedModels []string `json:"unclassified_models"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		require.True(t, response.Success, recorder.Body.String())
		keys := make([]string, 0, len(response.Data.Families))
		models := map[string][]string{}
		for _, family := range response.Data.Families {
			keys = append(keys, family.Key)
			models[family.Key] = family.Models
		}
		assert.Equal(t, []string{"claude", "gpt", "gemini", "deepseek", "qwen", "glm", "kimi", "grok"}, keys, "the eight default families in the administrator's order")
		assert.Equal(t, []string{"deepseek-ratio", "deepseek-tiered", "deepseek-v4-pro"}, models["deepseek"])
		assert.Equal(t, []string{"qwen-plus"}, models["qwen"])
		assert.Empty(t, models["claude"], "a family without available models stays listed")
		assert.Equal(t, []string{"gpt-plain"}, response.Data.UnclassifiedModels)
		for _, private := range []string{"eco-a", "perf-a", "channel"} {
			assert.NotContains(t, recorder.Body.String(), private)
		}
	})

	t.Run("policy validation rejects unsafe rules", func(t *testing.T) {
		family := f.families["deepseek"]
		var stored model.AggregationPolicy
		require.NoError(t, model.DB.Where("family_id = ? AND model_name = ?", family.Id, "").First(&stored).Error)
		base, err := stored.GetConfig()
		require.NoError(t, err)
		for _, tc := range []struct {
			name   string
			mutate func(*model.AggregationPolicyConfig)
			key    string
		}{
			{"ratios may not fall toward performance", func(config *model.AggregationPolicyConfig) {
				tier := config.Tiers[perf]
				tier.Ratio = 0.1
				config.Tiers[perf] = tier
			}, "Tier ratios must not decrease from economy to performance ({{tier}} is cheaper than {{previous}})"},
			{"an enabled tier needs a channel", func(config *model.AggregationPolicyConfig) {
				tier := config.Tiers[perf]
				tier.Channels = nil
				config.Tiers[perf] = tier
			}, "Enabled tier {{tier}} needs at least one enabled channel"},
			{"channels must serve the family", func(config *model.AggregationPolicyConfig) {
				tier := config.Tiers[eco]
				tier.Channels = append(tier.Channels, model.AggregationTierChannel{ChannelId: f.channels["plain"].Id, Enabled: true})
				config.Tiers[eco] = tier
			}, "Channel #{{id}} {{name}} supports none of the models in this family"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				config := model.AggregationPolicyConfig{Tiers: map[string]model.AggregationTierConfig{}, Strategy: base.Strategy}
				for tier, value := range base.Tiers {
					value.Channels = append([]model.AggregationTierChannel(nil), value.Channels...)
					config.Tiers[tier] = value
				}
				tc.mutate(&config)
				_, _, err := service.SaveServiceTierPolicy(service.ServiceTierPolicyInput{FamilyID: family.Id, Enabled: true, Config: config, Version: stored.Version}, 1)
				var message *common.Message
				require.ErrorAs(t, err, &message)
				assert.Equal(t, tc.key, message.Key)
			})
		}
	})

	t.Run("the route test matches live routing without calling upstream", func(t *testing.T) {
		f.upstream.set(nil)
		result := call(SimulateChannelAggregation, http.MethodPost, "/api/channel_aggregation/simulate",
			`{"model_name":"deepseek-v4-pro","user_group":"default","settings":{"enabled_tiers":["economy","balanced","performance"],"preference":"lowest","allow_escalation":true,"max_escalation_tier":"performance"}}`)
		require.Equal(t, true, result["success"], result)
		data := result["data"].(map[string]any)
		assert.Equal(t, true, data["applies"])
		assert.Equal(t, 0.4, data["ceiling_ratio"])
		steps := data["steps"].([]any)
		require.Len(t, steps, 2, "balanced is not offered by this policy")
		first := steps[0].(map[string]any)["channels"].([]any)[0].(map[string]any)
		assert.Equal(t, "eco-a", first["name"])
		assert.Equal(t, true, first["eligible"])
		assert.Empty(t, f.upstream.callCounts())
	})
}

// limitKey stores the model families the fixture key is limited to; no
// families remove the limit.
func (f *serviceTierFixture) limitKey(t *testing.T, families ...int) {
	t.Helper()
	token := model.Token{}
	require.NoError(t, token.SetModelFamilies(families))
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", f.token.Id).Update("model_families", token.ModelFamilies).Error)
}

// A key may carry its own service tier settings and a model family limit. The
// request path applies both on every request.
func TestServiceTierKeyOverrideAndFamilyLimit(t *testing.T) {
	f := newServiceTierFixture(t)
	eco, perf := model.ServiceTierEconomy, model.ServiceTierPerformance
	f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a"}, perf: {"perf-a"}}, nil)
	f.savePolicy(t, "qwen", tierPool{eco: {"eco-c"}}, nil)
	own := service.ServiceTierSettings{EnabledTiers: []string{perf}, Preference: "performance"}

	t.Run("a key's own tiers replace the account default and bill the tier that served", func(t *testing.T) {
		f.reset(t, 10000, nil)
		require.NoError(t, service.SaveTokenServiceTierSettings(f.user.Id, f.token.Id, &own))
		response := f.chat(t, "deepseek-v4-pro")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		_, logs := f.settled(t, 10000-400)
		assert.Equal(t, map[string]int{"perf-a": 1}, f.upstream.callCounts())
		require.Len(t, logs, 1)
		assert.Equal(t, 400, logs[0].Quota)
		assert.Equal(t, perf, serviceTierLogInfo(t, logs[0])["final_tier"])
	})

	t.Run("a key that follows the account uses the account settings again", func(t *testing.T) {
		f.reset(t, 10000, nil)
		require.NoError(t, service.SaveTokenServiceTierSettings(f.user.Id, f.token.Id, nil))
		response := f.chat(t, "deepseek-v4-pro")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		f.settled(t, 10000-150)
		assert.Equal(t, map[string]int{"eco-a": 1}, f.upstream.callCounts())
	})

	t.Run("the dashboard counts the tier that served and shows users no channel", func(t *testing.T) {
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
		f.reset(t, 10000, nil)
		for _, name := range []string{"deepseek-v4-pro", "gpt-plain"} {
			response := f.chat(t, name)
			require.Equal(t, http.StatusOK, response.Code, name+": "+response.Body.String())
		}
		model.SaveQuotaDataCache()
		rows, err := model.GetFlowQuotaData(0, time.Now().Unix()+7200, "", f.user.Id, common.RoleCommonUser)
		require.NoError(t, err)
		tiers := map[string]string{}
		for _, row := range rows {
			tiers[row.ModelName] = row.ServiceTier
			assert.Equal(t, f.token.Id, row.TokenID)
			assert.Empty(t, row.NodeName)
			assert.Empty(t, row.UseGroup)
			assert.Zero(t, row.ChannelID)
		}
		assert.Equal(t, map[string]string{"deepseek-v4-pro": eco, "gpt-plain": ""}, tiers, "requests that did not use tiers carry none")
	})
}

func TestServiceTierKeyFamilyLimit(t *testing.T) {
	f := newServiceTierFixture(t)
	eco := model.ServiceTierEconomy
	f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a"}}, nil)
	f.savePolicy(t, "qwen", tierPool{eco: {"eco-c"}}, nil)
	qwen := f.families["qwen"]
	allowed := func(modelName string) bool {
		t.Helper()
		response := f.chat(t, modelName)
		if response.Code != http.StatusOK {
			assert.Equal(t, http.StatusForbidden, response.Code, modelName+": "+response.Body.String())
		}
		return response.Code == http.StatusOK
	}

	t.Run("a key limited to one family refuses the other families and unclassified models", func(t *testing.T) {
		f.reset(t, 10000, nil)
		f.limitKey(t, qwen.Id)
		assert.False(t, allowed("deepseek-v4-pro"))
		assert.False(t, allowed("gpt-plain"), "models without a family need id 0")
		assert.True(t, allowed("qwen-plus"))
		assert.Equal(t, map[string]int{"eco-c": 1}, f.upstream.callCounts(), "refused requests never reach an upstream")
		models := f.request(t, http.MethodGet, "/v1/models", "").Body.String()
		assert.Contains(t, models, `"id":"qwen-plus"`)
		assert.NotContains(t, models, "deepseek-v4-pro")
		assert.NotContains(t, models, "gpt-plain")
	})

	t.Run("id 0 allows the models that belong to no family", func(t *testing.T) {
		f.reset(t, 10000, nil)
		f.limitKey(t, qwen.Id, 0)
		assert.True(t, allowed("gpt-plain"))
		assert.False(t, allowed("deepseek-v4-pro"))
	})

	t.Run("membership is read when the request arrives", func(t *testing.T) {
		// The gemini family has no aggregation policy, so the model keeps its
		// ordinary group routing after it joins.
		gemini := f.families["gemini"]
		f.reset(t, 10000, nil)
		f.limitKey(t, gemini.Id)
		assert.False(t, allowed("gpt-plain"))
		var current model.ModelFamily
		require.NoError(t, model.DB.First(&current, gemini.Id).Error)
		_, err := model.ReplaceModelFamilyMembers(gemini.Id, current.Version, []string{"gpt-plain"})
		require.NoError(t, err)
		require.NoError(t, service.RefreshServiceTierCache())
		assert.True(t, allowed("gpt-plain"), "a model added to the family is allowed without editing the key")
	})

	t.Run("an unreadable family limit refuses everything", func(t *testing.T) {
		f.reset(t, 10000, nil)
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", f.token.Id).Update("model_families", "not json").Error)
		for _, name := range []string{"deepseek-v4-pro", "qwen-plus", "gpt-plain"} {
			assert.False(t, allowed(name), name)
		}
		models := f.request(t, http.MethodGet, "/v1/models", "")
		assert.Equal(t, http.StatusOK, models.Code)
		assert.NotContains(t, models.Body.String(), "qwen-plus")
	})

	t.Run("a key without a limit keeps every model", func(t *testing.T) {
		f.reset(t, 10000, nil)
		f.limitKey(t)
		for _, name := range []string{"deepseek-v4-pro", "qwen-plus", "gpt-plain"} {
			assert.True(t, allowed(name), name)
		}
	})
}
