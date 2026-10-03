// Package httpserver 适配 HTTP 请求、认证授权和业务接口，统一响应与审计信息
package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"shiftory-server/internal/auth"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/storage"
)

const userIDKey = "authenticatedUserID"
const requestIDKey = "requestID"
const errorCodeKey = "errorCode"

// Dependencies 提供 HTTP 层依赖，连接池和外部任务生命周期由调用方管理
type Dependencies struct {
	DB           *sql.DB
	Config       config.Config
	Tokens       *auth.TokenManager
	Store        storage.Store
	ImportWakeup func()
	ImportCancel func(context.Context, uint64, uint64)
	QueueReady   func(context.Context) bool
	AIMetrics    func(context.Context) (string, error)
	Logger       *slog.Logger
}

// server 聚合 HTTP 处理所需的服务与适配器
type server struct {
	db           *sql.DB
	config       config.Config
	tokens       *auth.TokenManager
	authService  *auth.Service
	store        storage.Store
	importWakeup func()
	importCancel func(context.Context, uint64, uint64)
	queueReady   func(context.Context) bool
	aiMetrics    func(context.Context) (string, error)
	logger       *slog.Logger
}

// New 组装认证、业务路由与静态页面处理器，不启动网络监听
func New(deps Dependencies) (http.Handler, error) {
	if deps.DB == nil || deps.Tokens == nil {
		return nil, errors.New("database and token manager are required")
	}
	store := deps.Store
	if store == nil {
		var err error
		store, err = storage.NewLocal(deps.Config.UploadDir)
		if err != nil {
			return nil, err
		}
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &server{
		db: deps.DB, config: deps.Config, tokens: deps.Tokens,
		authService: auth.NewService(auth.NewPostgresRepository(deps.DB), auth.NewPasswordHasher(auth.DefaultPasswordParams()), deps.Tokens),
		store:       store, importWakeup: deps.ImportWakeup, importCancel: deps.ImportCancel, queueReady: deps.QueueReady, aiMetrics: deps.AIMetrics, logger: logger,
	}
	// 全局中间件先负责恢复、请求跟踪和跨域，业务路由再按认证要求分组
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.Recovery(), s.requestContext(), s.cors())
	router.GET("/health", func(c *gin.Context) { success(c, http.StatusOK, gin.H{"status": "ok"}) })

	router.GET("/health/ai", func(c *gin.Context) {
		ready := s.queueReady != nil && s.queueReady(c.Request.Context())
		success(c, 200, gin.H{"enabled": s.config.AIEnabled, "queueReady": ready})
	})
	router.GET("/metrics/ai", func(c *gin.Context) {
		if s.aiMetrics == nil {
			c.String(200, "shiftory_ai_enabled 0\n")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		body, e := s.aiMetrics(ctx)
		if e != nil {
			c.String(503, "AI metrics unavailable\n")
			return
		}
		c.Data(200, "text/plain; version=0.0.4; charset=utf-8", []byte(body))
	})
	v1 := router.Group("/api/v1")
	authRoutes := v1.Group("/auth")
	authRoutes.POST("/register", s.register)
	authRoutes.POST("/login", s.login)
	authRoutes.POST("/refresh", s.refresh)
	authRoutes.POST("/logout", s.logout)
	authRoutes.GET("/me", s.authenticate(), s.me)
	authRoutes.PATCH("/profile", s.authenticate(), s.updateProfile)
	authRoutes.PUT("/password", s.authenticate(), s.changePassword)

	// 这些业务入口统一验证账号，工作区角色仍在每个处理函数中重新查询
	protected := v1.Group("")
	protected.Use(s.authenticate())
	protected.POST("/invitations/accept", s.acceptInvitation)
	protected.GET("/invitations/mine", s.listMyInvitations)
	protected.GET("/preferences", s.getPreferences)
	protected.PUT("/preferences", s.updatePreferences)
	protected.GET("/me/schedules", s.listSchedules)
	protected.PUT("/me/schedules/:date", s.upsertSchedule)
	protected.POST("/me/schedules/batch", s.batchSchedules)
	protected.GET("/me/schedules/:date/history", s.scheduleHistory)
	protected.GET("/workspaces", s.listWorkspaces)
	protected.POST("/workspaces", s.createWorkspace)
	protected.PATCH("/workspaces/:workspaceId", s.updateWorkspace)
	protected.POST("/workspaces/:workspaceId/transfer", s.transferWorkspace)
	protected.DELETE("/workspaces/:workspaceId", s.deleteWorkspace)
	protected.GET("/workspaces/:workspaceId/members", s.listMembers)
	protected.PATCH("/workspaces/:workspaceId/members/:userId", s.updateMember)
	protected.POST("/workspaces/:workspaceId/invitations", s.createInvitation)
	protected.GET("/workspaces/:workspaceId/invitations", s.listInvitations)
	protected.DELETE("/workspaces/:workspaceId/invitations/:invitationId", s.revokeInvitation)
	protected.GET("/workspaces/:workspaceId/shifts", s.listShifts)
	protected.POST("/workspaces/:workspaceId/shifts", s.createShift)
	protected.PUT("/workspaces/:workspaceId/shifts/:shiftId", s.updateShift)
	protected.PUT("/workspaces/:workspaceId/schedules/:userId/:date", s.upsertSchedule)
	protected.GET("/workspaces/:workspaceId/schedules/:userId", s.listSchedules)
	protected.POST("/workspaces/:workspaceId/schedules/batch", s.batchSchedules)
	protected.GET("/workspaces/:workspaceId/schedules/:userId/:date/history", s.scheduleHistory)
	protected.GET("/workspaces/:workspaceId/calendar", s.getCalendar)
	protected.GET("/workspaces/:workspaceId/overview", s.overview)
	protected.GET("/workspaces/:workspaceId/audits", s.listAudits)
	protected.POST("/workspaces/:workspaceId/imports", s.createImport)
	protected.POST("/workspaces/:workspaceId/imports/image", s.createImageImport)
	protected.POST("/workspaces/:workspaceId/imports/text", s.createTextImport)
	protected.POST("/workspaces/:workspaceId/imports/:importId/retry", s.retryImport)
	protected.POST("/workspaces/:workspaceId/imports/:importId/refresh-preview", s.refreshPreview)
	protected.GET("/workspaces/:workspaceId/imports/:importId/attempts", s.attempts)
	protected.GET("/workspaces/:workspaceId/imports/:importId/attempts/:attemptId/calls", s.calls)
	protected.GET("/workspaces/:workspaceId/imports/:importId/attempts/:attemptId/calls/:callId/raw", s.callRaw)

	protected.GET("/workspaces/:workspaceId/imports/template.xlsx", s.downloadImportTemplate)
	protected.GET("/workspaces/:workspaceId/imports", s.listImports)
	protected.GET("/workspaces/:workspaceId/imports/:importId", s.getImport)
	protected.GET("/workspaces/:workspaceId/imports/:importId/file", s.downloadImportFile)
	protected.PUT("/workspaces/:workspaceId/imports/:importId/decisions", s.updateImportDecisions)
	protected.PUT("/workspaces/:workspaceId/imports/:importId/items/:itemId", s.correctImportItem)
	protected.POST("/workspaces/:workspaceId/imports/:importId/cancel", s.cancelImport)
	protected.POST("/workspaces/:workspaceId/imports/:importId/commit", s.commitImport)
	protected.POST("/workspaces/:workspaceId/imports/:importId/rollback", s.rollbackImport)
	s.configureSPA(router)
	return router, nil
}

// requestContext 生成请求标识、限制请求体大小并记录请求完成日志
func (s *server) requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set(requestIDKey, requestID)
		c.Header("X-Request-ID", requestID)
		started := time.Now()
		// 普通 JSON 请求限制为 1 MiB，文件上传预留协议开销后限制为 12 MiB
		if !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		} else {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12<<20)
		}
		c.Next()
		attributes := []any{
			"request_id", requestID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
		}
		if code, exists := c.Get(errorCodeKey); exists {
			attributes = append(attributes, "error_code", code)
		}
		s.logger.InfoContext(c.Request.Context(), "http request", attributes...)
	}
}

