import * as React from "react";
import { renderHook } from "@testing-library/react-hooks";
import { PaxeerNetworkConfigProvider } from "../src/configProvider";
import { useAccount } from "../src/useAccount";

describe("useAccount", () => {
  test("should return the correct initial values", async () => {
    const wrapper = ({ children }) => (
      <PaxeerNetworkConfigProvider brokerId={"paxeer-network"} networkId={"testnet"}>
        {children}
      </PaxeerNetworkConfigProvider>
    );
    const { result, waitFor } = renderHook(
      () => {
        return useAccount();
      },
      {
        wrapper,
      },
    );

    // expect(result.current.state).toBeDefined();
    await waitFor(
      () => {
        console.log("****", result.current.state);
        expect(result.current.state).toEqual([{ title: "Star Wars" }]);
      },
      {
        timeout: 5000,
      },
    );
  });
});
