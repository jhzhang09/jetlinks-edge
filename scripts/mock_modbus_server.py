#!/usr/bin/env python3
"""极简 Modbus TCP 模拟器（仅用于本地开发测试）。

监听 127.0.0.1:5020，实现以下功能码：
  - FC03：读保持寄存器
  - FC04：读输入寄存器
  - FC06：写单寄存器

寄存器存储：200 个 uint16，初始预置 100/200/300 三个值。

使用方法：
    python3 scripts/mock_modbus_server.py [port]

默认端口 5020（JetLinks Edge 端配置时使用此端口）。
"""
import socket
import struct
import sys
import threading

HOST = "127.0.0.1"
PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 5020
SLAVE_ID = 1
HOLDING = [0] * 200


def handle(conn, addr):
    print(f"[mock-modbus] connected: {addr}")
    try:
        while True:
            hdr = b""
            while len(hdr) < 7:
                chunk = conn.recv(7 - len(hdr))
                if not chunk:
                    return
                hdr += chunk
            _, _, length, _ = struct.unpack(">HHHB", hdr[:7])
            body_len = length - 1
            body = b""
            while len(body) < body_len:
                chunk = conn.recv(body_len - len(body))
                if not chunk:
                    return
                body += chunk
            fc = body[0]
            if fc == 0x03 and len(body) >= 5:
                start, qty = struct.unpack(">HH", body[1:5])
                data = HOLDING[start:start + qty]
                payload = b"".join(struct.pack(">H", x) for x in data)
                resp = hdr[:4] + struct.pack(">H", 2 + len(payload) + 1) + bytes([SLAVE_ID, fc]) + bytes([len(payload)]) + payload
                conn.sendall(resp)
                print(f"  <- read holding start={start} qty={qty} -> {data}")
            elif fc == 0x04 and len(body) >= 5:
                start, qty = struct.unpack(">HH", body[1:5])
                data = HOLDING[start:start + qty]
                payload = b"".join(struct.pack(">H", x) for x in data)
                resp = hdr[:4] + struct.pack(">H", 2 + len(payload) + 1) + bytes([SLAVE_ID, fc]) + bytes([len(payload)]) + payload
                conn.sendall(resp)
                print(f"  <- read input start={start} qty={qty} -> {data}")
            elif fc == 0x06 and len(body) >= 5:
                addr, val = struct.unpack(">HH", body[1:5])
                HOLDING[addr] = val
                resp = hdr[:4] + struct.pack(">H", 6) + bytes([SLAVE_ID, fc]) + body[1:5]
                conn.sendall(resp)
                print(f"  <- write single addr={addr} val={val}")
            else:
                print(f"  <- unsupported fc={fc}")
                return
    except Exception as e:
        print(f"[mock-modbus] handler error: {e}")
    finally:
        conn.close()


def main():
    HOLDING[0] = 100
    HOLDING[1] = 200
    HOLDING[2] = 300
    s = socket.socket()
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind((HOST, PORT))
    s.listen(8)
    print(f"[mock-modbus] listening on {HOST}:{PORT} (HOLDING[0..2] = 100, 200, 300)")
    while True:
        conn, addr = s.accept()
        threading.Thread(target=handle, args=(conn, addr), daemon=True).start()


if __name__ == "__main__":
    main()