// configureSPA 在构建入口存在时提供静态文件与页面路由回退，API 未命中仍返回错误
func (s *server) configureSPA(router *gin.Engine) {
	index := filepath.Join(s.config.WebDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return
	}
	assets := http.FileServer(http.Dir(s.config.WebDir))
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			failure(c, http.StatusNotFound, "NOT_FOUND", "接口不存在", nil)
			return
		}
		requested := filepath.Join(s.config.WebDir, filepath.FromSlash(strings.TrimPrefix(c.Request.URL.Path, "/")))
		if info, err := os.Stat(requested); err == nil && !info.IsDir() {
			assets.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.File(index)
	})
}

// cors 仅为配置来源设置凭据跨域响应头，并结束 OPTIONS 预检请求
func (s *server) cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && origin == s.config.PublicOrigin {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, Idempotency-Key")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID, Content-Disposition")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// authenticate 验证访问令牌后查询当前账号状态，将用户 ID 写入请求上下文
func (s *server) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			failure(c, http.StatusUnauthorized, "UNAUTHORIZED", "缺少访问令牌", nil)
			c.Abort()
			return
		}
		claims, err := s.tokens.ParseAccess(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			failure(c, http.StatusUnauthorized, "INVALID_TOKEN", "访问令牌无效或已过期", nil)
			c.Abort()
			return
		}
		// 令牌有效不代表账号仍可用，每次请求都从数据库核对当前账号状态
		user, err := s.authService.CurrentUser(c.Request.Context(), claims.UserID)
		if err != nil {
			failure(c, http.StatusUnauthorized, "USER_UNAVAILABLE", "账号不可用", nil)
			c.Abort()
			return
		}
		c.Set(userIDKey, user.ID)
		c.Next()
	}
}

