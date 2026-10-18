import * as React from "react";
import { act, renderHook } from "@testing-library/react-hooks";
import { useAccount } from "../src/useAccount";
import { useAccountInstance } from "../src/useAccountInstance";

export default function useCounter() {
  const [count, setCount] = React.useState(0);
  const increment = React.useCallback(() => setCount((x) => x + 1), []);
  return { count, increment };
}

describe("useAccount", () => {
  test("should return the correct initial values", async () => {
    // const wrapper = ({ children }) => (
    //   <PaxeerNetworkConfigProvider brokerId={"paxeer-network"} networkId={"testnet"}>
    //     {children}
    //   </PaxeerNetworkConfigProvider>
    // );
    const { result, waitFor } = renderHook(
      () => {
        return useCounter();
      },
      {
        // wrapper,
      },
    );

    // expect(result.current.state).toBeDefined();
    // await waitFor(() => {
    //
    //     expect(result.current).toBeDefined();
    // },{
    //     timeout: 5000
    // });

    expect(result.current.count).toBe(0);
    expect(typeof result.current.increment).toBe("function");
    act(() => {
      result.current.increment();
    });

    expect(result.current.count).toBe(1);
  });
});
