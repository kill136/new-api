package model

// RequestLog stores the full request and response body for API relay calls.
// Linked to the Log table via RequestId for detail lookups.
type RequestLog struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"index"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
	RequestId    string `json:"request_id" gorm:"type:varchar(64);index:idx_request_log_request_id;default:''"`
	RequestBody  string `json:"request_body" gorm:"type:text"`
	ResponseBody string `json:"response_body" gorm:"type:text"`
	ModelName    string `json:"model_name" gorm:"type:varchar(255);index;default:''"`
	TokenName    string `json:"token_name" gorm:"type:varchar(255);default:''"`
	ChannelId    int    `json:"channel_id" gorm:"default:0"`
	Endpoint     string `json:"endpoint" gorm:"type:varchar(512);default:''"`
	StatusCode   int    `json:"status_code" gorm:"default:0"`
	UseTime      int    `json:"use_time" gorm:"default:0"`
	IsStream     bool   `json:"is_stream"`
}

func CreateRequestLog(log *RequestLog) error {
	return LOG_DB.Create(log).Error
}

func GetRequestLogByRequestId(requestId string) (*RequestLog, error) {
	var log RequestLog
	err := LOG_DB.Where("request_id = ?", requestId).First(&log).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

func GetRequestLogByRequestIdAndUserId(requestId string, userId int) (*RequestLog, error) {
	var log RequestLog
	err := LOG_DB.Where("request_id = ? AND user_id = ?", requestId, userId).First(&log).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}

func GetAllRequestLogs(startTimestamp int64, endTimestamp int64, modelName string, username string, startIdx int, num int) (logs []*RequestLog, total int64, err error) {
	tx := LOG_DB.Model(&RequestLog{})

	if modelName != "" {
		tx = tx.Where("model_name like ?", modelName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	err = tx.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	return logs, total, err
}

func DeleteOldRequestLog(targetTimestamp int64, limit int) (int64, error) {
	result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&RequestLog{})
	return result.RowsAffected, result.Error
}
