#!/usr/bin/env python3
"""设置页 / 管理员接口测试（对 8091 测试实例）。用法：tests/admintest.py <设置码>"""
import json, sys, time, urllib.request, urllib.error, http.cookiejar, struct, zlib

B = "http://127.0.0.1:8091"
CODE = sys.argv[1] if len(sys.argv) > 1 else ""
CFG = "/tmp/hl-test/data/config.json"
results = []

def check(name, cond, detail=""):
    results.append((name, bool(cond), detail))

def mk():
    jar = http.cookiejar.CookieJar()
    return jar, urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

def call(op, path, body=None, method=None, headers=None, raw=None):
    h = {"X-HL-Admin": "1"} if (body is not None or raw is not None or method not in (None, "GET")) else {}
    h.update(headers or {})
    data = None
    if raw is not None:
        data = raw
    elif body is not None:
        data = json.dumps(body).encode(); h["Content-Type"] = "application/json"
    req = urllib.request.Request(B + path, data=data, headers=h, method=method or ("POST" if data is not None else "GET"))
    try:
        r = op.open(req, timeout=30); return r.status, dict(r.headers), r.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()

def js(b):
    try: return json.loads(b)
    except Exception: return {}

def png(w=2, h=2):
    raw = b"".join(b"\x00" + b"\xff\x00\x00\xff" * w for _ in range(h))
    def chunk(t, d): return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")

jar, op = mk()
st, _, b = call(op, "/api/admin/state"); d = js(b)
check("首次：需要设置", st == 200 and d.get("setup") is True and d.get("logged") is False, d)
st, h, b = call(op, "/admin"); check("设置页能打开且带 CSP", st == 200 and "script-src 'self'" in h.get("Content-Security-Policy", ""), h.get("Content-Security-Policy"))
st, _, b = call(op, "/api/admin/login", {"password": "x"}); check("没设置前不能登录", st == 409, st)
st, _, b = call(op, "/api/admin/setup", {"code": "AAAA-AAAA", "password": "pw-test-123"}); check("设置码错 → 403", st == 403, js(b))
st, _, b = call(op, "/api/admin/setup", {"code": CODE, "password": "short"}); check("密码太短 → 400", st == 400, js(b))
st, _, b = call(op, "/api/admin/setup", {"code": CODE.lower(), "password": "pw-test-123", "title": "", "name": ""})
check("设置码对（小写也行）→ 设置成功", st == 200, js(b))
st, _, b = call(op, "/api/admin/setup", {"code": CODE, "password": "pw-test-456"}); check("设置过以后不能再设置", st == 409, st)
saved = json.load(open(CFG))
check("配置文件里存的是 pbkdf2 哈希", saved["admin"]["password_hash"].startswith("pbkdf2_sha256$200000$") and "pw-test-123" not in open(CFG).read())

st, _, b = call(op, "/api/admin/config"); d = js(b)
check("登录后能读配置", st == 200 and d.get("ok"), st)
txt = b.decode()
leak = [k for k in (saved["bangumi"]["token"], saved["jellyfin"]["key"], saved["netease"]["bind_key"], saved["pc_key"], saved["admin"]["secret"], saved["admin"]["password_hash"]) if k and k in txt]
check("读配置不会带出密钥", not leak, f"{len(leak)} 项泄露")
check("密钥只显示有没有", d.get("config", {}).get("secrets_set", {}).get("bangumi.token") is True)

_, anon = mk()
st, _, _ = call(anon, "/api/admin/config"); check("没登录读配置 → 401", st == 401, st)
st, _, _ = call(op, "/api/admin/config", {"config": {}}, headers={"X-HL-Admin": ""}); check("不带 X-HL-Admin → 403", st == 403, st)
st, _, _ = call(op, "/api/admin/config", {"config": {}}, headers={"Origin": "https://evil.example"}); check("跨站 → 403", st == 403, st)

profile = {"config": {
    "site": {"title": "TestSite", "domain": "example.com", "description": "TestSite personal page."},
    "profile": {"name": "TestSite", "handle": "@testuser", "email": "user@example.com",
                "links": [{"title": "TestSite", "sub": "demo", "url": "https://example.com/user", "avatar": True}]}},
    "secrets": {}, "clear": []}
