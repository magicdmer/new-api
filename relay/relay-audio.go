package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"one-api/common"
	"one-api/dto"
	"one-api/model"
	relaycommon "one-api/relay/common"
	relayconstant "one-api/relay/constant"
	"one-api/service"
	"one-api/setting"
)

func getAndValidAudioRequest(c *gin.Context, info *relaycommon.RelayInfo) (*dto.AudioRequest, error) {
	audioRequest := &dto.AudioRequest{}
	err := common.UnmarshalBodyReusable(c, audioRequest)
	if err != nil {
		return nil, err
	}
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		if audioRequest.Model == "" {
			return nil, errors.New("model is required")
		}
		if setting.ShouldCheckPromptSensitive() {
			err := service.CheckSensitiveInput(audioRequest.Input)
			if err != nil {
				return nil, err
			}
		}
	default:
		err = c.Request.ParseForm()
		if err != nil {
			return nil, err
		}
		formData := c.Request.PostForm
		if audioRequest.Model == "" {
			audioRequest.Model = formData.Get("model")
		}

		if audioRequest.Model == "" {
			return nil, errors.New("model is required")
		}
		audioRequest.ResponseFormat = formData.Get("response_format")
		if audioRequest.ResponseFormat == "" {
			audioRequest.ResponseFormat = "json"
		}
	}
	return audioRequest, nil
}

func AudioHelper(c *gin.Context) (openaiErr *dto.OpenAIErrorWithStatusCode) {
	relayInfo := relaycommon.GenRelayInfo(c)
	audioRequest, err := getAndValidAudioRequest(c, relayInfo)

	if err != nil {
		common.LogError(c, fmt.Sprintf("getAndValidAudioRequest failed: %s", err.Error()))
		return service.OpenAIErrorWrapper(err, "invalid_audio_request", http.StatusBadRequest)
	}

	promptTokens := 0
	preConsumedTokens := common.PreConsumedQuota
	if relayInfo.RelayMode == relayconstant.RelayModeAudioSpeech {
		promptTokens, err = service.CountTTSToken(audioRequest.Input, audioRequest.Model)
		if err != nil {
			return service.OpenAIErrorWrapper(err, "count_audio_token_failed", http.StatusInternalServerError)
		}
		preConsumedTokens = promptTokens
		relayInfo.PromptTokens = promptTokens
	}

	modelRatio := common.GetModelRatio(audioRequest.Model)
	groupRatio := setting.GetGroupRatio(relayInfo.Group)
	ratio := modelRatio * groupRatio
	preConsumedQuota := int(float64(preConsumedTokens) * ratio)

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
			return service.OpenAIErrorWrapperLocal(errors.New(fmt.Sprintf("audio pre-consumed quota failed, user quota: %d, need quota: %d", userQuota, preConsumedQuota)), "insufficient_user_quota", http.StatusBadRequest)
		}
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return service.OpenAIErrorWrapper(err, "unmarshal_model_mapping_failed", http.StatusInternalServerError)
		}
		if modelMap[audioRequest.Model] != "" {
			audioRequest.Model = modelMap[audioRequest.Model]
		}
	}
	relayInfo.UpstreamModelName = audioRequest.Model

	adaptor := GetAdaptor(relayInfo.ApiType)
	if adaptor == nil {
		return service.OpenAIErrorWrapperLocal(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), "invalid_api_type", http.StatusBadRequest)
	}
	adaptor.Init(relayInfo)

	ioReader, err := adaptor.ConvertAudioRequest(c, relayInfo, *audioRequest)
	if err != nil {
		return service.OpenAIErrorWrapperLocal(err, "convert_request_failed", http.StatusInternalServerError)
	}

	resp, err := adaptor.DoRequest(c, relayInfo, ioReader)
	if err != nil {
		return service.OpenAIErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	statusCodeMappingStr := c.GetString("status_code_mapping")

	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			openaiErr = service.RelayErrorHandler(httpResp)
			// reset status code 重置状态码
			service.ResetStatusCode(openaiErr, statusCodeMappingStr)
			return openaiErr
		}
	}

	usage, openaiErr := adaptor.DoResponse(c, httpResp, relayInfo)
	if openaiErr != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(openaiErr, statusCodeMappingStr)
		return openaiErr
	}

	if !isUnlimited {
		postConsumeQuota(c, relayInfo, audioRequest.Model, usage.(*dto.Usage), ratio, preConsumedQuota, userQuota, modelRatio, groupRatio, 0, false, "")
	} else {
		logContent := "（无限额度）"
		postConsumeQuota(c, relayInfo, audioRequest.Model, usage.(*dto.Usage), ratio, 0, 0, modelRatio, groupRatio, 0, false, logContent)
	}

	return nil
}
