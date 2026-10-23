package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LoginRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	PasswordEncrypted string `json:"password_encrypted"`
	EncryptionKeyID   string `json:"encryption_key_id"`
}

func GetPasswordEncryptionKey(c *gin.Context) {
	if !common.PasswordLoginEncryptionEnabled {
		common.ApiSuccess(c, gin.H{"enabled": false})
		return
	}
	keyID, publicKey := common.PasswordEncryptionPublicKey()
	if keyID == "" || publicKey == "" {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return
	}
	common.ApiSuccess(c, gin.H{
		"enabled":    true,
		"kid":        keyID,
		"public_key": publicKey,
	})
}

func Login(c *gin.Context) {
	if !common.PasswordLoginEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordLoginDisabled)
		return
	}
	var loginRequest LoginRequest
	err := common.DecodeJson(c.Request.Body, &loginRequest)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	username := loginRequest.Username
	password := loginRequest.Password
	if common.PasswordLoginEncryptionEnabled {
		if loginRequest.PasswordEncrypted == "" || loginRequest.EncryptionKeyID == "" {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		password, err = common.DecryptPassword(loginRequest.PasswordEncrypted, loginRequest.EncryptionKeyID)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
			return
		}
	}
	if username == "" || password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user := model.User{
		Username: username,
		Password: password,
	}
	err = user.ValidateAndFill()
	if err != nil {
		switch {
		case errors.Is(err, model.ErrDatabase):
			common.SysLog(fmt.Sprintf("Login database error for user %s: %v", username, err))
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		case errors.Is(err, model.ErrUserEmptyCredentials):
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		default:
			common.ApiErrorI18n(c, i18n.MsgUserUsernameOrPasswordError)
		}
		return
	}

	setupLogin(&user, nil, c)
}

// loginMethodFromContext 根据请求路径推导登录方式，用于登录审计日志。
func loginMethodFromContext(c *gin.Context) string {
	if method := c.GetString("login_method"); method != "" {
		return method
	}
	switch c.FullPath() {
	case "/api/user/login":
		return "password"
	case "/api/user/login/2fa":
		return "2fa"
	case "/api/user/passkey/login/finish":
		return "passkey"
	case "/api/oauth/wechat":
		return "wechat"
	case "/api/oauth/telegram/login":
		return "telegram"
	case "/api/oauth/:provider":
		if provider := c.Param("provider"); provider != "" {
			return "oauth:" + provider
		}
		return "oauth"
	default:
		return "unknown"
	}
}

// recordLoginAudit 记录登录成功审计日志（对所有用户启用，仅记录成功，不记录失败）。
func recordLoginAudit(user *model.User, c *gin.Context) {
	method := loginMethodFromContext(c)
	ip := c.ClientIP()
	extra := model.AuditOther{
		LoginMethod: method,
		UserAgent:   c.Request.UserAgent(),
	}
	content := fmt.Sprintf("Logged in successfully via %s", method)
	params := map[string]any{
		"method": method,
	}
	if verifiedMethod := c.GetString("login_verification_method"); verifiedMethod != "" {
		params["verification_method"] = verifiedMethod
	}
	model.RecordLoginLog(user.Id, user.Role, user.Username, content, ip, "login", params, extra, c)
}

// setupLogin evaluates the shared login policy after primary authentication.
// Only a completed Passkey ceremony may go directly to session issuance. A
// pending legacy GitHub binding rewrite travels inside the challenge and is
// written only when the verification completes.
func setupLogin(user *model.User, migration *service.LegacyGitHubMigration, c *gin.Context) {
	challenge, err := service.StartLoginVerification(user, loginMethodFromContext(c), migration)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	if challenge != nil {
		setAuthNoStore(c)
		common.ApiSuccess(c, challenge)
		return
	}
	setupLoginAtAuthVersion(user, user.AuthVersion, c)
}

func setupLoginAtAuthVersion(user *model.User, expectedAuthVersion int64, c *gin.Context) {
	if user == nil || user.Id <= 0 || user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgAuthUserBanned)
		return
	}
	currentUser, err := model.GetSelfUserById(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var bundle *service.AuthBundle
	if expectedAuthVersion > 0 {
		bundle, err = service.CreateLoginSessionAtAuthVersion(
			user.Id,
			expectedAuthVersion,
			loginMethodFromContext(c),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	} else {
		bundle, err = service.CreateLoginSession(
			user.Id,
			loginMethodFromContext(c),
			c.ClientIP(),
			c.Request.UserAgent(),
		)
	}
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	writeLoginResponse(c, currentUser, bundle)
}

func writeLoginResponse(c *gin.Context, user *model.User, bundle *service.AuthBundle) {
	c.Set("login_method", bundle.Session.LoginMethod)
	model.UpdateUserLastLoginAt(user.Id)
	service.WriteRefreshCookie(c, bundle.RefreshToken)
	setAuthNoStore(c)
	recordLoginAudit(user, c)
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": gin.H{
			"access_token":      bundle.AccessToken,
			"token_type":        bundle.TokenType,
			"access_expires_at": bundle.AccessExpiresAt,
			"session":           bundle.Session,
			"user":              buildSelfUserData(user),
		},
	})
}

