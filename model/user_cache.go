package model

import (
	"fmt"
	"one-api/common"
	"one-api/constant"
	"strconv"
	"time"
)

// Change UserCache struct to userCache
type userCache struct {
	Id             int    `json:"id"`
	Group          string `json:"group"`
	Quota          int    `json:"quota"`
	Status         int    `json:"status"`
	Role           int    `json:"role"`
	Username       string `json:"username"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
}

// Rename all exported functions to private ones
// invalidateUserCache clears all user related cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}

	keys := []string{
		fmt.Sprintf(constant.UserGroupKeyFmt, userId),
		fmt.Sprintf(constant.UserQuotaKeyFmt, userId),
		fmt.Sprintf(constant.UserEnabledKeyFmt, userId),
		fmt.Sprintf(constant.UserUsernameKeyFmt, userId),
	}

	for _, key := range keys {
		if err := common.RedisDel(key); err != nil {
			return fmt.Errorf("failed to delete cache key %s: %w", key, err)
		}
	}
	return nil
}

// updateUserGroupCache updates user group cache
func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisSet(
		fmt.Sprintf(constant.UserGroupKeyFmt, userId),
		group,
		time.Duration(constant.UserId2QuotaCacheSeconds)*time.Second,
	)
}

// updateUserQuotaCache updates user quota cache
func updateUserQuotaCache(userId int, quota int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisSet(
		fmt.Sprintf(constant.UserQuotaKeyFmt, userId),
		fmt.Sprintf("%d", quota),
		time.Duration(constant.UserId2QuotaCacheSeconds)*time.Second,
	)
}

// updateUserStatusCache updates user status cache
func updateUserStatusCache(userId int, userEnabled bool) error {
	if !common.RedisEnabled {
		return nil
	}
	enabled := "0"
	if userEnabled {
		enabled = "1"
	}
	return common.RedisSet(
		fmt.Sprintf(constant.UserEnabledKeyFmt, userId),
		enabled,
		time.Duration(constant.UserId2StatusCacheSeconds)*time.Second,
	)
}

// updateUserNameCache updates username cache
func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisSet(
		fmt.Sprintf(constant.UserUsernameKeyFmt, userId),
		username,
		time.Duration(constant.UserId2QuotaCacheSeconds)*time.Second,
	)
}

// updateUserUnlimitedQuotaCache updates user unlimited quota cache
func updateUserUnlimitedQuotaCache(userId int, unlimitedQuota bool) error {
	if !common.RedisEnabled {
		return nil
	}
	value := "0"
	if unlimitedQuota {
		value = "1"
	}
	return common.RedisSet(
		fmt.Sprintf("user:%d:unlimited_quota", userId),
		value,
		time.Duration(constant.UserId2QuotaCacheSeconds)*time.Second,
	)
}

// updateUserCache updates all user cache fields
func updateUserCache(userId int, username string, userGroup string, quota int, status int) error {
	if !common.RedisEnabled {
		return nil
	}

	if err := updateUserGroupCache(userId, userGroup); err != nil {
		return fmt.Errorf("update group cache: %w", err)
	}

	if err := updateUserQuotaCache(userId, quota); err != nil {
		return fmt.Errorf("update quota cache: %w", err)
	}

	if err := updateUserStatusCache(userId, status == common.UserStatusEnabled); err != nil {
		return fmt.Errorf("update status cache: %w", err)
	}

	if err := updateUserNameCache(userId, username); err != nil {
		return fmt.Errorf("update username cache: %w", err)
	}

	// Get user from database to update unlimited quota cache
	var user User
	if err := DB.First(&user, userId).Error; err != nil {
		return fmt.Errorf("get user from db: %w", err)
	}

	if err := updateUserUnlimitedQuotaCache(userId, user.UnlimitedQuota); err != nil {
		return fmt.Errorf("update unlimited quota cache: %w", err)
	}

	return nil
}

// getUserGroupCache gets user group from cache
func getUserGroupCache(userId int) (string, error) {
	if !common.RedisEnabled {
		return "", nil
	}
	return common.RedisGet(fmt.Sprintf(constant.UserGroupKeyFmt, userId))
}

// getUserQuotaCache gets user quota from cache
func getUserQuotaCache(userId int) (int, error) {
	if !common.RedisEnabled {
		return 0, nil
	}
	quotaStr, err := common.RedisGet(fmt.Sprintf(constant.UserQuotaKeyFmt, userId))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(quotaStr)
}

// getUserStatusCache gets user status from cache
func getUserStatusCache(userId int) (int, error) {
	if !common.RedisEnabled {
		return 0, nil
	}
	statusStr, err := common.RedisGet(fmt.Sprintf(constant.UserEnabledKeyFmt, userId))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(statusStr)
}

// getUserNameCache gets username from cache
func getUserNameCache(userId int) (string, error) {
	if !common.RedisEnabled {
		return "", nil
	}
	return common.RedisGet(fmt.Sprintf(constant.UserUsernameKeyFmt, userId))
}

// getUserUnlimitedQuotaCache gets user unlimited quota from cache
func getUserUnlimitedQuotaCache(userId int) (bool, error) {
	if !common.RedisEnabled {
		return false, nil
	}
	value, err := common.RedisGet(fmt.Sprintf("user:%d:unlimited_quota", userId))
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

// getUserCache gets complete user cache
func getUserCache(userId int) (*userCache, error) {
	if !common.RedisEnabled {
		return nil, nil
	}

	group, err := getUserGroupCache(userId)
	if err != nil {
		return nil, fmt.Errorf("get group cache: %w", err)
	}

	quota, err := getUserQuotaCache(userId)
	if err != nil {
		return nil, fmt.Errorf("get quota cache: %w", err)
	}

	status, err := getUserStatusCache(userId)
	if err != nil {
		return nil, fmt.Errorf("get status cache: %w", err)
	}

	username, err := getUserNameCache(userId)
	if err != nil {
		return nil, fmt.Errorf("get username cache: %w", err)
	}

	unlimitedQuota, err := getUserUnlimitedQuotaCache(userId)
	if err != nil {
		return nil, fmt.Errorf("get unlimited quota cache: %w", err)
	}

	return &userCache{
		Id:             userId,
		Group:          group,
		Quota:          quota,
		Status:         status,
		Username:       username,
		UnlimitedQuota: unlimitedQuota,
	}, nil
}

// Add atomic quota operations
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	key := fmt.Sprintf(constant.UserQuotaKeyFmt, userId)
	return common.RedisIncr(key, delta)
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

func cacheSetUser(user *User) error {
	return common.RedisHSetObj(
		fmt.Sprintf("user:%d", user.Id),
		map[string]interface{}{
			"unlimited_quota": user.UnlimitedQuota,
		},
		time.Duration(constant.UserId2QuotaCacheSeconds)*time.Second,
	)
}
