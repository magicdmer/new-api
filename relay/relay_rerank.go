package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"one-api/common"
	"one-api/dto"
	"one-api/model"
	relaycommon "one-api/relay/common"
	"one-api/service"
	"one-api/setting"
)

func getRerankPromptToken(rerankRequest dto.RerankRequest) int {
	token, _ := service.CountTokenInput(rerankRequest.Query, rerankRequest.Model)
	for _, document := range rerankRequest.Documents {
		tkm, err := service.CountTokenInput(document, rerankRequest.Model)
		if err == nil {
			token += tkm
		}
	}
	return token
}

func RerankHelper(c *gin.Context, relayMode int) (openaiErr *dto.OpenAIErrorWithStatusCode) {
	relayInfo := relaycommon.GenRelayInfo(c)

	var rerankRequest *dto.RerankRequest
	err := common.UnmarshalBodyReusable(c, &rerankRequest)
	if err != nil {
		common.LogError(c, fmt.Sprintf("getAndValidateTextRequest failed: %s", err.Error()))
		return service.OpenAIErrorWrapperLocal(err, "invalid_text_request", http.StatusBadRequest)
	}
	if rerankRequest.Query == "" {
		return service.OpenAIErrorWrapperLocal(fmt.Errorf("query is empty"), "invalid_query", http.StatusBadRequest)
	}
	if len(rerankRequest.Documents) == 0 {
		return service.OpenAIErrorWrapperLocal(fmt.Errorf("documents is empty"), "invalid_documents", http.StatusBadRequest)
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return service.OpenAIErrorWrapperLocal(err, "unmarshal_model_mapping_failed", http.StatusInternalServerError)
		}
		if modelMap[rerankRequest.Model] != "" {
			rerankRequest.Model = modelMap[rerankRequest.Model]
		}
	}

	relayInfo.UpstreamModelName = rerankRequest.Model
	modelPrice, success := common.GetModelPrice(rerankRequest.Model, false)
	groupRatio := setting.GetGroupRatio(relayInfo.Group)

	var preConsumedQuota int
	var ratio float64
	var modelRatio float64

	promptToken := getRerankPromptToken(*rerankRequest)
	if !success {
		preConsumedTokens := promptToken
		modelRatio = common.GetModelRatio(rerankRequest.Model)
		ratio = modelRatio * groupRatio
		preConsumedQuota = int(float64(preConsumedTokens) * ratio)
	} else {
		preConsumedQuota = int(modelPrice * common.QuotaPerUnit * groupRatio)
	}
	relayInfo.PromptTokens = promptToken

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
			return service.OpenAIErrorWrapperLocal(fmt.Errorf("rerank pre-consumed quota failed, user quota: %d, need quota: %d", userQuota, preConsumedQuota), "insufficient_user_quota", http.StatusBadRequest)
		}
	}

	adaptor := GetAdaptor(relayInfo.ApiType)
	if adaptor == nil {
		return service.OpenAIErrorWrapperLocal(fmt.Errorf("invalid api type: %d", relayInfo.ApiType), "invalid_api_type", http.StatusBadRequest)
	}
	adaptor.Init(relayInfo)

	convertedRequest, err := adaptor.ConvertRerankRequest(c, relayInfo.RelayMode, *rerankRequest)
	if err != nil {
		return service.OpenAIErrorWrapperLocal(err, "convert_request_failed", http.StatusInternalServerError)
	}
	jsonData, err := json.Marshal(convertedRequest)
	if err != nil {
		return service.OpenAIErrorWrapperLocal(err, "json_marshal_failed", http.StatusInternalServerError)
	}
	requestBody := bytes.NewBuffer(jsonData)
	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, relayInfo, requestBody)
	if err != nil {
		return service.OpenAIErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}

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
		postConsumeQuota(c, relayInfo, rerankRequest.Model, usage.(*dto.Usage), ratio, preConsumedQuota, userQuota, modelRatio, groupRatio, modelPrice, success, "")
	} else {
		logContent := "（无限额度）"
		postConsumeQuota(c, relayInfo, rerankRequest.Model, usage.(*dto.Usage), ratio, 0, 0, modelRatio, groupRatio, modelPrice, success, logContent)
	}
	return nil
}
