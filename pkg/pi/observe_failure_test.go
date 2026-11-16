package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestObserveNetworkPolicyReason 验证未开放回环时向调用方保留明确策略原因。
func TestObserveNetworkPolicyReason(t *testing.T) {
	_, err := observeHTTP(context.Background(), "http://127.0.0.1:18080/authorized", nil)
	if err == nil || !strings.Contains(err.Error(), "PI_ALLOWED_CIDRS") {
		t.Fatalf("期望可操作的 CIDR 错误，得到 %v", err)
	}
}

// TestObserveAuthorizedLocalTarget 验证未授权时不发请求、显式授权后读取真实目标。
func TestObserveAuthorizedLocalTarget(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); fmt.Fprint(w, "local-regression-marker") }))
	defer server.Close()
	_, err := observeHTTP(context.Background(), server.URL+"/authorized", nil)
	if !errors.Is(err, errNetworkPolicy) || hits.Load() != 0 {
		t.Fatalf("未授权目标不应收到请求：err=%v hits=%d", err, hits.Load())
	}
	content, err := observeHTTP(context.Background(), server.URL+"/authorized", []string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	var result HTTPObservation
	if err = json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || result.Status != 200 || result.Body != "local-regression-marker" {
		t.Fatalf("授权观察不正确：%+v hits=%d", result, hits.Load())
	}
}

// TestFailureMessageNeverLeaks 验证未知外部错误不会直接出现在用户可见诊断中。
func TestFailureMessageNeverLeaks(t *testing.T) {
	const secret = "provider-secret-canary"
	if strings.Contains(FailureMessage(errors.New(secret)), secret) {
		t.Fatal("未知错误正文泄漏")
	}
	if !strings.Contains(FailureMessage(fmt.Errorf("wrapped: %w", errNetworkPolicy)), "PI_ALLOWED_CIDRS") {
		t.Fatal("安全网络诊断未保留")
	}
}
