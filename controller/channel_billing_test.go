package controller

import (
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpstreamAccountBalanceQueriesEachSiteOnce(t *testing.T) {
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open("file:upstream_account_balance?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	oldDB, oldLogDB, oldRate := model.DB, model.LOG_DB, operation_setting.USDExchangeRate
	model.DB, model.LOG_DB, operation_setting.USDExchangeRate = db, db, 7.3
	t.Cleanup(func() {
		model.DB, model.LOG_DB, operation_setting.USDExchangeRate = oldDB, oldLogDB, oldRate
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	var selfCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			selfCalls.Add(1)
			assert.Equal(t, "Bearer upstream-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1000000}}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	withToken := &model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "with token", Key: "sk-a", BaseURL: &baseURL,
		OtherSettings: `{"upstream_access_token":"upstream-token","upstream_recharge_rate":0.73}`}
	sameSite := &model.Channel{Id: 2, Type: constant.ChannelTypeOpenAI, Name: "same site", Key: "sk-b", BaseURL: &baseURL}
	require.NoError(t, db.Create(withToken).Error)
	require.NoError(t, db.Create(sameSite).Error)

	for _, channel := range []*model.Channel{withToken, sameSite} {
		result, err := updateChannelBalance(channel)
		require.NoError(t, err)
		// 1000000 / 500000 = 2 upstream $, x0.73 CNY per upstream $ / 7.3 CNY per $
		assert.InDelta(t, 0.2, result.Balance, 1e-9)
	}
	assert.Equal(t, int32(1), selfCalls.Load())
}

func TestSyncUpstreamBills(t *testing.T) {
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open("file:upstream_bill_sync?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Log{}))
	oldDB, oldLogDB, oldRate, oldBatch := model.DB, model.LOG_DB, operation_setting.USDExchangeRate, common.BatchUpdateEnabled
	model.DB, model.LOG_DB, operation_setting.USDExchangeRate, common.BatchUpdateEnabled = db, db, 7.3, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, operation_setting.USDExchangeRate, common.BatchUpdateEnabled = oldDB, oldLogDB, oldRate, oldBatch
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	var logQueries atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/log/self":
			logQueries.Add(1)
			assert.Equal(t, "Bearer upstream-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[
				{"request_id":"up-ours","quota":250000,"created_at":1700000000},
				{"request_id":"up-not-ours","quota":999999,"created_at":1700000000}]}}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	channel := &model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "site", Key: "sk-a", BaseURL: &baseURL, UpstreamCost: 100,
		OtherSettings: `{"upstream_access_token":"upstream-token","upstream_recharge_rate":0.73}`}
	require.NoError(t, db.Create(channel).Error)
	entry := &model.Log{Type: model.LogTypeConsume, ChannelId: 1, UpstreamRequestId: "up-ours", Other: `{"admin_info":{"upstream_cost":100}}`}
	require.NoError(t, db.Create(entry).Error)

	// a Sub2API upstream on another site: no access token, only its live rate multiplier is read
	sub2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sub2api/billing" {
			http.NotFound(w, r)
			return
		}
		assert.Equal(t, "Bearer sk-sub2", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"object":"sub2api.key_billing","effective_rate_multiplier":0.055}`))
	}))
	t.Cleanup(sub2.Close)
	sub2URL := sub2.URL
	sub2Channel := &model.Channel{Id: 9002, Type: constant.ChannelTypeOpenAI, Name: "sub2", Key: "sk-sub2", BaseURL: &sub2URL,
		Status: common.ChannelStatusEnabled, OtherSettings: `{"upstream_recharge_rate":1}`}
	require.NoError(t, db.Create(sub2Channel).Error)

	syncUpstreamBills()
	syncUpstreamBills()

	// 250000 / 500000 = 0.5 upstream $, x0.73 CNY per upstream $ / 7.3 CNY per $ = 0.05 $ = 25000 quota
	var synced model.Log
	require.NoError(t, db.First(&synced, entry.Id).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(synced.Other, &other))
	adminInfo := other["admin_info"].(map[string]any)
	assert.Equal(t, float64(25000), adminInfo["upstream_cost"])
	assert.Equal(t, true, adminInfo["upstream_cost_exact"])

	var updated model.Channel
	require.NoError(t, db.First(&updated, channel.Id).Error)
	assert.Equal(t, int64(25000), updated.UpstreamCost)
	assert.Equal(t, int32(2), logQueries.Load())

	multiplier, ok := service.UpstreamRateMultiplier(sub2Channel.Id)
	require.True(t, ok)
	assert.Equal(t, 0.055, multiplier)
}

func TestGetDeepSeekBalanceUSD(t *testing.T) {
	tests := []struct {
		name            string
		responseJSON    string
		usdExchangeRate float64
		want            float64
		wantErrContains string
	}{
		{
			name:            "prefers USD when USD precedes CNY",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"12.50"},{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 7.3,
			want:            12.5,
		},
		{
			name:            "prefers USD when CNY precedes USD",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"},{"currency":"USD","total_balance":"12.50"}]}`,
			usdExchangeRate: 7.3,
			want:            12.5,
		},
		{
			name:            "converts CNY when USD is absent",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 7.3,
			want:            10,
		},
		{
			name:            "returns error when USD and CNY are absent",
			responseJSON:    `{"balance_infos":[{"currency":"EUR","total_balance":"10.00"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "currency USD or CNY not found",
		},
		{
			name:            "returns USD parse error instead of falling back to CNY",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"invalid"},{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "invalid syntax",
		},
		{
			name:            "rejects NaN USD balance",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"NaN"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "USD balance must be finite",
		},
		{
			name:            "rejects negative USD balance",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"-1.00"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "USD balance must be non-negative",
		},
		{
			name:            "rejects positive infinity CNY balance",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"+Inf"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "CNY balance must be finite",
		},
		{
			name:            "rejects negative CNY balance",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"-7.30"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "CNY balance must be non-negative",
		},
		{
			name:            "returns error for non-positive CNY exchange rate",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 0,
			wantErrContains: "USD exchange rate must be greater than zero",
		},
		{
			name:            "rejects NaN CNY exchange rate",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: math.NaN(),
			wantErrContains: "USD exchange rate must be finite",
		},
		{
			name:            "rejects positive infinity CNY exchange rate",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: math.Inf(1),
			wantErrContains: "USD exchange rate must be finite",
		},
		{
			name:            "rejects CNY conversion overflow",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"1.7976931348623157e+308"}]}`,
			usdExchangeRate: math.SmallestNonzeroFloat64,
			wantErrContains: "converted USD balance must be finite",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var response DeepSeekUsageResponse
			require.NoError(t, common.Unmarshal([]byte(test.responseJSON), &response))

			balance, err := getDeepSeekBalanceUSD(response, test.usdExchangeRate)
			if test.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErrContains)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, test.want, balance, 1e-12)
		})
	}
}
