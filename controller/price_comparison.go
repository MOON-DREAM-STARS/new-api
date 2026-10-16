package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// The price comparison page (/api/price_comparison). Every logged-in user reads
// the page; the settings routes are for administrators.

func GetPriceComparison(c *gin.Context) {
	view, err := service.GetPriceComparisonView(serviceTierUserGroup(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func GetPriceComparisonAdmin(c *gin.Context) {
	view, err := service.GetPriceComparisonAdminView()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func UpdatePriceComparisonSettings(c *gin.Context) {
	var input service.PriceComparisonSettings
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	settings, err := service.SavePriceComparisonSettings(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	count := 0
	for _, pick := range settings.Families {
		count += len(pick.Models)
	}
	recordManageAudit(c, "price_comparison.settings_update", map[string]any{
		"baseline_model": settings.BaselineModel,
		"price_basis":    settings.PriceBasis,
		"currency":       settings.Currency,
		"count":          count,
	})
	common.ApiSuccess(c, settings)
}
