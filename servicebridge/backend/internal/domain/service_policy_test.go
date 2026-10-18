package domain

import (
	"strings"
	"testing"
	"time"
)

// TestServicePolicyDefaultsAndNormalize 覆盖策略默认值与归一：
// 非法枚举回落、负数归零、超上限收敛、话术去空白并截断。
func TestServicePolicyDefaultsAndNormalize(t *testing.T) {
	defaults := DefaultServicePolicy()
	if !defaults.QueueEnabled || !defaults.UnattendedEnabled || !defaults.TakeoverPopupEnabled {
		t.Fatalf("默认开关应为 true：%+v", defaults)
	}
	if defaults.MaxWaitSeconds != 600 || defaults.SLAFirstResponseSeconds != 60 || defaults.SLAIdleMinutes != 5 {
		t.Fatalf("默认数值不符：%+v", defaults)
	}
	if defaults.SLAIdleAction != SLAIdleActionAI || defaults.AIKeywordPriority != AIKeywordPriorityKeywordFirst {
		t.Fatalf("默认枚举不符：%+v", defaults)
	}
	if defaults.AutoCloseIdleMinutes != 30 || defaults.UnattendedReply != "" {
		t.Fatalf("默认自动关闭/兜底话术不符：%+v", defaults)
	}

	normalized := ServicePolicy{
		QueueEnabled:            false,
		MaxWaitSeconds:          -10,
		SLAFirstResponseSeconds: 999999,
		SLAIdleMinutes:          -1,
		SLAIdleAction:           "bogus",
		AutoCloseIdleMinutes:    99999999,
		AIKeywordPriority:       "BOGUS",
		UnattendedEnabled:       false,
		UnattendedReply:         "  请稍候，客服马上回来。  ",
		TakeoverPopupEnabled:    false,
	}.Normalize()
	if normalized.QueueEnabled || normalized.UnattendedEnabled || normalized.TakeoverPopupEnabled {
		t.Fatalf("布尔字段不应被归一改写：%+v", normalized)
	}
	if normalized.MaxWaitSeconds != 0 || normalized.SLAIdleMinutes != 0 {
		t.Fatalf("负数应归零：%+v", normalized)
	}
	if normalized.SLAFirstResponseSeconds != ServicePolicyMaxSLAFirstResponseSecondsLimit {
		t.Fatalf("首响超时应收敛到上限：%d", normalized.SLAFirstResponseSeconds)
	}
	if normalized.AutoCloseIdleMinutes != ServicePolicyMaxAutoCloseIdleMinutesLimit {
		t.Fatalf("自动关闭应收敛到上限：%d", normalized.AutoCloseIdleMinutes)
	}
	if normalized.SLAIdleAction != SLAIdleActionAI {
		t.Fatalf("非法 sla_idle_action 应回落 ai：%q", normalized.SLAIdleAction)
	}
	if normalized.AIKeywordPriority != AIKeywordPriorityKeywordFirst {
		t.Fatalf("非法 ai_keyword_priority 应回落 keyword_first：%q", normalized.AIKeywordPriority)
	}
	if normalized.UnattendedReply != "请稍候，客服马上回来。" {
		t.Fatalf("兜底话术应去首尾空白：%q", normalized.UnattendedReply)
	}
}

// TestNormalizeAIKeywordPriority 覆盖四种合法枚举与非法回落。
func TestNormalizeAIKeywordPriority(t *testing.T) {
	for _, value := range []string{
		AIKeywordPriorityKeywordFirst,
		AIKeywordPriorityAIFirst,
		AIKeywordPriorityKeywordOnly,
		AIKeywordPriorityAIOnly,
	} {
		if !ValidAIKeywordPriority(value) {
			t.Fatalf("%q 应为合法取值", value)
		}
		if got := NormalizeAIKeywordPriority(value); got != value {
			t.Fatalf("归一化改变了合法值：%q -> %q", value, got)
		}
	}
	if ValidAIKeywordPriority("keyword") {
		t.Fatal("非法取值不应通过校验")
	}
	if got := NormalizeAIKeywordPriority("keyword"); got != AIKeywordPriorityKeywordFirst {
		t.Fatalf("非法取值应回落 keyword_first，实际 %q", got)
	}
	if got := NormalizeSLAIdleAction("REASSIGN"); got != SLAIdleActionReassign {
		t.Fatalf("大小写应被归一：%q", got)
	}
}

// TestBlacklistEntryExpired 覆盖过期判断。
func TestBlacklistEntryExpired(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	if (BlacklistEntry{ExpiresAt: &past}).Expired(now) != true {
		t.Fatal("过去时间应视为过期")
	}
	if (BlacklistEntry{ExpiresAt: &future}).Expired(now) != false {
		t.Fatal("未来时间不应过期")
	}
	if (BlacklistEntry{}).Expired(now) != false {
		t.Fatal("无过期时间应永不过期")
	}
}