// registerRequest 定义注册接口的必填 JSON 字段
type registerRequest struct {
	Username    string `json:"username" binding:"required"`
	Email       string `json:"email" binding:"required"`
	DisplayName string `json:"displayName" binding:"required"`
	Password    string `json:"password" binding:"required"`
}

// register 接收注册资料并返回公开用户信息，账号冲突返回 HTTP 409
func (s *server) register(c *gin.Context) {
	var request registerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "注册信息不完整", nil)
		return
	}
	user, err := s.authService.Register(c.Request.Context(), auth.RegisterInput{
		Username: request.Username, Email: request.Email, DisplayName: request.DisplayName, Password: request.Password,
	})
	if err != nil {
		status, code := http.StatusBadRequest, "INVALID_REGISTRATION"
		if errors.Is(err, auth.ErrUserExists) {
			status, code = http.StatusConflict, "USER_EXISTS"
		}
		failure(c, status, code, err.Error(), nil)
		return
	}
	success(c, http.StatusCreated, userResponse(user))
}

// loginRequest 定义登录接口的账号标识和密码字段
type loginRequest struct {
	Login    string `json:"login" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// login 验证凭据并交付访问令牌、刷新 Cookie 与 CSRF Cookie
func (s *server) login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_REQUEST", "登录信息不完整", nil)
		return
	}
	pair, user, err := s.authService.Login(c.Request.Context(), auth.LoginInput{Login: request.Login, Password: request.Password}, clientInfo(c))
	if err != nil {
		s.recordSecurityAudit(c, nil, "LOGIN_FAILED", "credential", strings.TrimSpace(request.Login), nil)
		failure(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名、邮箱或密码错误", nil)
		return
	}
	s.setRefreshCookie(c, pair.RefreshToken, pair.RefreshExpiry)
	if err := s.setCSRFCookie(c); err != nil {
		failure(c, http.StatusInternalServerError, "CSRF_TOKEN_ERROR", "无法创建安全会话", nil)
		return
	}
	s.recordSecurityAudit(c, user.ID, "LOGIN_SUCCEEDED", "user", user.ID, nil)
	success(c, http.StatusOK, gin.H{"accessToken": pair.AccessToken, "accessExpiresAt": pair.AccessExpiry, "user": userResponse(user)})
}

// refresh 通过来源和 CSRF 校验后轮换刷新 Cookie，拒绝时清除刷新 Cookie
func (s *server) refresh(c *gin.Context) {
	if !s.validOrigin(c) {
		failure(c, http.StatusForbidden, "INVALID_ORIGIN", "请求来源不受信任", nil)
		return
	}
	if !s.validCSRF(c) {
		failure(c, http.StatusForbidden, "INVALID_CSRF", "CSRF 校验失败", nil)
		return
	}
	raw, err := c.Cookie("shiftory_refresh")
	if err != nil {
		failure(c, http.StatusUnauthorized, "REFRESH_REQUIRED", "缺少刷新令牌", nil)
		return
	}
	pair, err := s.authService.Refresh(c.Request.Context(), raw, clientInfo(c))
	if err != nil {
		claims, _ := s.tokens.ParseRefresh(raw)
		s.recordSecurityAudit(c, nullableActor(claims.UserID), "REFRESH_FAILED", "refresh_token", claims.ID, nil)
		s.clearRefreshCookie(c)
		failure(c, http.StatusUnauthorized, "REFRESH_REJECTED", err.Error(), nil)
		return
	}
	s.setRefreshCookie(c, pair.RefreshToken, pair.RefreshExpiry)
	claims, _ := s.tokens.ParseAccess(pair.AccessToken)
	s.recordSecurityAudit(c, nullableActor(claims.UserID), "REFRESH_SUCCEEDED", "user", claims.UserID, nil)
	success(c, http.StatusOK, gin.H{"accessToken": pair.AccessToken, "accessExpiresAt": pair.AccessExpiry})
}

// logout 尝试撤销刷新令牌族并清除会话 Cookie
func (s *server) logout(c *gin.Context) {
	if !s.validOrigin(c) {
		failure(c, http.StatusForbidden, "INVALID_ORIGIN", "请求来源不受信任", nil)
		return
	}
	if !s.validCSRF(c) {
		failure(c, http.StatusForbidden, "INVALID_CSRF", "CSRF 校验失败", nil)
		return
	}
	if raw, err := c.Cookie("shiftory_refresh"); err == nil {
		claims, _ := s.tokens.ParseRefresh(raw)
		_ = s.authService.Logout(c.Request.Context(), raw)
		s.recordSecurityAudit(c, nullableActor(claims.UserID), "LOGOUT", "user", claims.UserID, nil)
	}
	s.clearRefreshCookie(c)
	s.clearCSRFCookie(c)
	success(c, http.StatusOK, gin.H{"loggedOut": true})
}

// me 读取当前有效用户的公开资料
func (s *server) me(c *gin.Context) {
	user, err := s.authService.CurrentUser(c.Request.Context(), currentUserID(c))
	if err != nil {
		failure(c, http.StatusUnauthorized, "USER_UNAVAILABLE", "账号不可用", nil)
		return
	}
	success(c, http.StatusOK, userResponse(user))
}

// updateProfile 接收展示名与头像地址并交由认证服务校验保存
func (s *server) updateProfile(c *gin.Context) {
	var request struct {
		DisplayName string `json:"displayName" binding:"required"`
		AvatarURL   string `json:"avatarUrl"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_PROFILE", "个人资料无效", nil)
		return
	}
	user, err := s.authService.UpdateProfile(c.Request.Context(), currentUserID(c), auth.ProfileInput{DisplayName: request.DisplayName, AvatarURL: request.AvatarURL})
	if err != nil {
		failure(c, http.StatusBadRequest, "INVALID_PROFILE", err.Error(), nil)
		return
	}
	success(c, http.StatusOK, userResponse(user))
}

