package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// UpstreamAmountToLocal 把上游站的美元额度按渠道的充值比例折算成本站美元；没填充值比例时原样返回。
func UpstreamAmountToLocal(amount decimal.Decimal, settings dto.ChannelOtherSettings) decimal.Decimal {
	if settings.UpstreamRechargeRate <= 0 || operation_setting.USDExchangeRate <= 0 {
		return amount
	}
	return amount.Mul(decimal.NewFromFloat(settings.UpstreamRechargeRate)).Div(decimal.NewFromFloat(operation_setting.USDExchangeRate))
}

// recordTextUpstreamCost 估算这次请求的上游成本，写进日志的 admin_info 并累加到渠道。
// 成本基数按分组倍率 1 重算：本站倍率为 0 的分组不扣费，但上游照样收钱。
func recordTextUpstreamCost(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, tokens billingexpr.TokenParams, summary textQuotaSummary, other *model.LogOther) {
	settings := relayInfo.ChannelOtherSettings
	if settings.UpstreamRechargeRate <= 0 || usage == nil {
		return
	}
	baseQuota := summary.Quota
	if summary.GroupRatio != 1 {
		info := *relayInfo
		info.PriceData.GroupRatioInfo.GroupRatio = 1
		if relayInfo.TieredBillingSnapshot != nil {
			snap := *relayInfo.TieredBillingSnapshot
			snap.GroupRatio = 1
			info.TieredBillingSnapshot = &snap
		}
		base := calculateTextQuotaSummary(ctx, &info, usage)
		baseQuota = base.Quota
		if ok, quota, result := TryTieredSettle(&info, tokens); ok {
			baseQuota = composeTieredTextQuota(&info, base, quota, result)
		}
	}
	upstreamGroupRatio := settings.UpstreamGroupRatio
	if upstreamGroupRatio <= 0 {
		upstreamGroupRatio = 1
	}
	amount := decimal.NewFromInt(int64(baseQuota)).Mul(decimal.NewFromFloat(upstreamGroupRatio))
	cost := common.QuotaFromDecimal(UpstreamAmountToLocal(amount, settings))
	if cost <= 0 {
		return
	}
	other.SetAdmin("upstream_cost", cost)
	model.UpdateChannelUpstreamCost(relayInfo.ChannelId, cost)
}
