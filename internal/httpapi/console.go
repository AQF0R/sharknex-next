package httpapi

import (
	"github.com/gin-gonic/gin"
	"os"
)

type runsQuery struct {
	Limit  int `form:"limit,default=100" binding:"gte=1,lte=200"`   // 有界运行导航
	Offset int `form:"offset,default=0" binding:"gte=0,lte=100000"` // 分页偏移
}

// listRuns 提供有鉴权的控制台运行导航，不开放内部 Worker 权限。
func (a *API) listRuns(c *gin.Context) {
	var q runsQuery
	if c.ShouldBindQuery(&q) != nil {
		c.JSON(400, reply{Error: "分页参数无效"})
		return
	}
	v, er := a.Engine.ListRuns(c.Request.Context(), q.Limit, q.Offset)
	if er != nil {
		fail(c, er)
		return
	}
	c.JSON(200, reply{Data: v})
}

// consoleRoutes 仅托管明确的构建产物，保留 API 和未知路径的真实错误响应。
func (a *API) consoleRoutes(r *gin.Engine) {
	r.Static("/assets", "web/dist/assets")
	r.GET("/", func(c *gin.Context) {
		if _, er := os.Stat("web/dist/index.html"); er != nil {
			c.Data(200, "text/html; charset=utf-8", []byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>协作控制台</title><body><h1>协作控制台尚未构建</h1><p>开发模式请执行 make dev，打开 http://127.0.0.1:5174。</p><p>构建静态入口：cd web &amp;&amp; npm run build</p></body></html>`))
			return
		}
		c.File("web/dist/index.html")
	})
}