// changePassword 验证并更新密码，成功后清除 Cookie 并记录安全审计
func (s *server) changePassword(c *gin.Context) {
	var request struct {
		CurrentPassword string `json:"currentPassword" binding:"required"`
		NewPassword     string `json:"newPassword" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		failure(c, http.StatusBadRequest, "INVALID_PASSWORD", "密码信息无效", nil)
		return
	}
	if err := s.authService.ChangePassword(c.Request.Context(), currentUserID(c), auth.PasswordInput{CurrentPassword: request.CurrentPassword, NewPassword: request.NewPassword}); err != nil {
		failure(c, http.StatusBadRequest, "PASSWORD_CHANGE_FAILED", err.Error(), nil)
		return
	}
	s.clearRefreshCookie(c)
	s.clearCSRFCookie(c)
	s.recordSecurityAudit(c, currentUserID(c), "PASSWORD_CHANGED", "user", currentUserID(c), nil)
	success(c, http.StatusOK, gin.H{"changed": true})
}

// nullableActor 将未知用户的零 ID 映射为可空审计操作者
func nullableActor(userID uint64) any {
	if userID == 0 {
		return nil
	}
	return userID
}

// recordSecurityAudit 尽力写入不属于工作区的安全事件，写入失败不改变 HTTP 响应
func (s *server) recordSecurityAudit(c *gin.Context, actorID any, action, targetType string, targetID any, details any) {
	payload, _ := json.Marshal(details)
	requestID, _ := c.Get(requestIDKey)
	userAgent := c.Request.UserAgent()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, _ = s.db.ExecContext(c.Request.Context(), `
INSERT INTO audit_logs (workspace_id, actor_user_id, action, target_type, target_id, request_id, ip_address, user_agent, details)
VALUES (NULL, $1, $2, $3, $4, $5, $6, $7, $8)`, actorID, action, targetType, fmt.Sprint(targetID), requestID, c.ClientIP(), userAgent, payload)
}

// setRefreshCookie 设置仅认证路径可用的 HttpOnly Cookie，HTTPS 来源启用 Secure
func (s *server) setRefreshCookie(c *gin.Context, token string, expiry time.Time) {
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_refresh", token, int(time.Until(expiry).Seconds()), "/api/v1/auth", "", secure, true)
}

// clearRefreshCookie 使用相同路径和安全属性使刷新 Cookie 立即过期
func (s *server) clearRefreshCookie(c *gin.Context) {
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_refresh", "", -1, "/api/v1/auth", "", secure, true)
}

// setCSRFCookie 创建前端可读的随机 CSRF Cookie，用于后续双提交校验
func (s *server) setCSRFCookie(c *gin.Context) error {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_csrf", base64.RawURLEncoding.EncodeToString(random), int(s.config.RefreshTokenTTL.Seconds()), "/", "", secure, false)
	return nil
}

// clearCSRFCookie 使站点根路径的 CSRF Cookie 立即过期
func (s *server) clearCSRFCookie(c *gin.Context) {
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_csrf", "", -1, "/", "", secure, false)
}

// validCSRF 以常量时间比较非空 Cookie 和 X-CSRF-Token 请求头
func (s *server) validCSRF(c *gin.Context) bool {
	cookie, err := c.Cookie("shiftory_csrf")
	header := c.GetHeader("X-CSRF-Token")
	return err == nil && cookie != "" && header != "" && subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

// validOrigin 接受空 Origin 或与配置公开来源完全一致的 Origin
func (s *server) validOrigin(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	// 非浏览器客户端通常不发送 Origin，浏览器请求则必须精确匹配公开来源
	return origin == "" || origin == s.config.PublicOrigin
}

// clientInfo 提取客户端 IP 和 User-Agent 作为会话记录信息
func clientInfo(c *gin.Context) auth.ClientInfo {
	return auth.ClientInfo{IPAddress: c.ClientIP(), UserAgent: c.GetHeader("User-Agent")}
}

// currentUserID 从鉴权上下文读取用户 ID，缺失或类型不符时返回零
func currentUserID(c *gin.Context) uint64 {
	value, _ := c.Get(userIDKey)
	id, _ := value.(uint64)
	return id
}

// userResponse 只序列化公开账号字段，排除密码摘要等认证数据
func userResponse(user auth.User) gin.H {
	return gin.H{"id": user.ID, "username": user.Username, "email": user.Email, "displayName": user.DisplayName, "avatarUrl": user.AvatarURL, "status": user.Status}
}

// success 按统一 data 外层格式写入成功响应
func success(c *gin.Context, status int, data any) { c.JSON(status, gin.H{"data": data}) }

// failure 记录业务错误码并返回包含请求标识的统一错误响应
func failure(c *gin.Context, status int, code, message string, details any) {
	c.Set(errorCodeKey, code)
	requestID, _ := c.Get(requestIDKey)
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "details": details, "requestId": requestID}})
}
