package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// The models.dev price import (/api/option/models_dev). Root only: it writes
// the billing expressions of the models.

func GetModelsDevOverview(c *gin.Context) {
	overview, err := service.GetModelsDevOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func UpdateModelsDevConfig(c *gin.Context) {
	var input service.ModelsDevConfig
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	config, err := service.SaveModelsDevConfig(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "models_dev.config_update", map[string]any{
		"auto_sync": config.AutoSyncEnabled,
		"selected":  len(config.SelectedKeys),
		"mappings":  len(config.Mappings),
	})
	common.ApiSuccess(c, config)
}

func queryFlag(c *gin.Context, name string) bool {
	value, _ := strconv.ParseBool(c.Query(name))
	return value
}

func GetModelsDevCatalog(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	page, err := service.ListModelsDevCatalog(c.Request.Context(), service.ModelsDevSearch{
		Query: c.Query("q"), Provider: c.Query("provider"),
		OnlyCommon: queryFlag(c, "common"), OnlyOfficial: queryFlag(c, "official"),
	}, limit, offset, queryFlag(c, "refresh"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func GetModelsDevExpression(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		common.ApiErrorT(c, "Choose a models.dev model")
		return
	}
	result, err := service.ModelsDevExpression(c.Request.Context(), key, c.Query("model"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func PreviewModelsDev(c *gin.Context) {
	plan, err := service.PreviewModelsDev(c.Request.Context(), queryFlag(c, "refresh"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, plan)
}

// modelsDevTaskRunning tells whether the automatic import is running, so a
// manual import does not start beside it.
func modelsDevTaskRunning() (bool, error) {
	task, err := model.GetActiveSystemTask(model.SystemTaskTypeModelsDevPricing)
	return task != nil, err
}

func SyncModelsDev(c *gin.Context) {
	var request struct {
		PlanHash string   `json:"plan_hash"`
		Models   []string `json:"models"`
		Approved []string `json:"approved"`
		Refresh  bool     `json:"refresh"`
	}
	if c.Request.ContentLength != 0 {
		if err := common.DecodeJson(c.Request.Body, &request); err != nil {
			common.ApiErrorT(c, "Invalid request body")
			return
		}
	}
	if running, err := modelsDevTaskRunning(); err != nil {
		common.ApiError(c, err)
		return
	} else if running {
		common.ApiError(c, service.ErrModelsDevBusy)
		return
	}
	result, err := service.SyncModelsDev(c.Request.Context(), service.ModelsDevSyncOptions{
		Trigger: service.ModelsDevTriggerManual, Refresh: request.Refresh, PlanHash: request.PlanHash,
		Models: request.Models, Approved: request.Approved,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "models_dev.sync", map[string]any{
		"created": result.Summary.Created,
		"updated": result.Summary.Updated,
		"skipped": result.Summary.SkippedReview,
	})
	common.ApiSuccess(c, result)
}

func UndoModelsDevSync(c *gin.Context) {
	if running, err := modelsDevTaskRunning(); err != nil {
		common.ApiError(c, err)
		return
	} else if running {
		common.ApiError(c, service.ErrModelsDevBusy)
		return
	}
	result, err := service.UndoModelsDevSync()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "models_dev.undo", map[string]any{
		"restored":  len(result.Restored),
		"conflicts": len(result.Conflicts),
	})
	common.ApiSuccess(c, result)
}