func Register(c *gin.Context) {
	if !common.RegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !common.PasswordRegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordRegisterDisabled)
		return
	}
	var user model.User
	err := common.DecodeJson(c.Request.Body, &user)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user.Username = strings.TrimSpace(user.Username)
	user.Email = model.NormalizeEmail(user.Email)
	if user.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if common.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			common.ApiErrorI18n(c, i18n.MsgUserEmailVerificationRequired)
			return
		}
		if !common.VerifyCodeWithKey(user.Email, user.VerificationCode, common.EmailVerificationPurpose) {
			common.ApiErrorI18n(c, i18n.MsgUserVerificationCodeError)
			return
		}
		if err := model.EnsureEmailAvailable(user.Email, 0); err != nil {
			if errors.Is(err, model.ErrEmailAlreadyTaken) {
				common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
				return
			}
			common.ApiErrorI18n(c, i18n.MsgDatabaseError)
			return
		}
	}
	emailForExistCheck := ""
	if common.EmailVerificationEnabled {
		emailForExistCheck = user.Email
	}
	exist, err := model.CheckUserExistOrDeleted(user.Username, emailForExistCheck)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		common.SysLog(fmt.Sprintf("CheckUserExistOrDeleted error: %v", err))
		return
	}
	if exist {
		common.ApiErrorI18n(c, i18n.MsgUserExists)
		return
	}
	affCode := user.AffCode // this code is the inviter's code, not the user's own code
	inviterId, _ := model.GetUserIdByAffCode(affCode)
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
		InviterId:   inviterId,
		Role:        common.RoleCommonUser, // 明确设置角色为普通用户
	}
	if common.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}
	if err := cleanUser.Insert(inviterId); err != nil {
		if errors.Is(err, model.ErrEmailAlreadyTaken) {
			common.ApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
			return
		}
		common.ApiError(c, err)
		return
	}

	// 获取插入后的用户ID
	var insertedUser model.User
	if err := model.DB.Where("username = ?", cleanUser.Username).First(&insertedUser).Error; err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterFailed)
		return
	}
	// 生成默认令牌
	if constant.GenerateDefaultToken {
		key, err := common.GenerateKey()
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgUserDefaultTokenFailed)
			common.SysLog("failed to generate token key: " + err.Error())
			return
		}
		// 生成默认令牌
		token := model.Token{
			UserId:             insertedUser.Id, // 使用插入后的用户ID
			Name:               cleanUser.Username + "的初始令牌",
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        -1,     // 永不过期
			RemainQuota:        500000, // 示例额度
			UnlimitedQuota:     true,
			ModelLimitsEnabled: false,
		}
		if setting.DefaultUseAutoGroup {
			token.Group = "auto"
		}
		if err := token.Insert(); err != nil {
			common.ApiErrorI18n(c, i18n.MsgCreateDefaultTokenErr)
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func GetAllUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	findUsers := model.FindManagedUsers
	if c.Query("hierarchy") == "true" {
		findUsers = model.FindManagedUserTreeRoots
	}
	users, total, err := findUsers(userManagementScope(c), "", "", nil, nil, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)

	common.ApiSuccess(c, pageInfo)
	return
}

func SearchUsers(c *gin.Context) {
	keyword, group, role, status := userListFilters(c)
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	findUsers := model.FindManagedUsers
	if c.Query("hierarchy") == "true" {
		findUsers = model.FindManagedUserTreeRoots
	}
	users, total, err := findUsers(userManagementScope(c), keyword, group, role, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetManagedUserChildren(c *gin.Context) {
	parentID, err := strconv.Atoi(c.Param("id"))
	if err != nil || parentID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	keyword, group, role, status := userListFilters(c)
	pageInfo := common.GetPageQuery(c)
	sortOptions := model.NewUserSortOptions(c.Query("sort_by"), c.Query("sort_order"))
	users, total, err := model.FindManagedUserChildren(userManagementScope(c), parentID, keyword, group, role, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), sortOptions)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}

func userListFilters(c *gin.Context) (string, string, *int, *int) {
	var role *int
	if roleStr := c.Query("role"); roleStr != "" {
		if parsed, err := strconv.Atoi(roleStr); err == nil {
			role = &parsed
		}
	}
	var status *int
	if statusStr := c.Query("status"); statusStr != "" {
		if parsed, err := strconv.Atoi(statusStr); err == nil {
			status = &parsed
		}
	}
	return c.Query("keyword"), c.Query("group"), role, status
}

func userManagementScope(c *gin.Context) model.UserManagementScope {
	return model.UserManagementScope{OperatorID: c.GetInt("id"), OperatorRole: c.GetInt("role")}
}

func writeUserManagementError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound) && common.IsDelegatedUserManager(c.GetInt("role")):
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
	case errors.Is(err, model.ErrUserManagementPermission):
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
	case errors.Is(err, model.ErrInheritedUserGroup):
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
	case errors.Is(err, model.ErrManagedUserBalanceRemaining):
		common.ApiErrorI18n(c, i18n.MsgManagedUserDeleteBalance)
	case errors.Is(err, model.ErrManagedUserHasChildren):
		common.ApiErrorI18n(c, i18n.MsgManagedUserHasChildren)
	case errors.Is(err, model.ErrQuotaTransferUnavailable):
		common.ApiErrorI18n(c, i18n.MsgQuotaTransferUnavailable)
	default:
		common.ApiError(c, err)
	}
}

