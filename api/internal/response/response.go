package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Details any    `json:"details,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: "OK", Message: "ok", Data: data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{Code: "OK", Message: "ok", Data: data})
}

func Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, Body{Code: code, Message: message})
}

func FailDetails(c *gin.Context, status int, code, message string, details any) {
	c.AbortWithStatusJSON(status, Body{Code: code, Message: message, Details: details})
}

func BindJSON(c *gin.Context, dest any) bool {
	if err := c.ShouldBindJSON(dest); err != nil {
		Fail(c, http.StatusBadRequest, "INVALID_BODY", "请求参数不正确")
		return false
	}
	return true
}
