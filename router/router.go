package router

import (
	"net/http"
	"strings"

	"github.com/CodeDogRun/go-framework/logger"
	"github.com/gin-gonic/gin"
)

var engine *gin.Engine

type Route struct {
	Path     string
	Method   string
	Handlers []gin.HandlerFunc
}

var routes []Route

func AddRoute(method, path string, handlers ...gin.HandlerFunc) {
	routes = append(routes, Route{Path: path, Method: strings.ToUpper(method), Handlers: handlers})
}

func GET(path string, handlers ...gin.HandlerFunc) {
	AddRoute("GET", path, handlers...)
}

func POST(path string, handlers ...gin.HandlerFunc) {
	AddRoute("POST", path, handlers...)
}

func PUT(path string, handlers ...gin.HandlerFunc) {
	AddRoute("PUT", path, handlers...)
}

func PATCH(path string, handlers ...gin.HandlerFunc) {
	AddRoute("PATCH", path, handlers...)
}

func DELETE(path string, handlers ...gin.HandlerFunc) {
	AddRoute("DELETE", path, handlers...)
}

func OPTIONS(path string, handlers ...gin.HandlerFunc) {
	AddRoute("OPTIONS", path, handlers...)
}

func HEAD(path string, handlers ...gin.HandlerFunc) {
	AddRoute("HEAD", path, handlers...)
}

func ANY(path string, handlers ...gin.HandlerFunc) {
	AddRoute("ANY", path, handlers...)
}

// 跨域中间件
func crossDomain() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "*")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// 404 和 405 handler
func noRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.String(http.StatusNotFound, "啊噢，页面丢失了～")
		c.Abort()
	}
}

func noMethod() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.String(http.StatusNotFound, "啊噢，页面丢失了！")
		c.Abort()
	}
}

func New() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderUseNumber()

	engine = gin.New()
	engine.Use(gin.Recovery(), crossDomain())
	engine.Static("/data", "data")
	engine.NoRoute(noRoute())
	engine.NoMethod(noMethod())

	// HTTP 方法注册映射
	for _, route := range routes {
		switch route.Method {
		case "GET":
			engine.GET(route.Path, route.Handlers...)
		case "POST":
			engine.POST(route.Path, route.Handlers...)
		case "DELETE":
			engine.DELETE(route.Path, route.Handlers...)
		case "PATCH":
			engine.PATCH(route.Path, route.Handlers...)
		case "PUT":
			engine.PUT(route.Path, route.Handlers...)
		case "OPTIONS":
			engine.OPTIONS(route.Path, route.Handlers...)
		case "HEAD":
			engine.HEAD(route.Path, route.Handlers...)
		case "ANY":
			engine.Any(route.Path, route.Handlers...)
		default:
			logger.Error("未知请求方法: %s 路径: %s", route.Method, route.Path)
		}
	}

	return engine
}

func Engine() *gin.Engine {
	return engine
}
