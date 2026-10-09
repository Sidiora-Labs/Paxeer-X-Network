defmodule ConfigHelperTest do
  use ExUnit.Case

  setup do
    current_env_vars = System.get_env()
    clear_env_variables()

    on_exit(fn ->
      clear_env_variables()
      System.put_env(current_env_vars)
    end)
  end

  describe "parse_urls_list/3" do
    test "common case" do
      System.put_env("ETHEREUM_JSONRPC_HTTP_URLS", "test")
      assert ConfigHelper.parse_urls_list(:http) == ["test"]
    end

    test "using defined default" do
      System.put_env("ETHEREUM_JSONRPC_HTTP_URL", "test")
      refute System.get_env("ETHEREUM_JSONRPC_ETH_CALL_URLS")
      refute System.get_env("ETHEREUM_JSONRPC_ETH_CALL_URL")
      assert ConfigHelper.parse_urls_list(:eth_call) == ["test"]
    end

    test "using defined fallback default" do
      System.put_env("ETHEREUM_JSONRPC_FALLBACK_HTTP_URL", "test")
      refute System.get_env("ETHEREUM_JSONRPC_FALLBACK_ETH_CALL_URLS")
      refute System.get_env("ETHEREUM_JSONRPC_FALLBACK_ETH_CALL_URL")

      assert ConfigHelper.parse_urls_list(:fallback_eth_call) == ["test"]
    end

    test "base http urls are used if fallback is not provided" do
      System.put_env("ETHEREUM_JSONRPC_HTTP_URL", "test")
      refute System.get_env("ETHEREUM_JSONRPC_FALLBACK_TRACE_URLS")
      refute System.get_env("ETHEREUM_JSONRPC_FALLBACK_TRACE_URL")

      assert ConfigHelper.parse_urls_list(:fallback_trace) == ["test"]
    end

    test "accepts http and https node endpoints" do
      System.put_env("ETHEREUM_JSONRPC_HTTP_URLS", "https://node.example:8545,http://other.example:8545")

      assert ConfigHelper.parse_urls_list(:http) == ["https://node.example:8545", "http://other.example:8545"]
    end

    test "rejects a wss endpoint in ETHEREUM_JSONRPC_HTTP_URL without echoing the URL" do
      System.put_env("ETHEREUM_JSONRPC_HTTP_URL", "wss://node.example/secret-key")

      error = assert_raise ArgumentError, fn -> ConfigHelper.parse_urls_list(:http) end

      assert error.message =~ "ETHEREUM_JSONRPC_HTTP_URL holds a wss:// URL"
      assert error.message =~ "ETHEREUM_JSONRPC_WS_URL"
      refute error.message =~ "secret-key"
    end

    test "rejects a ws endpoint in a list" do
      System.put_env("ETHEREUM_JSONRPC_ETH_CALL_URLS", "https://node.example,ws://node.example:8546")

      assert_raise ArgumentError, ~r/ETHEREUM_JSONRPC_ETH_CALL_URLS holds a ws:\/\/ URL/, fn ->
        ConfigHelper.parse_urls_list(:eth_call)
      end
    end

    test "rejects a wss endpoint inherited by the eth_call and fallback lists" do
      System.put_env("ETHEREUM_JSONRPC_HTTP_URL", "WSS://node.example")

      assert_raise ArgumentError, fn -> ConfigHelper.parse_urls_list(:eth_call) end
      assert_raise ArgumentError, fn -> ConfigHelper.parse_urls_list(:fallback_trace) end
    end
  end

  describe "parse_json_rpc_transport/0" do
    test "defaults to http" do
      assert ConfigHelper.parse_json_rpc_transport() == :http
    end

    test "accepts http and ipc" do
      System.put_env("ETHEREUM_JSONRPC_TRANSPORT", "http")
      assert ConfigHelper.parse_json_rpc_transport() == :http

      System.put_env("ETHEREUM_JSONRPC_TRANSPORT", "ipc")
      assert ConfigHelper.parse_json_rpc_transport() == :ipc
    end

    test "rejects a websocket scheme instead of selecting ipc" do
      for value <- ["wss", "ws", ""] do
        System.put_env("ETHEREUM_JSONRPC_TRANSPORT", value)

        assert_raise ArgumentError, ~r/Invalid value "#{value}" of ETHEREUM_JSONRPC_TRANSPORT/, fn ->
          ConfigHelper.parse_json_rpc_transport()
        end
      end
    end
  end

  describe "parse_microservice_url/2" do
    test "returns nil when the service is disabled and no URL is set" do
      assert ConfigHelper.parse_microservice_url("MICROSERVICE_METADATA_URL", "MICROSERVICE_METADATA_ENABLED") == nil
    end

    test "returns the URL of an enabled service" do
      System.put_env("MICROSERVICE_METADATA_ENABLED", "true")
      System.put_env("MICROSERVICE_METADATA_URL", "https://metadata.example/")

      assert ConfigHelper.parse_microservice_url("MICROSERVICE_METADATA_URL", "MICROSERVICE_METADATA_ENABLED") ==
               "https://metadata.example"
    end

    test "raises when an enabled service has no usable URL" do
      System.put_env("MICROSERVICE_METADATA_ENABLED", "true")

      for url <- [nil, "", "metadata.example", "wss://metadata.example"] do
        if url,
          do: System.put_env("MICROSERVICE_METADATA_URL", url),
          else: System.delete_env("MICROSERVICE_METADATA_URL")

        assert_raise ArgumentError,
                     "MICROSERVICE_METADATA_ENABLED=true requires MICROSERVICE_METADATA_URL to be the service's http:// or https:// base URL",
                     fn ->
                       ConfigHelper.parse_microservice_url("MICROSERVICE_METADATA_URL", "MICROSERVICE_METADATA_ENABLED")
                     end
      end
    end
  end

  describe "parse_path_env_var/2" do
    test "common case" do
      System.put_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "test")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER") == "/test"
    end

    test "don't use defined default" do
      System.put_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "test")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "default") == "/test"
    end

    test "don't prepend / if path already has it" do
      System.put_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "/test")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "default") == "/test"
    end

    test "using defined fallback default" do
      System.delete_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "default") == "/default"
    end

    test "invalid path" do
      System.put_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "//test")

      assert_raise RuntimeError, "Invalid path in environment variable NFT_MEDIA_HANDLER_BUCKET_FOLDER: //test", fn ->
        ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER")
      end
    end

    test "invalid path with default" do
      System.put_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "//test")

      assert_raise RuntimeError, "Invalid path in environment variable NFT_MEDIA_HANDLER_BUCKET_FOLDER: //test", fn ->
        ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "default")
      end
    end

    test "empty path with default" do
      System.delete_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "default") == "/default"
    end

    test "nil path" do
      System.delete_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER") == nil
    end

    test "nil path with default" do
      System.delete_env("NFT_MEDIA_HANDLER_BUCKET_FOLDER")
      assert ConfigHelper.parse_path_env_var("NFT_MEDIA_HANDLER_BUCKET_FOLDER", "default") == "/default"
    end
  end

  defp clear_env_variables do
    System.delete_env("ETHEREUM_JSONRPC_HTTP_URLS")
    System.delete_env("ETHEREUM_JSONRPC_HTTP_URL")
    System.delete_env("ETHEREUM_JSONRPC_TRACE_URLS")
    System.delete_env("ETHEREUM_JSONRPC_TRACE_URL")
    System.delete_env("ETHEREUM_JSONRPC_ETH_CALL_URLS")
    System.delete_env("ETHEREUM_JSONRPC_ETH_CALL_URL")
    System.delete_env("ETHEREUM_JSONRPC_FALLBACK_HTTP_URLS")
    System.delete_env("ETHEREUM_JSONRPC_FALLBACK_HTTP_URL")
    System.delete_env("ETHEREUM_JSONRPC_FALLBACK_TRACE_URLS")
    System.delete_env("ETHEREUM_JSONRPC_FALLBACK_TRACE_URL")
    System.delete_env("ETHEREUM_JSONRPC_FALLBACK_ETH_CALL_URLS")
    System.delete_env("ETHEREUM_JSONRPC_FALLBACK_ETH_CALL_URL")
    System.delete_env("ETHEREUM_JSONRPC_TRANSPORT")
    System.delete_env("MICROSERVICE_METADATA_URL")
    System.delete_env("MICROSERVICE_METADATA_ENABLED")
  end
end