func canManageTargetRole(myRole int, targetRole int) bool {
	return myRole == common.RoleRootUser || myRole > targetRole
}

func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	scope := userManagementScope(c)
	user, err := model.GetUserByIdInManagementScope(id, scope, false)
	if err != nil {
		writeUserManagementError(c, err)
		return
	}
	user.GroupInherited, err = model.IsThirdLevelUserDescendant(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.AdminPermissions = authz.Capabilities(user.Id, user.Role)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
	return
}

type TransferAffQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}

func TransferAffQuota(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tran := TransferAffQuotaRequest{}
	if err := c.ShouldBindJSON(&tran); err != nil {
		common.ApiError(c, err)
		return
	}
	err = user.TransferAffQuotaToQuota(tran.Quota)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserTransferFailed, map[string]any{"Error": err.Error()})
		return
	}
	common.ApiSuccessI18n(c, i18n.MsgUserTransferSuccess, nil)
}

func GetAffCode(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.AffCode == "" {
		user.AffCode = common.GetRandomString(4)
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AffCode,
	})
	return
}

func GetSelf(c *gin.Context) {
	id := c.GetInt("id")
	userRole := c.GetInt("role")
	user, err := model.GetSelfUserById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	responseData := buildSelfUserData(user)
	// The authenticated role is loaded from GetUserCache. It should equal the
	// row role, but use it for capabilities so GetSelf and login/refresh remain
	// consistent with the authorization decision made for this request.
	permissions := calculateUserPermissions(userRole)
	permissions["admin_permissions"] = authz.Capabilities(id, userRole)
	responseData["permissions"] = permissions

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    responseData,
	})
	return
}

// buildSelfUserData is the single safe dashboard-user DTO used by GetSelf,
// login and refresh. It intentionally excludes password, management PAT and
// administrator-only remarks.
func buildSelfUserData(user *model.User) map[string]any {
	userSetting := user.GetSetting()
	permissions := calculateUserPermissions(user.Role)
	permissions["admin_permissions"] = authz.Capabilities(user.Id, user.Role)
	return map[string]any{
		"id":                user.Id,
		"username":          user.Username,
		"display_name":      user.DisplayName,
		"has_password":      user.HasPassword,
		"role":              user.Role,
		"status":            user.Status,
		"email":             user.Email,
		"github_id":         user.GitHubId,
		"discord_id":        user.DiscordId,
		"oidc_id":           user.OidcId,
		"wechat_id":         user.WeChatId,
		"telegram_id":       user.TelegramId,
		"group":             user.Group,
		"user_groups":       user.UserGroups,
		"quota":             user.Quota,
		"used_quota":        user.UsedQuota,
		"request_count":     user.RequestCount,
		"aff_code":          user.AffCode,
		"aff_count":         user.AffCount,
		"aff_quota":         user.AffQuota,
		"aff_history_quota": user.AffHistoryQuota,
		"inviter_id":        user.InviterId,
		"linux_do_id":       user.LinuxDOId,
		"setting":           user.Setting,
		"stripe_customer":   user.StripeCustomer,
		"sidebar_modules":   userSetting.SidebarModules, // 正确提取sidebar_modules字段
		"permissions":       permissions,
	}
}

