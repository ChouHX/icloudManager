#!/usr/bin/env python3
"""iCloud HME 取件客户端示例（只用标准库，可直接复制进你的程序）。

覆盖外部程序接入最常用的两个动作：
  1. 获取别名邮箱列表  GET /api/aliases
  2. 取件              GET /api/inbox 与 GET /api/inbox/:id

用法:
    python3 icloud_hme_client.py --base http://127.0.0.1:8081 --password '你的管理员密码'
    python3 icloud_hme_client.py --base http://127.0.0.1:8081 --password '...' --alias xyz@icloud.com --watch

说明:
  - 只用标准库（urllib + http.cookiejar），无需 pip 安装任何依赖
  - 会话 Cookie 由 CookieJar 自动维护；纯读接口不需要 CSRF，写操作才需要
  - 会话过期（401）会自动重新登录一次
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


class HmeError(RuntimeError):
    """服务端返回的业务错误，带稳定错误码。"""

    def __init__(self, status: int, code: str, message: str) -> None:
        super().__init__(f"HTTP {status} {code}: {message}")
        self.status = status
        self.code = code
        self.message = message


class HmeClient:
    def __init__(self, base_url: str, password: str, timeout: float = 30.0) -> None:
        self.base = base_url.rstrip("/")
        self.password = password
        self.timeout = timeout
        self.csrf = ""
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    # ---- 底层请求 ----

    def _request(self, method: str, path: str, payload: dict | None = None, csrf: bool = False) -> dict:
        data = json.dumps(payload).encode("utf-8") if payload is not None else None
        request = urllib.request.Request(self.base + path, data=data, method=method)
        request.add_header("Accept", "application/json")
        if data is not None:
            request.add_header("Content-Type", "application/json")
        if csrf and self.csrf:
            request.add_header("X-CSRF-Token", self.csrf)

        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                body = json.loads(response.read() or b"{}")
        except urllib.error.HTTPError as err:
            raw = err.read()
            try:
                body = json.loads(raw or b"{}")
            except json.JSONDecodeError:
                body = {"code": f"HTTP_{err.code}", "message": raw[:200].decode("utf-8", "replace")}
            raise HmeError(err.code, body.get("code", ""), body.get("message", "")) from None
        except urllib.error.URLError as err:
            raise HmeError(0, "NETWORK_ERROR", f"无法连接 {self.base}: {err.reason}") from None

        if not body.get("success", False):
            raise HmeError(200, body.get("code", ""), body.get("message", ""))
        return body.get("data") or {}

    def _request_with_relogin(self, method: str, path: str, payload: dict | None = None, csrf: bool = False) -> dict:
        """会话过期时自动重新登录一次（401 AUTH_REQUIRED）。"""
        try:
            return self._request(method, path, payload, csrf)
        except HmeError as err:
            if err.status != 401 or path.endswith("/api/auth/login"):
                raise
            self.login()
            return self._request(method, path, payload, csrf)

    # ---- 认证 ----

    def login(self) -> dict:
        """POST /api/auth/login → 设置 hme_session Cookie，并返回 csrf_token。"""
        data = self._request("POST", "/api/auth/login", {"password": self.password})
        self.csrf = data.get("csrf_token", "")
        return data

    # ---- 账号与别名 ----

    def accounts(self) -> list[dict]:
        """GET /api/accounts → 账号列表，account_id 从这里取。"""
        return self._request_with_relogin("GET", "/api/accounts")

    def aliases(self, account_id: str) -> list[dict]:
        """GET /api/aliases → 别名邮箱列表。"""
        query = urllib.parse.urlencode({"account_id": account_id})
        data = self._request_with_relogin("GET", f"/api/aliases?{query}")
        return data.get("aliases") or []

    # ---- 取件 ----

    def inbox(self, account_id: str, alias: str | None = None, limit: int = 20, days: int = 7) -> dict:
        """GET /api/inbox → 邮件摘要列表（method 表示走 IMAP 还是 Web API）。"""
        params = {"account_id": account_id, "limit": limit, "days": days}
        if alias:
            params["alias"] = alias
        data = self._request_with_relogin("GET", "/api/inbox?" + urllib.parse.urlencode(params))
        return data

    def message(self, account_id: str, message_id: str) -> dict:
        """GET /api/inbox/:id → 单封邮件正文（body 纯文本，body_html 可渲染的 HTML）。"""
        query = urllib.parse.urlencode({"account_id": account_id})
        return self._request_with_relogin("GET", f"/api/inbox/{urllib.parse.quote(str(message_id))}?{query}")

    def delete_message(self, account_id: str, message_id: str) -> dict:
        """DELETE /api/inbox/:id → 删除邮件（写操作，需 CSRF）。"""
        query = urllib.parse.urlencode({"account_id": account_id})
        return self._request_with_relogin(
            "DELETE", f"/api/inbox/{urllib.parse.quote(str(message_id))}?{query}", csrf=True
        )


def pick_account(client: HmeClient, wanted: str | None) -> str:
    accounts = client.accounts()
    if not accounts:
        raise SystemExit("账号列表为空：请先在管理界面添加 iCloud 账号并配置凭据")
    if wanted:
        for account in accounts:
            if account.get("id") == wanted or account.get("name") == wanted:
                return account["id"]
        raise SystemExit(f"找不到账号: {wanted}")
    return accounts[0]["id"]


def print_messages(result: dict, limit: int = 10) -> None:
    messages = result.get("messages") or []
    print(f"共 {result.get('count', len(messages))} 封，读取方式: {result.get('method')}")
    for message in messages[:limit]:
        print(f"  [{message.get('id')}] {message.get('date')} | {message.get('from')}")
        print(f"      {message.get('subject')}")


def main() -> int:
    parser = argparse.ArgumentParser(description="iCloud HME 取件示例客户端")
    parser.add_argument("--base", default="http://127.0.0.1:8081", help="服务地址")
    parser.add_argument("--password", required=True, help="管理员密码（ICLOUD_HME_ADMIN_PASSWORD）")
    parser.add_argument("--account", help="账号 ID 或名称，缺省用第一个")
    parser.add_argument("--alias", help="只看发到该别名的邮件")
    parser.add_argument("--limit", type=int, default=20, help="返回上限 1-100")
    parser.add_argument("--days", type=int, default=7, help="只看近 N 天 1-90（仅 IMAP 路径生效）")
    parser.add_argument("--message-id", help="读取指定邮件的正文")
    parser.add_argument("--watch", action="store_true", help="轮询新邮件（每 15 秒）")
    args = parser.parse_args()

    client = HmeClient(args.base, args.password)
    client.login()
    print(f"登录成功: {args.base}")

    account_id = pick_account(client, args.account)
    print(f"使用账号: {account_id}")

    aliases = client.aliases(account_id)
    print(f"别名邮箱 {len(aliases)} 个:")
    for alias in aliases[:10]:
        state = "启用" if alias.get("active") else "停用"
        print(f"  {alias.get('email')}  [{state}]  {alias.get('label') or '-'}")
    if not aliases:
        print("  (还没有别名,可先调用 POST /api/create 创建)")

    if args.message_id:
        detail = client.message(account_id, args.message_id)
        print(f"\n--- {detail.get('subject')} ---")
        print(f"发件人: {detail.get('from')}")
        print(f"内容类型: {detail.get('content_type')}")
        print(detail.get("body") or "(无正文)")
        return 0

    if args.watch:
        print("\n开始轮询新邮件（Ctrl-C 退出）…")
        seen: set[str] = set()
        while True:
            result = client.inbox(account_id, args.alias, args.limit, args.days)
            for message in result.get("messages") or []:
                if message.get("id") in seen:
                    continue
                seen.add(message.get("id"))
                print(f"新邮件 [{message.get('id')}] {message.get('subject')} ← {message.get('from')}")
            time.sleep(15)

    print_messages(client.inbox(account_id, args.alias, args.limit, args.days))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except HmeError as error:
        print(f"调用失败: {error}", file=sys.stderr)
        if error.code in ("UPSTREAM_UNAUTHORIZED", "VALIDATION_ERROR"):
            print("提示: Cookie 失效或账号未配置凭据时，请先在管理界面更新凭据", file=sys.stderr)
        sys.exit(1)