// TestAlertChannelConfigNormalizeAndMask 覆盖渠道配置校验与密钥掩码。
func TestAlertChannelConfigNormalizeAndMask(t *testing.T) {
	telegram, err := NormalizeAlertChannelConfig(AlertChannelKindTelegram, map[string]string{
		"token":   "  123456:ABCDEF  ",
		"chat_id": " 10086 ",
		"extra":   "",
	})
	if err != nil {
		t.Fatalf("telegram 配置应通过：%v", err)
	}
	if telegram["token"] != "123456:ABCDEF" || telegram["chat_id"] != "10086" {
		t.Fatalf("配置应去首尾空白：%#v", telegram)
	}
	if _, ok := telegram["extra"]; ok {
		t.Fatalf("空值应被丢弃：%#v", telegram)
	}
	masked := MaskAlertChannelConfig(AlertChannelKindTelegram, telegram)
	if strings.Contains(masked["token"], "ABCDEF") {
		t.Fatalf("token 必须被掩码，实际 %q", masked["token"])
	}
	if !strings.HasSuffix(masked["token"], "CDEF") || !strings.HasPrefix(masked["token"], "****") {
		t.Fatalf("token 掩码应为 ****+末四位，实际 %q", masked["token"])
	}
	if masked["chat_id"] != "10086" {
		t.Fatalf("chat_id 非密钥，应保持可读：%q", masked["chat_id"])
	}
	// 原始配置不能被掩码修改。
	if telegram["token"] != "123456:ABCDEF" {
		t.Fatalf("掩码不应修改原配置：%#v", telegram)
	}

	if _, err := NormalizeAlertChannelConfig(AlertChannelKindTelegram, map[string]string{"token": "x"}); err == nil {
		t.Fatal("缺少 chat_id 应报错")
	}
	if _, err := NormalizeAlertChannelConfig(AlertChannelKindEmail, map[string]string{"address": "not-an-email"}); err == nil {
		t.Fatal("非法邮箱应报错")
	}
	if _, err := NormalizeAlertChannelConfig("sms", map[string]string{"value": "x"}); err == nil {
		t.Fatal("非法渠道种类应报错")
	}

	bark, err := NormalizeAlertChannelConfig(AlertChannelKindBark, map[string]string{"key": "bark-key-1234"})
	if err != nil {
		t.Fatalf("bark 配置应通过：%v", err)
	}
	if bark["server"] != DefaultBarkServer {
		t.Fatalf("bark server 应补默认值：%q", bark["server"])
	}
	if _, err := NormalizeAlertChannelConfig(AlertChannelKindBark, map[string]string{"key": "k", "server": "ftp://x"}); err == nil {
		t.Fatal("非 http(s) server 应报错")
	}

	emailMasked := MaskAlertChannelConfig(AlertChannelKindEmail, map[string]string{"address": "alice@example.com"})
	if strings.Contains(emailMasked["address"], "alice") {
		t.Fatalf("邮箱本地部分必须被掩码：%q", emailMasked["address"])
	}
	if !strings.Contains(emailMasked["address"], "@example.com") {
		t.Fatalf("邮箱域名应保留：%q", emailMasked["address"])
	}
}

// TestAlertSMTPSettingsNormalize 覆盖 SMTP 端口缺省与非法值。
func TestAlertSMTPSettingsNormalize(t *testing.T) {
	settings, err := AlertSMTPSettings{Host: " smtp.example.com ", FromAddress: " bot@example.com "}.Normalize()
	if err != nil {
		t.Fatalf("SMTP 设置应通过：%v", err)
	}
	if settings.Port != DefaultAlertSMTPPort {
		t.Fatalf("端口应补默认 %d，实际 %d", DefaultAlertSMTPPort, settings.Port)
	}
	if settings.Host != "smtp.example.com" || settings.FromAddress != "bot@example.com" {
		t.Fatalf("SMTP 字段应去首尾空白：%+v", settings)
	}
	if _, err := (AlertSMTPSettings{Port: 70000}).Normalize(); err == nil {
		t.Fatal("非法端口应报错")
	}
	if _, err := (AlertSMTPSettings{FromAddress: "nope"}).Normalize(); err == nil {
		t.Fatal("非法发件人应报错")
	}
	masked := AlertSMTPSettings{Password: "secret-pass"}.WithMask()
	if masked.PasswordMasked != "****pass" {
		t.Fatalf("密码掩码应为 ****+末四位：%q", masked.PasswordMasked)
	}
}

// TestAgentPreferencesTakeoverDefaults 覆盖批次 4 新增的两个偏好默认值。
func TestAgentPreferencesTakeoverDefaults(t *testing.T) {
	preferences := AgentPreferences{}.WithDefaults()
	if preferences.TakeoverPopupEnabled == nil || !*preferences.TakeoverPopupEnabled {
		t.Fatalf("takeover_popup_enabled 默认应为 true：%+v", preferences)
	}
	if preferences.ReadOnlyWatch == nil || *preferences.ReadOnlyWatch {
		t.Fatalf("read_only_watch 默认应为 false：%+v", preferences)
	}
	disabled := false
	enabled := true
	preferences = AgentPreferences{TakeoverPopupEnabled: &disabled, ReadOnlyWatch: &enabled}.WithDefaults()
	if *preferences.TakeoverPopupEnabled || !*preferences.ReadOnlyWatch {
		t.Fatalf("显式设置应被保留：%+v", preferences)
	}
}
