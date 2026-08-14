package server

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/account"
	"icloud-hme/internal/auditlog"
	"icloud-hme/internal/hme"
	"icloud-hme/internal/upstream"
)

const (
	apiErrorSessionExpired      = "icloud_session_expired"
	apiErrorEntitlementRequired = "icloud_entitlement_required"
	apiErrorAliasLimit          = "alias_limit_reached"
	apiErrorAliasDailyLimit     = "alias_daily_limit_reached"
	apiErrorRateLimited         = "icloud_rate_limited"
	apiErrorDeviceTrust         = "icloud_device_trust_required"
	apiErrorValidation          = "request_validation_failed"
	apiErrorStateConflict       = "request_state_conflict"
	apiErrorServiceUnavailable  = "icloud_service_unavailable"
	apiErrorUpstreamRejected    = "icloud_upstream_rejected"
	apiErrorCredentialsInvalid  = "icloud_credentials_invalid"
	apiErrorOTPInvalid          = "icloud_otp_invalid"
	apiErrorPrivacyTerms        = "icloud_privacy_terms_required"
	apiErrorLoginChallenge      = "login_challenge_expired"
)

type apiErrorContract struct {
	Code      string
	Message   string
	Action    string
	Retryable bool
}

func failWithContract(c *gin.Context, status int, auditCode string, contract apiErrorContract) {
	setOperationLogErrorCode(c, auditCode)
	retryable := contract.Retryable
	c.JSON(status, apiResp{
		Success:   false,
		Code:      contract.Code,
		Message:   contract.Message,
		Action:    contract.Action,
		Retryable: &retryable,
	})
}

func (s *Server) failUpstream(c *gin.Context, accountID string, err error) {
	contract, status, auditCode := classifyAPIError(err)
	if contract.Code == apiErrorSessionExpired {
		s.notifySessionExpired(accountID)
	}
	failWithContract(c, status, auditCode, contract)
}

func isConfirmedSessionError(err error) bool {
	var classified *upstream.Error
	return errors.As(err, &classified) && classified.Kind == upstream.KindSessionExpired
}

