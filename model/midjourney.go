package model

type Midjourney struct {
	Id          int    `json:"id"`
	Code        int    `json:"code"`
	UserId      int    `json:"user_id" gorm:"index"`
	Action      string `json:"action" gorm:"type:varchar(40);index"`
	MjId        string `json:"mj_id" gorm:"index"`
	Prompt      string `json:"prompt"`
	PromptEn    string `json:"prompt_en"`
	Description string `json:"description"`
	State       string `json:"state"`
	SubmitTime  int64  `json:"submit_time" gorm:"index;index:idx_mj_status_submit_time,priority:2"`
	StartTime   int64  `json:"start_time" gorm:"index"`
	FinishTime  int64  `json:"finish_time" gorm:"index"`
	ImageUrl    string `json:"image_url"`
	VideoUrl    string `json:"video_url"`
	VideoUrls   string `json:"video_urls"`
	Status      string `json:"status" gorm:"type:varchar(20);index;index:idx_mj_status_submit_time,priority:1"`
	Progress    string `json:"progress" gorm:"type:varchar(30);index"`
	FailReason  string `json:"fail_reason"`
	ChannelId   int    `json:"channel_id"`
	Quota       int    `json:"quota"`
	Buttons     string `json:"buttons"`
	Properties  string `json:"properties"`
}

// TaskQueryParams 用于包含所有搜索条件的结构体，可以根据需求添加更多字段
type TaskQueryParams struct {
	ChannelID      string
	MjID           string
	StartTimestamp string
	EndTimestamp   string
}

func GetAllUserTask(userId int, startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB.Where("user_id = ?", userId)

	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		// 假设您已将前端传来的时间戳转换为数据库所需的时间格式，并处理了时间戳的验证和解析
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllTasks(startIdx int, num int, queryParams TaskQueryParams) []*Midjourney {
	var tasks []*Midjourney
	var err error

	// 初始化查询构建器
	query := DB

	// 添加过滤条件
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}

	// 获取数据
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&tasks).Error
	if err != nil {
		return nil
	}

	return tasks
}

func GetAllUnFinishTasks() []*Midjourney {
	var tasks []*Midjourney
	var err error
	err = DB.Select("id", "code", "user_id", "action", "mj_id", "prompt", "prompt_en", "description", "state", "submit_time", "start_time", "finish_time", "image_url", "video_url", "video_urls", "status", "progress", "fail_reason", "channel_id", "quota", "buttons", "properties").
		Where("status NOT IN ?", []string{"SUCCESS", "FAILURE"}).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}

func GetMidjourneyTasksByIDs(ids []int64) ([]*Midjourney, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var tasks []*Midjourney
	err := DB.Select("id", "code", "user_id", "action", "mj_id", "prompt", "prompt_en", "description", "state", "submit_time", "start_time", "finish_time", "image_url", "video_url", "video_urls", "status", "progress", "fail_reason", "channel_id", "quota", "buttons", "properties").
		Where("id IN ?", ids).
		Order("id").
		Find(&tasks).Error
	return tasks, err
}

