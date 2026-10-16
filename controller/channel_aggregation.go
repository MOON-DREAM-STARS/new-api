package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// Administrator channel aggregation API (/api/channel_aggregation) and the
// user's service tier API (/api/user/service_tier). Every write checks the
// version the client loaded, so concurrent edits fail instead of
// overwriting each other.

func GetChannelAggregationOverview(c *gin.Context) {
	overview, err := service.GetServiceTierOverview()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func GetChannelAggregationFamily(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid family ID")
		return
	}
	detail, err := service.GetServiceTierFamilyDetail(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func refreshChannelAggregation() {
	if err := service.RefreshServiceTierCache(); err != nil {
		common.SysError(common.LogText("failed to refresh channel aggregation config: %s", err.Error()))
	}
}

func CreateChannelAggregationFamily(c *gin.Context) {
	var input service.ServiceTierFamilyInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	family, err := service.NormalizeServiceTierFamily(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.CreateModelFamily(family); err != nil {
		common.ApiError(c, err)
		return
	}
	refreshChannelAggregation()
	recordManageAudit(c, "channel_aggregation.family_create", map[string]any{"id": family.Id, "key": family.Key, "name": family.Name})
	common.ApiSuccess(c, family)
}

func UpdateChannelAggregationFamily(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid family ID")
		return
	}
	var input service.ServiceTierFamilyInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	family, err := service.NormalizeServiceTierFamily(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	family.Id = id
	if err := model.UpdateModelFamily(family, input.Version); err != nil {
		common.ApiError(c, err)
		return
	}
	refreshChannelAggregation()
	recordManageAudit(c, "channel_aggregation.family_update", map[string]any{"id": id, "name": family.Name})
	common.ApiSuccess(c, gin.H{"version": family.Version})
}

func DeleteChannelAggregationFamily(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid family ID")
		return
	}
	version, err := strconv.ParseInt(c.Query("version"), 10, 64)
	if err != nil {
		common.ApiErrorT(c, "Invalid version")
		return
	}
	if err := model.DeleteModelFamily(id, version); err != nil {
		common.ApiError(c, err)
		return
	}
	refreshChannelAggregation()
	recordManageAudit(c, "channel_aggregation.family_delete", map[string]any{"id": id})
	common.ApiSuccess(c, nil)
}

func UpdateChannelAggregationMembers(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid family ID")
		return
	}
	var input struct {
		Models  []string `json:"models"`
		Version int64    `json:"version"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	members, err := service.NormalizeServiceTierMembers(input.Models)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	removedOverrides, err := model.ReplaceModelFamilyMembers(id, input.Version, members)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	refreshChannelAggregation()
	recordManageAudit(c, "channel_aggregation.members_update", map[string]any{"id": id, "count": len(members), "removed_overrides": removedOverrides})
	common.ApiSuccess(c, gin.H{"version": input.Version + 1, "removed_overrides": removedOverrides})
}

func SaveChannelAggregationPolicy(c *gin.Context) {
	var input service.ServiceTierPolicyInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	policy, warnings, err := service.SaveServiceTierPolicy(input, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel_aggregation.policy_save", map[string]any{
		"family_id": input.FamilyID, "model": input.ModelName, "enabled": input.Enabled, "version": policy.Version,
	})
	common.ApiSuccess(c, gin.H{"id": policy.Id, "version": policy.Version, "warnings": warnings})
}

func DeleteChannelAggregationPolicy(c *gin.Context) {
	familyID, err := strconv.Atoi(c.Query("family_id"))
	if err != nil {
		common.ApiErrorT(c, "Invalid family ID")
		return
	}
	version, err := strconv.ParseInt(c.Query("version"), 10, 64)
	if err != nil {
		common.ApiErrorT(c, "Invalid version")
		return
	}
	modelName := c.Query("model")
	if err := model.DeleteAggregationPolicy(familyID, modelName, version); err != nil {
		common.ApiError(c, err)
		return
	}
	refreshChannelAggregation()
	recordManageAudit(c, "channel_aggregation.policy_delete", map[string]any{"family_id": familyID, "model": modelName})
	common.ApiSuccess(c, nil)
}

func ListChannelAggregationChannels(c *gin.Context) {
	familyID, _ := strconv.Atoi(c.Query("family_id"))
	channels, err := service.ListServiceTierChannelOptions(familyID, c.Query("model"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

func SimulateChannelAggregation(c *gin.Context) {
	var input service.ServiceTierSimulationInput
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	result, err := service.SimulateServiceTierRoute(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// serviceTierUserGroup is the requesting user's account group.
func serviceTierUserGroup(c *gin.Context) string {
	if group := common.GetContextKeyString(c, constant.ContextKeyUserGroup); group != "" {
		return group
	}
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		return ""
	}
	return user.Group
}

// GetSelfModelFamilies lists the model families with the models the
// requesting user can use, for the key creation wizard.
func GetSelfModelFamilies(c *gin.Context) {
	families, err := service.GetServiceTierUserFamilies(serviceTierUserGroup(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, families)
}

func GetSelfServiceTier(c *gin.Context) {
	overview, err := service.GetServiceTierUserOverview(c.GetInt("id"), serviceTierUserGroup(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func UpdateSelfServiceTier(c *gin.Context) {
	var input service.ServiceTierSettings
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	userID := c.GetInt("id")
	settings, err := service.SaveUserServiceTierSettings(userID, input, input.Version)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	preview := service.PreviewServiceTierSettings(settings, serviceTierUserGroup(c))
	common.ApiSuccess(c, &service.ServiceTierUserOverview{Available: len(preview.Families) > 0, Settings: settings, Preview: preview})
}

func PreviewSelfServiceTier(c *gin.Context) {
	var input service.ServiceTierSettings
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorT(c, "Invalid request body")
		return
	}
	settings, err := service.NormalizeServiceTierSettings(input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, service.PreviewServiceTierSettings(settings, serviceTierUserGroup(c)))
}
