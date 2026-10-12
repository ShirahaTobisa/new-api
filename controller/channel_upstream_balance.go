package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/shopspring/decimal"
)

// updateUpstreamAccountBalance 查上游 new-api / one-api 站的账户余额（/api/user/self），按充值比例折算后存为渠道余额。
func updateUpstreamAccountBalance(channel *model.Channel, settings dto.ChannelOtherSettings) (float64, error) {
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(settings.UpstreamAccessToken))
	if userId := strings.TrimSpace(settings.UpstreamUserId); userId != "" {
		headers.Set("New-Api-User", userId)
	}
	body, err := GetResponseBody(http.MethodGet, baseURL+"/api/user/self", channel, headers)
	if err != nil {
		return 0, err
	}
	var self struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Quota *float64 `json:"quota"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &self); err != nil {
		return 0, err
	}
	if !self.Success || self.Data.Quota == nil {
		return 0, fmt.Errorf("upstream account query failed: %s", self.Message)
	}

	// 上游站可能改过每美元对应的额度，读不到时按默认值 500000 算
	quotaPerUnit := 500000.0
	if body, err := GetResponseBody(http.MethodGet, baseURL+"/api/status", channel, nil); err == nil {
		var status struct {
			Data struct {
				QuotaPerUnit float64 `json:"quota_per_unit"`
			} `json:"data"`
		}
		if common.Unmarshal(body, &status) == nil && status.Data.QuotaPerUnit > 0 {
			quotaPerUnit = status.Data.QuotaPerUnit
		}
	}

	usd := decimal.NewFromFloat(*self.Data.Quota).Div(decimal.NewFromFloat(quotaPerUnit))
	balance := service.UpstreamAmountToLocal(usd, settings).InexactFloat64()
	channel.UpdateBalance(balance)
	return balance, nil
}
