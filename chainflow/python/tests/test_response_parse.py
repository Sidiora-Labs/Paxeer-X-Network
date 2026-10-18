from typing import Any, Dict, List

import httpx
import pytest

from chainflow_data_gateway import BaseModel, ChainflowDataGateway, AsyncChainflowDataGateway
from chainflow_data_gateway._constants import RAW_RESPONSE_HEADER

BASE_URL = "http://test.local"

PAYLOADS: Dict[str, Any] = {
    "/dict": {"a": 1, "b": {"c": [1, 2]}},
    "/list": [1, "two", {"three": 3}],
    "/empty-dict": {},
    "/empty-list": [],
    "/typed-dict": {"x": {"name": "foo"}, "y": {"name": "bar"}},
    "/typed-list": [{"name": "foo"}, {"name": "bar"}],
}

RAW_OPTIONS: Any = {"headers": {RAW_RESPONSE_HEADER: "true"}}


class Item(BaseModel):
    name: str


def _handler(request: httpx.Request) -> httpx.Response:
    return httpx.Response(200, json=PAYLOADS[request.url.path])


def _sync_client() -> ChainflowDataGateway:
    return ChainflowDataGateway(
        rest_api_key="test-key",
        base_url=BASE_URL,
        http_client=httpx.Client(transport=httpx.MockTransport(_handler)),
    )


def _async_client() -> AsyncChainflowDataGateway:
    return AsyncChainflowDataGateway(
        rest_api_key="test-key",
        base_url=BASE_URL,
        http_client=httpx.AsyncClient(transport=httpx.MockTransport(_handler)),
    )


UNTYPED_CASES = [
    ("/dict", dict),
    ("/list", list),
    ("/empty-dict", dict),
    ("/empty-list", list),
    ("/dict", Dict),
    ("/list", List),
]


@pytest.mark.parametrize("path,cast_to", UNTYPED_CASES)
def test_sync_raw_bare_container(path: str, cast_to: Any) -> None:
    client = _sync_client()
    response = client.get(path, cast_to=cast_to, options=RAW_OPTIONS)
    parsed = response.parse()
    assert parsed == PAYLOADS[path]
    assert type(parsed) is type(PAYLOADS[path])


@pytest.mark.parametrize("path,cast_to", UNTYPED_CASES)
@pytest.mark.asyncio
async def test_async_raw_bare_container(path: str, cast_to: Any) -> None:
    client = _async_client()
    response = await client.get(path, cast_to=cast_to, options=RAW_OPTIONS)
    parsed = await response.parse()
    assert parsed == PAYLOADS[path]
    assert type(parsed) is type(PAYLOADS[path])


@pytest.mark.parametrize("path,cast_to", UNTYPED_CASES)
def test_sync_raw_parse_to_bare_container(path: str, cast_to: Any) -> None:
    client = _sync_client()
    response = client.get(path, cast_to=object, options=RAW_OPTIONS)
    assert response.parse(to=cast_to) == PAYLOADS[path]


@pytest.mark.parametrize("path,cast_to", UNTYPED_CASES)
@pytest.mark.asyncio
async def test_async_raw_parse_to_bare_container(path: str, cast_to: Any) -> None:
    client = _async_client()
    response = await client.get(path, cast_to=object, options=RAW_OPTIONS)
    assert await response.parse(to=cast_to) == PAYLOADS[path]


def _assert_typed_dict(parsed: Any) -> None:
    assert isinstance(parsed, dict)
    assert set(parsed) == {"x", "y"}
    assert all(isinstance(v, Item) for v in parsed.values())
    assert parsed["x"].name == "foo"
    assert parsed["y"].name == "bar"


def _assert_typed_list(parsed: Any) -> None:
    assert isinstance(parsed, list)
    assert all(isinstance(v, Item) for v in parsed)
    assert [v.name for v in parsed] == ["foo", "bar"]


def test_sync_raw_typed_containers() -> None:
    client = _sync_client()
    _assert_typed_dict(client.get("/typed-dict", cast_to=Dict[str, Item], options=RAW_OPTIONS).parse())
    _assert_typed_list(client.get("/typed-list", cast_to=List[Item], options=RAW_OPTIONS).parse())
    assert client.get("/list", cast_to=List[Any], options=RAW_OPTIONS).parse() == PAYLOADS["/list"]
    assert client.get("/empty-list", cast_to=List[Item], options=RAW_OPTIONS).parse() == []
    assert client.get("/empty-dict", cast_to=Dict[str, Item], options=RAW_OPTIONS).parse() == {}


@pytest.mark.asyncio
async def test_async_raw_typed_containers() -> None:
    client = _async_client()
    _assert_typed_dict(await (await client.get("/typed-dict", cast_to=Dict[str, Item], options=RAW_OPTIONS)).parse())
    _assert_typed_list(await (await client.get("/typed-list", cast_to=List[Item], options=RAW_OPTIONS)).parse())
    assert await (await client.get("/list", cast_to=List[Any], options=RAW_OPTIONS)).parse() == PAYLOADS["/list"]
    assert await (await client.get("/empty-list", cast_to=List[Item], options=RAW_OPTIONS)).parse() == []
    assert await (await client.get("/empty-dict", cast_to=Dict[str, Item], options=RAW_OPTIONS)).parse() == {}
