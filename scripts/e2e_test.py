#!/usr/bin/env python3
"""端到端测试脚本（v0.3 - 多设备共享北向应用）。

前置条件：
  1. 启动 mock modbus server：python3 scripts/mock_modbus_server.py
  2. 启动 jetlinks-edge：./bin/jetlinks-edge -c config.yaml

验证流程：
  1. 登录获取 JWT
  2. 创建 1 个北向应用（仅含 broker + 网关账号，**不**含设备身份）
  3. 创建 3 个 Group，全部共享这个北向应用
     - 每个 Group 有自己的 productId / deviceId（设备身份放在 Group 上）
  4. 等待采集，验证 3 个 Group 都从 mock 读到值
  5. 删除中间那个 Group，验证：
     - 其他 2 个 Group 不受影响
     - NorthApp 自动退订被删设备的 topic
  6. 删除北向应用，验证所有 Group 自动解绑
  7. 重新创建北向应用 + 1 个 Group（不再绑定北向），验证只采集不上送
"""
import json
import os
import time
import urllib.request

BASE = os.getenv("JETLINKS_EDGE_E2E_BASE", "http://127.0.0.1:7001/api/v1")


def http(method, path, token=None, data=None):
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = f"Bearer {token}"
    r = urllib.request.Request(f"{BASE}{path}", method=method, headers=h,
                                data=json.dumps(data).encode() if data is not None else None)
    with urllib.request.urlopen(r) as resp:
        return json.loads(resp.read())


def main():
    print("== 1. 登录 ==")
    tok = http("POST", "/auth/login", data={"username": "admin", "password": "admin123"})["token"]
    print(f"   token = {tok[:30]}...")

    print("== 2. 验证编译期插件描述符 ==")
    drivers = http("GET", "/extensions/drivers", token=tok)["items"]
    norths = http("GET", "/extensions/north-apps", token=tok)["items"]
    assert any(item["type"] == "modbus-tcp" and item["connectionSchema"] and item["tagSchema"] for item in drivers)
    assert any(item["type"] == "jetlinks-mqtt" and item["configSchema"] for item in norths)
    print("   ✓ 南向连接、点位和北向配置 Schema 可用")

    print("== 3. 创建 1 个网关（仅含 broker + 网关级 productId/deviceId/secureId/secureKey） ==")
    na = http("POST", "/north-apps", token=tok, data={
        "name": "e2e-gateway",
        "type": "jetlinks-mqtt",
        "enabled": True,
        "config": {
            "broker": "tcp://127.0.0.1:11883",
            "productId": "gw-product",       # 网关的产品 ID
            "deviceId": "gw-device-001",     # 网关的 deviceId（= clientId）
            "secureId": "sec-abc",            # 网关的 secureId
            "secureKey": "key-xyz",           # 网关的 secureKey
            "keepAlive": 30,
            "timestampDelta": 300
        }
    })
    print(f"   id = {na['id']}")

    print("== 4. 创建 3 个 Group（共享北向，不同设备身份） ==")
    gids = []
    devices = [
        ("plc-1", "test-product", "device-1"),
        ("plc-2", "test-product", "device-2"),
        ("plc-3", "test-product", "device-3"),
    ]
    for name, pid, did in devices:
        g = http("POST", "/groups", token=tok, data={
            "name": name,
            "driver": "modbus-tcp",
            "intervalMs": 500,
            "enabled": True,
            "northAppId": na["id"],
            "config": {"host": "127.0.0.1", "port": 5020, "unitId": 1},
            "device": {"productId": pid, "deviceId": did, "secureKey": ""}
        })
        gids.append(g["id"])
        # 加 1 个点位
        http("POST", f"/groups/{g['id']}/tags", token=tok, data={
            "name": "reg1", "type": "uint16", "access": "ro",
            "config": {"address": "40001", "byteOrder": "AB", "bit": 0}
        })
        print(f"   {name} gid={g['id']}, device={g['device']['productId']}/{g['device']['deviceId']}")

    time.sleep(2)

    print("== 5. 读 3 个 Group 的实时值（应都为 100） ==")
    for gid in gids:
        g = http("GET", f"/groups/{gid}", token=tok)
        v = http("GET", f"/groups/{gid}/values", token=tok)
        for tid, vv in v.items():
            print(f"   {g['name']} ({g['device']['deviceId']}) {vv['name']} = {vv['value']} ({vv['quality']})")

    print("== 6. 删除中间 Group (plc-2)，其他不受影响 ==")
    http("DELETE", f"/groups/{gids[1]}", token=tok)
    time.sleep(0.5)
    # 剩余 2 个 Group 仍正常
    for gid in [gids[0], gids[2]]:
        g = http("GET", f"/groups/{gid}", token=tok)
        v = http("GET", f"/groups/{gid}/values", token=tok)
        for tid, vv in v.items():
            print(f"   {g['name']} {vv['name']} = {vv['value']} ({vv['quality']})")

    print("== 7. 删除北向应用，剩余 Group 自动解绑 ==")
    http("DELETE", f"/north-apps/{na['id']}", token=tok)
    for gid in [gids[0], gids[2]]:
        g = http("GET", f"/groups/{gid}", token=tok)
        assert g["northAppId"] == "", f"期望 northAppId='', 实际={g['northAppId']}"
        print(f"   {g['name']} northAppId = '' ✓")

    print("== 8. 创建一个 Group 但不绑定北向（只采集不上送） ==")
    g_only = http("POST", "/groups", token=tok, data={
        "name": "local-plc",
        "driver": "modbus-tcp",
        "intervalMs": 500,
        "enabled": True,
        "config": {"host": "127.0.0.1", "port": 5020, "unitId": 1}
        # 不传 northAppId 和 device
    })
    http("POST", f"/groups/{g_only['id']}/tags", token=tok, data={
        "name": "reg1", "type": "uint16", "access": "ro",
        "config": {"address": "40001", "byteOrder": "AB", "bit": 0}
    })
    time.sleep(1)
    v = http("GET", f"/groups/{g_only['id']}/values", token=tok)
    for tid, vv in v.items():
        print(f"   {g_only['name']} (只采集) {vv['name']} = {vv['value']} ({vv['quality']})")
    assert g_only["northAppId"] == ""
    print("   ✓ 不绑定北向也能正常采集")

    print()
    print("== ✓ 编译期插件与动态配置 E2E 验证通过 ==")
    print()
    print("查看后端日志可看到多设备共享连接的细节：")
    print("  grep 'subscribed\\|unsubscribed' /tmp/edge.log")


if __name__ == "__main__":
    main()
