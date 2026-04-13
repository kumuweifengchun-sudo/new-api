package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

func UpdateMidjourneyTaskBulk() {
	ctx := context.TODO()
	fallback := model.TaskFallbackInterval()
	backfillTicker := time.NewTicker(fallback)
	defer backfillTicker.Stop()

	wakeCh := make(chan struct{}, 1)
	if pubsub := model.SubscribeTaskSync(); pubsub != nil {
		go func() {
			defer pubsub.Close()
			for msg := range pubsub.Channel() {
				var event model.TaskSyncEvent
				if err := common.UnmarshalJsonStr(msg.Payload, &event); err != nil {
					continue
				}
				if event.Kind != "midjourney" {
					continue
				}
				select {
				case wakeCh <- struct{}{}:
				default:
				}
			}
		}()
	}

	process := func(cause string) {
		ids, err := model.FetchDuePendingMidjourneyIDs(constant.TaskQueryLimit)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("load due midjourney ids failed: %v", err))
			return
		}
		if len(ids) == 0 {
			return
		}

		tasks, err := model.GetMidjourneyTasksByIDs(ids)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("load due midjourney tasks failed: %v", err))
			return
		}

		logger.LogInfo(ctx, fmt.Sprintf("检测到待更新 Midjourney 任务数: %v, cause=%s", len(tasks), cause))
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Midjourney)
		nullTaskIds := make([]int, 0)
		found := make(map[int64]bool, len(tasks))
		for _, task := range tasks {
			if task == nil {
				continue
			}
			found[int64(task.Id)] = true
			if task.MjId == "" {
				nullTaskIds = append(nullTaskIds, task.Id)
				continue
			}
			taskM[task.MjId] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task.MjId)
		}
		if len(nullTaskIds) > 0 {
			err := model.MjBulkUpdateByTaskIds(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null mj_id task error: %v", err))
			}
		}
		var missingIDs []int64
		for _, id := range ids {
			if !found[id] {
				missingIDs = append(missingIDs, id)
			}
		}
		if len(missingIDs) > 0 {
			_ = model.RemovePendingMidjourney(missingIDs)
		}
		if len(taskChannelM) == 0 {
			return
		}

		for channelId, taskIds := range taskChannelM {
			logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
			if len(taskIds) == 0 {
				continue
			}
			midjourneyChannel, err := model.CacheGetChannel(channelId)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("CacheGetChannel: %v", err))
				err := model.MjBulkUpdate(taskIds, map[string]any{
					"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
					"status":      "FAILURE",
					"progress":    "100%",
				})
				if err != nil {
					logger.LogInfo(ctx, fmt.Sprintf("UpdateMidjourneyTask error: %v", err))
				}
				continue
			}
			requestUrl := fmt.Sprintf("%s/mj/task/list-by-condition", *midjourneyChannel.BaseURL)

			body, _ := json.Marshal(map[string]any{
				"ids": taskIds,
			})
			req, err := http.NewRequest("POST", requestUrl, bytes.NewBuffer(body))
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Get Task error: %v", err))
				continue
			}
			// 设置超时时间
			timeout := time.Second * 15
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			// 使用带有超时的 context 创建新的请求
			req = req.WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("mj-api-secret", midjourneyChannel.Key)
			resp, err := service.GetHttpClient().Do(req)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Get Task Do req error: %v", err))
				continue
			}
			if resp.StatusCode != http.StatusOK {
				logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
				continue
			}
			responseBody, err := io.ReadAll(resp.Body)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error: %v", err))
				continue
			}
			var responseItems []dto.MidjourneyDto
			err = json.Unmarshal(responseBody, &responseItems)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Get Mjp Task parse body error2: %v, body: %s", err, string(responseBody)))
				continue
			}
			resp.Body.Close()
			req.Body.Close()
			cancel()

			for _, responseItem := range responseItems {
				task := taskM[responseItem.MjId]

				useTime := (time.Now().UnixNano() / int64(time.Millisecond)) - task.SubmitTime
				// 如果时间超过一小时，且进度不是100%，则认为任务失败
				if useTime > 3600000 && task.Progress != "100%" {
					responseItem.FailReason = "上游任务超时（超过1小时）"
					responseItem.Status = "FAILURE"
				}
				if !checkMjTaskNeedUpdate(task, responseItem) {
					continue
				}
				preStatus := task.Status
				task.Code = 1
				task.Progress = responseItem.Progress
				task.PromptEn = responseItem.PromptEn
				task.State = responseItem.State
				task.SubmitTime = responseItem.SubmitTime
				task.StartTime = responseItem.StartTime
				task.FinishTime = responseItem.FinishTime
				task.ImageUrl = responseItem.ImageUrl
				task.Status = responseItem.Status
				task.FailReason = responseItem.FailReason
				if responseItem.Properties != nil {
					propertiesStr, _ := json.Marshal(responseItem.Properties)
					task.Properties = string(propertiesStr)
				}
				if responseItem.Buttons != nil {
					buttonStr, _ := json.Marshal(responseItem.Buttons)
					task.Buttons = string(buttonStr)
				}
				// 映射 VideoUrl
				task.VideoUrl = responseItem.VideoUrl

				// 映射 VideoUrls - 将数组序列化为 JSON 字符串
				if responseItem.VideoUrls != nil && len(responseItem.VideoUrls) > 0 {
					videoUrlsStr, err := json.Marshal(responseItem.VideoUrls)
					if err != nil {
						logger.LogError(ctx, fmt.Sprintf("序列化 VideoUrls 失败: %v", err))
						task.VideoUrls = "[]" // 失败时设置为空数组
					} else {
						task.VideoUrls = string(videoUrlsStr)
					}
				} else {
					task.VideoUrls = "" // 空值时清空字段
				}

				shouldReturnQuota := false
				if (task.Progress != "100%" && responseItem.FailReason != "") || (task.Progress == "100%" && task.Status == "FAILURE") {
					logger.LogInfo(ctx, task.MjId+" 构建失败，"+task.FailReason)
					task.Progress = "100%"
					if task.Quota != 0 {
						shouldReturnQuota = true
					}
				}
				won, err := task.UpdateWithStatus(preStatus)
				if err != nil {
					logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
				} else if won && shouldReturnQuota {
					err = model.IncreaseUserQuota(task.UserId, task.Quota, false)
					if err != nil {
						logger.LogError(ctx, "fail to increase user quota: "+err.Error())
					}
					model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
						UserId:    task.UserId,
						LogType:   model.LogTypeRefund,
						Content:   "",
						ChannelId: task.ChannelId,
						ModelName: service.CovertMjpActionToModelName(task.Action),
						Quota:     task.Quota,
						Other: map[string]interface{}{
							"task_id": task.MjId,
							"reason":  "构图失败",
						},
					})
				}
			}
		}

		var activeIDs []int64
		var doneIDs []int64
		for _, task := range taskM {
			if task == nil {
				continue
			}
			if model.IsActiveMidjourneyStatus(task.Status) {
				activeIDs = append(activeIDs, int64(task.Id))
			} else {
				doneIDs = append(doneIDs, int64(task.Id))
			}
		}
		if len(doneIDs) > 0 {
			_ = model.RemovePendingMidjourney(doneIDs)
		}
		if len(activeIDs) > 0 {
			_ = model.SchedulePendingMidjourney(activeIDs, model.TaskPollInterval())
		}
	}

	if backfillIDs, err := model.BackfillPendingMidjourneyIDs(constant.TaskQueryLimit); err == nil && len(backfillIDs) > 0 {
		_ = model.SchedulePendingMidjourney(backfillIDs, 0)
	}
	process("startup")

	for {
		timer := time.NewTimer(model.NextPendingMidjourneyDelay(fallback))
		select {
		case <-timer.C:
			process("due")
		case <-wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			process("event")
		case <-backfillTicker.C:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if backfillIDs, err := model.BackfillPendingMidjourneyIDs(constant.TaskQueryLimit); err == nil && len(backfillIDs) > 0 {
				_ = model.SchedulePendingMidjourney(backfillIDs, 0)
			}
			process("fallback")
		}
	}
}

func checkMjTaskNeedUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
	if oldTask.Code != 1 {
		return true
	}
	if oldTask.Progress != newTask.Progress {
		return true
	}
	if oldTask.PromptEn != newTask.PromptEn {
		return true
	}
	if oldTask.State != newTask.State {
		return true
	}
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.ImageUrl != newTask.ImageUrl {
		return true
	}
	if oldTask.Status != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if oldTask.Progress != "100%" && newTask.FailReason != "" {
		return true
	}
	// 检查 VideoUrl 是否需要更新
	if oldTask.VideoUrl != newTask.VideoUrl {
		return true
	}
	// 检查 VideoUrls 是否需要更新
	if newTask.VideoUrls != nil && len(newTask.VideoUrls) > 0 {
		newVideoUrlsStr, _ := json.Marshal(newTask.VideoUrls)
		if oldTask.VideoUrls != string(newVideoUrlsStr) {
			return true
		}
	} else if oldTask.VideoUrls != "" {
		// 如果新数据没有 VideoUrls 但旧数据有，需要更新（清空）
		return true
	}

	return false
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items := model.GetAllUserTask(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllUserTask(userId, queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
