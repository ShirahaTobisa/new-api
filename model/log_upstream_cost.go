package model

// FindConsumeLogsByUpstreamRequestIds 找出这些渠道里、上游请求编号在列表中的消费日志，用来和上游账单对账
func FindConsumeLogsByUpstreamRequestIds(channelIds []int, upstreamRequestIds []string) ([]*Log, error) {
	var logs []*Log
	err := LOG_DB.Select("id", "channel_id", "upstream_request_id", "other").
		Where("type = ? AND channel_id IN ? AND upstream_request_id IN ?", LogTypeConsume, channelIds, upstreamRequestIds).
		Find(&logs).Error
	return logs, err
}

// UpdateLogOther 改写一条日志的 other，对账后用来写入上游实际成本
func UpdateLogOther(id int, other string) error {
	return LOG_DB.Model(&Log{}).Where("id = ?", id).Update("other", other).Error
}
