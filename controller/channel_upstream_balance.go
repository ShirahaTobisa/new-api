package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/shopspring/decimal"
)

// withSiteCredentials 同一个上游站（同域名）只需在一个渠道里填访问令牌，其他渠道查余额时借用它的令牌，
// 自己没填充值比例时也借用它的。
func withSiteCredentials(channel *model.Channel, settings dto.ChannelOtherSettings) dto.ChannelOtherSettings {
	if settings.UpstreamAccessToken != "" {
		return settings
	}
	site, err := url.Parse(channel.GetBaseURL())
	if err != nil || site.Host == "" {
		return settings
	}
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return settings
	}
	for _, other := range channels {
		otherSite, err := url.Parse(other.GetBaseURL())
		if other.Id == channel.Id || err != nil || !strings.EqualFold(otherSite.Host, site.Host) {
			continue
		}
		shared := other.GetOtherSettings()
		if shared.UpstreamAccessToken == "" {
			continue
		}
		settings.UpstreamAccessToken = shared.UpstreamAccessToken
		settings.UpstreamUserId = shared.UpstreamUserId
		if settings.UpstreamRechargeRate <= 0 {
			settings.UpstreamRechargeRate = shared.UpstreamRechargeRate
		}
		return settings
	}
	return settings
}

// upstreamAccountCache 同一个上游账户一分钟内只查一次：同站多个渠道、连续点击都共用结果，避免触发上游限速
var upstreamAccountCache sync.Map

type upstreamAccountQuota struct {
	usd       decimal.Decimal
	fetchedAt time.Time
}

// updateUpstreamAccountBalance 查上游 new-api / one-api 站的账户余额，按充值比例折算后存为渠道余额。
func updateUpstreamAccountBalance(channel *model.Channel, settings dto.ChannelOtherSettings) (float64, error) {
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	userId := strings.TrimSpace(settings.UpstreamUserId)
	token := strings.TrimSpace(settings.UpstreamAccessToken)
	cacheKey := baseURL + "|" + userId + "|" + token
	cached, ok := upstreamAccountCache.Load(cacheKey)
	if !ok || time.Since(cached.(upstreamAccountQuota).fetchedAt) >= time.Minute {
		usd, err := fetchUpstreamAccountUSD(channel, baseURL, userId, token)
		if err != nil {
			return 0, err
		}
		cached = upstreamAccountQuota{usd: usd, fetchedAt: time.Now()}
		upstreamAccountCache.Store(cacheKey, cached)
	}
	balance := service.UpstreamAmountToLocal(cached.(upstreamAccountQuota).usd, settings).InexactFloat64()
	channel.UpdateBalance(balance)
	return balance, nil
}

// upstreamAuthHeaders 用账户令牌访问上游 new-api 站的请求头：新版只认令牌，旧版还要 New-Api-User
func upstreamAuthHeaders(userId string, token string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	if userId != "" {
		headers.Set("New-Api-User", userId)
	}
	return headers
}

// upstreamQuotaPerUnit 上游站每美元对应的额度：站长可能改过，读不到时按默认值 500000 算
func upstreamQuotaPerUnit(channel *model.Channel, baseURL string) float64 {
	body, err := GetResponseBody(http.MethodGet, baseURL+"/api/status", channel, nil)
	if err != nil {
		return 500000
	}
	var status struct {
		Data struct {
			QuotaPerUnit float64 `json:"quota_per_unit"`
		} `json:"data"`
	}
	if common.Unmarshal(body, &status) != nil || status.Data.QuotaPerUnit <= 0 {
		return 500000
	}
	return status.Data.QuotaPerUnit
}

// fetchUpstreamAccountUSD 调上游 /api/user/self 读账户额度，按上游的每美元额度换算成上游美元。
func fetchUpstreamAccountUSD(channel *model.Channel, baseURL string, userId string, token string) (decimal.Decimal, error) {
	body, err := GetResponseBody(http.MethodGet, baseURL+"/api/user/self", channel, upstreamAuthHeaders(userId, token))
	if err != nil {
		return decimal.Zero, err
	}
	var self struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Quota *float64 `json:"quota"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &self); err != nil {
		return decimal.Zero, err
	}
	if !self.Success || self.Data.Quota == nil {
		return decimal.Zero, fmt.Errorf("upstream account query failed: %s", self.Message)
	}
	quotaPerUnit := upstreamQuotaPerUnit(channel, baseURL)
	return decimal.NewFromFloat(*self.Data.Quota).Div(decimal.NewFromFloat(quotaPerUnit)), nil
}

// updateSub2APIBalance 用渠道 key 查 Sub2API 站的 /v1/usage：key 没设额度时 remaining 就是账户钱包余额。
// 不是 Sub2API 站时返回 false，交回原来的查询方式。
func updateSub2APIBalance(channel *model.Channel, settings dto.ChannelOtherSettings) (float64, bool) {
	switch channel.Type {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeAnthropic, constant.ChannelTypeCustom, constant.ChannelTypeSub2API:
	default:
		return 0, false
	}
	baseURL := strings.TrimRight(channel.GetBaseURL(), "/")
	if baseURL == "" {
		return 0, false
	}
	body, err := GetResponseBody(http.MethodGet, baseURL+"/v1/usage", channel, GetAuthHeader(strings.TrimSpace(channel.Key)))
	if err != nil {
		return 0, false
	}
	var usage struct {
		Remaining *float64 `json:"remaining"`
		Unit      string   `json:"unit"`
	}
	if common.Unmarshal(body, &usage) != nil || usage.Remaining == nil || usage.Unit != "USD" {
		return 0, false
	}
	balance := service.UpstreamAmountToLocal(decimal.NewFromFloat(*usage.Remaining), settings).InexactFloat64()
	channel.UpdateBalance(balance)
	return balance, true
}
