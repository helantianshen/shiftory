package httpserver

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"shiftory-server/internal/importer"
)

// writeImportValidationFailure 将 importer 校验错误转换为公开 API 响应
// HTTP 层负责状态码和 JSON 结构，importer 错误不依赖 Gin 或 HTTP
func writeImportValidationFailure(c *gin.Context, err error) {
	var validationErr *importer.ValidationError
	if errors.As(err, &validationErr) {
		failure(c, http.StatusBadRequest, "INVALID_WORKBOOK", validationErr.Message, gin.H{
			"row":      validationErr.Row,
			"fields":   validationErr.Fields,
			"ruleCode": validationErr.Code,
			"hint":     validationErr.Hint,
		})
		return
	}
	failure(c, http.StatusBadRequest, "INVALID_WORKBOOK", "导入文件内容无效，请检查模板和数据格式", gin.H{
		"hint": "请使用最新排班导入模板，并检查日期、状态、班次和时间格式",
	})
}
