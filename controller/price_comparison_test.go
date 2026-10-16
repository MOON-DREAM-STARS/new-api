package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type priceComparisonResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	MessageKey string `json:"message_key"`
	Data       struct {
		Settings struct {
			BaselineModel string `json:"baseline_model"`
			PriceBasis    string `json:"price_basis"`
			Currency      string `json:"currency"`
			RatioFormat   string `json:"ratio_format"`
		} `json:"settings"`
		Baseline struct {
			ModelName  string  `json:"model_name"`
			PriceRatio float64 `json:"price_ratio"`
			Price      struct {
				ModelRatio float64 `json:"model_ratio"`
			} `json:"price"`
		} `json:"baseline"`
		Families []struct {
			Key    string `json:"key"`
			Models []struct {
				ModelName  string  `json:"model_name"`
				PriceRatio float64 `json:"price_ratio"`
				Tier       string  `json:"tier"`
			} `json:"models"`
		} `json:"families"`
		Models []struct {
			ModelName string `json:"model_name"`
			Enabled   bool   `json:"enabled"`
			FamilyID  int    `json:"family_id"`
		} `json:"models"`
	} `json:"data"`
}

func TestPriceComparison(t *testing.T) {
	f := newServiceTierFixture(t)
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })
	eco, perf := model.ServiceTierEconomy, model.ServiceTierPerformance
	f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a"}, perf: {"perf-a"}}, nil)
	// deepseek-offline belongs to the family but no group offers it; the
	// baseline is priced although nothing serves it.
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"deepseek-ratio":1,"deepseek-offline":3,"baseline-only":2}`))
	store, err := model.LoadServiceTierStore()
	require.NoError(t, err)
	for _, family := range store.Families {
		if family.Key == "deepseek" {
			_, err = model.ReplaceModelFamilyMembers(family.Id, family.Version, []string{"deepseek-v4-pro", "deepseek-ratio", "deepseek-tiered", "deepseek-offline"})
			require.NoError(t, err)
		}
	}
	refreshChannelAggregation()

	call := func(handler gin.HandlerFunc, method, path, body string) (priceComparisonResponse, string) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		c.Set("id", f.user.Id)
		c.Set("role", common.RoleAdminUser)
		handler(c)
		var response priceComparisonResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		return response, recorder.Body.String()
	}
	save := func(body string) (response struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		MessageKey string `json:"message_key"`
	}) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/price_comparison/settings", bytes.NewBufferString(body))
		c.Set("id", f.user.Id)
		c.Set("role", common.RoleAdminUser)
		UpdatePriceComparisonSettings(c)
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		return response
	}
	deepseekID, qwenID := strconv.Itoa(f.families["deepseek"].Id), strconv.Itoa(f.families["qwen"].Id)

	t.Run("the page works before the administrator chooses anything", func(t *testing.T) {
		response, _ := call(GetPriceComparison, http.MethodGet, "/api/price_comparison", "")
		require.True(t, response.Success)
		assert.Equal(t, "deepseek-v4.1-flash", response.Data.Settings.BaselineModel)
		assert.Equal(t, "peak", response.Data.Settings.PriceBasis)
		assert.Equal(t, "USD", response.Data.Settings.Currency)
		assert.Empty(t, response.Data.Families, "no family is shown until models are chosen")
	})

	t.Run("saving rejects choices that do not hold", func(t *testing.T) {
		valid := func(overrides string) string {
			return `{"baseline_model":"deepseek-ratio","price_basis":"peak","currency":"USD","ratio_format":"multiplier",` + overrides + `}`
		}
		for _, tc := range []struct {
			name, body, key string
		}{
			{"a model outside the family", valid(`"families":[{"family_id":` + deepseekID + `,"models":["gpt-plain"]}]`), "{{model}} is not a member of this model family"},
			{"an unknown family", valid(`"families":[{"family_id":9999,"models":["deepseek-ratio"]}]`), "A selected model family no longer exists. Reload the page and try again."},
			{"the same family twice", valid(`"families":[{"family_id":` + deepseekID + `,"models":["deepseek-ratio"]},{"family_id":` + deepseekID + `,"models":["deepseek-tiered"]}]`), "Each model family can be listed only once"},
			{"a baseline without a price", `{"baseline_model":"no-such-model","price_basis":"peak","currency":"USD","ratio_format":"multiplier","families":[]}`, "The baseline model has no price. Choose a model with a price"},
			{"an unknown currency", `{"baseline_model":"deepseek-ratio","price_basis":"peak","currency":"EUR","ratio_format":"multiplier","families":[]}`, "Invalid price display option"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				response := save(tc.body)
				assert.False(t, response.Success)
				assert.Equal(t, tc.key, response.MessageKey)
			})
		}
		response, _ := call(GetPriceComparison, http.MethodGet, "/api/price_comparison", "")
		assert.Equal(t, "deepseek-v4.1-flash", response.Data.Settings.BaselineModel, "a rejected save changes nothing")
	})

	t.Run("users see the chosen models they can use at their lowest price", func(t *testing.T) {
		saved := save(`{"baseline_model":"baseline-only","price_basis":"off_peak","currency":"CNY","ratio_format":"percent","families":[` +
			`{"family_id":` + qwenID + `,"models":["qwen-plus"]},` +
			`{"family_id":` + deepseekID + `,"models":["deepseek-tiered","deepseek-offline","deepseek-ratio","deepseek-tiered"]}]}`)
		require.True(t, saved.Success, saved.Message)
		response, body := call(GetPriceComparison, http.MethodGet, "/api/price_comparison", "")
		require.True(t, response.Success)
		assert.Equal(t, "off_peak", response.Data.Settings.PriceBasis)
		assert.Equal(t, "CNY", response.Data.Settings.Currency)
		assert.Equal(t, "baseline-only", response.Data.Baseline.ModelName)
		assert.Equal(t, 2.0, response.Data.Baseline.Price.ModelRatio, "the baseline is shown even though no group offers it")
		assert.Equal(t, 1.0, response.Data.Baseline.PriceRatio, "the baseline is compared at its list price")

		require.Len(t, response.Data.Families, 2)
		assert.Equal(t, "deepseek", response.Data.Families[0].Key, "families follow the family order of the administrator")
		assert.Equal(t, "qwen", response.Data.Families[1].Key)
		deepseek := response.Data.Families[0].Models
		require.Len(t, deepseek, 2, "the model nobody offers is hidden and a repeated model is listed once")
		assert.Equal(t, "deepseek-tiered", deepseek[0].ModelName, "the chosen order is kept")
		assert.Equal(t, eco, deepseek[0].Tier)
		assert.Equal(t, 0.15, deepseek[0].PriceRatio, "the lowest offered tier replaces the group ratio")
		assert.Equal(t, "deepseek-ratio", deepseek[1].ModelName)
		qwen := response.Data.Families[1].Models
		require.Len(t, qwen, 1)
		assert.Empty(t, qwen[0].Tier, "a family without a rule is billed by group")
		assert.Equal(t, 1.0, qwen[0].PriceRatio)
		for _, private := range []string{"eco-a", "perf-a", "enable_groups", "channel"} {
			assert.NotContains(t, body, private)
		}
	})

	t.Run("a rule limited to other user groups does not apply", func(t *testing.T) {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"group_ratio_setting.group_ratio": `{"default":1,"vip":1}`}))
		f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a"}, perf: {"perf-a"}}, func(strategy *model.AggregationStrategy) {
			strategy.UserGroups = []string{"vip"}
		})
		response, _ := call(GetPriceComparison, http.MethodGet, "/api/price_comparison", "")
		require.True(t, response.Success)
		first := response.Data.Families[0].Models[0]
		assert.Empty(t, first.Tier)
		assert.Equal(t, 1.0, first.PriceRatio)
		f.savePolicy(t, "deepseek", tierPool{eco: {"eco-a"}, perf: {"perf-a"}}, nil)
	})

	t.Run("a group that costs nothing is no price to compare", func(t *testing.T) {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"group_ratio_setting.group_ratio": `{"default":0}`}))
		response, _ := call(GetPriceComparison, http.MethodGet, "/api/price_comparison", "")
		require.True(t, response.Success)
		qwen := response.Data.Families[1].Models[0]
		assert.Equal(t, "qwen-plus", qwen.ModelName)
		assert.Equal(t, 1.0, qwen.PriceRatio, "the list price stands in for a free group")
	})

	t.Run("administrators choose from every offered model and family member", func(t *testing.T) {
		response, _ := call(GetPriceComparisonAdmin, http.MethodGet, "/api/price_comparison/settings", "")
		require.True(t, response.Success)
		enabled := map[string]bool{}
		for _, item := range response.Data.Models {
			enabled[item.ModelName] = item.Enabled
		}
		assert.True(t, enabled["deepseek-ratio"])
		assert.False(t, enabled["deepseek-offline"], "a family member that no group offers is listed as unavailable")
		_, listed := enabled["baseline-only"]
		assert.True(t, listed, "the current baseline stays selectable")
	})
}
