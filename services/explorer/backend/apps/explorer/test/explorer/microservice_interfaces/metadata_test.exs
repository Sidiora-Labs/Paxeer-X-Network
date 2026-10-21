defmodule Explorer.MicroserviceInterfaces.MetadataTest do
  use ExUnit.Case

  alias Explorer.MicroserviceInterfaces.Metadata
  alias Plug.Conn

  @address "0x0000000000000000000000000000000000000001"

  setup do
    bypass = Bypass.open()
    old_env = Application.get_env(:explorer, Metadata)

    Application.put_env(:tesla, :adapter, Tesla.Adapter.Mint)
    Application.put_env(:explorer, Metadata, service_url: "http://localhost:#{bypass.port}", enabled: true)

    on_exit(fn ->
      Application.put_env(:explorer, Metadata, old_env)
      Application.put_env(:tesla, :adapter, Explorer.Mock.TeslaAdapter)
    end)

    {:ok, bypass: bypass}
  end

  describe "get_addresses/1" do
    test "returns the service status and an encodable error when the error body is not JSON", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/api/v1/addresses", fn conn ->
        Conn.resp(conn, 502, "<html><body>Bad Gateway</body></html>")
      end)

      assert {502, %{error: "Error while sending request to Metadata microservice"} = body} =
               Metadata.get_addresses(%{})

      assert {:ok, _} = Jason.encode(body)
    end

    test "passes a JSON error body through with its status", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/api/v1/addresses", fn conn ->
        Conn.resp(conn, 404, Jason.encode!(%{"message" => "chain not found"}))
      end)

      assert Metadata.get_addresses(%{}) == {404, %{"message" => "chain not found"}}
    end

    test "returns 500 with an encodable error when the service is unreachable", %{bypass: bypass} do
      Bypass.down(bypass)

      assert {500, %{error: "Error while sending request to Metadata microservice"} = body} =
               Metadata.get_addresses(%{})

      assert {:ok, _} = Jason.encode(body)
    end

    test "returns 501 when the service is disabled" do
      Application.put_env(:explorer, Metadata, service_url: nil, enabled: true)

      assert Metadata.get_addresses(%{}) == {501, %{error: "Service is disabled"}}
    end
  end

  describe "get_addresses_tags/1" do
    test "decodes the tags of the listed addresses", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/api/v1/metadata", fn conn ->
        Conn.resp(
          conn,
          200,
          Jason.encode!(%{
            "addresses" => %{@address => %{"tags" => [%{"slug" => "tag", "meta" => "{\"styles\":\"danger_high\"}"}]}}
          })
        )
      end)

      assert {:ok, %{"addresses" => %{@address => %{"tags" => [%{"meta" => %{"styles" => "danger_high"}}]}}}} =
               Metadata.get_addresses_tags([@address])
    end

    test "gives up after the configured requests timeout instead of holding the list request", %{bypass: bypass} do
      Application.put_env(:explorer, Metadata,
        service_url: "http://localhost:#{bypass.port}",
        enabled: true,
        requests_timeout: 100
      )

      Bypass.stub(bypass, "GET", "/api/v1/metadata", fn conn ->
        Process.sleep(1_000)
        Conn.resp(conn, 200, Jason.encode!(%{"addresses" => %{}}))
      end)

      {elapsed, result} = :timer.tc(fn -> Metadata.get_addresses_tags([@address]) end, :millisecond)

      assert result == {:error, "Error while sending request to Metadata microservice"}
      assert elapsed < 1_000
    end
  end
end