func classifyAPIError(err error) (apiErrorContract, int, string) {
	var classified *upstream.Error
	if errors.As(err, &classified) {
		switch classified.Kind {
		case upstream.KindSessionExpired:
			return apiErrorContract{
				Code:      apiErrorSessionExpired,
				Message:   "会话已过期，请更新 Cookie。",
				Action:    "update_cookie",
				Retryable: false,
			}, http.StatusUnauthorized, auditlog.ErrorCodeUnauthorized
		case upstream.KindEntitlementRequired:
			return apiErrorContract{
				Code:    apiErrorEntitlementRequired,
				Message: "当前 Apple 账户未满足 Hide My Email 或 iCloud+ 使用资格，请确认账户权限后重试。",
				Action:  "check_icloud_plus",
			}, http.StatusForbidden, auditlog.ErrorCodeForbidden
		case upstream.KindAliasLimitReached:
			return apiErrorContract{
				Code:    apiErrorAliasLimit,
				Message: "Hide My Email 别名数量已达到上限，请清理不再使用的别名后重试。",
				Action:  "review_alias_limits",
			}, http.StatusConflict, auditlog.ErrorCodeConflict
		case upstream.KindAliasDailyLimit:
			return apiErrorContract{
				Code:    apiErrorAliasDailyLimit,
				Message: "今日别名创建额度已用尽，请明天再试或调整自动化额度。",
				Action:  "wait_for_daily_limit",
			}, http.StatusTooManyRequests, auditlog.ErrorCodeRateLimited
		case upstream.KindRateLimited:
			return apiErrorContract{
				Code:    apiErrorRateLimited,
				Message: "Apple 暂时限制了请求频率，请稍后手动重试。",
				Action:  "wait_before_retry",
			}, http.StatusTooManyRequests, auditlog.ErrorCodeRateLimited
		case upstream.KindDeviceTrustRequired:
			return apiErrorContract{
				Code:    apiErrorDeviceTrust,
				Message: "Apple 要求完成设备信任或双重验证，请在 Apple 设备或官网登录后重试。",
				Action:  "complete_device_trust",
			}, http.StatusForbidden, auditlog.ErrorCodeForbidden
		case upstream.KindValidationFailed:
			return apiErrorContract{
				Code:    apiErrorValidation,
				Message: "Apple 拒绝了请求参数，请检查输入后重试。",
				Action:  "fix_request",
			}, http.StatusBadRequest, auditlog.ErrorCodeValidationFailed
		case upstream.KindStateConflict:
			return apiErrorContract{
				Code:    apiErrorStateConflict,
				Message: "当前 Apple 资源状态与请求冲突，请刷新后再试。",
				Action:  "refresh_state",
			}, http.StatusConflict, auditlog.ErrorCodeConflict
		case upstream.KindServiceUnavailable:
			return apiErrorContract{
				Code:      apiErrorServiceUnavailable,
				Message:   "Apple 服务暂时不可用，请稍后重试。",
				Action:    "retry_later",
				Retryable: classified.Retryable,
			}, upstreamStatusOr(classified.Status, http.StatusBadGateway), auditlogForUpstream(classified)
		default:
			return apiErrorContract{
				Code:    apiErrorUpstreamRejected,
				Message: "Apple 拒绝了请求，暂时无法安全判断具体原因，请稍后重试。",
				Action:  "retry_later",
			}, http.StatusBadGateway, auditlog.ErrorCodeUpstreamRejected
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return apiErrorContract{
			Code:      apiErrorServiceUnavailable,
			Message:   "Apple 服务响应超时，请稍后重试。",
			Action:    "retry_later",
			Retryable: true,
		}, http.StatusGatewayTimeout, auditlog.ErrorCodeUpstreamTimeout
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return apiErrorContract{
			Code:      apiErrorServiceUnavailable,
			Message:   "Apple 服务响应超时，请稍后重试。",
			Action:    "retry_later",
			Retryable: true,
		}, http.StatusGatewayTimeout, auditlog.ErrorCodeUpstreamTimeout
	}
	return apiErrorContract{
		Code:    apiErrorUpstreamRejected,
		Message: "Apple 请求失败，暂时无法安全判断具体原因，请稍后重试。",
		Action:  "retry_later",
	}, http.StatusBadGateway, auditlog.ErrorCodeUpstreamRejected
}

func upstreamStatusOr(status, fallback int) int {
	if status >= 400 && status <= 599 {
		return status
	}
	return fallback
}

func auditlogForUpstream(err *upstream.Error) string {
	if err != nil && err.Status == http.StatusGatewayTimeout {
		return auditlog.ErrorCodeUpstreamTimeout
	}
	return auditlog.ErrorCodeServiceUnavailable
}

func credentialErrorContract(err error) (apiErrorContract, int, string) {
	switch {
	case errors.Is(err, hme.ErrInvalidCredentials):
		return apiErrorContract{
			Code:    apiErrorCredentialsInvalid,
			Message: "Apple ID 或密码错误，请检查后重新登录。",
			Action:  "restart_login",
		}, http.StatusUnauthorized, auditlog.ErrorCodeUnauthorized
	case errors.Is(err, hme.ErrInvalidOTP):
		return apiErrorContract{
			Code:    apiErrorOTPInvalid,
			Message: "双重认证验证码无效，请重新发起登录并输入最新验证码。",
			Action:  "restart_login",
		}, http.StatusUnauthorized, auditlog.ErrorCodeUnauthorized
	case errors.Is(err, hme.ErrPrivacyTermsRequired):
		return apiErrorContract{
			Code:    apiErrorPrivacyTerms,
			Message: "请先在 Apple 账户页面确认隐私条款，再重新登录。",
			Action:  "accept_privacy_terms",
		}, http.StatusForbidden, auditlog.ErrorCodeForbidden
	case errors.Is(err, hme.ErrLoginChallengeInvalid), errors.Is(err, account.ErrLoginSessionInvalid):
		return apiErrorContract{
			Code:    apiErrorLoginChallenge,
			Message: "登录验证已过期，请重新提交 Apple ID 密码。",
			Action:  "restart_login",
		}, http.StatusGone, auditlog.ErrorCodeConflict
	default:
		return classifyAPIError(err)
	}
}
