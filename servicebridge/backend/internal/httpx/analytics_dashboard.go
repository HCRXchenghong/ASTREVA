package httpx

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata" // 容器里没有系统 zoneinfo 时也能解析 Asia/Shanghai 等看板时区。

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/store"
)

// ===== 数据看板：引擎侧只读聚合接口 =====
//
// 看板的「今日询盘」数据只存在于客服引擎（conversations/messages），
// admin-server 无法从自己的 CMS 库里算出来。因此这里提供一个**只读**聚合接口，
// 由 admin-server 通过服务间令牌（X-Internal-Token）调用后合并进
// /site-admin-api/analytics/overview。
//
// 口径（用户已确认，务必与 store.CountInquiryConversations 保持一致）：
//   - 询盘 = 当天有访客消息的会话数；
//   - 只看 messages.sender_type = 'visitor'，同一会话当天多条消息只计 1 次；
//   - **不是**官网表单提交数（site_leads 是另一个指标，不要混用）。
//
// 该接口不写库、不产生审计事件，也不暴露消息内容，只返回聚合计数。

const (
	dashboardAnalyticsPath     = "/api/internal/analytics/dashboard"
	dashboardDefaultTimezone   = "Asia/Shanghai"
	dashboardInquiryDefinition = "询盘=当天有访客消息的会话数；同一会话当天多条消息只计 1 次；不含官网表单提交。"
)

// registerAnalyticsDashboardRoutes 注册看板询盘聚合路由。
// 鉴权方式与 /api/internal/visitor-activity 一致：共享令牌，未配置时直接 503。
func registerAnalyticsDashboardRoutes(mux *http.ServeMux, service *app.Service, cfg config.Config) {
	mux.HandleFunc("GET "+dashboardAnalyticsPath, func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(cfg.InternalAPIToken)
		if expected == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "internal_token_unconfigured",
				"message": "服务间接口未配置 INTERNAL_API_TOKEN，已拒绝请求",
			})
			return
		}
		provided := strings.TrimSpace(r.Header.Get(internalTokenHeader))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, store.ErrUnauthorized)
			return
		}
		counter, ok := service.Store().(store.InquiryCounter)
		if !ok {
			// 理论上所有内置 Store 都实现了；真遇到自定义 Store 就如实报不可用，
			// 绝不用 0 冒充「今天没有询盘」。
			writeJSON(w, http.StatusNotImplemented, map[string]any{
				"error":   "inquiry_counter_unavailable",
				"message": "当前存储驱动不支持询盘聚合",
			})
			return
		}

		loc := dashboardTimezone(r.URL.Query().Get("tz"))
		now := time.Now().In(loc)
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		monthStart := dashboardMonthStart(now, loc, r.URL.Query().Get("month"))
		monthEnd := monthStart.AddDate(0, 1, 0)

		counts, err := counter.CountInquiryConversations(r.Context(), todayStart, monthStart, monthEnd)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"range": map[string]any{
				"timezone":    loc.String(),
				"today_start": todayStart.Format(time.RFC3339),
				"month_start": monthStart.Format(time.RFC3339),
				"month_end":   monthEnd.Format(time.RFC3339),
			},
			"today": map[string]int{"inquiries": counts.Today},
			"month": map[string]int{"inquiries": counts.Month},
			// 把口径随响应返回，前端 tooltip 与真值同源，避免文档和实现漂移。
			"definition":   dashboardInquiryDefinition,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
		})
	})
}

// dashboardTimezone 解析看板时区；空值或非法值回退到 Asia/Shanghai，
// 与 admin-server 的 analyticsSummary 默认口径保持一致。
func dashboardTimezone(raw string) *time.Location {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		candidate = dashboardDefaultTimezone
	}
	loc, err := time.LoadLocation(candidate)
	if err != nil {
		loc, err = time.LoadLocation(dashboardDefaultTimezone)
		if err != nil {
			return time.UTC
		}
	}
	return loc
}

// dashboardMonthStart 解析 YYYY-MM（按看板时区）；非法/空值回退到当前月 1 日。
func dashboardMonthStart(now time.Time, loc *time.Location, raw string) time.Time {
	if parsed, err := time.ParseInLocation("2006-01", strings.TrimSpace(raw), loc); err == nil {
		return parsed
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
}
