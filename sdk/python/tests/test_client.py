"""Tests for Loom SDK client."""

from unittest.mock import MagicMock

import pytest

from loom_sdk import LoomClient


def test_client_init():
    client = LoomClient("http://localhost:7890")
    assert client.base_url == "http://localhost:7890"
    client.close()


def test_client_init_strips_trailing_slash():
    client = LoomClient("http://localhost:7890/")
    assert client.base_url == "http://localhost:7890"
    client.close()


def test_client_context_manager():
    with LoomClient("http://localhost:7890") as client:
        assert client is not None
