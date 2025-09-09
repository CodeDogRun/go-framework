package response

import (
	"net/http"

	"github.com/CodeDogRun/go-framework/logger"
	"github.com/gin-gonic/gin"
)

const (
	CodeOK          = 200
	CodeBadRequest  = -401
	CodeNotFound    = 404
	CodeServerError = 500
)

type Body struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Data    any    `json:"data,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

func getTrace(c *gin.Context) string {
	if v, ok := c.Get("trace_id"); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

func Success(c *gin.Context, data any, msg ...string) {
	body := Body{
		Code:    CodeOK,
		Msg:     "请求成功",
		Data:    data,
		TraceID: getTrace(c),
	}
	if len(msg) > 0 {
		body.Msg = msg[0]
	}
	c.AbortWithStatusJSON(http.StatusOK, body)
}

func Error(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(http.StatusOK, Body{
		Code:    code,
		Msg:     msg,
		TraceID: getTrace(c),
	})
}

func ErrorWithData(c *gin.Context, code int, msg string, data any) {
	c.AbortWithStatusJSON(http.StatusOK, Body{
		Code:    code,
		Msg:     msg,
		Data:    data,
		TraceID: getTrace(c),
	})
}

func BindAs[T any](c *gin.Context, binder func(any) error) (T, bool) {
	var v T
	if err := binder(&v); err != nil {
		logger.Error("表单参数解析异常: %v", err)
		Error(c, CodeBadRequest, "服务器内部错误，请稍候再试！")
		return v, false
	}
	return v, true
}

func BindAsError[T any](c *gin.Context, binder func(any) error) (T, error) {
	var v T
	if err := binder(&v); err != nil {
		return v, err
	}
	return v, nil
}

func BindInto(c *gin.Context, binder func(any) error, dst any) bool {
	if err := binder(dst); err != nil {
		logger.Error("表单参数解析异常: %v", err)
		Error(c, CodeBadRequest, "服务器内部错误，请稍候再试！")
		return false
	}
	return true
}

func BindIntoWithError(c *gin.Context, binder func(any) error, dst any) error {
	if err := binder(&dst); err != nil {
		return err
	}
	return nil
}
