package httpserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"shiftory-server/internal/importer"
	"shiftory-server/internal/schedule"
)

func TestWriteImportValidationFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	validationErr := &importer.ValidationError{
		Row: 2, Fields: []string{"状态", "班次"}, Code: "REST_HAS_SEGMENTS",
		Message: "休息日不能填写班次、开始时间、结束时间或跨日",
		Hint:    "请清空班次、开始时间和结束时间，并将是否跨日设为“否”", Cause: schedule.ErrRestHasSegments,
	}

	writeImportValidationFailure(context, validationErr)
	if recorder.Code != 400 {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	var payload struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "INVALID_WORKBOOK" || payload.Error.Message != validationErr.Message {
		t.Fatalf("unexpected error envelope: %+v", payload.Error)
	}
	if payload.Error.Details["row"] != float64(2) || payload.Error.Details["ruleCode"] != "REST_HAS_SEGMENTS" {
		t.Fatalf("missing row/rule details: %+v", payload.Error.Details)
	}
	fields, ok := payload.Error.Details["fields"].([]any)
	if !ok || len(fields) != 2 || fields[0] != "状态" || fields[1] != "班次" {
		t.Fatalf("missing field details: %+v", payload.Error.Details)
	}
	if _, leaked := payload.Error.Details["cause"]; leaked {
		t.Fatalf("internal cause must not be exposed: %+v", payload.Error.Details)
	}
}
