package service

import (
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// Model families group models for users: a key can be limited to some
// families, the key wizard lists them and the dashboard groups traffic by
// them. Membership is the explicit member list of the channel aggregation
// configuration, so it needs no separate storage.

// familyIDOf returns the id of the family the model belongs to, or 0 when it
// belongs to none. Names match the way routing matches them: the exact name
// first, then the name without modifiers and legacy aliases.
func (s *serviceTierSnapshot) familyIDOf(modelName string) int {
	if familyID, ok := s.memberFamily[modelName]; ok {
		return familyID
	}
	if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != "" && normalized != modelName {
		return s.memberFamily[normalized]
	}
	return 0
}

// modelFamilyFilter returns a predicate telling whether a model belongs to one
// of the families. Id 0 stands for models that belong to no family. Family
// membership is read per call, so models added to a family later are allowed
// without editing the key.
func modelFamilyFilter(families []int) (func(modelName string) bool, error) {
	snapshot, err := currentServiceTierSnapshot()
	if err != nil {
		return nil, err
	}
	allowed := make(map[int]struct{}, len(families))
	for _, familyID := range families {
		allowed[familyID] = struct{}{}
	}
	return func(modelName string) bool {
		familyID := 0
		if snapshot != nil {
			familyID = snapshot.familyIDOf(modelName)
		}
		_, ok := allowed[familyID]
		return ok
	}, nil
}

// TokenModelFamilyFilter returns the model filter of the request's key, or nil
// when the key is not limited to model families. An unreadable family
// configuration is an error, so a limited key never becomes unlimited.
func TokenModelFamilyFilter(c *gin.Context) (func(modelName string) bool, error) {
	families, ok := common.GetContextKeyType[[]int](c, constant.ContextKeyTokenModelFamilies)
	if !ok || len(families) == 0 {
		return nil, nil
	}
	return modelFamilyFilter(families)
}

// CheckTokenModelFamily applies the key's model family limit to modelName and
// returns the rejection, or nil when the model may be used.
func CheckTokenModelFamily(c *gin.Context, modelName string) *ServiceTierError {
	allows, err := TokenModelFamilyFilter(c)
	if err != nil {
		common.SysError(common.LogText("failed to load model families: %s", err.Error()))
		return &ServiceTierError{StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeQueryDataError, MessageID: i18n.MsgServiceTierConfigUnavailable}
	}
	if allows == nil || allows(modelName) {
		return nil
	}
	return &ServiceTierError{StatusCode: http.StatusForbidden, Code: types.ErrorCodeAccessDenied, MessageID: i18n.MsgDistributorTokenModelForbidden, Params: map[string]any{"Model": modelName}}
}

// NormalizeTokenModelFamilies validates the family ids a key is limited to and
// returns them sorted without duplicates. Id 0 stands for models without a
// family; every other id must be an existing family.
func NormalizeTokenModelFamilies(families []int) ([]int, error) {
	if len(families) > serviceTierMaxKeyFamilies {
		return nil, common.NewMessage("Select at most {{max}} model families", map[string]any{"max": serviceTierMaxKeyFamilies})
	}
	result := slices.Clone(families)
	slices.Sort(result)
	result = slices.Compact(result)
	if len(result) > 0 && result[0] < 0 {
		return nil, common.NewMessage("A selected model family no longer exists. Reload the page and try again.")
	}
	var ids []int
	for _, familyID := range result {
		if familyID > 0 {
			ids = append(ids, familyID)
		}
	}
	existing, err := model.ExistingModelFamilyIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(existing) != len(ids) {
		return nil, common.NewMessage("A selected model family no longer exists. Reload the page and try again.")
	}
	return result, nil
}

// ServiceTierUserFamily is one model family as a user sees it: the models of
// the family that the user's groups offer.
type ServiceTierUserFamily struct {
	Id          int      `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Models      []string `json:"models"`
}

type ServiceTierUserFamilies struct {
	Families           []ServiceTierUserFamily `json:"families"`
	UnclassifiedModels []string                `json:"unclassified_models"`
}

// GetServiceTierUserFamilies groups the models the user's groups offer by
// model family, in the administrator's family order.
func GetServiceTierUserFamilies(userGroup string) (*ServiceTierUserFamilies, error) {
	snapshot, err := currentServiceTierSnapshot()
	if err != nil {
		return nil, err
	}
	groups := make([]string, 0)
	for group := range GetUserUsableGroups(userGroup) {
		groups = append(groups, group)
	}
	slices.Sort(groups)
	models := GetGroupsEnabledModels(groups)
	slices.Sort(models)

	result := &ServiceTierUserFamilies{Families: make([]ServiceTierUserFamily, 0), UnclassifiedModels: make([]string, 0)}
	position := make(map[int]int)
	if snapshot != nil {
		for _, family := range snapshot.families {
			position[family.Id] = len(result.Families)
			result.Families = append(result.Families, ServiceTierUserFamily{
				Id: family.Id, Key: family.Key, Name: family.Name, Description: family.Description, Models: make([]string, 0),
			})
		}
	}
	for _, name := range models {
		familyID := 0
		if snapshot != nil {
			familyID = snapshot.familyIDOf(name)
		}
		if index, ok := position[familyID]; ok {
			result.Families[index].Models = append(result.Families[index].Models, name)
			continue
		}
		result.UnclassifiedModels = append(result.UnclassifiedModels, name)
	}
	return result, nil
}

// AnnotateFlowModelFamilies adds the model family to dashboard flow rows. An
// unreadable configuration leaves the rows without a family instead of failing
// the dashboard.
func AnnotateFlowModelFamilies(rows []*model.FlowQuotaData) {
	snapshot, err := currentServiceTierSnapshot()
	if err != nil || snapshot == nil {
		return
	}
	for _, row := range rows {
		if familyID := snapshot.familyIDOf(row.ModelName); familyID != 0 {
			row.ModelFamilyID = familyID
			row.ModelFamily = snapshot.familyByID[familyID].Name
		}
	}
}
