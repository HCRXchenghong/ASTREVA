package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ===== 客服提醒渠道（每客服 1–3 个）与全局 SMTP 设置 =====

// 提醒渠道种类。
const (
	AlertChannelKindTelegram = "telegram"
	AlertChannelKindBark     = "bark"
	AlertChannelKindEmail    = "email"
)

// AlertChannelMaxPerAgent 是每个客服的提醒渠道数量上限（超出返回 400/ErrInvalidInput）。
const AlertChannelMaxPerAgent = 3

// DefaultBarkServer 是未配置 server 时使用的 Bark 服务地址。
const DefaultBarkServer = "https://api.day.app"

// DefaultAlertSMTPPort 是未配置端口时的默认 SMTP 端口（587，STARTTLS 提交口）。
const DefaultAlertSMTPPort = 587

// AgentAlertChannel 是某客服的一条提醒渠道。
// Config 保存解密后的明文配置（token / chat_id / key / server / address），
// 通过 json:"-" 保证永远不会被响应序列化；对外只暴露 ConfigMasked（密钥掩码）。
type AgentAlertChannel struct {
	ID           string            `json:"id"`
	AgentID      string            `json:"agent_id,omitempty"`
	Kind         string            `json:"kind"`
	Config       map[string]string `json:"-"`
	ConfigMasked map[string]string `json:"config_masked,omitempty"`
	// Masked 是该渠道"最主要密钥"的掩码字符串（telegram→token、bark→key、email→address），
	// 供控制台直接展示，不必再从 ConfigMasked 里挑键。始终为字符串（可能是空串）。
	Masked    string    `json:"masked"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgentAlertChannelPatch 描述渠道的可选更新字段：nil 表示不修改。
// 渠道种类不允许在 PATCH 中变更（换种类请删除后重建），避免历史配置含义漂移。
type AgentAlertChannelPatch struct {
	Enabled *bool
}

// AlertSMTPSettings 是全局 SMTP 发信配置（单例表 alert_smtp_settings）。
// Password 是解密后的明文，仅供通知层发信使用，json:"-" 防止泄漏；
// 对接口响应只暴露 PasswordMasked。写接口使用 UpdateAlertSMTPSettings 的 password 参数：
// nil=不改，""=清除，其它=覆盖。
type AlertSMTPSettings struct {
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Username       string     `json:"username"`
	Password       string     `json:"-"`
	PasswordMasked string     `json:"password_masked,omitempty"`
	FromAddress    string     `json:"from_address"`
	UseTLS         bool       `json:"use_tls"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

// DefaultAlertSMTPSettings 返回 SMTP 设置的默认值（未配置状态，端口按 587 展示）。
func DefaultAlertSMTPSettings() AlertSMTPSettings {
	return AlertSMTPSettings{Port: DefaultAlertSMTPPort}
}

// Normalize 归一化 SMTP 设置：去空白、端口缺省补 587、非法端口报错。
func (s AlertSMTPSettings) Normalize() (AlertSMTPSettings, error) {
	out := s
	out.Host = strings.TrimSpace(out.Host)
	out.Username = strings.TrimSpace(out.Username)
	out.FromAddress = strings.TrimSpace(out.FromAddress)
	if out.Port == 0 {
		out.Port = DefaultAlertSMTPPort
	}
	if out.Port < 1 || out.Port > 65535 {
		return AlertSMTPSettings{}, errors.New("SMTP 端口必须在 1-65535 之间")
	}
	if out.FromAddress != "" && !strings.Contains(out.FromAddress, "@") {
		return AlertSMTPSettings{}, errors.New("发件人地址格式不正确")
	}
	return out, nil
}

// WithMask 填充密钥掩码（明文密码仍需由调用方另行保留，不会写进 JSON）。
func (s AlertSMTPSettings) WithMask() AlertSMTPSettings {
	out := s
	out.PasswordMasked = MaskSecretValue(out.Password)
	return out
}

// ValidAlertChannelKind 判断提醒渠道种类是否合法。
func ValidAlertChannelKind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AlertChannelKindTelegram, AlertChannelKindBark, AlertChannelKindEmail:
		return true
	default:
		return false
	}
}