// 计算用户权限的辅助函数
func calculateUserPermissions(userRole int) map[string]any {
	permissions := map[string]any{}

	// 根据用户角色计算权限
	if userRole == common.RoleRootUser {
		// 超级管理员不需要边栏设置功能
		permissions["sidebar_settings"] = false
		permissions["sidebar_modules"] = map[string]any{}
	} else if common.IsDelegatedUserManager(userRole) {
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{"admin": map[string]any{"enabled": true, "user": true, "channel": false, "models": false, "redemption": false, "setting": false}}
	} else if userRole == common.RoleAdminUser {
		// 管理员可以设置边栏，但不包含系统设置功能
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": map[string]any{
				"setting": false, // 管理员不能访问系统设置
			},
		}
	} else {
		// 普通用户只能设置个人功能，不包含管理员区域
		permissions["sidebar_settings"] = true
		permissions["sidebar_modules"] = map[string]any{
			"admin": false, // 普通用户不能访问管理员区域
		}
	}

	return permissions
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfig(userRole int) string {
	defaultConfig := map[string]any{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]any{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]any{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]any{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if common.IsDelegatedUserManager(userRole) {
		defaultConfig["admin"] = map[string]any{"enabled": true, "user": true}
	} else if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]any{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]any{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := common.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

func GetUserModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		id = c.GetInt("id")
	}
	user, err := model.GetUserCache(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups := service.GetUserChannelGroupsForUser(user.Group, user.UserGroups)
	userUsableGroups := service.GetUserUsableGroupsForUser(user.Group, user.UserGroups)
	group := c.Query("group")
	var groupsToQuery []string
	switch {
	case group == "":
		for g := range groups {
			groupsToQuery = append(groupsToQuery, g)
		}
	case group == "auto":
		if _, ok := userUsableGroups[group]; ok {
			groupsToQuery = service.GetUserAutoGroupForUser(user.Group, user.UserGroups)
		}
	default:
		if _, ok := groups[group]; ok {
			groupsToQuery = []string{group}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    service.GetGroupsEnabledModels(groupsToQuery),
	})
}

func UpdateUser(c *gin.Context) {
	var updatedUser model.User
	err := common.DecodeJson(c.Request.Body, &updatedUser)
	if err != nil || updatedUser.Id == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	updatedUser.Username = strings.TrimSpace(updatedUser.Username)
	if updatedUser.Username == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.StructExcept(&updatedUser, "Password"); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	scope := userManagementScope(c)
	originUser, err := model.GetUserByIdInManagementScope(updatedUser.Id, scope, false)
	if err != nil {
		writeUserManagementError(c, err)
		return
	}
	if updatedUser.Role != common.RoleGuestUser && updatedUser.Role != originUser.Role {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	updatedUser.Role = originUser.Role
	if !scope.CanManage(originUser) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	if scope.OperatorRole >= common.RoleAdminUser && updatedUser.Password != "" {
		common.ApiErrorMsg(c, "管理员不能直接设置其他用户的密码，请发送密码重置链接")
		return
	}
	if updatedUser.Group != "" && updatedUser.Group != originUser.Group && !service.IsKnownUserGroup(updatedUser.Group) {
		common.ApiErrorMsg(c, "用户分组不存在")
		return
	}
	for _, group := range updatedUser.UserGroups {
		if !service.IsKnownUserGroup(group) {
			common.ApiErrorMsg(c, "用户分组不存在")
			return
		}
	}
	if common.IsDelegatedUserManager(scope.OperatorRole) {
		if updatedUser.CreatorID != 0 || updatedUser.AdminPermissions != nil || (updatedUser.Group != "" && updatedUser.Group != originUser.Group) {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		updatedUser.Group = originUser.Group
		if updatedUser.Password != "" {
			payload, err := common.Marshal(service.UserPasswordResetContext{TargetUserID: updatedUser.Id})
			if err != nil {
				common.ApiError(c, err)
				return
			}
			if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeUserPasswordReset, Context: payload}) == nil {
				return
			}
		}
	}
	updatePassword := updatedUser.Password != ""
	authzTouched := false
	var inheritedGroupChangedIDs []int
	var assignmentChangedIDs []int
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := scope.LockManagedUser(tx, updatedUser.Id, false)
		if err != nil {
			return err
		}
		if locked.Role != originUser.Role {
			return model.ErrUserManagementPermission
		}
		if updatedUser.Group == "" {
			updatedUser.Group = locked.Group
		}
		if scope.OperatorRole >= common.RoleAdminUser && updatedUser.Group != locked.Group {
			inherited, err := model.IsThirdLevelUserDescendantTx(tx, locked.Id)
			if err != nil {
				return err
			}
			if inherited {
				return model.ErrInheritedUserGroup
			}
		}
		if common.IsDelegatedUserManager(scope.OperatorRole) {
			updatedUser.Group = locked.Group
		}
		if updatedUser.UserGroups != nil {
			currentGroups, err := model.GetUserGroupAssignmentsTx(tx, locked.Id)
			if err != nil {
				return err
			}
			if scope.OperatorRole >= common.RoleAdminUser && locked.Role != common.RoleThirdLevelUser {
				inherited, err := model.IsThirdLevelUserDescendantTx(tx, locked.Id)
				if err != nil {
					return err
				}
				if inherited && !model.SameUserGroupAssignments(updatedUser.UserGroups, currentGroups) {
					return model.ErrInheritedUserGroup
				}
			}
			if common.IsDelegatedUserManager(scope.OperatorRole) {
				managerGroups, err := model.GetEffectiveUserGroupAssignmentsTx(tx, scope.OperatorID)
				if err != nil {
					return err
				}
				if !model.UserGroupAssignmentsSubset(updatedUser.UserGroups, managerGroups) {
					return model.ErrUserManagementPermission
				}
			}
			changed, err := model.SetUserGroupAssignmentsTx(tx, locked.Id, updatedUser.UserGroups)
			if err != nil {
				return err
			}
			if changed {
				assignmentChangedIDs, err = model.PruneDescendantUserGroupAssignmentsTx(tx, locked.Id, updatedUser.UserGroups)
				if err != nil {
					return err
				}
			}
		}
		if err := updatedUser.EditWithTx(tx, updatePassword); err != nil {
			return err
		}
		if locked.Role == common.RoleThirdLevelUser {
			inheritedGroupChangedIDs, err = model.UpdateThirdLevelUserDescendantGroupsTx(tx, locked.Id, updatedUser.Group)
			if err != nil {
				return err
			}
		}
		touched, err := updateAdminPermissionsForUserInTx(c, tx, updatedUser.Id, originUser.Role, updatedUser.AdminPermissions)
		authzTouched = touched
		return err
	}); err != nil {
		writeUserManagementError(c, err)
		return
	}
	if authzTouched {
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	var postCommitErrors []error
	if err := model.PublishUserAuthCache(updatedUser.Id); err != nil {
		postCommitErrors = append(postCommitErrors, err)
	}
	for _, userID := range inheritedGroupChangedIDs {
		if err := model.PublishUserAuthCache(userID); err != nil {
			postCommitErrors = append(postCommitErrors, err)
		}
	}
	for _, userID := range assignmentChangedIDs {
		if err := model.PublishUserAuthCache(userID); err != nil {
			postCommitErrors = append(postCommitErrors, err)
		}
	}
	if updatedUser.AuthVersion > originUser.AuthVersion {
		if _, err := model.RevokeAllUserSessions(updatedUser.Id, "admin_user_update"); err != nil {
			postCommitErrors = append(postCommitErrors, err)
		}
	}
	for _, userID := range inheritedGroupChangedIDs {
		if _, err := model.RevokeAllUserSessions(userID, "admin_inherited_group_update"); err != nil {
			postCommitErrors = append(postCommitErrors, err)
		}
	}
	for _, userID := range assignmentChangedIDs {
		if _, err := model.RevokeAllUserSessions(userID, "user_group_assignment_update"); err != nil {
			postCommitErrors = append(postCommitErrors, err)
		}
	}
	if len(postCommitErrors) > 0 {
		common.ApiError(c, errors.Join(postCommitErrors...))
		return
	}
	recordManageAuditFor(c, updatedUser.Id, "user.update", map[string]any{
		"username": originUser.Username,
		"id":       updatedUser.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AdminClearUserBinding(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	bindingType := strings.ToLower(strings.TrimSpace(c.Param("binding_type")))
	if bindingType == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return
	}

	if err := user.ClearBinding(bindingType); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, user.Id, "user.binding_clear", map[string]any{
		"bindingType": bindingType,
		"username":    user.Username,
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "success",
	})
}

func UpdateSelf(c *gin.Context) {
	var requestData map[string]any
	if err := common.DecodeJson(c.Request.Body, &requestData); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	passwordRequested := false
	if value, exists := requestData["password"]; exists && value != nil {
		password, isString := value.(string)
		passwordRequested = !isString || password != ""
	}
	succeeded, notificationFailed := false, false
	if passwordRequested {
		defer func() {
			recordUserSecurityAudit(c, c.GetInt("id"), "user.password_change", map[string]any{"success": succeeded, "notification_failed": notificationFailed})
		}()
	}
	// 检查是否是用户设置更新请求 (sidebar_modules 或 language)
	if sidebarModules, sidebarExists := requestData["sidebar_modules"]; sidebarExists && !passwordRequested {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新sidebar_modules字段
		if sidebarModulesStr, ok := sidebarModules.(string); ok {
			currentSetting.SidebarModules = sidebarModulesStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 检查是否是语言偏好更新请求
	if language, langExists := requestData["language"]; langExists && !passwordRequested {
		userId := c.GetInt("id")
		user, err := model.GetUserById(userId, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 获取当前用户设置
		currentSetting := user.GetSetting()

		// 更新language字段
		if langStr, ok := language.(string); ok {
			currentSetting.Language = langStr
		}

		if err := model.UpdateUserSetting(user.Id, currentSetting); err != nil {
			common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
			return
		}

		common.ApiSuccessI18n(c, i18n.MsgUpdateSuccess, nil)
		return
	}

	// 原有的用户信息更新逻辑
	var user model.User
	requestDataBytes, err := common.Marshal(requestData)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err = common.Unmarshal(requestDataBytes, &user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	if err := common.Validate.StructExcept(&user, "Password"); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidInput)
		return
	}

	cleanUser := model.User{
		Id:          c.GetInt("id"),
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if user.Password != "" {
		identity, ok := middleware.GetSessionAuthIdentity(c)
		if !ok {
			writeSecurityOperationError(c, service.ErrAuthTokenInvalid)
			return
		}
		current, err := model.GetUserById(identity.UserID, true)
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		firstPassword := current.Password == ""
		scope := service.VerificationScopePasswordChange
		if firstPassword {
			scope = service.VerificationScopePasswordSet
		}
		if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: scope}) == nil {
			return
		}
		cleanUser.OriginalPassword = user.OriginalPassword
		if err := model.ChangeUserPassword(identity, &cleanUser, firstPassword); err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		succeeded = true
		notificationFailed = service.NotifyAccountSecurityChange(current.Email, "Password updated") != nil
		if err := model.PublishUserAuthCache(cleanUser.Id); err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		bundle, err := service.AdvanceCurrentSessionToUserVersion(identity, "password_changed")
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"access_token":         bundle.AccessToken,
				"token_type":           bundle.TokenType,
				"access_expires_at":    bundle.AccessExpiresAt,
				"session":              bundle.Session,
				"has_password":         true,
				"notification_warning": notificationFailed,
			},
		})
		return
	}
	if err := cleanUser.Update(false); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
	return
}

