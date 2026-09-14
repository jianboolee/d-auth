package sms

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// 模板同时使用 ${code} 与 ${time}，缺任一阿里云会以 isv.SMS_TEMPLATE_ILLEGAL 拒绝。
func TestTemplateParamHasCodeAndTime(t *testing.T) {
	var got map[string]string
	if err := json.Unmarshal([]byte(templateParam("744440", 5*time.Minute)), &got); err != nil {
		t.Fatalf("templateParam is not valid json: %v", err)
	}
	if got["code"] != "744440" {
		t.Fatalf("code = %q", got["code"])
	}
	if got["time"] != "5" {
		t.Fatalf("time = %q want 5", got["time"])
	}
	if len(got) != 2 {
		t.Fatalf("unexpected extra params: %v", got)
	}
}

func TestTemplateParamTimeMatchesTTL(t *testing.T) {
	for _, ttl := range []time.Duration{5 * time.Minute, 10 * time.Minute, 90 * time.Second} {
		var got map[string]string
		if err := json.Unmarshal([]byte(templateParam("123456", ttl)), &got); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if want := strconv.Itoa(int(ttl.Minutes())); got["time"] != want {
			t.Fatalf("ttl %v: time %q want %q", ttl, got["time"], want)
		}
	}
}

func TestTemplateParamFallsBackWhenTTLZero(t *testing.T) {
	var got map[string]string
	if err := json.Unmarshal([]byte(templateParam("123456", 0)), &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got["time"] != "5" {
		t.Fatalf("time = %q want 5 fallback", got["time"])
	}
}
