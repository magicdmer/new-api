package service

import (
	"errors"
	"fmt"
	"one-api/common"
	constant2 "one-api/constant"
	"one-api/dto"
	"one-api/model"
	relaycommon "one-api/relay/common"
	"one-api/relay/helper"
	"one-api/setting"
	"one-api/setting/operation_setting"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

type TokenDetails struct {
	TextTokens  int
	AudioTokens int
}

type QuotaInfo struct {
	InputDetails  TokenDetails
	OutputDetails TokenDetails
	ModelName     string
	UsePrice      bool
	ModelPrice    float64
	ModelRatio    float64
	GroupRatio    float64
}

func calculateAudioQuota(info QuotaInfo) int {
	if info.UsePrice {
		return int(info.ModelPrice * common.QuotaPerUnit * info.GroupRatio)
	}

	completionRatio := operation_setting.GetCompletionRatio(info.ModelName)
	audioRatio := operation_setting.GetAudioRatio(info.ModelName)
	audioCompletionRatio := operation_setting.GetAudioCompletionRatio(info.ModelName)
	ratio := info.GroupRatio * info.ModelRatio

	quota := 0.0
	quota += float64(info.InputDetails.TextTokens)
	quota += float64(info.OutputDetails.TextTokens) * completionRatio
	quota += float64(info.InputDetails.AudioTokens) * audioRatio
	quota += float64(info.OutputDetails.AudioTokens) * audioRatio * audioCompletionRatio

	quota = quota * ratio
	if ratio != 0 && quota <= 0 {
		quota = 1
	}

	return int(quota)
}

func PreWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage) error {
	if relayInfo.UsePrice {
		return nil
	}

	// 检查用户是否有无限额度
	isUnlimited, err := model.IsUnlimitedQuota(relayInfo.UserId)
	if err != nil {
		return err
	}
	if isUnlimited {
		return nil
	}

	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return err
	}

	token, err := model.GetTokenByKey(strings.TrimLeft(relayInfo.TokenKey, "sk-"), false)
	if err != nil {
		return err
	}

	modelName := relayInfo.OriginModelName
	textInputTokens := usage.InputTokenDetails.TextTokens
	textOutTokens := usage.OutputTokenDetails.TextTokens
	audioInputTokens := usage.InputTokenDetails.AudioTokens
	audioOutTokens := usage.OutputTokenDetails.AudioTokens
	groupRatio := setting.GetGroupRatio(relayInfo.Group)
	modelRatio, _ := operation_setting.GetModelRatio(modelName)

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:  modelName,
		UsePrice:   relayInfo.UsePrice,
		ModelRatio: modelRatio,
		GroupRatio: groupRatio,
	}

	quota := calculateAudioQuota(quotaInfo)

	if userQuota < quota {
		return fmt.Errorf("user quota is not enough, user quota: %s, need quota: %s", common.FormatQuota(userQuota), common.FormatQuota(quota))
	}

	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", common.FormatQuota(token.RemainQuota), common.FormatQuota(quota))
	}

	err = PostConsumeQuota(relayInfo, quota, 0, false)
	if err != nil {
		return err
	}
	common.LogInfo(ctx, "realtime streaming consume quota success, quota: "+fmt.Sprintf("%d", quota))
	return nil
}

func PostWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelName string,
	usage *dto.RealtimeUsage, preConsumedQuota int, userQuota int, modelRatio float64, groupRatio float64,
	modelPrice float64, usePrice bool, extraContent string) {

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	textInputTokens := usage.InputTokenDetails.TextTokens
	textOutTokens := usage.OutputTokenDetails.TextTokens

	audioInputTokens := usage.InputTokenDetails.AudioTokens
	audioOutTokens := usage.OutputTokenDetails.AudioTokens

	tokenName := ctx.GetString("token_name")
	completionRatio := operation_setting.GetCompletionRatio(modelName)
	audioRatio := operation_setting.GetAudioRatio(relayInfo.OriginModelName)
	audioCompletionRatio := operation_setting.GetAudioCompletionRatio(modelName)

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:  modelName,
		UsePrice:   usePrice,
		ModelRatio: modelRatio,
		GroupRatio: groupRatio,
	}

	quota := calculateAudioQuota(quotaInfo)

	totalTokens := usage.TotalTokens
	var logContent string
	if !usePrice {
		logContent = fmt.Sprintf("模型倍率 %.2f，补全倍率 %.2f，音频倍率 %.2f，音频补全倍率 %.2f，分组倍率 %.2f", modelRatio, completionRatio, audioRatio, audioCompletionRatio, groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// 检查是否是无限额度用户
	isUnlimited, err := model.IsUnlimitedQuota(relayInfo.UserId)
	if err == nil && isUnlimited {
		logContent += "（无限额度）"
		quota = 0 // 无限额度用户不扣除配额
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += fmt.Sprintf("（可能是上游超时）")
		common.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, "+
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, modelName, preConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	logModel := modelName
	if extraContent != "" {
		logContent += ", " + extraContent
	}
	other := GenerateWssOtherInfo(ctx, relayInfo, usage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice)
	model.RecordConsumeLog(ctx, relayInfo.UserId, relayInfo.ChannelId, usage.InputTokens, usage.OutputTokens, logModel,
		tokenName, quota, logContent, relayInfo.TokenId, userQuota, int(useTimeSeconds), relayInfo.IsStream, relayInfo.Group, other)
}

func PostAudioConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo,
	usage *dto.Usage, preConsumedQuota int, userQuota int, priceData helper.PriceData, extraContent string) {

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	textInputTokens := usage.PromptTokensDetails.TextTokens
	textOutTokens := usage.CompletionTokenDetails.TextTokens

	audioInputTokens := usage.PromptTokensDetails.AudioTokens
	audioOutTokens := usage.CompletionTokenDetails.AudioTokens

	tokenName := ctx.GetString("token_name")
	completionRatio := operation_setting.GetCompletionRatio(relayInfo.OriginModelName)
	audioRatio := operation_setting.GetAudioRatio(relayInfo.OriginModelName)
	audioCompletionRatio := operation_setting.GetAudioCompletionRatio(relayInfo.OriginModelName)

	modelRatio := priceData.ModelRatio
	groupRatio := priceData.GroupRatio
	modelPrice := priceData.ModelPrice
	usePrice := priceData.UsePrice

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:  relayInfo.OriginModelName,
		UsePrice:   usePrice,
		ModelRatio: modelRatio,
		GroupRatio: groupRatio,
	}

	quota := calculateAudioQuota(quotaInfo)

	totalTokens := usage.TotalTokens
	var logContent string
	if !usePrice {
		logContent = fmt.Sprintf("模型倍率 %.2f，补全倍率 %.2f，音频倍率 %.2f，音频补全倍率 %.2f，分组倍率 %.2f", modelRatio, completionRatio, audioRatio, audioCompletionRatio, groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// 检查是否是无限额度用户
	isUnlimited, err := model.IsUnlimitedQuota(relayInfo.UserId)
	if err == nil && isUnlimited {
		logContent += "（无限额度）"
		quota = 0 // 无限额度用户不扣除配额
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += fmt.Sprintf("（可能是上游超时）")
		common.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, "+
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, relayInfo.OriginModelName, preConsumedQuota))
	} else {
		quotaDelta := quota - preConsumedQuota
		if quotaDelta != 0 {
			err := PostConsumeQuota(relayInfo, quotaDelta, preConsumedQuota, true)
			if err != nil {
				common.LogError(ctx, "error consuming token remain quota: "+err.Error())
			}
		}
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	logModel := relayInfo.OriginModelName
	if extraContent != "" {
		logContent += ", " + extraContent
	}
	other := GenerateAudioOtherInfo(ctx, relayInfo, usage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice)
	model.RecordConsumeLog(ctx, relayInfo.UserId, relayInfo.ChannelId, usage.PromptTokens, usage.CompletionTokens, logModel,
		tokenName, quota, logContent, relayInfo.TokenId, userQuota, int(useTimeSeconds), relayInfo.IsStream, relayInfo.Group, other)
}

func PreConsumeTokenQuota(relayInfo *relaycommon.RelayInfo, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if relayInfo.IsPlayground {
		return nil
	}
	//if relayInfo.TokenUnlimited {
	//	return nil
	//}
	token, err := model.GetTokenByKey(relayInfo.TokenKey, false)
	if err != nil {
		return err
	}
	if !relayInfo.TokenUnlimited && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", common.FormatQuota(token.RemainQuota), common.FormatQuota(quota))
	}
	err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
	if err != nil {
		return err
	}
	return nil
}

func PostConsumeQuota(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int, sendEmail bool) (err error) {
	// 检查用户是否有无限额度
	isUnlimited, err := model.IsUnlimitedQuota(relayInfo.UserId)
	if err != nil {
		return err
	}

	// 无限额度用户不需要扣除额度
	if isUnlimited {
		return nil
	}

	if quota > 0 {
		err = model.DecreaseUserQuota(relayInfo.UserId, quota)
	} else {
		err = model.IncreaseUserQuota(relayInfo.UserId, -quota, false)
	}
	if err != nil {
		return err
	}

	if !relayInfo.IsPlayground {
		if quota > 0 {
			err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
		} else {
			err = model.IncreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, -quota)
		}
		if err != nil {
			return err
		}
	}

	if sendEmail {
		if (quota + preConsumedQuota) != 0 {
			checkAndSendQuotaNotify(relayInfo, quota, preConsumedQuota)
		}
	}

	return nil
}

func checkAndSendQuotaNotify(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int) {
	gopool.Go(func() {
		userSetting := relayInfo.UserSetting
		threshold := common.QuotaRemindThreshold
		if userCustomThreshold, ok := userSetting[constant2.UserSettingQuotaWarningThreshold]; ok {
			threshold = int(userCustomThreshold.(float64))
		}

		//noMoreQuota := userCache.Quota-(quota+preConsumedQuota) <= 0
		quotaTooLow := false
		consumeQuota := quota + preConsumedQuota
		if relayInfo.UserQuota-consumeQuota < threshold {
			quotaTooLow = true
		}
		if quotaTooLow {
			prompt := "您的额度即将用尽"
			topUpLink := fmt.Sprintf("%s/topup", setting.ServerAddress)
			content := "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
			err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, []interface{}{prompt, common.FormatQuota(relayInfo.UserQuota), topUpLink, topUpLink}))
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send quota notify to user %d: %s", relayInfo.UserId, err.Error()))
			}
		}
	})
}
