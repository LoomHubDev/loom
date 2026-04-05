"""Loom SDK - Python client for the Loom Agent API."""

import base64
from typing import Any

import httpx


class LoomClient:
    """Client for the Loom Agent HTTP API."""

    def __init__(self, base_url: str = "http://localhost:7890", token: str | None = None):
        self.base_url = base_url.rstrip("/")
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = f"Bearer {token}"
        self._client = httpx.Client(base_url=self.base_url, headers=headers, timeout=30.0)

    def close(self):
        self._client.close()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    def checkpoint(self, title: str, summary: str = "", tags: dict[str, str] | None = None) -> dict[str, Any]:
        body: dict[str, Any] = {"title": title}
        if summary:
            body["summary"] = summary
        if tags:
            body["tags"] = tags
        resp = self._client.post("/api/v1/checkpoint", json=body)
        resp.raise_for_status()
        return resp.json()

    def rollback(self, checkpoint_id: str, entity_id: str | None = None) -> None:
        body: dict[str, Any] = {"checkpoint_id": checkpoint_id}
        if entity_id:
            body["scope"] = "entity"
            body["entity_id"] = entity_id
        else:
            body["scope"] = "full"
        resp = self._client.post("/api/v1/rollback", json=body)
        resp.raise_for_status()

    def diff(self, from_ref: str = "HEAD~1", to_ref: str = "HEAD", space: str = "", summary: bool = False) -> dict[str, Any]:
        params: dict[str, Any] = {"from": from_ref, "to": to_ref}
        if space:
            params["space"] = space
        if summary:
            params["summary"] = "true"
        resp = self._client.get("/api/v1/diff", params=params)
        resp.raise_for_status()
        return resp.json()

    def log(self, limit: int = 10) -> list[dict[str, Any]]:
        resp = self._client.get("/api/v1/log", params={"limit": limit})
        resp.raise_for_status()
        return resp.json()

    def status(self) -> dict[str, Any]:
        resp = self._client.get("/api/v1/status")
        resp.raise_for_status()
        return resp.json()

    def search(self, query: str, limit: int = 10) -> list[dict[str, Any]]:
        resp = self._client.get("/api/v1/search", params={"q": query, "limit": limit})
        resp.raise_for_status()
        return resp.json()

    def explain(self, from_ref: str = "HEAD~1", to_ref: str = "HEAD") -> dict[str, Any]:
        resp = self._client.post("/api/v1/explain", json={"from": from_ref, "to": to_ref})
        resp.raise_for_status()
        return resp.json()

    def record(self, space_id: str, path: str, content: bytes) -> None:
        encoded = base64.b64encode(content).decode("ascii")
        resp = self._client.post("/api/v1/record", json={"space_id": space_id, "path": path, "content": encoded})
        resp.raise_for_status()

    def tools(self) -> list[dict[str, Any]]:
        resp = self._client.get("/api/v1/tools")
        resp.raise_for_status()
        return resp.json()
