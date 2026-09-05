package httpserver

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"shiftory-server/internal/importer"
)

// writeImportValidationFailure translates importer-layer validation errors into
// the public API envelope. The HTTP layer owns status codes and JSON shape;
// importer errors remain independent from Gin and HTTP concerns.
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