// NormalizeAlertChannelConfig 归一化并校验渠道配置，返回新的 map（不修改入参）。
// 各渠道的必填键：telegram=token+chat_id，bark=key（server 可选），email=address。
func NormalizeAlertChannelConfig(kind string, config map[string]string) (map[string]string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !ValidAlertChannelKind(kind) {
		return nil, fmt.Errorf("提醒渠道种类只允许 telegram、bark、email，收到 %q", kind)
	}
	out := map[string]string{}
	for key, value := range config {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	switch kind {
	case AlertChannelKindTelegram:
		if err := requireAlertConfig(out, "telegram", "token", "chat_id"); err != nil {
			return nil, err
		}
	case AlertChannelKindBark:
		if err := requireAlertConfig(out, "bark", "key"); err != nil {
			return nil, err
		}
		server := strings.TrimSpace(out["server"])
		if server == "" {
			server = DefaultBarkServer
		}
		parsed, err := url.Parse(server)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, errors.New("bark 的 server 必须是 http(s) 地址")
		}
		out["server"] = strings.TrimRight(server, "/")
	case AlertChannelKindEmail:
		if err := requireAlertConfig(out, "email", "address"); err != nil {
			return nil, err
		}
		if !strings.Contains(out["address"], "@") {
			return nil, errors.New("email 的 address 格式不正确")
		}
	}
	return out, nil
}

// MaskAlertChannelConfig 返回配置的掩码副本：密钥类字段（telegram token、bark key、email address）
// 只保留末四位；非敏感字段（chat_id、server）保持原样，便于后台识别渠道。
func MaskAlertChannelConfig(kind string, config map[string]string) map[string]string {
	if len(config) == 0 {
		return nil
	}
	out := make(map[string]string, len(config))
	for key, value := range config {
		out[key] = value
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	for _, secretKey := range alertChannelSecretKeys(kind) {
		if value, ok := out[secretKey]; ok {
			out[secretKey] = maskAlertChannelValue(secretKey, value)
		}
	}
	return out
}

// MaskAlertChannelPrimary 返回该渠道最主要密钥的掩码：telegram→token、bark→key、email→address。
// 配置缺失时返回空串（而不是回退到其它字段），避免把非密钥值当密钥展示。
func MaskAlertChannelPrimary(kind string, config map[string]string) string {
	keys := alertChannelSecretKeys(strings.ToLower(strings.TrimSpace(kind)))
	if len(keys) == 0 {
		return ""
	}
	value := strings.TrimSpace(config[keys[0]])
	if value == "" {
		return ""
	}
	return maskAlertChannelValue(keys[0], value)
}

// MaskSecretValue 把密钥替换为「****+末四位」；长度不足 4 时只返回 ****。
func MaskSecretValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

func alertChannelSecretKeys(kind string) []string {
	switch kind {
	case AlertChannelKindTelegram:
		return []string{"token"}
	case AlertChannelKindBark:
		return []string{"key"}
	case AlertChannelKindEmail:
		return []string{"address"}
	default:
		return nil
	}
}

func maskAlertChannelValue(key, value string) string {
	if key == "address" {
		return maskEmailAddress(value)
	}
	return MaskSecretValue(value)
}

// maskEmailAddress 保留首字符与域名，隐藏本地部分（如 a***@example.com）。
func maskEmailAddress(value string) string {
	value = strings.TrimSpace(value)
	at := strings.LastIndex(value, "@")
	if at <= 0 {
		return MaskSecretValue(value)
	}
	local := []rune(value[:at])
	return string(local[0]) + "***" + value[at:]
}

func requireAlertConfig(config map[string]string, kind string, keys ...string) error {
	for _, key := range keys {
		if strings.TrimSpace(config[key]) == "" {
			return fmt.Errorf("%s 渠道缺少配置项 %s", kind, key)
		}
	}
	return nil
}