func BackfillPendingMidjourneyIDs(limit int) ([]int64, error) {
	if limit <= 0 {
		limit = TaskPendingBatchSize()
	}
	var ids []int64
	err := DB.Model(&Midjourney{}).
		Select("id").
		Where("status NOT IN ?", []string{"SUCCESS", "FAILURE"}).
		Order("submit_time ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	return ids, err
}

func GetByOnlyMJId(mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("mj_id = ?", mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJId(userId int, mjId string) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id = ?", userId, mjId).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetByMJIds(userId int, mjIds []string) []*Midjourney {
	var mj []*Midjourney
	var err error
	err = DB.Where("user_id = ? and mj_id in (?)", userId, mjIds).Find(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func GetMjByuId(id int) *Midjourney {
	var mj *Midjourney
	var err error
	err = DB.Where("id = ?", id).First(&mj).Error
	if err != nil {
		return nil
	}
	return mj
}

func UpdateProgress(id int, progress string) error {
	return DB.Model(&Midjourney{}).Where("id = ?", id).Update("progress", progress).Error
}

func (midjourney *Midjourney) Insert() error {
	err := sqliteBusyRetry("midjourney insert", func() error {
		return DB.Create(midjourney).Error
	})
	if err != nil {
		return err
	}
	if isActiveMidjourneyStatus(midjourney.Status) {
		_ = RegisterPendingMidjourney(int64(midjourney.Id))
	} else {
		_ = RemovePendingMidjourney([]int64{int64(midjourney.Id)})
	}
	return nil
}

func (midjourney *Midjourney) Update() error {
	err := sqliteBusyRetry("midjourney update", func() error {
		return DB.Save(midjourney).Error
	})
	if err != nil {
		return err
	}
	if isActiveMidjourneyStatus(midjourney.Status) {
		_ = SchedulePendingMidjourney([]int64{int64(midjourney.Id)}, TaskPollInterval())
	} else {
		_ = RemovePendingMidjourney([]int64{int64(midjourney.Id)})
	}
	return nil
}

// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Returns (true, nil) if this caller won the update, (false, nil) if
// another process already moved the task out of fromStatus.
// UpdateWithStatus performs a conditional UPDATE guarded by fromStatus (CAS).
// Uses Model().Select("*").Updates() to avoid GORM Save()'s INSERT fallback.
func (midjourney *Midjourney) UpdateWithStatus(fromStatus string) (bool, error) {
	var resultRows int64
	err := sqliteBusyRetry("midjourney cas update", func() error {
		result := DB.Model(midjourney).Where("status = ?", fromStatus).Select("*").Updates(midjourney)
		if result.Error != nil {
			return result.Error
		}
		resultRows = result.RowsAffected
		return nil
	})
	if err != nil {
		return false, err
	}
	if resultRows > 0 {
		if isActiveMidjourneyStatus(midjourney.Status) {
			_ = SchedulePendingMidjourney([]int64{int64(midjourney.Id)}, TaskPollInterval())
		} else {
			_ = RemovePendingMidjourney([]int64{int64(midjourney.Id)})
		}
	}
	return resultRows > 0, nil
}

func MjBulkUpdate(mjIds []string, params map[string]any) error {
	return sqliteBusyRetry("midjourney bulk update by mj_id", func() error {
		return DB.Model(&Midjourney{}).
			Where("mj_id in (?)", mjIds).
			Updates(params).Error
	})
}

func MjBulkUpdateByTaskIds(taskIDs []int, params map[string]any) error {
	err := sqliteBusyRetry("midjourney bulk update by id", func() error {
		return DB.Model(&Midjourney{}).
			Where("id in (?)", taskIDs).
			Updates(params).Error
	})
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(taskIDs))
	for _, id := range taskIDs {
		ids = append(ids, int64(id))
	}
	if statusRaw, ok := params["status"].(string); ok {
		if isActiveMidjourneyStatus(statusRaw) {
			_ = SchedulePendingMidjourney(ids, TaskPollInterval())
		} else {
			_ = RemovePendingMidjourney(ids)
		}
		return nil
	}
	if progressRaw, ok := params["progress"].(string); ok && progressRaw == "100%" {
		_ = RemovePendingMidjourney(ids)
		return nil
	}
	_ = SchedulePendingMidjourney(ids, TaskPollInterval())
	return nil
}

// CountAllTasks returns total midjourney tasks for admin query
func CountAllTasks(queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{})
	if queryParams.ChannelID != "" {
		query = query.Where("channel_id = ?", queryParams.ChannelID)
	}
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}

// CountAllUserTask returns total midjourney tasks for user
func CountAllUserTask(userId int, queryParams TaskQueryParams) int64 {
	var total int64
	query := DB.Model(&Midjourney{}).Where("user_id = ?", userId)
	if queryParams.MjID != "" {
		query = query.Where("mj_id = ?", queryParams.MjID)
	}
	if queryParams.StartTimestamp != "" {
		query = query.Where("submit_time >= ?", queryParams.StartTimestamp)
	}
	if queryParams.EndTimestamp != "" {
		query = query.Where("submit_time <= ?", queryParams.EndTimestamp)
	}
	_ = query.Count(&total).Error
	return total
}
