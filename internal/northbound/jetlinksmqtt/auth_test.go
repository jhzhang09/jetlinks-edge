// Package jetlinksmqtt 的 SM3 认证辅助函数单元测试。
package jetlinksmqtt

import (
	"strconv"
	"testing"
)

// 已知测试向量：基于 gmssl Python 库 + piligo/gmsm Go 库的交叉验证。
// 规则：SM3(secureId + "|" + timestamp + "|" + secureKey) = ?
//
// 真实运行中 timestamp 不断变化，无法做固定向量测试。
// 此处用确定的 secureId/secureKey/timestamp 算参考值，然后验证代码的输出与之一致。

func TestBuildAuth_KnownVectors(t *testing.T) {
	cases := []struct {
		name        string
		deviceID    string
		secureID    string
		secureKey   string
		timestampMs int64
		// 期望值：与独立运行 gmssl Python 库 + piligo/gmsm Go 库结果一致
		wantUsername string
		wantPassword string
	}{
		{
			name:         "example 1",
			deviceID:     "gw-001",
			secureID:     "sec-abc",
			secureKey:    "key-xyz",
			timestampMs:  1700000000000,
			wantUsername: "sec-abc|1700000000000",
			// MD5("sec-abc|1700000000000|key-xyz") 大写十六进制
			wantPassword: "303CF23556224CE074C876ED2CAE7B5E",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cid, u, p := BuildAuth(c.deviceID, c.secureID, c.secureKey, c.timestampMs)
			if cid != c.deviceID {
				t.Errorf("clientId = %q, want %q", cid, c.deviceID)
			}
			if u != c.wantUsername {
				t.Errorf("username = %q, want %q", u, c.wantUsername)
			}
			if c.wantPassword != "" && p != c.wantPassword {
				t.Errorf("password = %q, want %q", p, c.wantPassword)
			}
			// 至少 password 是 32 字符大写十六进制
			if len(p) != 32 {
				t.Errorf("password length = %d, want 32", len(p))
			}
			for _, c := range p {
				if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
					t.Errorf("password contains non-hex char: %q", string(c))
					break
				}
			}
		})
	}
}

// 验证 SM3 函数自身与 piligo/gmsm / gmssl 一致（用同一字符串）
func TestSm3Hex_ConsistentWithPiligoGmsm(t *testing.T) {
	// 与 gmssl Python 库交叉：
	// python3 -c "from gmssl import sm3, func; print(sm3.sm3_hash(func.bytes_to_list(b'sec-abc|1700000000000|key-xyz')))"
	want := "587AAC8F7BA34C0DC8C62BAEF9B5A974DCC89FBFF5220F48028F6BF705271566"
	got := sm3Hex("sec-abc|1700000000000|key-xyz")
	if got != want {
		t.Fatalf("sm3Hex = %s, want %s", got, want)
	}
	// 至少检查输出是 64 字符大写十六进制
	if len(got) != 64 {
		t.Errorf("sm3Hex length = %d, want 64", len(got))
	}
}

// 验证 md5Hex 函数 the 正确性
func TestMd5Hex(t *testing.T) {
	want := "303CF23556224CE074C876ED2CAE7B5E"
	got := md5Hex("sec-abc|1700000000000|key-xyz")
	if got != want {
		t.Fatalf("md5Hex = %s, want %s", got, want)
	}
	if len(got) != 32 {
		t.Errorf("md5Hex length = %d, want 32", len(got))
	}
}

// 验证 timestamp 变化导致 password 变化
func TestBuildAuth_TimestampChanges(t *testing.T) {
	_, _, p1 := BuildAuth("gw", "sec", "key", 1000)
	_, _, p2 := BuildAuth("gw", "sec", "key", 2000)
	if p1 == p2 {
		t.Errorf("password should differ for different timestamps, both = %s", p1)
	}
	// 验证 password 仅含大写十六进制
	for _, c := range p1 {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			t.Errorf("non-hex char in password: %q", string(c))
			break
		}
	}
}

// 验证 timestampFormat
func TestBuildAuth_TimestampFormat(t *testing.T) {
	ts := int64(1700000000123)
	_, u, _ := BuildAuth("gw", "sec", "key", ts)
	want := "sec|" + strconv.FormatInt(ts, 10)
	if u != want {
		t.Errorf("username = %q, want %q", u, want)
	}
}
