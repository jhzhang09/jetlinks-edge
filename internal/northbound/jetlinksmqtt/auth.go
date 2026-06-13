// Package jetlinksmqtt 的 JetLinks MQTT 私有协议认证辅助函数。
//
// JetLinks 平台 MQTT 密码规则（按你提供的接入规范）：
//
//	username = secureId + "|" + timestamp        (timestamp 为毫秒)
//	password = SM3(secureId + "|" + timestamp + "|" + secureKey)  (大写十六进制)
//
// SM3 使用 github.com/piligo/gmsm/sm3（与 gmssl Python 库输出完全一致）。
package jetlinksmqtt

import (
	"strconv"
)

// BuildAuth 根据 secureId/secureKey/timestampMillis 计算 JetLinks MQTT 认证三件套。
// 返回 (clientId, username, password)。
//
// clientId = deviceId（由调用方提供，本函数不处理）
// username = secureId + "|" + timestamp
// password = MD5(secureId + "|" + timestamp + "|" + secureKey)
func BuildAuth(deviceId, secureId, secureKey string, timestampMillis int64) (clientId, username, password string) {
	clientId = deviceId
	ts := strconv.FormatInt(timestampMillis, 10)
	username = secureId + "|" + ts
	password = md5Hex(secureId + "|" + ts + "|" + secureKey) // md5Hex defined in mqtt_app.go
	return
}