st, _, b = call(op, "/api/admin/config", profile); d = js(b)
check("保存资料", st == 200 and d.get("ok"), js(b))
saved2 = json.load(open(CFG))
check("ID 去掉了 @", saved2["profile"]["handle"] == "testuser", saved2["profile"]["handle"])
check("没填的密钥保持不变", saved2["bangumi"]["token"] == saved["bangumi"]["token"] and saved2["jellyfin"]["key"] == saved["jellyfin"]["key"])

st, _, b = call(op, "/api/admin/config", {"config": {"netease": {"api": "ftp://x", "refresh_hours": 12}}}); check("非法地址 → 400", st == 400, js(b).get("error"))
st, _, b = call(op, "/api/admin/config", {"config": {"timezone": "Mars/Base"}}); check("非法时区 → 400", st == 400, js(b).get("error"))
st, _, b = call(op, "/api/admin/config", {"config": {"profile": {"name": "x", "links": [{"title": "坏", "url": "javascript:alert(1)"}]}}})
check("javascript: 链接 → 400", st == 400, js(b).get("error"))
check("校验失败时配置不变", json.load(open(CFG))["profile"]["name"] == "TestSite")

st, _, b = call(anon, "/"); html = b.decode()
check("首页标题来自配置", "<title>TestSite | example.com</title>" in html, html[html.find("<title>"):html.find("</title>") + 8])
check("首页注入了站点信息", "window.HL_SITE=" in html and "{{HL_" not in html and "<!--HL_SITE-->" not in html)
st, _, b = call(anon, "/api/site"); d = js(b).get("site", {})
check("/api/site", d.get("name") == "TestSite" and d.get("links"), d)

xss = dict(profile); xss = json.loads(json.dumps(profile)); xss["config"]["profile"]["name"] = '</script><script>alert(1)</script>'
call(op, "/api/admin/config", xss)
st, _, b = call(anon, "/"); html = b.decode()
check("名字里的 </script> 被转义", "<script>alert(1)" not in html, "")
call(op, "/api/admin/config", profile)

img = png()
st, _, b = call(op, "/api/admin/avatar", raw=img, headers={"Content-Type": "image/png"}); check("上传头像", st == 200, js(b))
st, _, b = call(anon, "/img/avatar.png"); check("头像用上传的那张", b == img, len(b))
st, _, b = call(op, "/api/admin/avatar", raw=b"not an image", headers={"Content-Type": "image/png"}); check("不是图片 → 400", st == 400, st)
st, _, b = call(op, "/api/admin/avatar", method="DELETE"); st2, _, b2 = call(anon, "/img/avatar.png")
check("恢复默认头像", st == 200 and b2 != img and b2[:8] == b"\x89PNG\r\n\x1a\n", len(b2))

st, _, b = call(op, "/api/admin/logout", {}); st2, _, _ = call(op, "/api/admin/config")
check("退出登录", st == 200 and st2 == 401, (st, st2))
st, _, b = call(op, "/api/admin/login", {"password": "wrong-pass"}); check("密码错 → 401", st == 401, st)
st, _, b = call(op, "/api/admin/login", {"password": "pw-test-123"}); check("密码对 → 登录", st == 200, js(b))

jar2, op2 = mk()
call(op2, "/api/admin/login", {"password": "pw-test-123"})
st, _, b = call(op, "/api/admin/password", {"old": "pw-test-123", "new": "pw-test-789"}); check("改密码", st == 200, js(b))
st, _, _ = call(op, "/api/admin/config"); check("改密码的这台还在登录", st == 200, st)
st, _, _ = call(op2, "/api/admin/config"); check("其他设备被踢下线", st == 401, st)

forged = http.cookiejar.Cookie(0, "hl_admin", "9999999999.0.deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", None, False, "127.0.0.1", False, False, "/", True, False, None, False, None, None, {})
jar3, op3 = mk(); jar3.set_cookie(forged)
st, _, _ = call(op3, "/api/admin/config"); check("伪造的 cookie 无效", st == 401, st)

codes = [call(anon, "/api/admin/login", {"password": f"bad{i}"})[0] for i in range(11)]
check("连续输错 10 次后限流 → 429", codes[-1] == 429 and codes[0] == 401, codes)
st, _, _ = call(anon, "/api/admin/login", {"password": "pw-test-789"}, headers={"X-Real-IP": "10.9.8.7"})
check("别的 IP 不受影响", st == 200, st)

ok = sum(1 for _, c, _ in results if c)
print(f"通过 {ok} / {len(results)}")
for name, c, detail in results:
    if not c:
        print(f"✗ {name}  {str(detail)[:200]}")
