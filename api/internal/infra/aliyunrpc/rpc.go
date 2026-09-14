package aliyunrpc

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func Call(ctx context.Context, host, action, version, ak, sk string, biz map[string]string) error {
	q := map[string]string{
		"Format":           "JSON",
		"Version":          version,
		"AccessKeyId":      ak,
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"SignatureVersion": "1.0",
		"SignatureNonce":   fmt.Sprintf("%d", time.Now().UnixNano()),
		"Action":           action,
		"RegionId":         "cn-hangzhou",
	}
	for k, v := range biz {
		q[k] = v
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonical []string
	for _, k := range keys {
		canonical = append(canonical, percent(k)+"="+percent(q[k]))
	}
	canonicalQS := strings.Join(canonical, "&")
	stringToSign := "GET&" + percent("/") + "&" + percent(canonicalQS)
	mac := hmac.New(sha1.New, []byte(sk+"&"))
	mac.Write([]byte(stringToSign))
	q["Signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u := url.URL{Scheme: "https", Host: host, Path: "/"}
	vals := url.Values{}
	for k, v := range q {
		vals.Set(k, v)
	}
	u.RawQuery = vals.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	if code, _ := parsed["Code"].(string); code != "" && !strings.EqualFold(code, "OK") {
		return fmt.Errorf("aliyun %s: %s", action, string(body))
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("aliyun http %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func percent(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
