package httpapi

import (
	"autonomous-engine/models"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"time"
)

// websocket 从数据库游标恢复事件，单写入者、有界批次、ping/pong 和进程取消保证连接可回收。
func (a *API) websocket(c *gin.Context) {
	id, ok := uri(c)
	if !ok {
		return
	}
	q, ok := query(c)
	if !ok {
		return
	}
	if _, er := a.Engine.GetRun(c.Request.Context(), id); er != nil {
		fail(c, er)
		return
	}
	upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, er := url.Parse(origin)
		return er == nil && u.Host == r.Host
	}}
	conn, er := upgrader.Upgrade(c.Writer, c.Request, nil)
	if er != nil {
		return
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(a.Context)
	defer cancel()
	conn.SetReadLimit(1024)
	if er := conn.SetReadDeadline(time.Now().Add(40 * time.Second)); er != nil {
		return
	}
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(40 * time.Second)) })
	go func() {
		defer cancel()
		for {
			if _, _, er := conn.ReadMessage(); er != nil {
				return
			}
		}
	}()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	cursor := q.After
	for {
		v, er := a.Engine.ListResource(ctx, id, "events", cursor, 200)
		if er != nil {
			return
		}
		for _, ev := range v.([]models.ExecutionEvent) {
			if er := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); er != nil {
				return
			}
			if er := conn.WriteJSON(ev); er != nil {
				return
			}
			cursor = ev.Sequence
		}
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		case <-ping.C:
			if er := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); er != nil {
				return
			}
		}
	}
}
