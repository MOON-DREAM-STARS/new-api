package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func seedFlowQuotaData(t *testing.T, quotaData QuotaData) {
	t.Helper()
	require.NoError(t, DB.Create(&quotaData).Error)
}

func seedFlowLookupData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "west"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, DB.Delete(&Token{Id: 11}).Error)
}

func TestGetFlowQuotaDataExposesDimensionsByRole(t *testing.T) {
	truncateTables(t)
	seedFlowLookupData(t)

	seedFlowQuotaData(t, QuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		UseGroup:    "vip",
		ModelName:   "gpt-a",
		ChannelID:   1,
		ServiceTier: ServiceTierEconomy,
		CreatedAt:   1000,
		Count:       2,
		Quota:       100,
		TokenUsed:   40,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		UseGroup:    "vip",
		ModelName:   "gpt-a",
		ChannelID:   1,
		ServiceTier: ServiceTierEconomy,
		CreatedAt:   1100,
		Count:       1,
		Quota:       50,
		TokenUsed:   20,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		UseGroup:    "vip",
		ModelName:   "gpt-a",
		ChannelID:   2,
		ServiceTier: ServiceTierBalanced,
		CreatedAt:   1200,
		Count:       1,
		Quota:       25,
		TokenUsed:   10,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "default",
		ModelName: "gpt-b",
		ChannelID: 1,
		CreatedAt: 1300,
		Count:     3,
		Quota:     70,
		TokenUsed: 30,
	})
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		ModelName: "legacy",
		CreatedAt: 1400,
		Count:     99,
		Quota:     999,
		TokenUsed: 999,
	})

	rootRows, err := GetFlowQuotaData(900, 2000, "", 0, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, rootRows, 3)
	// Token 11 was soft-deleted, so its name is intentionally left empty for the
	// frontend to render a localized "deleted (id)" label instead.
	require.Equal(t, FlowQuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		TokenName:   "",
		UseGroup:    "vip",
		ChannelID:   1,
		ChannelName: "east",
		ModelName:   "gpt-a",
		ServiceTier: ServiceTierEconomy,
		TokenUsed:   60,
		Count:       3,
		Quota:       150,
	}, *rootRows[0])
	// A token that still exists resolves to its current name; requests that did
	// not use service tiers carry no tier.
	require.Equal(t, 22, rootRows[1].TokenID)
	require.Equal(t, "backup", rootRows[1].TokenName)
	require.Empty(t, rootRows[1].ServiceTier)

	// Administrators and the super administrator see the same dimensions.
	adminRows, err := GetFlowQuotaData(900, 2000, "alice", 0, common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, adminRows, 2)
	require.Equal(t, *rootRows[0], *adminRows[0])
	require.Equal(t, ServiceTierBalanced, adminRows[1].ServiceTier)
	require.Equal(t, "west", adminRows[1].ChannelName)

	// A user never receives the nodes, groups or channels behind the traffic.
	selfRows, err := GetFlowQuotaData(900, 2000, "", 1, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, selfRows, 2)
	require.Equal(t, FlowQuotaData{
		UserID:      1,
		Username:    "alice",
		TokenID:     11,
		ModelName:   "gpt-a",
		ServiceTier: ServiceTierEconomy,
		TokenUsed:   60,
		Count:       3,
		Quota:       150,
	}, *selfRows[0])
	require.Equal(t, ServiceTierBalanced, selfRows[1].ServiceTier)
	require.Equal(t, 25, selfRows[1].Quota)
}

func TestLogQuotaDataSplitsRowsByUseGroupTokenChannelNodeAndServiceTier(t *testing.T) {
	truncateTables(t)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3661,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     100,
		TokenUsed: 40,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     50,
		TokenUsed: 20,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "default",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     25,
		TokenUsed: 10,
	})

	LogQuotaData(QuotaDataLogParams{
		UserID:      1,
		Username:    "alice",
		ModelName:   "gpt-a",
		CreatedAt:   3700,
		UseGroup:    "vip",
		TokenID:     11,
		ChannelID:   1,
		NodeName:    "node-a",
		ServiceTier: ServiceTierEconomy,
		Quota:       10,
		TokenUsed:   5,
	})

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Order("quota DESC").Find(&rows).Error)
	require.Len(t, rows, 3)
	require.Equal(t, int64(3600), rows[0].CreatedAt)
	require.Equal(t, "vip", rows[0].UseGroup)
	require.Equal(t, 11, rows[0].TokenID)
	require.Equal(t, 1, rows[0].ChannelID)
	require.Equal(t, "node-a", rows[0].NodeName)
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 150, rows[0].Quota)
	require.Equal(t, 60, rows[0].TokenUsed)
	require.Empty(t, rows[0].ServiceTier)
	require.Equal(t, "default", rows[1].UseGroup)
	require.Equal(t, 25, rows[1].Quota)
	// Requests served by a service tier are counted apart from the others.
	require.Equal(t, ServiceTierEconomy, rows[2].ServiceTier)
	require.Equal(t, 10, rows[2].Quota)
}