func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	scope := userManagementScope(c)
	originUser, err := model.GetUserByIdInManagementScope(id, scope, false)
	if err != nil {
		writeUserManagementError(c, err)
		return
	}
	if originUser.Role == common.RoleRootUser || !scope.CanManage(originUser) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	if common.IsDelegatedUserManager(scope.OperatorRole) && common.RedisEnabled {
		if _, err := model.GetUserCache(id); err != nil {
			writeUserManagementError(c, model.ErrQuotaTransferUnavailable)
			return
		}
	}
	err = originUser.HardDelete(scope)
	if err != nil {
		writeUserManagementError(c, err)
		return
	}
	recordManageAuditFor(c, originUser.Id, "user.delete", map[string]any{
		"username": originUser.Username,
		"id":       originUser.Id,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func DeleteSelf(c *gin.Context) {
	setAuthNoStore(c)
	succeeded := false
	defer func() {
		recordUserSecurityAudit(c, c.GetInt("id"), "user.account_delete", map[string]any{"success": succeeded})
	}()
	if middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeAccountDelete}) == nil {
		return
	}
	identity, _ := middleware.GetSessionAuthIdentity(c)
	if err := model.DeleteUserForSession(identity); err != nil {
		if errors.Is(err, model.ErrCannotDeleteRootUser) {
			common.ApiErrorI18n(c, i18n.MsgUserCannotDeleteRootUser)
			return
		}
		writeSecurityOperationError(c, err)
		return
	}
	succeeded = true
	service.ClearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{},
	})
}

