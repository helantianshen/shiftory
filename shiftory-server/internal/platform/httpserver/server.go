package httpserver

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
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

type Dependencies struct {
	DB           *sql.DB
	Config       config.Config
	Tokens       *auth.TokenManager
	Store        storage.Store
	ImportWakeup func()
}

type server struct {
	db           *sql.DB
	config       config.Config
	tokens       *auth.TokenManager
	authService  *auth.Service
	store        storage.Store
	importWakeup func()
}

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
	s := &server{
		db: deps.DB, config: deps.Config, tokens: deps.Tokens,
		authService: auth.NewService(auth.NewMySQLRepository(deps.DB), auth.NewPasswordHasher(auth.DefaultPasswordParams()), deps.Tokens),
		store:       store, importWakeup: deps.ImportWakeup,
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	router.Use(gin.Recovery(), s.requestContext(), s.cors())
	router.GET("/health", func(c *gin.Context) { success(c, http.StatusOK, gin.H{"status": "ok"}) })

	v1 := router.Group("/api/v1")
	authRoutes := v1.Group("/auth")
	authRoutes.POST("/register", s.register)
	authRoutes.POST("/login", s.login)
	authRoutes.POST("/refresh", s.refresh)
	authRoutes.POST("/logout", s.logout)
	authRoutes.GET("/me", s.authenticate(), s.me)
	authRoutes.PATCH("/profile", s.authenticate(), s.updateProfile)
	authRoutes.PUT("/password", s.authenticate(), s.changePassword)

	protected := v1.Group("")
	protected.Use(s.authenticate())
	protected.POST("/invitations/accept", s.acceptInvitation)
	protected.GET("/preferences", s.getPreferences)
	protected.PUT("/preferences", s.updatePreferences)
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

func (s *server) requestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set(requestIDKey, requestID)
		c.Header("X-Request-ID", requestID)
		if !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		} else {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12<<20)
		}
		c.Next()
	}
}

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

type registerRequest struct {
	Username    string `json:"username" binding:"required"`
	Email       string `json:"email" binding:"required"`
	DisplayName string `json:"displayName" binding:"required"`
	Password    string `json:"password" binding:"required"`
}

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

type loginRequest struct {
	Login    string `json:"login" binding:"required"`
	Password string `json:"password" binding:"required"`
}

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

func (s *server) me(c *gin.Context) {
	user, err := s.authService.CurrentUser(c.Request.Context(), currentUserID(c))
	if err != nil {
		failure(c, http.StatusUnauthorized, "USER_UNAVAILABLE", "账号不可用", nil)
		return
	}
	success(c, http.StatusOK, userResponse(user))
}

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

func nullableActor(userID uint64) any {
	if userID == 0 {
		return nil
	}
	return userID
}

func (s *server) recordSecurityAudit(c *gin.Context, actorID any, action, targetType string, targetID any, details any) {
	payload, _ := json.Marshal(details)
	requestID, _ := c.Get(requestIDKey)
	userAgent := c.Request.UserAgent()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, _ = s.db.ExecContext(c.Request.Context(), `
INSERT INTO audit_logs (workspace_id, actor_user_id, action, target_type, target_id, request_id, ip_address, user_agent, details)
VALUES (NULL, ?, ?, ?, ?, ?, ?, ?, ?)`, actorID, action, targetType, targetID, requestID, c.ClientIP(), userAgent, payload)
}

func (s *server) setRefreshCookie(c *gin.Context, token string, expiry time.Time) {
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_refresh", token, int(time.Until(expiry).Seconds()), "/api/v1/auth", "", secure, true)
}

func (s *server) clearRefreshCookie(c *gin.Context) {
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_refresh", "", -1, "/api/v1/auth", "", secure, true)
}

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

func (s *server) clearCSRFCookie(c *gin.Context) {
	secure := strings.HasPrefix(s.config.PublicOrigin, "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("shiftory_csrf", "", -1, "/", "", secure, false)
}

func (s *server) validCSRF(c *gin.Context) bool {
	cookie, err := c.Cookie("shiftory_csrf")
	header := c.GetHeader("X-CSRF-Token")
	return err == nil && cookie != "" && header != "" && subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) == 1
}

func (s *server) validOrigin(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	return origin == "" || origin == s.config.PublicOrigin
}

func clientInfo(c *gin.Context) auth.ClientInfo {
	return auth.ClientInfo{IPAddress: c.ClientIP(), UserAgent: c.GetHeader("User-Agent")}
}

func currentUserID(c *gin.Context) uint64 {
	value, _ := c.Get(userIDKey)
	id, _ := value.(uint64)
	return id
}

func userResponse(user auth.User) gin.H {
	return gin.H{"id": user.ID, "username": user.Username, "email": user.Email, "displayName": user.DisplayName, "avatarUrl": user.AvatarURL, "status": user.Status}
}

func success(c *gin.Context, status int, data any) { c.JSON(status, gin.H{"data": data}) }

func failure(c *gin.Context, status int, code, message string, details any) {
	requestID, _ := c.Get(requestIDKey)
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "details": details, "requestId": requestID}})
}
