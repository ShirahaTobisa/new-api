package controller

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/shopspring/decimal"
)

const (
	upstreamBillLookback = 6 * 60 * 60 // 重启后回看 6 小时，已对账的日志会跳过
	upstreamBillOverlap  = 5 * 60      // 每次多回看 5 分钟，接住上游已记账、本站还没写完日志的请求
	upstreamBillPageSize = 100
	upstreamBillMaxPages = 10
)

// upstreamBillCursors 每个上游账户已对账到的上游日志时间，只存内存
var upstreamBillCursors sync.Map

type upstreamBillItem struct {
	RequestId string  `json:"request_id"`
	Quota     float64 `json:"quota"`
	CreatedAt int64   `json:"created_at"`
}

// AutomaticallySyncUpstreamBills 定时拉取 new-api 上游的消费日志，按上游请求编号对上本站日志，
// 用上游实际扣费替换估算的上游成本。
func AutomaticallySyncUpstreamBills(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		syncUpstreamBills()
	}
}

func syncUpstreamBills() {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.SysError(common.LogText("upstream bill sync: failed to load channels: %s", err.Error()))
		return
	}
	// 同一个上游账户（同地址、同令牌）只拉一次账单
	sites := make(map[string][]*model.Channel)
	settingsById := make(map[int]dto.ChannelOtherSettings)
	for _, channel := range channels {
		settings := withSiteCredentials(channel, channel.GetOtherSettings())
		if settings.UpstreamAccessToken == "" {
			continue
		}
		key := strings.TrimRight(channel.GetBaseURL(), "/") + "|" + strings.TrimSpace(settings.UpstreamUserId) + "|" + strings.TrimSpace(settings.UpstreamAccessToken)
		sites[key] = append(sites[key], channel)
		settingsById[channel.Id] = settings
	}
	for key, siteChannels := range sites {
		if err := syncSiteUpstreamBill(key, siteChannels, settingsById); err != nil {
			common.SysLog(common.LogText("upstream bill sync failed: channel #%d: %s", siteChannels[0].Id, err.Error()))
		}
		time.Sleep(common.RequestInterval)
	}
}

func syncSiteUpstreamBill(key string, channels []*model.Channel, settingsById map[int]dto.ChannelOtherSettings) error {
	first := channels[0]
	settings := settingsById[first.Id]
	baseURL := strings.TrimRight(first.GetBaseURL(), "/")
	headers := upstreamAuthHeaders(strings.TrimSpace(settings.UpstreamUserId), strings.TrimSpace(settings.UpstreamAccessToken))
	channelIds := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIds = append(channelIds, channel.Id)
	}

	since := time.Now().Unix() - upstreamBillLookback
	if cursor, ok := upstreamBillCursors.Load(key); ok {
		since = cursor.(int64)
	}
	newest := since
	quotaPerUnit := 0.0 // 有日志对上了才去读上游的每美元额度
	for page := 1; page <= upstreamBillMaxPages; page++ {
		url := fmt.Sprintf("%s/api/log/self?type=%d&start_timestamp=%d&p=%d&page_size=%d", baseURL, model.LogTypeConsume, since, page, upstreamBillPageSize)
		body, err := GetResponseBody(http.MethodGet, url, first, headers)
		if err != nil {
			return err
		}
		var response struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Data    struct {
				Items []upstreamBillItem `json:"items"`
			} `json:"data"`
		}
		if err := common.Unmarshal(body, &response); err != nil {
			return err
		}
		if !response.Success {
			return fmt.Errorf("upstream log query failed: %s", response.Message)
		}

		bills := make(map[string]float64, len(response.Data.Items))
		requestIds := make([]string, 0, len(response.Data.Items))
		for _, item := range response.Data.Items {
			newest = max(newest, item.CreatedAt)
			if item.RequestId != "" {
				bills[item.RequestId] = item.Quota
				requestIds = append(requestIds, item.RequestId)
			}
		}
		if len(requestIds) > 0 {
			logs, err := model.FindConsumeLogsByUpstreamRequestIds(channelIds, requestIds)
			if err != nil {
				return err
			}
			for _, entry := range logs {
				if quotaPerUnit == 0 {
					quotaPerUnit = upstreamQuotaPerUnit(first, baseURL)
				}
				applyUpstreamBill(entry, bills[entry.UpstreamRequestId], quotaPerUnit, settingsById[entry.ChannelId])
			}
		}
		if len(response.Data.Items) < upstreamBillPageSize {
			break
		}
		time.Sleep(common.RequestInterval)
	}
	upstreamBillCursors.Store(key, max(since, newest-upstreamBillOverlap))
	return nil
}

// applyUpstreamBill 把上游实际扣费写进本站日志的 admin_info，并把渠道累计成本里的估算换成实际值。
// 已对过账的日志直接跳过，重复同步不会重复累加。
func applyUpstreamBill(entry *model.Log, upstreamQuota float64, quotaPerUnit float64, settings dto.ChannelOtherSettings) {
	other := map[string]any{}
	if entry.Other != "" && common.UnmarshalJsonStr(entry.Other, &other) != nil {
		return
	}
	adminInfo, _ := other["admin_info"].(map[string]any)
	if adminInfo == nil {
		adminInfo = map[string]any{}
	}
	if adminInfo["upstream_cost_exact"] == true {
		return
	}
	usd := decimal.NewFromFloat(upstreamQuota).Div(decimal.NewFromFloat(quotaPerUnit))
	cost := common.QuotaFromDecimal(service.UpstreamAmountToLocal(usd, settings).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	estimate, _ := adminInfo["upstream_cost"].(float64)
	adminInfo["upstream_cost"] = cost
	adminInfo["upstream_cost_exact"] = true
	other["admin_info"] = adminInfo
	data, err := common.Marshal(other)
	if err != nil {
		return
	}
	if err := model.UpdateLogOther(entry.Id, string(data)); err != nil {
		common.SysLog(common.LogText("upstream bill sync: failed to update log #%d: %s", entry.Id, err.Error()))
		return
	}
	if delta := cost - int(estimate); delta != 0 {
		model.UpdateChannelUpstreamCost(entry.ChannelId, delta)
	}
}