func CreateUser(c *gin.Context) {
	var user model.User
	err := common.DecodeJson(c.Request.Body, &user)
	user.Username = strings.TrimSpace(user.Username)
	if err != nil || user.Username == "" || user.Password == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUserInputInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	if user.Role == common.RoleGuestUser {
		user.Role = common.RoleCommonUser
	}
	myRole := c.GetInt("role")
	if !common.IsValidateRole(user.Role) || user.Role == common.RoleGuestUser || user.Role >= myRole {
		common.ApiErrorI18n(c, i18n.MsgUserCannotCreateHigherLevel)
		return
	}
	if user.Group != "" && !service.IsKnownUserGroup(user.Group) {
		common.ApiErrorMsg(c, "用户分组不存在")
		return
	}
	for _, group := range user.UserGroups {
		if !service.IsKnownUserGroup(group) {
			common.ApiErrorMsg(c, "用户分组不存在")
			return
		}
	}
	canCreateManagedRole := user.Role == common.RoleCommonUser || (myRole == common.RoleThirdLevelUser && user.Role == common.RoleUserManager)
	if common.IsDelegatedUserManager(myRole) && (!canCreateManagedRole || user.CreatorID != 0 || user.AdminPermissions != nil || user.Group != "" || user.Quota != 0) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	// Even for admin users, we cannot fully trust them!
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
		Role:        user.Role, // 保持管理员设置的角色
		Group:       user.Group,
		UserGroups:  user.UserGroups,
	}
	authzTouched := false
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := userManagementScope(c).InsertUser(tx, &cleanUser); err != nil {
			return err
		}
		touched, err := updateAdminPermissionsForUserInTx(c, tx, cleanUser.Id, cleanUser.Role, user.AdminPermissions)
		authzTouched = touched
		return err
	}); err != nil {
		writeUserManagementError(c, err)
		return
	}
	if authzTouched {
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	cleanUser.FinishInsert(0)

	recordManageAuditFor(c, cleanUser.Id, "user.create", map[string]any{
		"username": cleanUser.Username,
		"role":     cleanUser.Role,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func updateAdminPermissionsForUserInTx(c *gin.Context, tx *gorm.DB, userID int, userRole int, permissions map[string]map[string]bool) (bool, error) {
	if permissions == nil {
		if userRole < common.RoleAdminUser && c.GetInt("role") == common.RoleRootUser {
			return true, authz.ClearUserAuthorizationInTx(tx, userID)
		}
		return false, nil
	}
	if c.GetInt("role") != common.RoleRootUser {
		return false, fmt.Errorf("only root can update admin permissions")
	}
	if userRole < common.RoleAdminUser {
		return true, authz.ClearUserAuthorizationInTx(tx, userID)
	}
	return true, authz.SetUserPermissionsInTx(tx, userID, permissions)
}

type ManageRequest struct {
	Id     int    `json:"id"`
	Action string `json:"action"`
	Value  int    `json:"value"`
	Mode   string `json:"mode"`
}

// ManageUser applies account actions within the operator's management scope.
func ManageUser(c *gin.Context) {
	var req ManageRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.Action == "add_quota" {
		manageUserQuota(c, req)
		return
	}
	scope := userManagementScope(c)
	if common.IsDelegatedUserManager(scope.OperatorRole) && req.Action != "enable" && req.Action != "disable" && req.Action != "delete" && req.Action != "promote_manager" && req.Action != "demote" {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return
	}
	if req.Action == "delete" {
		user, err := model.GetUserByIdInManagementScope(req.Id, scope, false)
		if err != nil {
			writeUserManagementError(c, err)
			return
		}
		if !scope.CanManage(user) || user.Role == common.RoleRootUser {
			common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
			return
		}
		if common.IsDelegatedUserManager(scope.OperatorRole) && common.RedisEnabled {
			if _, err := model.GetUserCache(user.Id); err != nil {
				writeUserManagementError(c, model.ErrQuotaTransferUnavailable)
				return
			}
		}
		if err := user.Delete(scope); err != nil {
			writeUserManagementError(c, err)
			return
		}
		if err := model.InvalidateUserTokensCache(user.Id); err != nil {
			common.SysError(fmt.Sprintf("failed to invalidate tokens cache for user %d: %v", user.Id, err))
		}
		recordManageAuditFor(c, user.Id, "user.manage", map[string]any{"action": req.Action, "username": user.Username, "id": user.Id})
		common.ApiSuccess(c, nil)
		return
	}
	var user *model.User
	var previousAuthVersion int64
	authzTouched := false
	clearCreator := false
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		user, err = scope.LockManagedUser(tx, req.Id, true)
		if err != nil {
			return err
		}
		previousAuthVersion = user.AuthVersion
		switch req.Action {
		case "disable":
			if user.Role == common.RoleRootUser {
				return model.ErrUserManagementPermission
			}
			user.Status = common.UserStatusDisabled
		case "enable":
			user.Status = common.UserStatusEnabled
		case "promote":
			if scope.OperatorRole != common.RoleRootUser || user.Role >= common.RoleAdminUser {
				return model.ErrUserManagementPermission
			}
			user.Role = common.RoleAdminUser
		case "promote_manager":
			if scope.OperatorRole < common.RoleThirdLevelUser || user.Role != common.RoleCommonUser {
				return model.ErrUserManagementPermission
			}
			user.Role = common.RoleUserManager
		case "promote_third_level":
			if scope.OperatorRole < common.RoleAdminUser || user.Role != common.RoleCommonUser {
				return model.ErrUserManagementPermission
			}
			user.Role = common.RoleThirdLevelUser
			user.CreatorID = 0
			clearCreator = true
		case "demote":
			if user.Role == common.RoleRootUser || user.Role == common.RoleCommonUser {
				return model.ErrUserManagementPermission
			}
			if common.IsDelegatedUserManager(user.Role) {
				if err := model.CheckManagedUserHasActiveChildren(tx, user.Id); err != nil {
					return err
				}
			}
			user.Role = common.RoleCommonUser
		default:
			return errors.New(common.TranslateMessage(c, i18n.MsgInvalidParams))
		}
		if err := user.UpdateWithTx(tx, false); err != nil {
			return err
		}
		if clearCreator {
			if err := tx.Model(&model.User{}).Where("id = ?", user.Id).Update("creator_id", 0).Error; err != nil {
				return err
			}
			user.CreatorID = 0
		}
		if req.Action == "demote" || req.Action == "promote_manager" || req.Action == "promote_third_level" {
			authzTouched = true
			return authz.ClearUserAuthorizationInTx(tx, user.Id)
		}
		return nil
	})
	if err != nil {
		writeUserManagementError(c, err)
		return
	}
	if authzTouched {
		if err := authz.ReloadPolicy(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := model.PublishUserAuthCache(user.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	if user.AuthVersion > previousAuthVersion {
		reason := "user_security_changed"
		if req.Action == "demote" {
			reason = "admin_demote"
		}
		if _, err := model.RevokeAllUserSessions(user.Id, reason); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate tokens cache for user %d: %v", user.Id, err))
	}
	recordManageAuditFor(c, user.Id, "user.manage", map[string]any{"action": req.Action, "username": user.Username, "id": user.Id})
	common.ApiSuccess(c, model.User{Role: user.Role, Status: user.Status})
}

type topUpRequest struct {
	Key string `json:"key"`
}

var topUpLocks sync.Map
var topUpCreateLock sync.Mutex

type topUpTryLock struct {
	ch chan struct{}
}

func newTopUpTryLock() *topUpTryLock {
	return &topUpTryLock{ch: make(chan struct{}, 1)}
}

func (l *topUpTryLock) TryLock() bool {
	select {
	case l.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *topUpTryLock) Unlock() {
	select {
	case <-l.ch:
	default:
	}
}

func getTopUpLock(userID int) *topUpTryLock {
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	topUpCreateLock.Lock()
	defer topUpCreateLock.Unlock()
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	l := newTopUpTryLock()
	topUpLocks.Store(userID, l)
	return l
}

func TopUp(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	id := c.GetInt("id")
	lock := getTopUpLock(id)
	if !lock.TryLock() {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer lock.Unlock()
	req := topUpRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := model.Redeem(req.Key, id)
	if err != nil {
		// 不向用户暴露兑换失败的细分原因，避免攻击者根据错误类型判断兑换码状态。
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		logger.LogError(c, fmt.Sprintf("failed to redeem key %s for user %d: %s", req.Key, id, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    quota,
	})
}

type UpdateUserSettingRequest struct {
	QuotaWarningType                 string  `json:"notify_type"`
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold"`
	WebhookUrl                       string  `json:"webhook_url,omitempty"`
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`
	NotificationEmail                string  `json:"notification_email,omitempty"`
	BarkUrl                          string  `json:"bark_url,omitempty"`
	GotifyUrl                        string  `json:"gotify_url,omitempty"`
	GotifyToken                      string  `json:"gotify_token,omitempty"`
	GotifyPriority                   int     `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool   `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool    `json:"accept_unset_model_ratio_model"`
	RecordIpLog                      bool    `json:"record_ip_log"`
}

func UpdateUserSetting(c *gin.Context) {
	var req UpdateUserSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	// 验证预警类型
	if req.QuotaWarningType != dto.NotifyTypeEmail && req.QuotaWarningType != dto.NotifyTypeWebhook && req.QuotaWarningType != dto.NotifyTypeBark && req.QuotaWarningType != dto.NotifyTypeGotify {
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
		return
	}

	// 验证预警阈值
	if req.QuotaWarningThreshold <= 0 {
		common.ApiErrorI18n(c, i18n.MsgQuotaThresholdGtZero)
		return
	}

	// 如果是webhook类型,验证webhook地址
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		if req.WebhookUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.WebhookUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
			return
		}
	}

	// 如果是邮件类型，验证邮箱地址
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		// 验证邮箱格式
		if !strings.Contains(req.NotificationEmail, "@") {
			common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
			return
		}
	}

	// 如果是Bark类型，验证Bark URL
	if req.QuotaWarningType == dto.NotifyTypeBark {
		if req.BarkUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.BarkUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.BarkUrl, "https://") && !strings.HasPrefix(req.BarkUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	// 如果是Gotify类型，验证Gotify URL和Token
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		if req.GotifyUrl == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
			return
		}
		if req.GotifyToken == "" {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
			return
		}
		// 验证URL格式
		if _, err := url.ParseRequestURI(req.GotifyUrl); err != nil {
			common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
			return
		}
		// 检查是否是HTTP或HTTPS
		if !strings.HasPrefix(req.GotifyUrl, "https://") && !strings.HasPrefix(req.GotifyUrl, "http://") {
			common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
			return
		}
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	existingSettings := user.GetSetting()
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if user.Role >= common.RoleAdminUser && req.UpstreamModelUpdateNotifyEnabled != nil {
		upstreamModelUpdateNotifyEnabled = *req.UpstreamModelUpdateNotifyEnabled
	}

	// 构建设置
	settings := dto.UserSetting{
		NotifyType:                       req.QuotaWarningType,
		QuotaWarningThreshold:            req.QuotaWarningThreshold,
		UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            req.AcceptUnsetModelRatioModel,
		RecordIpLog:                      req.RecordIpLog,
	}

	// 如果是webhook类型,添加webhook相关设置
	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		settings.WebhookUrl = req.WebhookUrl
		if req.WebhookSecret != "" {
			settings.WebhookSecret = req.WebhookSecret
		}
	}

	// 如果提供了通知邮箱，添加到设置中
	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		settings.NotificationEmail = req.NotificationEmail
	}

	// 如果是Bark类型，添加Bark URL到设置中
	if req.QuotaWarningType == dto.NotifyTypeBark {
		settings.BarkUrl = req.BarkUrl
	}

	// 如果是Gotify类型，添加Gotify配置到设置中
	if req.QuotaWarningType == dto.NotifyTypeGotify {
		settings.GotifyUrl = req.GotifyUrl
		settings.GotifyToken = req.GotifyToken
		// Gotify优先级范围0-10，超出范围则使用默认值5
		if req.GotifyPriority < 0 || req.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = req.GotifyPriority
		}
	}

	// 更新用户设置
	if err := model.UpdateUserSetting(user.Id, settings); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}

	common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
}
