package relay

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http"
	"one-api/common"
	"one-api/dto"
	"one-api/model"
	relaycommon "one-api/relay/common"
	"one-api/service"
	"one-api/setting"
)

func WssHelper(c *gin.Context, ws *websocket.Conn) (openaiErr *dto.OpenAIErrorWithStatusCode) {
	relayInfo := relaycommon.GenRelayInfoWs(c, ws)

	// get & validate textRequest 获取并验证文本请求
	//realtimeEvent, err := getAndValidateWssRequest(c, ws)
	//if err != nil {
	//	common.LogError(c, fmt.Sprintf("getAndValidateWssRequest failed: %s", err.Error()))
	//	return service.OpenAIErrorWrapperLocal(err, "invalid_text_request", http.StatusBadRequest)
	//}

	// map model name
	modelMapping := c.GetString("model_mapping")
	//isModelMapped := false
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return service.OpenAIErrorWrapperLocal(err, "unmarshal_model_mapping_failed", http.StatusInternalServerError)
		}
		if modelMap[relayInfo.OriginModelName] != "" {
			relayInfo.UpstreamModelName = modelMap[relayInfo.OriginModelName]
			// set upstream model name
			//isModelMapped = true
		}
	}
	//relayInfo.UpstreamModelName = textRequest.Model
	modelPrice, getModelPriceSuccess := common.GetModelPrice(relayInfo.UpstreamModelName, false)
	groupRatio := setting.GetGroupRatio(relayInfo.Group)

	var preConsumedQuota int
	var ratio float64
	var modelRatio float64
	//err := service.SensitiveWordsCheck(textRequest)

	//if constant.ShouldCheckPromptSensitive() {
	//	err = checkRequestSensitive(textRequest, relayInfo)
	//	if err != nil {
	//		return service.OpenAIErrorWrapperLocal(err, "sensitive_words_detected", http.StatusBadRequest)
	//	}
	//}

	//promptTokens, err := getWssPromptTokens(realtimeEvent, relayInfo)
	//// count messages token error 计算promptTokens错误
	//if err != nil {
	//	return service.OpenAIErrorWrapper(err, "count_token_messages_failed", http.StatusInternalServerError)
	//}
	//
	if !getModelPriceSuccess {
		preConsumedTokens := common.PreConsumedQuota
		//if realtimeEvent.Session.MaxResponseOutputTokens != 0 {
		//	preConsumedTokens = promptTokens + int(realtimeEvent.Session.MaxResponseOutputTokens)
		//}
		modelRatio = common.GetModelRatio(relayInfo.UpstreamModelName)
		ratio = modelRatio * groupRatio
		preConsumedQuota = int(float64(preConsumedTokens) * ratio)
	} else {
		preConsumedQuota = int(modelPrice * common.QuotaPerUnit * groupRatio)
		relayInfo.UsePrice = true
	}

	// 检查用户是否有无限额度
	isUnlimited, err := model.IsUnlimitedQuota(relayInfo.UserId)
	if err != nil {
		return service.OpenAIErrorWrapperLocal(err, "check_unlimited_quota_failed", http.StatusInternalServerError)
	}

	var userQuota int
	if !isUnlimited {
		userQuota, err = model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return service.OpenAIErrorWrapperLocal(err, "get_user_quota_failed", http.StatusInternalServerError)
		}
		if userQuota < preConsumedQuota {
			return service.OpenAIErrorWrapperLocal(fmt.Errorf("websocket pre-consumed quota failed, user quota: %d, need quota: %d", userQuota, preConsumedQuota), "insufficient_user_quota", http.StatusBadRequest)
		}
	}

	adaptor := GetAdaptor(relayInfo.ApiType)
	if adaptor == nil {
		return service.OpenAIErrorWrapperLocal(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), "invalid_api_type", http.StatusBadRequest)
	}
	adaptor.Init(relayInfo)
	//var requestBody io.Reader
	//firstWssRequest, _ := c.Get("first_wss_request")
	//requestBody = bytes.NewBuffer(firstWssRequest.([]byte))

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, relayInfo, nil)
	if err != nil {
		return service.OpenAIErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}

	if resp != nil {
		relayInfo.TargetWs = resp.(*websocket.Conn)
		defer relayInfo.TargetWs.Close()
	}

	usage, openaiErr := adaptor.DoResponse(c, nil, relayInfo)
	if openaiErr != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(openaiErr, statusCodeMappingStr)
		return openaiErr
	}

	if !isUnlimited {
		service.PostWssConsumeQuota(c, relayInfo, relayInfo.UpstreamModelName, usage.(*dto.RealtimeUsage), preConsumedQuota,
			userQuota, modelRatio, groupRatio, modelPrice, getModelPriceSuccess, "")
	} else {
		logContent := "（无限额度）"
		service.PostWssConsumeQuota(c, relayInfo, relayInfo.UpstreamModelName, usage.(*dto.RealtimeUsage), 0,
			0, modelRatio, groupRatio, modelPrice, getModelPriceSuccess, logContent)
	}

	return nil
}
