package store

import "testing"

func TestEffectiveBootstrapPasswordNeverFallsBackToDefault(t *testing.T) {
	password := effectiveBootstrapPassword("", "admin", nil)
	if password == "" || password == "123456" {
		t.Fatalf("bootstrap 口令不能回退到空或默认值，got %q", password)
	}
	if len(password) < 24 {
		t.Fatalf("随机 bootstrap 口令长度不足：%d", len(password))
	}
}
